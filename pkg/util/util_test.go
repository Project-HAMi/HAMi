/*
Copyright 2024 The HAMi Authors.

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

package util

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

var inRequestDevices map[string]string

func init() {
	inRequestDevices = make(map[string]string)
	inRequestDevices["NVIDIA"] = "hami.io/vgpu-devices-to-allocate"
}
func TestMarkAnnotationsToDelete(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-worker2"},
	}, metav1.CreateOptions{})
	type args struct {
		devType string
		nn      string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "node not found",
			args: args{
				devType: "huawei.com/Ascend910",
				nn:      "node-worker1",
			},
			wantErr: true,
		},
		{
			name: "mark annotations to delete",
			args: args{
				devType: "huawei.com/Ascend910",
				nn:      "node-worker2",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := MarkAnnotationsToDelete(tt.args.devType, tt.args.nn); (err != nil) != tt.wantErr {
				t.Errorf("MarkAnnotationsToDelete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetPendingPod(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	// Create test node and pod

	podList := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pending-pod",
				Namespace: "default",
				Annotations: map[string]string{
					BindTimeAnnotations:     "2024-01-01T00:00:00Z",
					DeviceBindPhase:         DeviceBindAllocating,
					AssignedNodeAnnotations: "test-node-0",
				},
			},
			Spec: corev1.PodSpec{NodeName: "test-node-0"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "ignore-pod-0",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: corev1.PodSpec{NodeName: "test-node-0"},
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "ignore-pod-1",
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: corev1.PodSpec{NodeName: "test-node-0"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ignore-pod-2",
				Namespace: "default",
				Annotations: map[string]string{
					BindTimeAnnotations:     "2024-01-01T00:00:00Z",
					AssignedNodeAnnotations: "test-node-0",
				},
			},
			Spec: corev1.PodSpec{NodeName: "test-node-0"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ignore-pod-3",
				Namespace: "default",
				Annotations: map[string]string{
					BindTimeAnnotations:     "2024-01-01T00:00:00Z",
					DeviceBindPhase:         "",
					AssignedNodeAnnotations: "test-node-2",
				},
			},
			Spec: corev1.PodSpec{NodeName: "test-node-2"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ignore-pod-4",
				Namespace: "default",
				Annotations: map[string]string{
					BindTimeAnnotations: "2024-01-01T00:00:00Z",
					DeviceBindPhase:     DeviceBindAllocating,
				},
			},
			Spec: corev1.PodSpec{NodeName: "test-node-2"},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		},
	}

	for _, pod := range podList {
		client.KubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
	}

	node0 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-node-0",
			Annotations: map[string]string{},
		},
	}
	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), node0, metav1.CreateOptions{})

	allocatedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allocated-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{NodeName: "test-node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodPhase(corev1.PodInitialized),
		},
	}
	client.KubeClient.CoreV1().Pods("default").Create(context.TODO(), allocatedPod, metav1.CreateOptions{})

	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-1",
			Annotations: map[string]string{
				nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(allocatedPod),
			},
		},
	}
	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), node1, metav1.CreateOptions{})

	tests := []struct {
		name    string
		node    string
		wantErr bool
		want    *corev1.Pod
	}{
		{
			name:    "find pending pod",
			node:    "test-node-0",
			wantErr: false,
			want:    podList[0],
		},
		{
			name:    "find allocated pod",
			node:    "test-node-1",
			wantErr: false,
			want:    allocatedPod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetPendingPod(context.TODO(), tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetPendingPod() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				assert.Equal(t, got.Name, tt.want.Name)
			}
		})
	}
}

func TestGetPendingPodFallsBackWhenLockOwnerMissing(t *testing.T) {
	client.KubeClient = fake.NewSimpleClientset()
	missingOwner := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "missing-owner",
			Namespace: "default",
		},
	}
	fallbackPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-pod-fallback",
			Namespace: "default",
			Annotations: map[string]string{
				BindTimeAnnotations:     "2024-01-01T00:00:00Z",
				DeviceBindPhase:         DeviceBindAllocating,
				AssignedNodeAnnotations: "test-node-fallback",
			},
		},
		Spec:   corev1.PodSpec{NodeName: "test-node-fallback"},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	_, err := client.KubeClient.CoreV1().Pods("default").Create(context.TODO(), fallbackPod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create fallback pod: %v", err)
	}
	_, err = client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node-fallback", Annotations: map[string]string{nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(missingOwner)}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create fallback node: %v", err)
	}

	got, err := GetPendingPod(context.TODO(), "test-node-fallback")
	if err != nil {
		t.Fatalf("GetPendingPod returned error: %v", err)
	}
	if got.Name != fallbackPod.Name {
		t.Fatalf("expected fallback pod %q, got %q", fallbackPod.Name, got.Name)
	}
}

func TestGetAllocatePodByNode(t *testing.T) {
	client.KubeClient = fake.NewClientset()

	emptyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "",
			Namespace: "",
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod0",
			Namespace: "default",
		},
	}
	client.KubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})

	emptyNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-node-0",
			Annotations: map[string]string{},
		},
	}
	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), emptyNode, metav1.CreateOptions{})

	node0 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-1",
			Annotations: map[string]string{
				nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(emptyPod),
			},
		},
	}
	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), node0, metav1.CreateOptions{})

	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-2",
			Annotations: map[string]string{
				nodelock.NodeLockKey: nodelock.GenerateNodeLockKeyByPod(pod),
			},
		},
	}
	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), node1, metav1.CreateOptions{})

	tests := []struct {
		name    string
		node    string
		wantErr bool
		want    *corev1.Pod
	}{
		{
			name:    "node not found",
			node:    "non-existent",
			wantErr: true,
			want:    nil,
		},
		{
			name:    "Missing NodeLockKey Annotation",
			node:    "test-node-0",
			wantErr: false,
			want:    nil,
		},
		{
			name:    "Missing ns and name",
			node:    "test-node-1",
			wantErr: false,
			want:    nil,
		},
		{
			name:    "finding allocated pod",
			node:    "test-node-2",
			wantErr: false,
			want:    pod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAllocatePodByNode(context.TODO(), tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAllocatePodByNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != nil {
				assert.Equal(t, got.Name, tt.want.Name)
			}
		})
	}
}
func TestPatchPodAnnotations(t *testing.T) {
	client.KubeClient = fake.NewClientset()

	// Create test pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	client.KubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})

	tests := []struct {
		name        string
		pod         *corev1.Pod
		annotations map[string]string
		wantErr     bool
	}{
		{
			name: "patch with valid annotations",
			pod:  pod,
			annotations: map[string]string{
				"test-key":              "test-value",
				AssignedNodeAnnotations: "node1",
			},
			wantErr: false,
		},
		{
			name: "patch non-existent pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-existent",
					Namespace: "default",
				},
			},
			annotations: map[string]string{
				"test-key": "test-value",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PatchPodAnnotations(tt.pod, tt.annotations)
			if (err != nil) != tt.wantErr {
				t.Errorf("PatchPodAnnotations() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_IsPodInTerminatedState(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Pod
		want bool
	}{
		{
			name: "pod in failed state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			want: true,
		},
		{
			name: "pod in succeeded state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			},
			want: true,
		},
		{
			name: "pod in running state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			want: false,
		},
		{
			name: "pod in pending state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			want: false,
		},
		{
			name: "pod in unknown state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodUnknown,
				},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := IsPodInTerminatedState(test.args)
			assert.Equal(t, test.want, got)
		})
	}
}
func Test_AllContainersCreated(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Pod
		want bool
	}{
		{
			name: "all containers created",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{},
						{},
					},
				},
			},
			want: true,
		},
		{
			name: "not all containers created",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{},
					},
				},
			},
			want: false,
		},
		{
			name: "no containers created",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{},
				},
			},
			want: false,
		},
		{
			name: "more container statuses than containers",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{},
						{},
					},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := AllContainersCreated(test.args)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestPatchPodLabels(t *testing.T) {
	client.KubeClient = fake.NewClientset()

	// Create test pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{},
		},
	}

	client.KubeClient.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})

	tests := []struct {
		name      string
		namespace string
		podName   string
		labels    map[string]string
		wantErr   bool
	}{
		{
			name:      "patch with valid labels",
			namespace: "default",
			podName:   "test-pod",
			labels: map[string]string{
				HAMiRoleLabel: HAMiRoleLabelValueLeader,
			},
			wantErr: false,
		},
		{
			name:      "update existing label",
			namespace: "default",
			podName:   "test-pod",
			labels: map[string]string{
				HAMiRoleLabel: HAMiRoleLabelValueFollower,
			},
			wantErr: false,
		},
		{
			name:      "add multiple labels",
			namespace: "default",
			podName:   "test-pod",
			labels: map[string]string{
				"test-key1": "test-value1",
				"test-key2": "test-value2",
			},
			wantErr: false,
		},
		{
			name:      "patch non-existent pod",
			namespace: "default",
			podName:   "non-existent",
			labels: map[string]string{
				HAMiRoleLabel: HAMiRoleLabelValueLeader,
			},
			wantErr: true,
		},
		{
			name:      "patch non-existent namespace",
			namespace: "non-existent",
			podName:   "test-pod",
			labels: map[string]string{
				HAMiRoleLabel: HAMiRoleLabelValueLeader,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PatchPodLabels(tt.namespace, tt.podName, tt.labels)
			if (err != nil) != tt.wantErr {
				t.Errorf("PatchPodLabels() error = %v, wantErr %v", err, tt.wantErr)
			}
			// If success, verify the labels were patched
			if err == nil {
				updatedPod, getErr := client.KubeClient.CoreV1().Pods(tt.namespace).Get(context.TODO(), tt.podName, metav1.GetOptions{})
				if getErr != nil {
					t.Errorf("Failed to get updated pod: %v", getErr)
					return
				}
				for k, v := range tt.labels {
					if updatedPod.Labels[k] != v {
						t.Errorf("Label %s = %s, want %s", k, updatedPod.Labels[k], v)
					}
				}
			}
		})
	}
}

func Test_IsPodTerminating(t *testing.T) {
	now := metav1.Now()
	tests := []struct {
		name string
		args *corev1.Pod
		want bool
	}{
		{
			name: "pod with deletion timestamp (terminating)",
			args: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
				},
			},
			want: true,
		},
		{
			name: "pod without deletion timestamp (normal)",
			args: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := IsPodTerminating(test.args)
			assert.Equal(t, test.want, got)
		})
	}
}
