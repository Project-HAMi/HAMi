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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

var inRequestDevices map[string]string

func init() {
	inRequestDevices = make(map[string]string)
	inRequestDevices["NVIDIA"] = "hami.io/vgpu-devices-to-allocate"
}
func TestRemoveAnnotation(t *testing.T) {
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
			name: "remove annotations",
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
				t.Errorf("RemoveAnnotation() error = %v, wantErr %v", err, tt.wantErr)
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
		{
			name: "patch nil pod",
			pod:  nil,
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
		{
			name: "pod is nil",
			args: nil,
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
			name: "pod is nil",
			args: nil,
			want: false,
		},
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

func TestIsPodGroupMember(t *testing.T) {
	podGroupName := "my-training-job"
	emptyPodGroupName := ""

	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "nil pod",
			pod:  nil,
			want: false,
		},
		{
			name: "no group membership at all",
			pod:  &corev1.Pod{},
			want: false,
		},
		{
			name: "scheduler-plugins Coscheduling label present",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{PodGroupLabel: podGroupName},
				},
			},
			want: true,
		},
		{
			name: "coscheduling label present but empty",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{PodGroupLabel: ""},
				},
			},
			want: false,
		},
		{
			name: "native GenericWorkload PodGroup via Spec.SchedulingGroup",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGroup: &corev1.PodSchedulingGroup{
						PodGroupName: &podGroupName,
					},
				},
			},
			want: true,
		},
		{
			name: "Spec.SchedulingGroup set but PodGroupName nil",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGroup: &corev1.PodSchedulingGroup{},
				},
			},
			want: false,
		},
		{
			name: "Spec.SchedulingGroup set but PodGroupName empty",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					SchedulingGroup: &corev1.PodSchedulingGroup{
						PodGroupName: &emptyPodGroupName,
					},
				},
			},
			want: false,
		},
		{
			name: "both coscheduling label and native SchedulingGroup present",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{PodGroupLabel: podGroupName},
				},
				Spec: corev1.PodSpec{
					SchedulingGroup: &corev1.PodSchedulingGroup{
						PodGroupName: &podGroupName,
					},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := IsPodGroupMember(test.pod)
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
		{
			name: "pod is nil",
			args: nil,
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

func TestGetGPUSchedulerPolicyByPod(t *testing.T) {
	tests := []struct {
		name          string
		defaultPolicy string
		pod           *corev1.Pod
		want          string
	}{
		{"nil pod", "binpack", nil, "binpack"},
		{"no annotation", "binpack", &corev1.Pod{}, "binpack"},
		{"other annotations", "binpack", &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"other-annotation": "value"},
			},
		}, "binpack"},
		{"with annotation", "binpack", &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{GPUSchedulerPolicyAnnotationKey: "spread"},
			},
		}, "spread"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetGPUSchedulerPolicyByPod(tt.defaultPolicy, tt.pod))
		})
	}
}

func TestPolicyContains(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		target SchedulerPolicyName
		want   bool
	}{
		{"single value match", "mutex", GPUSchedulerPolicyMutex, true},
		{"single value no match", "spread", GPUSchedulerPolicyMutex, false},
		{"empty policy", "", GPUSchedulerPolicyMutex, false},
		{"comma list match first", "mutex,spread", GPUSchedulerPolicyMutex, true},
		{"comma list match last", "spread,numa,mutex", GPUSchedulerPolicyMutex, true},
		{"comma list no match", "binpack,spread", GPUSchedulerPolicyMutex, false},
		{"comma list with spaces", "mutex, spread, numa", GPUSchedulerPolicyNuma, true},
		{"chain of sort keys, no filter", "binpack,spread,numa", GPUSchedulerPolicyTopology, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PolicyContains(tt.policy, tt.target))
		})
	}
}

func TestSchedulerPolicyName_String(t *testing.T) {
	tests := []struct {
		policy SchedulerPolicyName
		want   string
	}{
		{NodeSchedulerPolicyBinpack, "binpack"},
		{NodeSchedulerPolicySpread, "spread"},
		{GPUSchedulerPolicyTopology, "topology-aware"},
		{SchedulerPolicyName("custom"), "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.policy.String())
		})
	}
}

