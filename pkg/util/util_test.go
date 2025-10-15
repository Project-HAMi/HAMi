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
	"time"

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
	client.KubeClient = fake.NewSimpleClientset()
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
	client.KubeClient = fake.NewSimpleClientset()
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

	pendingPod := podList[0]

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
			want:    pendingPod,
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

func TestGetAllocatePodByNode(t *testing.T) {
	client.KubeClient = fake.NewSimpleClientset()

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
	client.KubeClient = fake.NewSimpleClientset()

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

func Test_IsPodTerminatingOrFinished(t *testing.T) {
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
			name: "pod terminating",
			args: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
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
			got := IsPodTerminatingOrFinished(test.args)
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
