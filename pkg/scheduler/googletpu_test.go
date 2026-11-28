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
	"encoding/json"
	"fmt"
	"maps"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/record"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/googletpu"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func tpuPod(name string, chipsPerContainer ...int64) *corev1.Pod {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name, UID: types.UID(name + "-uid")}}
	for i, chips := range chipsPerContainer {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name: fmt.Sprintf("app-%d", i),
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"google.com/tpu": *resource.NewQuantity(chips, resource.DecimalSI)},
			},
		})
	}
	return pod
}

// A TPU node is read from its capacity and labels, with no register
// annotation, and its chips go to one pod as a whole.
func TestFilter_GoogleTPUWholeNode(t *testing.T) {
	oldDevices, oldClient, oldSupport := device.DevicesMap, client.KubeClient, maps.Clone(device.SupportDevices)
	t.Cleanup(func() {
		device.DevicesMap, client.KubeClient, device.SupportDevices = oldDevices, oldClient, oldSupport
	})
	device.DevicesMap = map[string]device.Devices{
		googletpu.GoogleTPUDevice: googletpu.InitGoogleTPUDevice(googletpu.GoogleTPUConfig{ResourceCountName: "google.com/tpu"}),
	}

	for _, test := range []struct {
		name string
		// chips is what each container of the pod that takes the node requests.
		chips       []int64
		accelerator string
	}{
		{name: "one container takes the node", chips: []int64{4}, accelerator: "tpu-v6e-slice"},
		{name: "two Ironwood containers divide the node", chips: []int64{2, 2}, accelerator: "tpu7x"},
	} {
		t.Run(test.name, func(t *testing.T) {
			partial := tpuPod("partial", 2)
			tooMany := tpuPod("too-many", 4, 4)
			taker := tpuPod("taker", test.chips...)
			second := tpuPod("second", 4)
			split := tpuPod("split", 2, 2)
			s := schedulerWithPods(t, partial, tooMany, split, taker, second)
			s.eventRecorder = record.NewFakeRecorder(100)
			client.KubeClient = s.kubeClient

			chips := corev1.ResourceList{"google.com/tpu": resource.MustParse("4")}
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "tpu-node",
					Labels: map[string]string{googletpu.AcceleratorLabel: test.accelerator},
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
			// Outside Ironwood the first container takes the node and the second
			// finds it taken.
			if test.accelerator == "tpu7x" {
				assertRejected(tooMany, common.NodeRequiresAllDevices, 8)
			} else {
				assertRejected(tooMany, common.CardTimeSlicingExhausted, 4)
			}

			if test.accelerator != "tpu7x" {
				assertRejected(split, common.NodeRequiresAllDevices, 2)
			}

			result := filter(taker)
			assert.DeepEqual(t, result.NodeNames, &nodeNames)
			stored, err := s.kubeClient.CoreV1().Pods(taker.Namespace).Get(t.Context(), taker.Name, metav1.GetOptions{})
			assert.NilError(t, err)
			assert.Equal(t, stored.Annotations[util.AssignedNodeAnnotations], node.Name)
			allocated, err := device.DecodePodDevices(device.SupportDevices, stored.Annotations)
			assert.NilError(t, err)
			held := map[string]bool{}
			for i, want := range test.chips {
				row := allocated[googletpu.GoogleTPUDevice][i]
				assert.Equal(t, int64(len(row)), want, "container %d", i)
				for _, chip := range row {
					held[chip.UUID] = true
				}
			}
			assert.Equal(t, len(held), 4, "the containers must hold different chips: %v", allocated)

			assertRejected(second, common.CardTimeSlicingExhausted, 4)
		})
	}
}

// HAMi enforces nothing inside a TPU container, so a privileged TPU pod is
// admitted. Adding a device that HAMi does isolate brings the denial back.
func TestWebhook_PrivilegedGoogleTPU(t *testing.T) {
	oldDevices, oldSupport, oldName := device.DevicesMap, maps.Clone(device.SupportDevices), config.SchedulerName
	t.Cleanup(func() {
		device.DevicesMap, device.SupportDevices, config.SchedulerName = oldDevices, oldSupport, oldName
	})
	config.SchedulerName = "hami-scheduler"
	device.DevicesMap = map[string]device.Devices{
		googletpu.GoogleTPUDevice: googletpu.InitGoogleTPUDevice(googletpu.GoogleTPUConfig{ResourceCountName: "google.com/tpu"}),
		nvidia.NvidiaGPUDevice:    nvidia.InitNvidiaDevice(nvidia.NvidiaConfig{ResourceCountName: "nvidia.com/gpu"}),
	}
	wh, err := NewWebHook()
	assert.NilError(t, err)

	for _, test := range []struct {
		name    string
		extra   corev1.ResourceName
		allowed bool
	}{
		{name: "TPU only", allowed: true},
		{name: "TPU and GPU", extra: "nvidia.com/gpu"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pod := tpuPod("privileged", 4)
			privileged := true
			pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: &privileged}
			if test.extra != "" {
				pod.Spec.Containers[0].Resources.Limits[test.extra] = resource.MustParse("1")
			}
			raw, err := json.Marshal(pod)
			assert.NilError(t, err)
			response := wh.Handle(t.Context(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				UID: "uid", Namespace: pod.Namespace, Name: pod.Name, Operation: admissionv1.Create,
				Object: runtime.RawExtension{Raw: raw},
			}})
			assert.Equal(t, response.Allowed, test.allowed, "response: %+v", response.Result)
		})
	}
}