func TestEmitNodeWarningEvent(t *testing.T) {
	const (
		nodeName = "test-node"
		nodeUID  = types.UID("test-uid-1234")
		reason   = "AsymmetricGPUP2PLink"
		msg1     = "first message"
		msg2     = "updated message"
	)
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			UID:  nodeUID,
		},
	}
	dedupWindow := time.Hour

	t.Run("no existing event creates new event", func(t *testing.T) {
		client.KubeClient = fake.NewClientset()

		EmitNodeWarningEvent(node, reason, msg1, dedupWindow)

		events, err := client.KubeClient.CoreV1().Events(corev1.NamespaceDefault).List(
			context.TODO(), metav1.ListOptions{})
		assert.NilError(t, err)
		assert.Equal(t, 1, len(events.Items))
		assert.Equal(t, reason, events.Items[0].Reason)
		assert.Equal(t, msg1, events.Items[0].Message)
		assert.Equal(t, int32(1), events.Items[0].Count)
		assert.Equal(t, corev1.EventTypeWarning, events.Items[0].Type)
	})

	t.Run("existing event within dedupWindow updates count and message", func(t *testing.T) {
		past := metav1.NewTime(time.Now().Add(-30 * time.Minute)) // within 1h window
		existing := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeName + "-existing",
				Namespace: corev1.NamespaceDefault,
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Node",
				Name: nodeName,
				UID:  nodeUID,
			},
			Reason:         reason,
			Message:        msg1,
			Type:           corev1.EventTypeWarning,
			Count:          3,
			FirstTimestamp: past,
			LastTimestamp:  past,
		}
		client.KubeClient = fake.NewClientset(existing)

		EmitNodeWarningEvent(node, reason, msg2, dedupWindow)

		events, err := client.KubeClient.CoreV1().Events(corev1.NamespaceDefault).List(
			context.TODO(), metav1.ListOptions{})
		assert.NilError(t, err)
		// Must still be exactly one event — no new object created.
		assert.Equal(t, 1, len(events.Items))
		assert.Equal(t, int32(4), events.Items[0].Count)
		assert.Equal(t, msg2, events.Items[0].Message)
	})

	t.Run("existing event outside dedupWindow creates new event", func(t *testing.T) {
		old := metav1.NewTime(time.Now().Add(-2 * time.Hour)) // outside 1h window
		existing := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeName + "-old",
				Namespace: corev1.NamespaceDefault,
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Node",
				Name: nodeName,
				UID:  nodeUID,
			},
			Reason:         reason,
			Message:        msg1,
			Type:           corev1.EventTypeWarning,
			Count:          1,
			FirstTimestamp: old,
			LastTimestamp:  old,
		}
		client.KubeClient = fake.NewClientset(existing)

		EmitNodeWarningEvent(node, reason, msg2, dedupWindow)

		events, err := client.KubeClient.CoreV1().Events(corev1.NamespaceDefault).List(
			context.TODO(), metav1.ListOptions{})
		assert.NilError(t, err)
		// Old event still present plus one new event.
		assert.Equal(t, 2, len(events.Items))
	})
}

func TestIsSidecarContainer(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	other := corev1.ContainerRestartPolicy("Never")

	assert.Assert(t, !IsSidecarContainer(nil))
	assert.Assert(t, !IsSidecarContainer(&corev1.Container{Name: "c"}))
	assert.Assert(t, !IsSidecarContainer(&corev1.Container{Name: "c", RestartPolicy: &other}))
	assert.Assert(t, IsSidecarContainer(&corev1.Container{Name: "c", RestartPolicy: &always}))
}

func TestAllNonSidecarInitContainersSucceeded(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	sidecar := corev1.Container{Name: "sc", RestartPolicy: &always}
	initC := corev1.Container{Name: "init"} // nil restartPolicy → regular init

	term0 := corev1.ContainerStatus{Name: "init",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}
	term1 := corev1.ContainerStatus{Name: "init",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}}
	running := corev1.ContainerStatus{Name: "init",
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
	scRunning := corev1.ContainerStatus{Name: "sc",
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
	// A crash-looping sidecar momentarily shows Terminated exit 0 — the
	// exit-0 gap from the design. It must be irrelevant to the gate.
	scGap := corev1.ContainerStatus{Name: "sc",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}

	cases := []struct {
		name   string
		spec   []corev1.Container
		status []corev1.ContainerStatus
		want   bool
	}{
		{"no init containers", nil, nil, false},
		{"init running, sidecar running", []corev1.Container{initC, sidecar}, []corev1.ContainerStatus{running, scRunning}, false},
		{"init exit0, sidecar running", []corev1.Container{initC, sidecar}, []corev1.ContainerStatus{term0, scRunning}, true},
		{"init exit0, sidecar in exit-0 gap", []corev1.Container{initC, sidecar}, []corev1.ContainerStatus{term0, scGap}, true},
		{"init running, sidecar in exit-0 gap must NOT open gate", []corev1.Container{initC, sidecar}, []corev1.ContainerStatus{running, scGap}, false},
		{"init nonzero exit holds", []corev1.Container{initC, sidecar}, []corev1.ContainerStatus{term1, scRunning}, false},
		{"all sidecars: gate immediately satisfied", []corev1.Container{sidecar}, []corev1.ContainerStatus{scRunning}, true},
		{"missing status for non-sidecar init", []corev1.Container{initC, sidecar}, []corev1.ContainerStatus{scRunning}, false},
		{"plain init pod, all exit0 (legacy behavior)", []corev1.Container{initC}, []corev1.ContainerStatus{term0}, true},
		{"plain init pod, still running (legacy behavior)", []corev1.Container{initC}, []corev1.ContainerStatus{running}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{
				Spec:   corev1.PodSpec{InitContainers: tc.spec},
				Status: corev1.PodStatus{InitContainerStatuses: tc.status},
			}
			got := AllNonSidecarInitContainersSucceeded(pod)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPatchNodeAnnotations_NilNode(t *testing.T) {
	err := PatchNodeAnnotations(nil, map[string]string{"hami.io/test": "bar"})
	assert.ErrorContains(t, err, "node is nil")
}

func TestPatchNodeAnnotations_ValidNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}
	oldClient := client.KubeClient
	t.Cleanup(func() { client.KubeClient = oldClient })
	client.KubeClient = fake.NewClientset(node)

	err := PatchNodeAnnotations(node, map[string]string{"hami.io/test": "bar"})
	assert.NilError(t, err)

	updated, err := client.KubeClient.CoreV1().Nodes().Get(context.TODO(), "test-node", metav1.GetOptions{})
	assert.NilError(t, err)
	assert.Equal(t, "bar", updated.Annotations["hami.io/test"])
}

func TestRemoveNodeAnnotation_NilNode(t *testing.T) {
	err := RemoveNodeAnnotation(nil, "hami.io/test")
	assert.ErrorContains(t, err, "node is nil")
}

func TestRemoveNodeAnnotation_ValidNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Annotations: map[string]string{
				"hami.io/test": "bar",
			},
		},
	}
	oldClient := client.KubeClient
	t.Cleanup(func() { client.KubeClient = oldClient })
	client.KubeClient = fake.NewClientset(node)

	err := RemoveNodeAnnotation(node, "hami.io/test")
	assert.NilError(t, err)

	updated, err := client.KubeClient.CoreV1().Nodes().Get(context.TODO(), "test-node", metav1.GetOptions{})
	assert.NilError(t, err)
	_, hasAnno := updated.Annotations["hami.io/test"]
	assert.Assert(t, !hasAnno)
}

