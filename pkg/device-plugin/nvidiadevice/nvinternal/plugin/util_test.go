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
	"strings"
	"testing"

	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"tags.cncf.io/container-device-interface/specs-go"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

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
	nodeName := "test-node"
	if err := createPodAndLockedNode(t, pod, nodeName); err != nil {
		t.Fatal(err)
	}

	PodAllocationSuccess(nodeName, pod, "test-lock")

	assertDeviceBindPhase(t, pod, util.DeviceBindSuccess)
	assertNodeLockAbsent(t, nodeName)
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
	nodeName := "test-node"
	if err := createPodAndLockedNode(t, pod, nodeName); err != nil {
		t.Fatal(err)
	}

	PodAllocationFailed(nodeName, pod, "test-lock")

	assertDeviceBindPhase(t, pod, util.DeviceBindFailed)
	assertNodeLockAbsent(t, nodeName)
}

func TestUpdatePodAnnotationsAndReleaseLock(t *testing.T) {
	t.Run("releases lock when patch fails", func(t *testing.T) {
		for _, phase := range []string{util.DeviceBindFailed, util.DeviceBindSuccess} {
			t.Run(phase, func(t *testing.T) {
				client.KubeClient = fake.NewClientset()
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testpod",
						Namespace: "default",
					},
				}
				nodeName := "test-node"
				if _, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), lockedNode(pod, nodeName), metav1.CreateOptions{}); err != nil {
					t.Fatalf("Failed to create test node: %v", err)
				}

				updatePodAnnotationsAndReleaseLock(nodeName, pod, "test-lock", phase)

				assertNodeLockAbsent(t, nodeName)
			})
		}
	})

	t.Run("patches phase and releases lock when patch succeeds", func(t *testing.T) {
		for _, phase := range []string{util.DeviceBindFailed, util.DeviceBindSuccess} {
			t.Run(phase, func(t *testing.T) {
				client.KubeClient = fake.NewClientset()
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testpod",
						Namespace: "default",
					},
				}
				nodeName := "test-node"
				if err := createPodAndLockedNode(t, pod, nodeName); err != nil {
					t.Fatal(err)
				}

				updatePodAnnotationsAndReleaseLock(nodeName, pod, "test-lock", phase)

				assertDeviceBindPhase(t, pod, phase)
				assertNodeLockAbsent(t, nodeName)
			})
		}
	})

	t.Run("does not clear another pod lock when patch fails", func(t *testing.T) {
		client.KubeClient = fake.NewClientset()
		owner := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "owner-pod",
				Namespace: "default",
			},
		}
		caller := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "caller-pod",
				Namespace: "default",
			},
		}
		nodeName := "test-node"
		if _, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), lockedNode(owner, nodeName), metav1.CreateOptions{}); err != nil {
			t.Fatalf("Failed to create test node: %v", err)
		}

		updatePodAnnotationsAndReleaseLock(nodeName, caller, "test-lock", util.DeviceBindFailed)

		refreshedNode, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get refreshed node: %v", err)
		}
		lockStr, ok := refreshedNode.Annotations[nodelock.NodeLockKey]
		if !ok {
			t.Fatal("expected other pod's node lock to remain")
		}
		if !strings.HasSuffix(lockStr, nodelock.NodeLockSep+nodelock.GeneratePodNamespaceName(owner, nodelock.NodeLockSep)) {
			t.Fatalf("expected lock owned by %s/%s, got %q", owner.Namespace, owner.Name, lockStr)
		}
	})
}

func lockedNode(pod *corev1.Pod, nodeName string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(pod),
			},
		},
	}
}

func createPodAndLockedNode(t *testing.T, pod *corev1.Pod, nodeName string) error {
	t.Helper()
	if _, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		return err
	}
	if _, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), lockedNode(pod, nodeName), metav1.CreateOptions{}); err != nil {
		return err
	}
	return nil
}

func assertDeviceBindPhase(t *testing.T, pod *corev1.Pod, want string) {
	t.Helper()
	refreshedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed pod: %v", err)
	}
	got, ok := refreshedPod.Annotations[util.DeviceBindPhase]
	if !ok || got != want {
		t.Fatalf("Expected DeviceBindPhase annotation to be %q, got %q", want, got)
	}
}

func assertNodeLockAbsent(t *testing.T, nodeName string) {
	t.Helper()
	refreshedNode, err := client.KubeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed node: %v", err)
	}
	if _, ok := refreshedNode.Annotations[nodelock.NodeLockKey]; ok {
		t.Fatal("expected node lock to be released")
	}
}

