/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	// SchedulerEndpointEnvName holds the HAMi scheduler base URL used for
	// the NUMA refit, for example https://hami-scheduler.kube-system.svc:443.
	// Empty disables the refit: mismatches are then only logged, exactly as
	// before the refit existed.
	SchedulerEndpointEnvName = "HAMI_SCHEDULER_ENDPOINT"
	// SchedulerCAFileEnvName optionally points at a PEM bundle used to
	// verify the scheduler endpoint's TLS certificate.
	SchedulerCAFileEnvName = "HAMI_SCHEDULER_CA_FILE"
	// SchedulerTLSInsecureEnvName set to true skips TLS verification of the
	// scheduler endpoint. The scheduler serves the admission webhook's
	// self-signed certificate, so the chart enables this by default with the
	// same posture as the extender configmap (tlsConfig.insecure: true).
	SchedulerTLSInsecureEnvName = "HAMI_SCHEDULER_TLS_INSECURE"

	numaRefitPath = "/refit"

	// numaRefitTimeout bounds one refit round trip. Kubelet applies no
	// deadline of its own to GetPreferredAllocation and admits pods on a
	// single serialized loop, so this client timeout is the node's only
	// protection against a slow or unreachable scheduler.
	numaRefitTimeout = 2 * time.Second
)

var (
	// serviceAccountTokenFile is the default projected/bound ServiceAccount
	// token every pod gets mounted, used to authenticate the refit call
	// against the scheduler's TokenReview check. See issue #2878.
	serviceAccountTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// numaRefitTLSConfig returns a tls.Config for reaching the scheduler. When
// authenticated is true (a bearer token is being transmitted), TLS certificate
// verification is strictly enforced and HAMI_SCHEDULER_TLS_INSECURE cannot
// disable it. When unauthenticated, HAMI_SCHEDULER_TLS_INSECURE may skip
// verification for clusters using the self-signed webhook certificate.
func numaRefitTLSConfig(authenticated bool) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile := os.Getenv(SchedulerCAFileEnvName); caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read scheduler CA bundle %q: %w", caFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("scheduler CA bundle %q contains no usable certificates", caFile)
		}
		config.RootCAs = pool
	}
	if insecure, err := strconv.ParseBool(os.Getenv(SchedulerTLSInsecureEnvName)); err == nil && insecure {
		if authenticated {
			return nil, errors.New("insecure TLS verification is not permitted for authenticated refit requests")
		}
		config.InsecureSkipVerify = true
	}
	return config, nil
}

// numaRefitHTTPClient reaches the scheduler service. It is built per refit so
// a rotated CA bundle is picked up without restarting the device plugin; the
// refit is a rare path, taken only when an allocation mismatches. It rejects
// redirects to prevent token leakage across endpoints.
func numaRefitHTTPClient(authenticated bool) (*http.Client, error) {
	tlsConfig, err := numaRefitTLSConfig(authenticated)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: numaRefitTimeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		// Never follow redirects: the bearer token must not be forwarded to
		// an unvalidated redirect destination. Returning a non-nil error here
		// is sufficient; no header manipulation is needed because the redirect
		// request is never sent.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirects are not permitted for refit requests")
		},
	}, nil
}

// tryNumaRefit asks the scheduler to move this container's pending
// allocation onto kubelet's allowed device set. It returns the preferred
// replica IDs on success. A nil slice with a nil error means the refit did
// not apply (disabled, pod not opted in, or best-effort failure); a non-nil
// error means strict mode failed and the allocation must fail.
func (plugin *NvidiaDevicePlugin) tryNumaRefit(ctx context.Context, pod *corev1.Pod, containerIndex int, req *kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest, cause error) ([]string, error) {
	if pod == nil || plugin.operatingMode == nvidia.MigMode || !errors.Is(cause, errAnnotatedDeviceUnavailable) {
		return nil, nil
	}
	mode, parseErr := util.GetNumaAlignmentModeByPod(pod)
	if parseErr != nil || mode == util.NumaAlignmentNone {
		return nil, nil
	}
	if os.Getenv(SchedulerEndpointEnvName) == "" {
		return nil, nil
	}

	// Kubelet builds AvailableDeviceIDs as a superset of MustIncludeDeviceIDs
	// (available.Union(allocated) with mustInclude = allocated), so the
	// available set is the candidate pool. Restricting it to MustInclude
	// would shrink the pool below the requested device count; the pinned
	// replicas are honored by selectPreferredDeviceIDsFromAnnotatedDevices,
	// which seeds them first.
	allowedUUIDs := allowedPhysicalDeviceIDs(req.AvailableDeviceIDs)
	newDevices, err := plugin.requestNumaRefit(ctx, pod, containerIndex, allowedUUIDs)
	if err == nil {
		replicas, selectErr := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(req.AvailableDeviceIDs, req.MustIncludeDeviceIDs, newDevices, int(req.AllocationSize))
		if selectErr == nil {
			klog.InfoS("NUMA refit succeeded", "pod", klog.KObj(pod), "container", containerIndex, "devices", replicas)
			return replicas, nil
		}
		// The scheduler has already committed the move at this point.
		// Falling back to kubelet's own selection would leave runtime and
		// accounting divergent, so fail the allocation in both modes.
		return nil, fmt.Errorf("numa refit committed but kubelet cannot honor the selection: %w", selectErr)
	}

	if mode == util.NumaAlignmentStrict {
		return nil, fmt.Errorf("numa-alignment strict: %w", err)
	}
	klog.InfoS("NUMA refit failed; best-effort keeps kubelet's own selection", "pod", klog.KObj(pod), "container", containerIndex, "err", err)
	return nil, nil
}

