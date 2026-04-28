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
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

func TestGenerateMigTemplate(t *testing.T) {
	sconfig := nvidia.NvidiaConfig{
		MigGeometriesList: []device.AllowedMigGeometries{
			{
				Models: []string{"A30"},
				Geometries: []device.Geometry{
					{device.MigTemplate{Name: "1g.6gb", Core: 25, Memory: 6144, Count: 4}},
					{device.MigTemplate{Name: "2g.12gb", Core: 50, Memory: 12288, Count: 2}},
					{device.MigTemplate{Name: "4g.24gb", Core: 100, Memory: 24576, Count: 1}},
				},
			},
			{
				Models: []string{"A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB", "A100-SXM4-40GB"},
				Geometries: []device.Geometry{
					{device.MigTemplate{Name: "1g.5gb", Core: 14, Memory: 5120, Count: 7}},
					{device.MigTemplate{Name: "1g.5gb", Core: 14, Memory: 5120, Count: 1}, device.MigTemplate{Name: "2g.10gb", Core: 28, Memory: 10240, Count: 3}},
					{device.MigTemplate{Name: "3g.20gb", Core: 42, Memory: 20480, Count: 2}},
					{device.MigTemplate{Name: "7g.40gb", Core: 100, Memory: 40960, Count: 1}},
				},
			},
			{
				Models: []string{"A100-SXM4-80GB", "A100-80GB-PCIe", "A100-PCIE-80GB"},
				Geometries: []device.Geometry{
					{device.MigTemplate{Name: "1g.10gb", Core: 14, Memory: 10240, Count: 7}},
					{device.MigTemplate{Name: "1g.10gb", Core: 14, Memory: 10240, Count: 1}, device.MigTemplate{Name: "2g.20gb", Core: 28, Memory: 20480, Count: 3}},
					{device.MigTemplate{Name: "3g.40gb", Core: 42, Memory: 40960, Count: 2}},
					{device.MigTemplate{Name: "7g.80gb", Core: 100, Memory: 81920, Count: 1}},
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
				Usedmem: 8000,
			},
			expectedPos:   1,
			expectedReset: true,
			expectedMig: map[string]int32{
				"1g.5gb":  1,
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
				"1g.5gb": 7,
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

func TestGetNextDeviceRequest_DeviceInRegularContainer(t *testing.T) {
	// Save and restore InRequestDevices
	oldInRequestDevices := device.InRequestDevices
	defer func() { device.InRequestDevices = oldInRequestDevices }()

	device.InRequestDevices = map[string]string{
		"NVIDIA": "hami.io/vgpu-devices-to-allocate",
	}

	// Pod with no init containers, one regular container with a device
	// Annotation format: "UUID,Type,mem,cores:;"
	// After split by ";", we get ["UUID,Type,mem,cores:", ""]
	// Index 0 maps to regular container 0 (since no init containers)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-abc123,NVIDIA,1000,30:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main-container"},
			},
		},
	}

	ctr, ctrDevices, err := GetNextDeviceRequest("NVIDIA", pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctr.Name != "main-container" {
		t.Errorf("expected container name 'main-container', got '%s'", ctr.Name)
	}
	if len(ctrDevices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(ctrDevices))
	}
	if ctrDevices[0].UUID != "GPU-abc123" {
		t.Errorf("expected UUID 'GPU-abc123', got '%s'", ctrDevices[0].UUID)
	}
}

func TestGetNextDeviceRequest_DeviceInInitContainer(t *testing.T) {
	oldInRequestDevices := device.InRequestDevices
	defer func() { device.InRequestDevices = oldInRequestDevices }()

	device.InRequestDevices = map[string]string{
		"NVIDIA": "hami.io/vgpu-devices-to-allocate",
	}

	// Pod with 1 init container (has device) and 1 regular container (no device)
	// Annotation: "GPU-init1,NVIDIA,500,10:;;"
	// After split by ";": ["GPU-init1,NVIDIA,500,10:", "", ""]
	// Index 0 -> init container 0 (has device), Index 1 -> regular container 0 (empty)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-init",
			Namespace: "default",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-init1,NVIDIA,500,10:;;",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init-with-gpu"},
			},
			Containers: []corev1.Container{
				{Name: "main-no-gpu"},
			},
		},
	}

	ctr, ctrDevices, err := GetNextDeviceRequest("NVIDIA", pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctr.Name != "init-with-gpu" {
		t.Errorf("expected container name 'init-with-gpu', got '%s'", ctr.Name)
	}
	if len(ctrDevices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(ctrDevices))
	}
	if ctrDevices[0].UUID != "GPU-init1" {
		t.Errorf("expected UUID 'GPU-init1', got '%s'", ctrDevices[0].UUID)
	}
}

