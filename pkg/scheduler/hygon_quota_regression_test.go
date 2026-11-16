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
	"fmt"
	"maps"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func TestFilter_HygonResourceQuotaWholeCardRequest(t *testing.T) {
	oldDevices, oldClient := device.DevicesMap, client.KubeClient
	oldCount, oldMemory, oldCore, oldFactor := hygon.HygonResourceCount, hygon.HygonResourceMemory, hygon.HygonResourceCores, hygon.MemoryFactor
	oldRequests, oldSupport, oldHandshake := maps.Clone(device.InRequestDevices), maps.Clone(device.SupportDevices), maps.Clone(util.HandshakeAnnos)
	t.Cleanup(func() {
		device.DevicesMap, client.KubeClient = oldDevices, oldClient
		hygon.HygonResourceCount, hygon.HygonResourceMemory, hygon.HygonResourceCores, hygon.MemoryFactor = oldCount, oldMemory, oldCore, oldFactor
		device.InRequestDevices, device.SupportDevices, util.HandshakeAnnos = oldRequests, oldSupport, oldHandshake
	})
	hcu := hygon.InitHCUDevice(hygon.HygonConfig{
		ResourceCountName: "hygon.com/hcunum", ResourceMemoryName: "hygon.com/hcumem",
		ResourceCoreName: "hygon.com/hcucores", MemoryFactor: 1,
	})
	device.DevicesMap = map[string]device.Devices{hygon.HygonHCUDevice: hcu}
	for _, remaining := range []int64{0, 8191, 8192} {
		t.Run(fmt.Sprintf("%d_MiB", remaining), func(t *testing.T) {
			ns := fmt.Sprintf("hygon-filter-quota-%d", remaining)
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "whole-card", UID: "whole-card-uid"},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{"hygon.com/hcunum": resource.MustParse("1")},
				}}}},
			}
			s := schedulerWithPods(t, pod)
			s.eventRecorder = record.NewFakeRecorder(100)
			client.KubeClient = s.kubeClient
			quota := &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "hcu-memory"},
				Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
					"limits.hygon.com/hcumem": resource.MustParse("8192"),
				}},
			}
			s.quotaManager.AddQuota(quota)
			t.Cleanup(func() { s.quotaManager.DelQuota(quota) })
			existingPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "existing", UID: "existing-uid"}}
			existing := device.PodDevices{hygon.HygonHCUDevice: {{{UUID: "other-hcu", Type: hygon.HygonHCUDevice, Usedmem: 8192 - remaining}}}}
			assert.Assert(t, s.podManager.AddPod(existingPod, "other-node", existing))
			s.quotaManager.AddUsage(existingPod, existing)
			t.Cleanup(func() {
				if pi, ok := s.podManager.TakeAndDeletePod(pod); ok {
					s.quotaManager.RmUsage(pod, pi.Devices)
				}
				s.quotaManager.RmUsage(existingPod, existing)
			})
			// Admission sees no resolved memory and allows all three requests.
			assert.NilError(t, fitResourceQuota(pod))
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "hcu-node", Annotations: map[string]string{
				hygon.RegisterAnnos: device.EncodeNodeDevices([]*device.DeviceInfo{{
					ID: "hcu-0", Index: 0, Type: hygon.HygonHCUDevice, Count: 10,
					Devmem: 8192, Devcore: 100, Health: true,
				}}),
			}}}
			registered, err := hcu.GetNodeDevices(*node)
			assert.NilError(t, err)
			assert.Equal(t, len(registered), 1)
			assert.Equal(t, registered[0].Devmem, int64(8192))
			s.addNode(node.Name, &device.NodeInfo{ID: node.Name, Node: node,
				Devices: map[string][]device.DeviceInfo{hygon.HygonHCUDevice: {*registered[0]}},
			})
			nodeNames := []string{node.Name}
			expected := device.PodSingleDevice{{{UUID: "hcu-0", Type: hygon.HygonHCUDevice, Usedmem: 8192}}}
			// Repeat Filter to verify rejection leaves no reservation and success is charged once.
			for attempt := range 2 {
				result, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &nodeNames})
				assert.NilError(t, err)
				assert.Equal(t, result.Error, "")
				stored, err := s.kubeClient.CoreV1().Pods(ns).Get(t.Context(), pod.Name, metav1.GetOptions{})
				assert.NilError(t, err)
				pi, cached := s.podManager.GetPod(pod)
				used := (*s.quotaManager.GetResourceQuota()[ns])[hygon.HygonResourceMemory].Used
				if remaining < 8192 {
					assert.Assert(t, result.NodeNames == nil || len(*result.NodeNames) == 0)
					assert.Assert(t, common.ParseReason(result.FailedNodes[node.Name])[common.ResourceQuotaNotFit] > 0,
						"attempt %d: result %+v", attempt, result)
					assert.Assert(t, !cached, "rejection left cached reservation: %+v", pi)
					assert.Equal(t, stored.Annotations[device.SupportDevices[hygon.HygonHCUDevice]], "")
					assert.Equal(t, stored.Annotations[device.InRequestDevices[hygon.HygonHCUDevice]], "")
					assert.Equal(t, stored.Annotations[util.AssignedNodeAnnotations], "")
					assert.Equal(t, used, 8192-remaining)
				} else {
					assert.DeepEqual(t, result.NodeNames, &nodeNames)
					assert.Equal(t, len(result.FailedNodes), 0)
					assert.Assert(t, cached)
					assert.Equal(t, pi.NodeID, node.Name)
					assert.Equal(t, stored.Annotations[device.SupportDevices[hygon.HygonHCUDevice]], device.EncodePodSingleDevice(expected))
					assert.Equal(t, stored.Annotations[device.InRequestDevices[hygon.HygonHCUDevice]], device.EncodePodSingleDevice(expected))
					assert.Equal(t, stored.Annotations[util.AssignedNodeAnnotations], node.Name)
					assert.Equal(t, pi.Devices[hygon.HygonHCUDevice][0][0].Usedmem, int64(8192))
					assert.Equal(t, used, int64(8192))
				}
				previous, ok := s.podManager.GetPod(existingPod)
				assert.Assert(t, ok)
				assert.DeepEqual(t, previous.Devices, existing)
				usage, _, failed, err := s.getNodesUsage(&nodeNames, pod)
				assert.NilError(t, err)
				assert.Equal(t, len(failed), 0)
				card := (*usage)[node.Name].Devices.DeviceLists[0].Device
				if remaining < 8192 {
					assert.Equal(t, card.Used, int64(0))
					assert.Equal(t, card.Usedmem, int64(0))
				} else {
					assert.Equal(t, card.Used, int64(1))
					assert.Equal(t, card.Usedmem, int64(8192))
				}
			}
		})
	}
}
