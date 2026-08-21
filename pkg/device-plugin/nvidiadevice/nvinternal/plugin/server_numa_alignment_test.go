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
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	numaTestGPUA = "GPU-aaaaaaaa-1111-2222-3333-444444444444"
	numaTestGPUB = "GPU-bbbbbbbb-1111-2222-3333-444444444444"
)

func TestSelectPreferredMismatchErrorIsTyped(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}

	// The annotated GPU has no replica in kubelet's available set.
	_, err := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(
		[]string{numaTestGPUB + "-0", numaTestGPUB + "-1"},
		nil,
		device.ContainerDevices{{UUID: numaTestGPUA, Type: nvidia.NvidiaGPUDevice}},
		1)
	require.Error(t, err)
	require.ErrorIs(t, err, errAnnotatedDeviceUnavailable)
	require.EqualError(t, err, fmt.Sprintf("no available slice device found for annotated GPU %s", numaTestGPUA))
}

func TestSelectPreferredOtherErrorsAreNotTyped(t *testing.T) {
	plugin := &NvidiaDevicePlugin{}

	// Fewer annotated devices than the requested allocation size.
	_, err := plugin.selectPreferredDeviceIDsFromAnnotatedDevices(
		[]string{numaTestGPUA + "-0"},
		nil,
		device.ContainerDevices{{UUID: numaTestGPUA, Type: nvidia.NvidiaGPUDevice}},
		2)
	require.Error(t, err)
	require.False(t, errors.Is(err, errAnnotatedDeviceUnavailable))
}

func TestGetPreferredAllocationMismatchResponseUnchanged(t *testing.T) {
	tests := []struct {
		name          string
		annotations   map[string]string
		availableIDs  []string
		wantResponses int
		wantDeviceIDs []string
	}{
		{
			name:         "without numa-alignment annotation",
			annotations:  map[string]string{},
			availableIDs: []string{numaTestGPUB + "-0", numaTestGPUB + "-1"},
		},
		{
			name:         "best-effort",
			annotations:  map[string]string{util.NumaAlignmentAnnotationKey: "best-effort"},
			availableIDs: []string{numaTestGPUB + "-0", numaTestGPUB + "-1"},
		},
		{
			name:         "strict is not accepted yet",
			annotations:  map[string]string{util.NumaAlignmentAnnotationKey: "strict"},
			availableIDs: []string{numaTestGPUB + "-0", numaTestGPUB + "-1"},
		},
		{
			name:         "invalid value",
			annotations:  map[string]string{util.NumaAlignmentAnnotationKey: "bogus"},
			availableIDs: []string{numaTestGPUB + "-0", numaTestGPUB + "-1"},
		},
		{
			// Positive control: with the annotated GPU's replica available
			// the same harness returns a preferred response, proving the
			// mismatch cases above exercise the selection path rather than
			// passing vacuously.
			name:          "annotated GPU available returns preferred devices",
			annotations:   map[string]string{util.NumaAlignmentAnnotationKey: "best-effort"},
			availableIDs:  []string{numaTestGPUA + "-0", numaTestGPUB + "-0"},
			wantResponses: 1,
			wantDeviceIDs: []string{numaTestGPUA + "-0"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupInRequestDevices(t)

			annotations := map[string]string{
				"hami.io/vgpu-devices-to-allocate": device.EncodePodSingleDevice(device.PodSingleDevice{
					device.ContainerDevices{{UUID: numaTestGPUA, Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}},
				}),
			}
			for key, value := range test.annotations {
				annotations[key] = value
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:        "numa-pod",
				Namespace:   "default",
				Annotations: annotations,
			}}

			t.Setenv(util.NodeNameEnvName, "node-a")
			mockAllocateGlobals(t, pod)

			plugin := &NvidiaDevicePlugin{}
			response, err := plugin.GetPreferredAllocation(context.Background(), &kubeletdevicepluginv1beta1.PreferredAllocationRequest{
				ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerPreferredAllocationRequest{{
					AvailableDeviceIDs: test.availableIDs,
					AllocationSize:     1,
				}},
			})

			// Detection must not change the response: on a mismatch no
			// preferred devices are returned and no error is propagated to
			// kubelet, exactly as before this feature.
			require.NoError(t, err)
			require.Len(t, response.ContainerResponses, test.wantResponses)
			if test.wantResponses > 0 {
				require.ElementsMatch(t, test.wantDeviceIDs, response.ContainerResponses[0].DeviceIDs)
			}
		})
	}
}