func TestGetNextDeviceRequest_DeviceInRegularContainerWithInitOffset(t *testing.T) {
	oldInRequestDevices := device.InRequestDevices
	defer func() { device.InRequestDevices = oldInRequestDevices }()

	device.InRequestDevices = map[string]string{
		"NVIDIA": "hami.io/vgpu-devices-to-allocate",
	}

	// Pod with 2 init containers (no device) and 1 regular container (has device)
	// Annotation: ";;GPU-main1,NVIDIA,2000,50:;"
	// After split by ";": ["", "", "GPU-main1,NVIDIA,2000,50:", ""]
	// Index 0 -> init container 0 (empty)
	// Index 1 -> init container 1 (empty)
	// Index 2 -> regular container 0 (has device, regularIdx = 2 - 2 = 0)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-offset",
			Namespace: "default",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": ";;GPU-main1,NVIDIA,2000,50:;",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init1-no-gpu"},
				{Name: "init2-no-gpu"},
			},
			Containers: []corev1.Container{
				{Name: "main-with-gpu"},
			},
		},
	}

	ctr, ctrDevices, err := GetNextDeviceRequest("NVIDIA", pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctr.Name != "main-with-gpu" {
		t.Errorf("expected container name 'main-with-gpu', got '%s'", ctr.Name)
	}
	if len(ctrDevices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(ctrDevices))
	}
	if ctrDevices[0].UUID != "GPU-main1" {
		t.Errorf("expected UUID 'GPU-main1', got '%s'", ctrDevices[0].UUID)
	}
}

func TestGetNextDeviceRequest_NoDeviceFound(t *testing.T) {
	oldInRequestDevices := device.InRequestDevices
	defer func() { device.InRequestDevices = oldInRequestDevices }()

	device.InRequestDevices = map[string]string{
		"NVIDIA": "hami.io/vgpu-devices-to-allocate",
	}

	// Pod with annotation but all containers have empty devices
	// Annotation: ";;"
	// After split by ";": ["", "", ""]
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-empty",
			Namespace: "default",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": ";;",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init1"},
			},
			Containers: []corev1.Container{
				{Name: "main1"},
			},
		},
	}

	_, _, err := GetNextDeviceRequest("NVIDIA", pod)
	if err == nil {
		t.Fatal("expected error 'device request not found', got nil")
	}
	if err.Error() != "device request not found" {
		t.Errorf("expected error 'device request not found', got '%s'", err.Error())
	}
}

func TestGetNextDeviceRequest_DeviceTypeNotFound(t *testing.T) {
	oldInRequestDevices := device.InRequestDevices
	defer func() { device.InRequestDevices = oldInRequestDevices }()

	device.InRequestDevices = map[string]string{
		"NVIDIA": "hami.io/vgpu-devices-to-allocate",
	}

	// Pod with annotation for NVIDIA, but we ask for a non-existent device type
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-notype",
			Namespace: "default",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-abc,NVIDIA,1000,30:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		},
	}

	_, _, err := GetNextDeviceRequest("AMD", pod)
	if err == nil {
		t.Fatal("expected error 'device request not found', got nil")
	}
}

func Test_PodAllocationTrySuccess(t *testing.T) {
	// Initialize fake clientset and pre-load test data
	client.KubeClient = fake.NewClientset()
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
	podAllocationTrySuccess(nodeName, devName, lockName, pod)

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
	client.KubeClient = fake.NewClientset()
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
	_, err = client.KubeClient.CoreV1().Nodes().Create(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(pod)}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

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
	node, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get test node: %v", err)
	}
	if _, ok := node.Annotations[nodelock.NodeLockKey]; ok {
		t.Error("Expected node lock to be released")
	}
}
func TestUpdatePodAnnotationsAndReleaseLockReleasesWhenPodPatchFails(t *testing.T) {
	client.KubeClient = fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "missing-pod",
			Namespace: "default",
		},
	}
	nodeName := "test-node"
	_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(pod)}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	updatePodAnnotationsAndReleaseLock(nodeName, pod, "test-lock", util.DeviceBindFailed)

	node, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get test node: %v", err)
	}
	if _, ok := node.Annotations[nodelock.NodeLockKey]; ok {
		t.Error("Expected node lock to be released even when pod patch fails")
	}
}

func Test_PodAllocationFailed(t *testing.T) {

	client.KubeClient = fake.NewClientset()
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
	_, err = client.KubeClient.CoreV1().Nodes().Create(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(pod)}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	// simulate a failed pod allocation
	podAllocationFailed(nodeName, pod, lockName)

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
	node, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get test node: %v", err)
	}
	if _, ok := node.Annotations[nodelock.NodeLockKey]; ok {
		t.Error("Expected node lock to be released")
	}
}
