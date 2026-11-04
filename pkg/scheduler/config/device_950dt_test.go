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

package config

import (
	"flag"
	"maps"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// TestDefaultAscend950DTWholeCardAllocation exercises the embedded default
// configuration all the way through request parsing and the scheduler's Fit
// decision. The 950DT entry intentionally has no vNPU templates: it represents
// one whole card, which must stop being schedulable while occupied.
func TestDefaultAscend950DTWholeCardAllocation(t *testing.T) {
	oldDevicesMap := device.DevicesMap
	oldDevicesToHandle := device.DevicesToHandle
	oldInRequestDevices := maps.Clone(device.InRequestDevices)
	oldSupportDevices := maps.Clone(device.SupportDevices)
	oldHandshakeAnnos := maps.Clone(util.HandshakeAnnos)
	t.Cleanup(func() {
		device.DevicesMap = oldDevicesMap
		device.DevicesToHandle = oldDevicesToHandle
		device.InRequestDevices = oldInRequestDevices
		device.SupportDevices = oldSupportDevices
		util.HandshakeAnnos = oldHandshakeAnnos
	})

	flags := flag.NewFlagSet("ascend950dt-test", flag.ContinueOnError)
	ascend.ParseConfig(flags)
	assert.NilError(t, flags.Set("enable-ascend", "true"))
	t.Cleanup(func() {
		assert.NilError(t, flags.Set("enable-ascend", "false"))
	})

	// This parses and initializes the actual built-in defaults in config.go,
	// including the Ascend950DT entry under test.
	InitDefaultDevices()
	dev, ok := device.DevicesMap["Ascend950DT"]
	assert.Assert(t, ok, "default configuration did not register Ascend950DT")
	assert.Equal(t, dev.CommonWord(), "Ascend950DT")
	assert.DeepEqual(t, dev.GetResourceNames(), device.ResourceNames{
		ResourceCountName: "huawei.com/Ascend950DT",
	})

	ctr := &corev1.Container{
		Name: "whole-card",
		Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
			corev1.ResourceName("huawei.com/Ascend950DT"): resource.MustParse("1"),
		}},
	}
	request, err := dev.GenerateResourceRequests(ctr)
	assert.NilError(t, err)
	assert.DeepEqual(t, request, device.ContainerDeviceRequest{
		Nums:             1,
		Type:             "Ascend950DT",
		MemPercentagereq: 100,
	})

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "950dt-node",
		Annotations: map[string]string{
			"hami.io/node-register-Ascend950DT": `[{"ID":"950dt-0","Count":1,"Devmem":98304,"Devcore":32,"Type":"Ascend950DT","Health":true}]`,
		},
	}}
	registered, err := dev.GetNodeDevices(*node)
	assert.NilError(t, err)
	assert.Equal(t, len(registered), 1)
	assert.Equal(t, registered[0].Count, int64(1))
	assert.Equal(t, registered[0].Devmem, int64(98304))
	assert.Equal(t, registered[0].Devcore, int64(32))

	usageFromRegistration := func() *device.DeviceUsage {
		return &device.DeviceUsage{
			ID:        registered[0].ID,
			Index:     registered[0].Index,
			Count:     registered[0].Count,
			Totalmem:  registered[0].Devmem,
			Totalcore: registered[0].Devcore,
			Type:      dev.CommonWord(),
			Health:    registered[0].Health,
		}
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "first-tenant"}}
	nodeInfo := &device.NodeInfo{Node: node}
	current := usageFromRegistration()
	fit, allocations, reason := dev.Fit([]*device.DeviceUsage{current}, request, pod, nodeInfo, &device.PodDevices{})
	assert.Assert(t, fit, "a request for one card should fit a free Ascend950DT: %s", reason)
	assert.Equal(t, len(allocations["Ascend950DT"]), 1)
	assert.Equal(t, allocations["Ascend950DT"][0].UUID, "950dt-0")

	// Scheduler usage reconstruction accounts the allocated card as occupied;
	// with Count=1, a second pod must not reuse it.
	assert.NilError(t, dev.AddResourceUsage(pod, current, &allocations["Ascend950DT"][0]))
	secondPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "second-tenant"}}
	fit, _, reason = dev.Fit([]*device.DeviceUsage{current}, request, secondPod, nodeInfo, &device.PodDevices{})
	assert.Assert(t, !fit, "an occupied whole card must not be reused")
	assert.Assert(t, reason != "", "rejection should explain that the card is occupied")

	// Once the scheduler rebuilds usage after the first pod has released its
	// allocation, the same registered card is schedulable again.
	fit, allocations, reason = dev.Fit([]*device.DeviceUsage{usageFromRegistration()}, request, secondPod, nodeInfo, &device.PodDevices{})
	assert.Assert(t, fit, "the card should be available after its prior occupancy is released: %s", reason)
	assert.Equal(t, len(allocations["Ascend950DT"]), 1)
}
