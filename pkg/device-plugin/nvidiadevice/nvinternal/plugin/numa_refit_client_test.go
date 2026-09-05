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
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func numaRefitTestPod(mode string) *corev1.Pod {
	annotations := map[string]string{
		"hami.io/vgpu-devices-to-allocate": device.EncodePodSingleDevice(device.PodSingleDevice{
			device.ContainerDevices{{UUID: numaTestGPUA, Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}},
		}),
	}
	if mode != "" {
		annotations[util.NumaAlignmentAnnotationKey] = mode
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: "refit-pod-uid", Name: "numa-pod", Namespace: "default",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
}

// numaRefitTestServer serves /refit over HTTP with the given response and
// captures the last request. NOTE: after the fail-closed token change, plain
// HTTP callers get an early error and never reach this server; prefer
// numaRefitTestServerTLS for authenticated callers.
func numaRefitTestServer(t *testing.T, response device.NumaRefitResponse) (*httptest.Server, *device.NumaRefitRequest) {
	t.Helper()
	var lastRequest device.NumaRefitRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, numaRefitPath, r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&lastRequest))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(server.Close)
	return server, &lastRequest
}

// numaRefitTestServerTLS wraps numaRefitTestServer in TLS and wires the test
// token so the client always presents a valid bearer credential.
func numaRefitTestServerTLS(t *testing.T, response device.NumaRefitResponse) (*httptest.Server, *device.NumaRefitRequest) {
	t.Helper()
	var lastRequest device.NumaRefitRequest
	server, caPath := numaRefitTLSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, numaRefitPath, r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&lastRequest))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	})
	t.Setenv(SchedulerCAFileEnvName, caPath)
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	setupTestToken(t, "test-sa-token")
	return server, &lastRequest
}

func TestRequestNumaRefitRoundTrip(t *testing.T) {
	// Isolate from any projected SA token in the environment: requestNumaRefit
	// always reads serviceAccountTokenFile now (fail-closed), so we use the
	// TLS helper which installs a deterministic token and a matching CA.
	refitted := device.ContainerDevices{{UUID: numaTestGPUB, Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	server, lastRequest := numaRefitTestServerTLS(t, device.NumaRefitResponse{
		Succeeded:        true,
		ContainerDevices: device.EncodeContainerDevices(refitted),
	})
	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	devices, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("best-effort"), 0, []string{numaTestGPUB})

	require.NoError(t, err)
	require.Len(t, devices, 1)
	require.Equal(t, numaTestGPUB, devices[0].UUID)
	require.Equal(t, int32(20000), devices[0].Usedmem)

	require.Equal(t, "refit-pod-uid", lastRequest.PodUID)
	require.Equal(t, "default", lastRequest.PodNamespace)
	require.Equal(t, "numa-pod", lastRequest.PodName)
	require.Equal(t, "node-a", lastRequest.NodeName)
	require.Equal(t, 0, lastRequest.ContainerIndex)
	require.Equal(t, "main", lastRequest.ContainerName)
	require.Equal(t, nvidia.NvidiaGPUDevice, lastRequest.DeviceType)
	require.Equal(t, []string{numaTestGPUB}, lastRequest.AllowedDeviceUUIDs)
}

func TestRequestNumaRefitRefused(t *testing.T) {
	server, _ := numaRefitTestServerTLS(t, device.NumaRefitResponse{Succeeded: false, FailureReason: "no allowed device fits"})
	t.Setenv(SchedulerEndpointEnvName, server.URL)

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "no allowed device fits")
}

func TestRequestNumaRefitBadStatus(t *testing.T) {
	setupTestToken(t, "test-sa-token")
	server, caPath := numaRefitTLSTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	t.Setenv(SchedulerCAFileEnvName, caPath)
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	t.Setenv(SchedulerEndpointEnvName, server.URL)

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "status 500")
}

