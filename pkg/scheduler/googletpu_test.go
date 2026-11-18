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

package scheduler

import (
	"maps"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/record"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/googletpu"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func tpuPod(name string, chipsPerContainer ...int64) *corev1.Pod {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name, UID: types.UID(name + "-uid")}}
	for _, chips := range chipsPerContainer {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"google.com/tpu": *resource.NewQuantity(chips, resource.DecimalSI)},
			},
		})
	}
	return pod
}

// A TPU node is read from its capacity and labels, with no register
// annotation, and is given to one container as a whole.
func TestFilter_GoogleTPUWholeNode(t *testing.T) {
	oldDevices, oldClient, oldSupport := device.DevicesMap, client.KubeClient, maps.Clone(device.SupportDevices)
	t.Cleanup(func() {
		device.DevicesMap, client.KubeClient, device.SupportDevices = oldDevices, oldClient, oldSupport
	})
	device.DevicesMap = map[string]device.Devices{
		googletpu.GoogleTPUDevice: googletpu.InitGoogleTPUDevice(googletpu.GoogleTPUConfig{ResourceCountName: "google.com/tpu"}),
	}

	partial := tpuPod("partial", 2)
	twoContainers := tpuPod("two-containers", 4, 4)
	first := tpuPod("first", 4)
	second := tpuPod("second", 4)
	s := schedulerWithPods(t, partial, twoContainers, first, second)
	s.eventRecorder = record.NewFakeRecorder(100)
	client.KubeClient = s.kubeClient

	chips := corev1.ResourceList{"google.com/tpu": resource.MustParse("4")}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "tpu-node",
			Labels: map[string]string{googletpu.AcceleratorLabel: "tpu-v5-lite-podslice"},
		},
		Status: corev1.NodeStatus{Capacity: chips, Allocatable: chips},
	}
	t.Cleanup(func() { device.ForgetRegisteredDevices(googletpu.GoogleTPUDevice, node.Name) })
	nodeInformers := informers.NewSharedInformerFactoryWithOptions(s.kubeClient, time.Hour)
	assert.NilError(t, nodeInformers.Core().V1().Nodes().Informer().GetIndexer().Add(node))
	s.nodeLister = nodeInformers.Core().V1().Nodes().Lister()

	s.register(labels.Everything())
	registered, err := s.GetNode(node.Name)
	assert.NilError(t, err, "an unregistered node makes Filter answer node unregistered")
	assert.Equal(t, len(registered.Devices[googletpu.GoogleTPUCommonWord]), 4)

	nodeNames := []string{node.Name}
	filter := func(pod *corev1.Pod) *extenderv1.ExtenderFilterResult {
		t.Helper()
		result, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &nodeNames})
		assert.NilError(t, err)
		assert.Equal(t, result.Error, "")
		return result
	}
	assertRejected := func(pod *corev1.Pod, reason string, count int) {
		t.Helper()
		result := filter(pod)
		assert.Assert(t, result.NodeNames == nil || len(*result.NodeNames) == 0, "pod %s was given %v", pod.Name, result.NodeNames)
		assert.Equal(t, common.ParseReason(result.FailedNodes[node.Name])[reason], count, "result %+v", result)
	}

	assertRejected(partial, common.NodeRequiresAllDevices, 2)
	assertRejected(twoContainers, common.CardTimeSlicingExhausted, 4)

	result := filter(first)
	assert.DeepEqual(t, result.NodeNames, &nodeNames)
	stored, err := s.kubeClient.CoreV1().Pods(first.Namespace).Get(t.Context(), first.Name, metav1.GetOptions{})
	assert.NilError(t, err)
	assert.Equal(t, stored.Annotations[util.AssignedNodeAnnotations], node.Name)
	allocated, err := device.DecodePodDevices(device.SupportDevices, stored.Annotations)
	assert.NilError(t, err)
	assert.Equal(t, len(allocated[googletpu.GoogleTPUDevice][0]), 4)

	assertRejected(second, common.CardTimeSlicingExhausted, 4)
}
