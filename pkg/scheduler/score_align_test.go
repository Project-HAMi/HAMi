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

package scheduler

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// Test_calcScore_MultiTypeContainerAlignment guards against container-index
// misalignment of per-type device allocations for pods whose containers request
// different device types (or no device at all). score.Devices must have exactly
// one entry per container, indexed by container order, for every type that is
// discovered, otherwise PatchAnnotations hands a device to the wrong container.
func Test_calcScore_MultiTypeContainerAlignment(t *testing.T) {
	oldDevicesMap := device.DevicesMap
	defer func() { device.DevicesMap = oldDevicesMap }()
	device.DevicesMap = map[string]device.Devices{
		"mockA": &fitMockDevice{typeName: "mockA", uuid: "uuid-a"},
		"mockB": &fitMockDevice{typeName: "mockB", uuid: "uuid-b"},
	}

	node := func() *map[string]*NodeUsage {
		return &map[string]*NodeUsage{
			"node1": {
				Node:     &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				NodeInfo: &device.NodeInfo{},
				Devices: policy.DeviceUsageList{
					Policy: util.GPUSchedulerPolicySpread.String(),
					DeviceLists: []*policy.DeviceListsScore{
						{
							Device: &device.DeviceUsage{
								ID: "uuid-a", Index: 0, Type: "mockA",
								Count: 4, Used: 0, Totalcore: 100, Usedcores: 0,
								Totalmem: 8192, Usedmem: 0, Health: true,
							},
						},
						{
							Device: &device.DeviceUsage{
								ID: "uuid-b", Index: 0, Type: "mockB",
								Count: 4, Used: 0, Totalcore: 100, Usedcores: 0,
								Totalmem: 8192, Usedmem: 0, Health: true,
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name         string
		resourceReqs device.PodDeviceRequests
		wantA        device.PodSingleDevice
		wantB        device.PodSingleDevice
	}{
		{
			name: "container0 requests mockA and container1 requests mockB",
			resourceReqs: device.PodDeviceRequests{
				{"mockA": {Nums: 1, Type: "mockA", Memreq: 1024, MemPercentagereq: 101, Coresreq: 1}},
				{"mockB": {Nums: 1, Type: "mockB", Memreq: 1024, MemPercentagereq: 101, Coresreq: 1}},
			},
			wantA: device.PodSingleDevice{
				{{Idx: 0, UUID: "uuid-a", Type: "mockA"}},
				{},
			},
			wantB: device.PodSingleDevice{
				{},
				{{Idx: 0, UUID: "uuid-b", Type: "mockB"}},
			},
		},
		{
			name: "container1 requests mockB after a no-device container0",
			resourceReqs: device.PodDeviceRequests{
				{},
				{"mockB": {Nums: 1, Type: "mockB", Memreq: 1024, MemPercentagereq: 101, Coresreq: 1}},
			},
			wantA: nil,
			wantB: device.PodSingleDevice{
				{},
				{{Idx: 0, UUID: "uuid-b", Type: "mockB"}},
			},
		},
		{
			name: "container0 requests mockA and container2 requests mockB with a no-device container1 in between",
			resourceReqs: device.PodDeviceRequests{
				{"mockA": {Nums: 1, Type: "mockA", Memreq: 1024, MemPercentagereq: 101, Coresreq: 1}},
				{},
				{"mockB": {Nums: 1, Type: "mockB", Memreq: 1024, MemPercentagereq: 101, Coresreq: 1}},
			},
			wantA: device.PodSingleDevice{
				{{Idx: 0, UUID: "uuid-a", Type: "mockA"}},
				{},
				{},
			},
			wantB: device.PodSingleDevice{
				{},
				{},
				{{Idx: 0, UUID: "uuid-b", Type: "mockB"}},
			},
		},
	}

	s := NewScheduler()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "c0"}, {Name: "c1"}, {Name: "c2"}},
				},
			}
			got, err := s.calcScore(node(), test.resourceReqs, task, map[string]string{})
			assert.NilError(t, err)
			assert.Assert(t, got != nil)
			assert.Equal(t, len(got.NodeList), 1)

			devs := got.NodeList[0].Devices
			assert.DeepEqual(t, devs["mockA"], test.wantA)
			assert.DeepEqual(t, devs["mockB"], test.wantB)
		})
	}
}