func TestCheckCDISpec(t *testing.T) {
	testCases := []struct {
		name        string
		spec        specs.Spec
		kind        string
		expectError bool
		errorMsg    string
	}{
		{
			name: "success - no MIG device with correct kind",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "GPU-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{
									Path:     "/dev/nvidia0",
									HostPath: "/dev/nvidia0",
								},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: false,
		},
		{
			name: "success - MIG device with nvidia-cap and correct kind",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{
									Path: "/dev/nvidia0",
								},
								{
									Path: "/dev/nvidia-cap",
								},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: false,
		},
		{
			name: "success - multiple MIG devices all with cap and correct kind",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-11111111-1111-1111-1111-111111111111",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
								{Path: "/dev/nvidia-cap"},
							},
						},
					},
					{
						Name: "MIG-22222222-2222-2222-2222-222222222222",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia1"},
								{Path: "/dev/nvidia-cap"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: false,
		},
		{
			name: "fail - kind mismatch (nvidia.com/gpu vs expected)",
			spec: specs.Spec{
				Kind: "nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "GPU-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: true,
			errorMsg:    "kind mismatch. current: nvidia.com/gpu, expect: k8s.device-plugin.nvidia.com/gpu",
		},
		{
			name: "fail - kind mismatch (custom kind vs expected)",
			spec: specs.Spec{
				Kind: "mycompany.com/gpu",
				Devices: []specs.Device{
					{
						Name: "GPU-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: true,
			errorMsg:    "kind mismatch. current: mycompany.com/gpu, expect: k8s.device-plugin.nvidia.com/gpu",
		},
		{
			name: "fail - MIG device has no deviceNodes",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: true,
			errorMsg:    "MIG device MIG-12345678-1234-1234-1234-123456789012 has no deviceNodes",
		},
		{
			name: "fail - MIG device missing nvidia-cap",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
								{Path: "/dev/nvidia1"},
								{Path: "/dev/nvidia2"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: true,
			errorMsg:    "MIG device MIG-12345678-1234-1234-1234-123456789012 does not have a corresponding nvidia-cap device",
		},
		{
			name: "fail - multiple MIG devices one missing cap",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-11111111-1111-1111-1111-111111111111",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
								{Path: "/dev/nvidia-cap"},
							},
						},
					},
					{
						Name: "MIG-22222222-2222-2222-2222-222222222222",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia1"},
							},
						},
					},
					{
						Name: "MIG-33333333-3333-3333-3333-333333333333",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia2"},
								{Path: "/dev/nvidia-cap"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: true,
			errorMsg:    "MIG device MIG-22222222-2222-2222-2222-222222222222 does not have a corresponding nvidia-cap device",
		},
		{
			name: "success - MIG device with multiple cap devices and correct kind",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
								{Path: "/dev/nvidia-cap"},
								{Path: "/dev/nvidia-cap-extra"},
								{Path: "/dev/nvidia-cap-backup"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: false,
		},
		{
			name: "fail - MIG device with nil deviceNodes",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: nil,
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: true,
			errorMsg:    "MIG device MIG-12345678-1234-1234-1234-123456789012 has no deviceNodes",
		},
		{
			name: "success - MIG device with cap device path containing 'nvidia-cap' substring",
			spec: specs.Spec{
				Kind: "k8s.device-plugin.nvidia.com/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
								{Path: "/dev/nvidia-cap01"},
								{Path: "/dev/nvidia-cap-abc"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: false,
		},
		{
			name: "fail - wrong kind with MIG device",
			spec: specs.Spec{
				Kind: "wrong.kind/gpu",
				Devices: []specs.Device{
					{
						Name: "MIG-12345678-1234-1234-1234-123456789012",
						ContainerEdits: specs.ContainerEdits{
							DeviceNodes: []*specs.DeviceNode{
								{Path: "/dev/nvidia0"},
								{Path: "/dev/nvidia-cap"},
							},
						},
					},
				},
			},
			kind:        "k8s.device-plugin.nvidia.com/gpu",
			expectError: true,
			errorMsg:    "kind mismatch. current: wrong.kind/gpu, expect: k8s.device-plugin.nvidia.com/gpu",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkCDISpec(tc.spec, tc.kind)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				} else if tc.errorMsg != "" && err.Error() != tc.errorMsg {
					t.Errorf("Expected error message:\n'%s'\nGot:\n'%s'", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

func gpuContainer(limits corev1.ResourceList) *corev1.Container {
	return &corev1.Container{
		Name:      "main",
		Resources: corev1.ResourceRequirements{Limits: limits},
	}
}

// cardPlugin builds a plugin that knows one card of the given size, the way the
// registered node annotation tells the scheduler about it.
func cardPlugin(mode string, uuid string, totalMemoryMB uint64, scaling float64) *NvidiaDevicePlugin {
	return &NvidiaDevicePlugin{
		operatingMode:   mode,
		schedulerConfig: nvidia.NvidiaConfig{DeviceMemoryScaling: &scaling},
		rm: &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices {
			return rm.Devices{uuid: &rm.Device{TotalMemory: totalMemoryMB * 1024 * 1024}}
		}},
	}
}

// A forged hami.io/vgpu-devices-to-allocate annotation must not raise a
// container's memory limit past what it requested (issue #3041).
func TestValidateContainerAllocation(t *testing.T) {
	restore := device.DevicesMap
	defer func() { device.DevicesMap = restore }()

	if err := config.InitDevicesWithConfig(&config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			DefaultGPUNum:                1,
		},
	}); err != nil {
		t.Fatalf("failed to initialize devices: %v", err)
	}

	const uuid = "GPU-1"
	tests := []struct {
		name      string
		plugin    *NvidiaDevicePlugin
		limits    corev1.ResourceList
		allocated device.ContainerDevices
		wantErr   bool
	}{
		{
			name:      "within the requested memory",
			plugin:    cardPlugin("hami-core", uuid, 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem": resource.MustParse("3000")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 3000}},
		},
		{
			name:      "more memory than requested",
			plugin:    cardPlugin("hami-core", uuid, 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem": resource.MustParse("3000")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 20000}},
			wantErr:   true,
		},
		{
			name:      "more cores than requested",
			plugin:    cardPlugin("hami-core", uuid, 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpucores": resource.MustParse("10")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedcores: 100}},
			wantErr:   true,
		},
		{
			name:      "within the requested percentage",
			plugin:    cardPlugin("hami-core", uuid, 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem-percentage": resource.MustParse("50")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 12000}},
		},
		{
			name:      "more than the requested percentage",
			plugin:    cardPlugin("hami-core", uuid, 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem-percentage": resource.MustParse("50")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 20000}},
			wantErr:   true,
		},
		{
			name:      "percentage of an oversubscribed card",
			plugin:    cardPlugin("hami-core", uuid, 24000, 2),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem-percentage": resource.MustParse("50")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 24000}},
		},
		{
			name:      "percentage on a card this plugin does not know",
			plugin:    cardPlugin("hami-core", "GPU-other", 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem-percentage": resource.MustParse("50")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 20000}},
		},
		{
			name:      "memory left to the default",
			plugin:    cardPlugin("hami-core", uuid, 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 24000}},
		},
		{
			name:      "mig slice charged its profile",
			plugin:    cardPlugin("mig", uuid, 24000, 1),
			limits:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem": resource.MustParse("3000")},
			allocated: device.ContainerDevices{{UUID: uuid, Usedmem: 20000}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plugin.validateContainerAllocation(gpuContainer(tt.limits), tt.allocated)
			if tt.wantErr {
				if err == nil {
					t.Fatal("the allocation was accepted")
				}
				if !strings.Contains(err.Error(), "main") {
					t.Errorf("error %q does not name the container", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("the allocation was rejected: %v", err)
			}
		})
	}
}

// A device plugin built without the nvidia backend registered has no request to
// compare against, and must not reject the allocation on that account.
func TestValidateContainerAllocationWithoutDevices(t *testing.T) {
	restore := device.DevicesMap
	defer func() { device.DevicesMap = restore }()
	device.DevicesMap = nil

	limits := corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1"), "nvidia.com/gpumem": resource.MustParse("3000")}
	plugin := cardPlugin("hami-core", "GPU-1", 24000, 1)
	if err := plugin.validateContainerAllocation(gpuContainer(limits), device.ContainerDevices{{UUID: "GPU-1", Usedmem: 20000}}); err != nil {
		t.Fatalf("the allocation was rejected: %v", err)
	}
}

// Alignment can move an annotated entry onto the card the kubelet picked, so
// the share has to be measured against that card rather than the one the
// annotation named. Half of an 80 GB card is 40 GB, and carrying that onto a
// 24 GB card would hand the container a limit past the whole device.
func TestAllocate_PercentageCheckedAgainstAlignedDevice(t *testing.T) {
	setupInRequestDevices(t)
	restore := device.DevicesMap
	defer func() { device.DevicesMap = restore }()
	if err := config.InitDevicesWithConfig(&config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			DefaultGPUNum:                1,
		},
	}); err != nil {
		t.Fatalf("failed to initialize devices: %v", err)
	}

	annotated := &rm.Device{TotalMemory: 80000 * 1024 * 1024}
	annotated.ID = "GPU-aaa"
	selected := &rm.Device{TotalMemory: 24000 * 1024 * 1024}
	selected.ID = "GPU-bbb"
	plugin := newTestPluginWithRM(t, map[string]*rm.Device{"GPU-aaa": annotated, "GPU-bbb": selected})

	previous := enableGetPreferredAllocation
	enableGetPreferredAllocation = true
	t.Cleanup(func() { enableGetPreferredAllocation = previous })

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aligned-pod", Namespace: "default", UID: "aligned-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,40000,50:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "c0",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"nvidia.com/gpu":               resource.MustParse("1"),
				"nvidia.com/gpumem-percentage": resource.MustParse("50"),
			}},
		}}},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)

	_, err := plugin.Allocate(context.Background(), &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-bbb"}},
		},
	})
	if err == nil {
		t.Fatal("the allocation was accepted on the smaller card")
	}
	if !strings.Contains(err.Error(), "GPU-bbb") {
		t.Errorf("error %q does not name the card the container got", err)
	}
}
