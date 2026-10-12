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

package rm

import (
	"fmt"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	mock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const allInstances = uint32(0xFFFFFFFF)

// migSlice describes a MIG device and the parent GPU it belongs to.
type migSlice struct {
	id, parent string
	gi, ci     int
}

// healthEventUnhealthy feeds a single XID event for eventParent to checkHealth
// and returns the IDs of the devices reported as unhealthy.
func healthEventUnhealthy(t *testing.T, devices Devices, slices []migSlice, eventParent string, gi, ci uint32) []string {
	t.Helper()
	t.Setenv(envDisableHealthChecks, "")
	t.Setenv(envEnableHealthChecks, "")

	newGPU := func(uuid string) *mock.Device {
		return &mock.Device{
			GetUUIDFunc:                func() (string, nvml.Return) { return uuid, nvml.SUCCESS },
			GetSupportedEventTypesFunc: func() (uint64, nvml.Return) { return nvml.EventTypeXidCriticalError, nvml.SUCCESS },
			RegisterEventsFunc:         func(uint64, nvml.EventSet) nvml.Return { return nvml.SUCCESS },
		}
	}
	handles := map[string]nvml.Device{}
	for _, d := range devices {
		handles[d.GetUUID()] = newGPU(d.GetUUID())
	}
	for _, s := range slices {
		handles["GPU-"+s.parent] = newGPU("GPU-" + s.parent)
		handles[s.id] = &mock.Device{
			GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) { return handles["GPU-"+s.parent], nvml.SUCCESS },
			GetGpuInstanceIdFunc:                   func() (int, nvml.Return) { return s.gi, nvml.SUCCESS },
			GetComputeInstanceIdFunc:               func() (int, nvml.Return) { return s.ci, nvml.SUCCESS },
		}
	}

	// Deliver the event on the first wait and stop the loop on the second, so
	// that every notification triggered by the event is sent before stopping.
	stop := make(chan interface{})
	waits := 0
	eventSet := &mock.EventSet{
		FreeFunc: func() nvml.Return { return nvml.SUCCESS },
		WaitFunc: func(uint32) (nvml.EventData, nvml.Return) {
			waits++
			if waits > 1 {
				close(stop)
				return nvml.EventData{}, nvml.ERROR_TIMEOUT
			}
			return nvml.EventData{
				Device:        newGPU(eventParent),
				EventType:     nvml.EventTypeXidCriticalError,
				EventData:     79,
				GpuInstanceId: gi, ComputeInstanceId: ci,
			}, nvml.SUCCESS
		},
	}
	r := &nvmlResourceManager{nvml: &mock.Interface{
		InitFunc:                  func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:              func() nvml.Return { return nvml.SUCCESS },
		EventSetCreateFunc:        func() (nvml.EventSet, nvml.Return) { return eventSet, nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc: func(id string) (nvml.Device, nvml.Return) { return handles[id], nvml.SUCCESS },
	}}

	unhealthy := make(chan *Device, len(devices))
	require.NoError(t, r.checkHealth(stop, devices, unhealthy, make(chan bool)))
	var got []string
	for len(unhealthy) > 0 {
		got = append(got, (<-unhealthy).ID)
	}
	return got
}

func healthyDevice(id, index string) *Device {
	return &Device{Device: kubeletdevicepluginv1beta1.Device{ID: id, Health: kubeletdevicepluginv1beta1.Healthy}, Index: index}
}

// A GPU-wide event must reach every time-sliced replica of that GPU.
func TestCheckHealthEventReachesAllReplicas(t *testing.T) {
	devices := Devices{}
	var want []string
	for replica := 0; replica < 4; replica++ {
		id := string(NewAnnotatedID("GPU-a", replica))
		devices[id] = healthyDevice(id, "0")
		want = append(want, id)
	}
	other := healthyDevice("GPU-b", "1")
	devices[other.ID] = other

	got := healthEventUnhealthy(t, devices, nil, "GPU-a", allInstances, allInstances)
	require.ElementsMatch(t, want, got)
}

func TestCheckHealthEventMIGSlices(t *testing.T) {
	slices := []migSlice{
		{id: "MIG-a-1", parent: "a", gi: 1, ci: 0},
		{id: "MIG-a-2", parent: "a", gi: 2, ci: 0},
		{id: "MIG-a-3", parent: "a", gi: 3, ci: 0},
		{id: "MIG-b-1", parent: "b", gi: 1, ci: 0},
	}
	newDevices := func() Devices {
		devices := Devices{}
		for i, s := range slices {
			devices[s.id] = healthyDevice(s.id, fmt.Sprintf("0:%d", i))
		}
		return devices
	}

	tests := []struct {
		name   string
		parent string
		gi, ci uint32
		want   []string
	}{
		{name: "GPU-wide event reaches every slice of the parent", parent: "GPU-a", gi: allInstances, ci: allInstances,
			want: []string{"MIG-a-1", "MIG-a-2", "MIG-a-3"}},
		{name: "instance-scoped event reaches only that slice", parent: "GPU-a", gi: 2, ci: 0,
			want: []string{"MIG-a-2"}},
		{name: "instance-scoped event matching no slice is ignored", parent: "GPU-a", gi: 7, ci: 0},
		{name: "event for an unknown GPU is ignored", parent: "GPU-unknown", gi: allInstances, ci: allInstances},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := healthEventUnhealthy(t, newDevices(), slices, tt.parent, tt.gi, tt.ci)
			require.ElementsMatch(t, tt.want, got)
		})
	}
}