func TestReportAnnotatedDeviceMismatch(t *testing.T) {
	mismatch := fmt.Errorf("%w %s", errAnnotatedDeviceUnavailable, numaTestGPUA)
	podWithMode := func(mode string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name:        "numa-pod",
			Namespace:   "default",
			Annotations: map[string]string{util.NumaAlignmentAnnotationKey: mode},
		}}
	}
	const mismatchMessage = "scheduler-annotated GPU has no replica in kubelet's available devices"

	tests := []struct {
		name          string
		plugin        *NvidiaDevicePlugin
		pod           *corev1.Pod
		err           error
		wantLogged    string
		wantPrefix    string
		wantFields    []string
		wantNoLogging bool
	}{
		{
			name:          "nil pod is silent",
			plugin:        &NvidiaDevicePlugin{},
			pod:           nil,
			err:           mismatch,
			wantNoLogging: true,
		},
		{
			name:          "mig mode is silent",
			plugin:        &NvidiaDevicePlugin{operatingMode: nvidia.MigMode},
			pod:           podWithMode("best-effort"),
			err:           mismatch,
			wantNoLogging: true,
		},
		{
			name:          "unrelated error is silent",
			plugin:        &NvidiaDevicePlugin{},
			pod:           podWithMode("best-effort"),
			err:           errors.New("unrelated"),
			wantNoLogging: true,
		},
		{
			name:          "pod without the annotation is silent",
			plugin:        &NvidiaDevicePlugin{},
			pod:           &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "plain-pod", Namespace: "default"}},
			err:           mismatch,
			wantNoLogging: true,
		},
		{
			name:       "invalid annotation is warned about",
			plugin:     &NvidiaDevicePlugin{},
			pod:        podWithMode("bogus"),
			err:        mismatch,
			wantLogged: "ignoring invalid numa-alignment annotation",
		},
		{
			name:       "best-effort mismatch is reported at info severity",
			plugin:     &NvidiaDevicePlugin{},
			pod:        podWithMode("best-effort"),
			err:        mismatch,
			wantLogged: mismatchMessage,
			wantPrefix: "I",
			wantFields: []string{`numaAlignment="best-effort"`, "default/numa-pod", "containerRequest=0"},
		},
		{
			name:       "strict warns as invalid until the refit lands",
			plugin:     &NvidiaDevicePlugin{},
			pod:        podWithMode("strict"),
			err:        mismatch,
			wantLogged: "ignoring invalid numa-alignment annotation",
			wantPrefix: "W",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			klog.SetOutput(&buf)
			klog.LogToStderr(false)
			defer func() {
				klog.SetOutput(os.Stderr)
				klog.LogToStderr(true)
			}()

			test.plugin.reportAnnotatedDeviceMismatch(test.pod, 0, test.err)
			klog.Flush()

			logged := buf.String()
			if test.wantNoLogging {
				// Unrelated goroutines may log concurrently; assert only
				// that OUR messages are absent.
				require.NotContains(t, logged, mismatchMessage)
				require.NotContains(t, logged, "numa-alignment annotation")
				return
			}
			require.Contains(t, logged, test.wantLogged)
			if test.wantPrefix != "" {
				var matched string
				for _, line := range strings.Split(logged, "\n") {
					if strings.Contains(line, test.wantLogged) {
						matched = line
						break
					}
				}
				require.True(t, strings.HasPrefix(matched, test.wantPrefix),
					"expected severity %q, got line %q", test.wantPrefix, matched)
			}
			for _, field := range test.wantFields {
				require.Contains(t, logged, field)
			}
		})
	}
}