// TestRequestNumaRefitMissingToken verifies the fail-closed behavior added in
// issue #2878: when the projected SA token file cannot be read, requestNumaRefit
// returns an immediate error rather than silently degrading to unauthenticated.
func TestRequestNumaRefitMissingToken(t *testing.T) {
	// Point to a guaranteed-nonexistent token file so we do not accidentally
	// inherit a projected SA token from the test environment.
	origTokenFile := serviceAccountTokenFile
	serviceAccountTokenFile = filepath.Join(t.TempDir(), "nonexistent-token")
	t.Cleanup(func() { serviceAccountTokenFile = origTokenFile })

	// Use an HTTPS endpoint so the only reason for failure is the missing token.
	server, caPath := numaRefitTLSTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(SchedulerCAFileEnvName, caPath)
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot read service account token for refit authentication")
}

func TestAllowedPhysicalDeviceIDs(t *testing.T) {
	got := allowedPhysicalDeviceIDs([]string{
		numaTestGPUB + "-0", numaTestGPUB + "-1", numaTestGPUA + "-0",
	})
	require.Equal(t, []string{numaTestGPUB, numaTestGPUA}, got)
}

// End-to-end through GetPreferredAllocation: best-effort pod, mismatch, and a
// scheduler that refits onto GPU-B — kubelet receives GPU-B replicas.
func TestGetPreferredAllocationRefitBestEffort(t *testing.T) {
	refitted := device.ContainerDevices{{UUID: numaTestGPUB, Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	server, lastRequest := numaRefitTestServerTLS(t, device.NumaRefitResponse{
		Succeeded:        true,
		ContainerDevices: device.EncodeContainerDevices(refitted),
	})
	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(util.NodeNameEnvName, "node-a")
	setupInRequestDevices(t)

	pod := numaRefitTestPod("best-effort")
	mockAllocateGlobals(t, pod)

	plugin := &NvidiaDevicePlugin{}
	response, err := plugin.GetPreferredAllocation(context.Background(), &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{{
			AvailableDeviceIDs: []string{numaTestGPUB + "-0", numaTestGPUB + "-1"},
			AllocationSize:     1,
		}},
	})

	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	require.Equal(t, []string{numaTestGPUB + "-0"}, response.ContainerResponses[0].DeviceIDs)
	require.Equal(t, []string{numaTestGPUB}, lastRequest.AllowedDeviceUUIDs)
}

// Strict pod with a scheduler that refuses: the allocation must fail.
func TestGetPreferredAllocationRefitStrictFailure(t *testing.T) {
	server, _ := numaRefitTestServerTLS(t, device.NumaRefitResponse{Succeeded: false, FailureReason: "no allowed device fits"})
	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(util.NodeNameEnvName, "node-a")
	setupInRequestDevices(t)

	pod := numaRefitTestPod("strict")
	mockAllocateGlobals(t, pod)

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.GetPreferredAllocation(context.Background(), &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{{
			AvailableDeviceIDs: []string{numaTestGPUB + "-0"},
			AllocationSize:     1,
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "numa-alignment strict")
}

// Without the endpoint configured even strict pods keep detection-only
// behavior: empty response, no error.
func TestGetPreferredAllocationRefitDisabled(t *testing.T) {
	t.Setenv(SchedulerEndpointEnvName, "")
	t.Setenv(util.NodeNameEnvName, "node-a")
	setupInRequestDevices(t)

	pod := numaRefitTestPod("strict")
	mockAllocateGlobals(t, pod)

	plugin := &NvidiaDevicePlugin{}
	response, err := plugin.GetPreferredAllocation(context.Background(), &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{{
			AvailableDeviceIDs: []string{numaTestGPUB + "-0"},
			AllocationSize:     1,
		}},
	})

	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 0)
}

func TestNumaRefitTLSConfigVerifiesByDefault(t *testing.T) {
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	t.Setenv(SchedulerCAFileEnvName, "")
	config, err := numaRefitTLSConfig(false)
	require.NoError(t, err)
	require.False(t, config.InsecureSkipVerify)
	require.Nil(t, config.RootCAs)

	// Authenticated requests also verify by default
	configAuth, err := numaRefitTLSConfig(true)
	require.NoError(t, err)
	require.False(t, configAuth.InsecureSkipVerify)
}

func TestNumaRefitTLSConfigInsecureOptOut(t *testing.T) {
	t.Setenv(SchedulerTLSInsecureEnvName, "true")
	config, err := numaRefitTLSConfig(false)
	require.NoError(t, err)
	require.True(t, config.InsecureSkipVerify)

	// Insecure opt-out is strictly prohibited for authenticated requests
	_, err = numaRefitTLSConfig(true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insecure TLS verification is not permitted")
}

// A committed refit whose devices kubelet cannot honor must fail the
// allocation even in best-effort mode: falling back would leave scheduler
// accounting and runtime divergent.
func TestGetPreferredAllocationRefitCommittedButUnmappable(t *testing.T) {
	refitted := device.ContainerDevices{{UUID: numaTestGPUA, Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	server, _ := numaRefitTestServerTLS(t, device.NumaRefitResponse{
		Succeeded:        true,
		ContainerDevices: device.EncodeContainerDevices(refitted),
	})
	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(util.NodeNameEnvName, "node-a")
	setupInRequestDevices(t)

	pod := numaRefitTestPod("best-effort")
	mockAllocateGlobals(t, pod)

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.GetPreferredAllocation(context.Background(), &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{{
			AvailableDeviceIDs: []string{numaTestGPUB + "-0"},
			AllocationSize:     1,
		}},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "committed")
}

func TestNumaRefitTLSConfigRejectsUnusableCA(t *testing.T) {
	t.Setenv(SchedulerTLSInsecureEnvName, "")

	missing := filepath.Join(t.TempDir(), "absent.crt")
	t.Setenv(SchedulerCAFileEnvName, missing)
	_, err := numaRefitTLSConfig(false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot read scheduler CA bundle")

	garbage := filepath.Join(t.TempDir(), "garbage.crt")
	require.NoError(t, os.WriteFile(garbage, []byte("not a certificate"), 0o600))
	t.Setenv(SchedulerCAFileEnvName, garbage)
	_, err = numaRefitTLSConfig(false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no usable certificates")
}

// A refit must not send the allowed set shrunk to MustIncludeDeviceIDs:
// kubelet builds AvailableDeviceIDs as a superset of it.
func TestGetPreferredAllocationRefitUsesAvailableSuperset(t *testing.T) {
	refitted := device.ContainerDevices{{UUID: numaTestGPUB, Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	server, lastRequest := numaRefitTestServerTLS(t, device.NumaRefitResponse{
		Succeeded:        true,
		ContainerDevices: device.EncodeContainerDevices(refitted),
	})
	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(util.NodeNameEnvName, "node-a")
	setupInRequestDevices(t)

	pod := numaRefitTestPod("best-effort")
	mockAllocateGlobals(t, pod)

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.GetPreferredAllocation(context.Background(), &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{{
			AvailableDeviceIDs:   []string{numaTestGPUB + "-0", numaTestGPUC + "-0"},
			MustIncludeDeviceIDs: []string{numaTestGPUB + "-0"},
			AllocationSize:       1,
		}},
	})

	require.NoError(t, err)
	require.ElementsMatch(t, []string{numaTestGPUB, numaTestGPUC}, lastRequest.AllowedDeviceUUIDs)
}

func setupTestToken(t *testing.T, tokenContent string) {
	t.Helper()
	tokenDir := t.TempDir()
	tokenPath := filepath.Join(tokenDir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte(tokenContent), 0o600))
	origTokenFile := serviceAccountTokenFile
	serviceAccountTokenFile = tokenPath
	t.Cleanup(func() {
		serviceAccountTokenFile = origTokenFile
	})
}

func numaRefitTLSTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(caPath, certPEM, 0o600))
	return server, caPath
}

func TestRequestNumaRefitSecurity_HTTPWithTokenRejected(t *testing.T) {
	setupTestToken(t, "secret-service-account-token")

	receivedRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequests++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "authenticated refit requires HTTPS")
	require.Equal(t, 0, receivedRequests, "token must never be sent over plaintext HTTP")
}

func TestRequestNumaRefitSecurity_HTTPSWithValidTokenAndCA(t *testing.T) {
	setupTestToken(t, "secret-service-account-token")

	refitted := device.ContainerDevices{{UUID: numaTestGPUB, Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	var receivedAuthHeader string
	server, caPath := numaRefitTLSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(device.NumaRefitResponse{
			Succeeded:        true,
			ContainerDevices: device.EncodeContainerDevices(refitted),
		})
	})

	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(SchedulerCAFileEnvName, caPath)
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	devices, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.NoError(t, err)
	require.Len(t, devices, 1)
	require.Equal(t, "Bearer secret-service-account-token", receivedAuthHeader)
}

func TestRequestNumaRefitSecurity_HTTPSInsecureWithTokenRejected(t *testing.T) {
	setupTestToken(t, "secret-service-account-token")

	server, caPath := numaRefitTLSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(SchedulerCAFileEnvName, caPath)
	t.Setenv(SchedulerTLSInsecureEnvName, "true")
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "insecure TLS verification is not permitted")
}

func TestRequestNumaRefitSecurity_RedirectRejected(t *testing.T) {
	setupTestToken(t, "secret-service-account-token")

	var targetReceived bool
	targetServer, targetCA := numaRefitTLSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		targetReceived = true
		w.WriteHeader(http.StatusOK)
	})

	redirectServer, redirectCA := numaRefitTLSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL+"/refit", http.StatusTemporaryRedirect)
	})

	// Combine CA bundles so TLS to redirectServer succeeds initially
	combinedCA := filepath.Join(t.TempDir(), "combined_ca.crt")
	ca1, _ := os.ReadFile(redirectCA)
	ca2, _ := os.ReadFile(targetCA)
	require.NoError(t, os.WriteFile(combinedCA, append(ca1, ca2...), 0o600))

	t.Setenv(SchedulerEndpointEnvName, redirectServer.URL)
	t.Setenv(SchedulerCAFileEnvName, combinedCA)
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "redirects are not permitted")
	require.False(t, targetReceived, "authorization must not be followed across redirects")
}