func TestPatchNodeAnnotations_NilClient(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
	oldClient := client.KubeClient
	t.Cleanup(func() { client.KubeClient = oldClient })
	client.KubeClient = nil
	err := PatchNodeAnnotations(node, map[string]string{"hami.io/test": "bar"})
	assert.ErrorContains(t, err, "kubernetes client is not initialized")
}

func TestGetNode(t *testing.T) {
	tests := []struct {
		name        string
		setupClient func() kubernetes.Interface
		nodeName    string
		wantErr     bool
		checkNode   func(*testing.T, *corev1.Node)
	}{
		{
			name: "empty node name",
			setupClient: func() kubernetes.Interface {
				return fake.NewClientset()
			},
			nodeName: "",
			wantErr:  true,
		},
		{
			name: "client not initialized",
			setupClient: func() kubernetes.Interface {
				return nil
			},
			nodeName: "test-node",
			wantErr:  true,
		},
		{
			name: "node not found",
			setupClient: func() kubernetes.Interface {
				return fake.NewClientset()
			},
			nodeName: "non-existent-node",
			wantErr:  true,
		},
		{
			name: "success",
			setupClient: func() kubernetes.Interface {
				clientset := fake.NewClientset()
				clientset.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				}, metav1.CreateOptions{})
				return clientset
			},
			nodeName: "test-node",
			wantErr:  false,
			checkNode: func(t *testing.T, node *corev1.Node) {
				assert.Equal(t, node.Name, "test-node")
			},
		},
		{
			name: "unauthorized",
			setupClient: func() kubernetes.Interface {
				clientset := fake.NewClientset()
				clientset.PrependReactor("get", "nodes", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
					return false, nil, apierrors.NewUnauthorized("get nodes")
				})
				return clientset
			},
			nodeName: "test-node",
			wantErr:  true,
		},
		{
			name: "generic error",
			setupClient: func() kubernetes.Interface {
				clientset := fake.NewClientset()
				clientset.PrependReactor("get", "nodes", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
					return false, nil, apierrors.NewBadRequest("generic error")
				})
				return clientset
			},
			nodeName: "test-node",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousClient := client.KubeClient
			client.KubeClient = tt.setupClient()
			t.Cleanup(func() { client.KubeClient = previousClient })

			got, err := GetNode(tt.nodeName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkNode != nil {
				tt.checkNode(t, got)
			}
		})
	}
}

func TestPatchNodeStatusCapacity(t *testing.T) {
	fakeClient := fake.NewClientset()
	client.KubeClient = fakeClient
	nodeName := "capacity-node"

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}
	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
	assert.NilError(t, err)

	resList := corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpumem"):   *resource.NewQuantity(65536, resource.DecimalSI),
		corev1.ResourceName("nvidia.com/gpucores"): *resource.NewQuantity(200, resource.DecimalSI),
	}

	err = PatchNodeStatusCapacity(node, resList)
	assert.NilError(t, err)

	updatedNode, err := fakeClient.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	assert.NilError(t, err)

	gpuMem := updatedNode.Status.Capacity["nvidia.com/gpumem"]
	assert.Equal(t, gpuMem.Value(), int64(65536))
	gpuCores := updatedNode.Status.Capacity["nvidia.com/gpucores"]
	assert.Equal(t, gpuCores.Value(), int64(200))
}