// requestNumaRefit performs one refit round trip against the scheduler.
func (plugin *NvidiaDevicePlugin) requestNumaRefit(ctx context.Context, pod *corev1.Pod, containerIndex int, allowedUUIDs []string) (device.ContainerDevices, error) {
	rawEndpoint := os.Getenv(SchedulerEndpointEnvName)
	if rawEndpoint == "" {
		return nil, errors.New("scheduler endpoint is not configured")
	}
	rawURL := strings.TrimSuffix(rawEndpoint, "/") + numaRefitPath
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid scheduler endpoint URL: %w", err)
	}

	// Always authenticate: the scheduler requires a valid device-plugin token
	// for /refit (see issue #2878). Fail immediately if the token is missing so
	// the caller gets a clear error rather than a server-side authentication
	// rejection after a full round trip.
	tokenBytes, tokenErr := os.ReadFile(serviceAccountTokenFile)
	if tokenErr != nil {
		return nil, fmt.Errorf("cannot read service account token for refit authentication: %w", tokenErr)
	}
	token := strings.TrimSpace(string(tokenBytes))

	if parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("authenticated refit requires HTTPS, endpoint scheme is %q", parsedURL.Scheme)
	}

	payload, err := json.Marshal(device.NumaRefitRequest{
		PodUID:             string(pod.UID),
		PodNamespace:       pod.Namespace,
		PodName:            pod.Name,
		NodeName:           os.Getenv(util.NodeNameEnvName),
		ContainerIndex:     containerIndex,
		ContainerName:      podContainerNameAt(pod, containerIndex),
		DeviceType:         nvidia.NvidiaGPUDevice,
		AllowedDeviceUUIDs: allowedUUIDs,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, numaRefitTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	httpClient, err := numaRefitHTTPClient(true)
	if err != nil {
		return nil, err
	}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scheduler refit returned status %d", httpResp.StatusCode)
	}

	var response device.NumaRefitResponse
	if err := json.NewDecoder(io.LimitReader(httpResp.Body, 1<<20)).Decode(&response); err != nil {
		return nil, err
	}
	if !response.Succeeded {
		return nil, fmt.Errorf("scheduler refused refit: %s", response.FailureReason)
	}
	devices, err := device.DecodeContainerDevices(response.ContainerDevices)
	if err != nil {
		return nil, fmt.Errorf("cannot decode refit devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, errors.New("scheduler refit returned no devices")
	}
	return devices, nil
}

// podContainerNameAt returns the pod's container name at the PodDevices
// position, counting init containers first, for the scheduler's cross-check.
func podContainerNameAt(pod *corev1.Pod, index int) string {
	if index < 0 {
		return ""
	}
	if index < len(pod.Spec.InitContainers) {
		return pod.Spec.InitContainers[index].Name
	}
	index -= len(pod.Spec.InitContainers)
	if index >= 0 && index < len(pod.Spec.Containers) {
		return pod.Spec.Containers[index].Name
	}
	return ""
}

// allowedPhysicalDeviceIDs maps kubelet's replica IDs to their unique
// physical device UUIDs, preserving first-seen order.
func allowedPhysicalDeviceIDs(available []string) []string {
	seen := make(map[string]struct{}, len(available))
	physical := make([]string, 0, len(available))
	for _, id := range available {
		p := physicalDeviceID(id)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		physical = append(physical, p)
	}
	return physical
}