func TestRequestNumaRefitSecurity_RedirectHTTPSToHTTPRejected(t *testing.T) {
	setupTestToken(t, "secret-service-account-token")

	var httpTargetReceived bool
	httpTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpTargetReceived = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(httpTarget.Close)

	redirectServer, redirectCA := numaRefitTLSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, httpTarget.URL+"/refit", http.StatusTemporaryRedirect)
	})

	t.Setenv(SchedulerEndpointEnvName, redirectServer.URL)
	t.Setenv(SchedulerCAFileEnvName, redirectCA)
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "redirects are not permitted")
	require.False(t, httpTargetReceived, "token must not leak to plaintext HTTP via redirect")
}

func TestRequestNumaRefitSecurity_InvalidCABundleFailsClosed(t *testing.T) {
	setupTestToken(t, "secret-service-account-token")

	server, _ := numaRefitTLSTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Setenv(SchedulerEndpointEnvName, server.URL)
	t.Setenv(SchedulerCAFileEnvName, filepath.Join(t.TempDir(), "nonexistent.crt"))
	t.Setenv(SchedulerTLSInsecureEnvName, "")
	t.Setenv(util.NodeNameEnvName, "node-a")

	plugin := &NvidiaDevicePlugin{}
	_, err := plugin.requestNumaRefit(context.Background(), numaRefitTestPod("strict"), 0, []string{numaTestGPUB})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot read scheduler CA bundle")
}
