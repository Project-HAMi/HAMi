/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package plugin

import (
	"testing"

	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func TestGenerateMigTemplate(t *testing.T) {
	sconfig := nvidia.NvidiaConfig{
		MigGeometriesList: []device.AllowedMigGeometries{
			{
				Models: []string{"A30"},
				Geometries: []device.Geometry{
					{device.MigTemplate{Name: "1g.6gb", Memory: 6144, Count: 4}},
					{device.MigTemplate{Name: "2g.12gb", Memory: 12288, Count: 2}},
					{device.MigTemplate{Name: "4g.24gb", Memory: 24576, Count: 1}},
				},
			},
			{
				Models: []string{"A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB", "A100-SXM4-40GB"},
				Geometries: []device.Geometry{
					{device.MigTemplate{Name: "1g.5gb", Memory: 5120, Count: 7}},
					{device.MigTemplate{Name: "2g.10gb", Memory: 10240, Count: 3}},
					{device.MigTemplate{Name: "1g.5gb", Memory: 5120, Count: 1}},
					{device.MigTemplate{Name: "3g.20gb", Memory: 20480, Count: 2}},
					{device.MigTemplate{Name: "7g.40gb", Memory: 40960, Count: 1}},
				},
			},
			{
				Models: []string{"A100-SXM4-80GB", "A100-80GB-PCIe", "A100-PCIE-80GB"},
				Geometries: []device.Geometry{
					{device.MigTemplate{Name: "1g.10gb", Memory: 10240, Count: 7}},
					{device.MigTemplate{Name: "2g.20gb", Memory: 20480, Count: 3}},
					{device.MigTemplate{Name: "1g.10gb", Memory: 10240, Count: 1}},
					{device.MigTemplate{Name: "3g.40gb", Memory: 40960, Count: 2}},
					{device.MigTemplate{Name: "7g.80gb", Memory: 81920, Count: 1}},
				},
			},
		},
	}

	plugin := NvidiaDevicePlugin{
		operatingMode:   "mig",
		schedulerConfig: sconfig,
	}
	plugin.migCurrent = nvidia.MigPartedSpec{
		Version:    "v1",
		MigConfigs: make(map[string]nvidia.MigConfigSpecSlice),
	}
	plugin.migCurrent.MigConfigs["current"] = nvidia.MigConfigSpecSlice{
		nvidia.MigConfigSpec{
			Devices:    []int32{0, 1},
			MigEnabled: true,
			MigDevices: make(map[string]int32), // Ensure this map is initialized
		},
	}

	testCases := []struct {
		name          string
		model         string
		deviceIdx     int
		containerDev  device.ContainerDevice
		expectedPos   int
		expectedReset bool
		expectedMig   map[string]int32
	}{
		{
			name:      "2g.10gb template",
			model:     "A100-SXM4-40GB",
			deviceIdx: 0,
			containerDev: device.ContainerDevice{
				Idx:     0,
				UUID:    "aaaaabbbb[1-1]",
				Usedmem: 3000,
			},
			expectedPos:   1,
			expectedReset: true,
			expectedMig: map[string]int32{
				"2g.10gb": 3,
			},
		},
		{
			name:      "1g.5gb template",
			model:     "A100-SXM4-40GB",
			deviceIdx: 0,
			containerDev: device.ContainerDevice{
				Idx:     0,
				UUID:    "aaaaabbbb[0-1]",
				Usedmem: 3000,
			},
			expectedPos:   1,
			expectedReset: true,
			expectedMig: map[string]int32{
				"1g.5gb": 7,
			},
		},
		{
			name:      "no reset needed",
			model:     "A100-SXM4-40GB",
			deviceIdx: 0,
			containerDev: device.ContainerDevice{
				Idx:     0,
				UUID:    "aaaaabbbb[0-2]",
				Usedmem: 3000,
			},
			expectedPos:   2,
			expectedReset: false,
			expectedMig: map[string]int32{
				"1g.5gb": 8,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pos, needsreset := plugin.GenerateMigTemplate(tc.model, tc.deviceIdx, tc.containerDev)

			// Check if the position matches the expected value
			if pos != tc.expectedPos {
				t.Errorf("expected position %d, got %d", tc.expectedPos, pos)
			}

			// Check if the reset flag matches the expected value
			if needsreset != tc.expectedReset {
				t.Errorf("expected reset %v, got %v", tc.expectedReset, needsreset)
			}

			// Check if the mig devices match the expected values
			migDevices := plugin.migCurrent.MigConfigs["current"][0].MigDevices
			for k, v := range tc.expectedMig {
				actual, ok := migDevices[k]
				if !ok || actual != v {
					t.Errorf("expected %s count %d, got %d", k, v, actual)
				}
			}
		})
	}
}

func Test_PodAllocationTrySuccess(t *testing.T) {
	// Initialize fake clientset and pre-load test data
	client.KubeClient = fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testpod",
			Namespace:   "default",
			Annotations: map[string]string{"test-annotation-key": "test-annotation-value", device.InRequestDevices["NVIDIA"]: "some-value"},
		},
	}

	// Add the pod to the fake clientset
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	nodeName := "test-node"
	devName := "NVIDIA"
	lockName := "test-lock"

	// Call the function under test
	PodAllocationTrySuccess(nodeName, devName, lockName, pod)

	// Refresh the pod state from the fake clientset and check the annotations
	refreshedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed pod: %v", err)
	}

	annos, ok := refreshedPod.Annotations[device.InRequestDevices[devName]]
	if !ok || annos == "" {
		t.Error("Expected annotations to be updated")
	}
}

func Test_PodAllocationSuccess(t *testing.T) {
	// Initialize fake clientset and pre-load test data
	client.KubeClient = fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testpod",
			Namespace: "default",
			Annotations: map[string]string{
				"test-annotation-key": "test-annotation-value",
			},
		},
	}

	// Add the pod to the fake clientset
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	nodeName := "test-node"
	lockName := "test-lock"

	// Update pod annotations and release the lock as part of the setup for the test
	updatePodAnnotationsAndReleaseLock(nodeName, pod, lockName, util.DeviceBindSuccess)

	// Call the function under test
	PodAllocationSuccess(nodeName, pod, lockName)

	// Refresh the pod state from the fake clientset and check the DeviceBindPhase annotation
	refreshedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed pod: %v", err)
	}

	annos, ok := refreshedPod.Annotations[util.DeviceBindPhase]
	if !ok || annos != util.DeviceBindSuccess {
		t.Errorf("Expected DeviceBindPhase annotation to be '%s', got '%s'", util.DeviceBindSuccess, annos)
	}
}
func Test_PodAllocationFailed(t *testing.T) {

	client.KubeClient = fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testpod",
			Namespace:   "default",
			Annotations: map[string]string{"test-annotation-key": "test-annotation-value"},
		},
	}

	// add pod to the fake client
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	nodeName := "test-node"
	lockName := "test-lock"

	// simulate a failed pod allocation
	PodAllocationFailed(nodeName, pod, lockName)

	// retrieve the pod from the fake client
	refreshedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed pod: %v", err)
	}

	annos, ok := refreshedPod.Annotations[util.DeviceBindPhase]
	if !ok {
		t.Error("Expected DeviceBindPhase annotation to be present")
	} else if annos != util.DeviceBindFailed {
		t.Errorf("Expected DeviceBindPhase annotation to be '%s', got '%s'", util.DeviceBindFailed, annos)
	}
}
