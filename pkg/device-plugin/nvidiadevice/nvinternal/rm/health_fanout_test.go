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

const noInstance = uint32(0xFFFFFFFF)

// runCheckHealthWithEvent feeds a single XID event for parent into checkHealth
// and returns the IDs of every device it marked unhealthy.
func runCheckHealthWithEvent(t *testing.T, devices Devices, handles map[string]nvml.Device, parent string, gi, ci uint32) []string {
	t.Helper()
	t.Setenv(envDisableHealthChecks, "")
	t.Setenv(envEnableHealthChecks, "")

	stop := make(chan interface{})
	calls := 0
	eventSet := &mock.EventSet{
		FreeFunc: func() nvml.Return { return nvml.SUCCESS },
		WaitFunc: func(uint32) (nvml.EventData, nvml.Return) {
			calls++
			if calls > 1 {
				// The first event has been fully handled by now.
				close(stop)
				return nvml.EventData{}, nvml.ERROR_TIMEOUT
			}
			return nvml.EventData{
				Device:            &mock.Device{GetUUIDFunc: func() (string, nvml.Return) { return parent, nvml.SUCCESS }},
				EventType:         nvml.EventTypeXidCriticalError,
				EventData:         79,
				GpuInstanceId:     gi,
				ComputeInstanceId: ci,
			}, nvml.SUCCESS
		},
	}
	r := &nvmlResourceManager{nvml: &mock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		EventSetCreateFunc: func() (nvml.EventSet, nvml.Return) { return eventSet, nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc: func(uuid string) (nvml.Device, nvml.Return) {
			return handles[uuid], nvml.SUCCESS
		},
	}}

	unhealthy := make(chan *Device, len(devices))
	require.NoError(t, r.checkHealth(stop, devices, unhealthy, make(chan bool)))

	var got []string
	for len(unhealthy) > 0 {
		got = append(got, (<-unhealthy).ID)
	}
	return got
}

func newParentHandle(uuid string) nvml.Device {
	return &mock.Device{
		GetUUIDFunc:                func() (string, nvml.Return) { return uuid, nvml.SUCCESS },
		GetSupportedEventTypesFunc: func() (uint64, nvml.Return) { return uint64(nvml.EventTypeXidCriticalError), nvml.SUCCESS },
		RegisterEventsFunc:         func(uint64, nvml.EventSet) nvml.Return { return nvml.SUCCESS },
	}
}

func newHealthyDevice(id, index string, replicas int) *Device {
	return &Device{
		Device:   kubeletdevicepluginv1beta1.Device{ID: id, Health: kubeletdevicepluginv1beta1.Healthy},
		Index:    index,
		Replicas: replicas,
	}
}

// An XID on a shared GPU must mark every replica of that GPU unhealthy, not
// just whichever replica was last written into the parent map.
func TestCheckHealthMarksEveryReplica(t *testing.T) {
	handles := map[string]nvml.Device{
		"GPU-a": newParentHandle("GPU-a"),
		"GPU-b": newParentHandle("GPU-b"),
	}
	devices := Devices{}
	var want []string
	for i := 0; i < 4; i++ {
		id := string(NewAnnotatedID("GPU-a", i))
		devices[id] = newHealthyDevice(id, "0", 4)
		want = append(want, id)
	}
	other := string(NewAnnotatedID("GPU-b", 0))
	devices[other] = newHealthyDevice(other, "1", 4)

	got := runCheckHealthWithEvent(t, devices, handles, "GPU-a", noInstance, noInstance)
	require.ElementsMatch(t, want, got)
}

// Every MIG slice on a GPU shares the parent UUID. A GPU-wide event reaches all
// of them, while an event scoped to one GPU/compute instance reaches only that slice.
func TestCheckHealthMIGSlices(t *testing.T) {
	handles := map[string]nvml.Device{
		"GPU-a": newParentHandle("GPU-a"),
		"GPU-b": newParentHandle("GPU-b"),
	}
	devices := Devices{}
	addSlice := func(parent string, gi int) string {
		id := fmt.Sprintf("MIG-%s-%d", parent, gi)
		handles[id] = &mock.Device{
			GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) { return handles[parent], nvml.SUCCESS },
			GetGpuInstanceIdFunc:                   func() (int, nvml.Return) { return gi, nvml.SUCCESS },
			GetComputeInstanceIdFunc:               func() (int, nvml.Return) { return 0, nvml.SUCCESS },
		}
		devices[id] = newHealthyDevice(id, fmt.Sprintf("0:%d", gi), 0)
		return id
	}
	var sliceA []string
	for gi := 1; gi <= 3; gi++ {
		sliceA = append(sliceA, addSlice("GPU-a", gi))
	}
	addSlice("GPU-b", 1)

	tests := []struct {
		name   string
		parent string
		gi, ci uint32
		want   []string
	}{
		{name: "GPU-wide event reaches every slice", parent: "GPU-a", gi: noInstance, ci: noInstance, want: sliceA},
		{name: "instance event reaches only its slice", parent: "GPU-a", gi: 2, ci: 0, want: []string{sliceA[1]}},
		{name: "unknown instance reaches nothing", parent: "GPU-a", gi: 7, ci: 0},
		{name: "unknown parent is ignored", parent: "GPU-unknown", gi: noInstance, ci: noInstance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runCheckHealthWithEvent(t, devices, handles, tt.parent, tt.gi, tt.ci)
			require.ElementsMatch(t, tt.want, got)
		})
	}
}
