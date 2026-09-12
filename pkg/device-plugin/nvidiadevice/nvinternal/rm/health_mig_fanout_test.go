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
	"sync"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	mock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// IsMigDevice reads only the immutable index. A value receiver would copy
// Health while the device-plugin notification loop updates it concurrently.
func TestIsMigDeviceConcurrentHealthUpdate(t *testing.T) {
	d := &Device{Index: "0:1"}
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; i < 1000; i++ {
			d.Health = kubeletdevicepluginv1beta1.Unhealthy
		}
	}()
	for i := 0; i < 1000; i++ {
		require.True(t, d.IsMigDevice())
	}
	writer.Wait()
}

func TestCheckHealthMIGFanout(t *testing.T) {
	const allInstances = uint32(0xFFFFFFFF)
	tests := []struct {
		name   string
		parent string
		gi, ci uint32
		xid    uint64
		want   []string
	}{
		{name: "parent failure reaches every slice", parent: "GPU-a", gi: allInstances, ci: allInstances, xid: 79,
			want: []string{"MIG-a-1", "MIG-a-2", "MIG-a-3", "MIG-a-4", "MIG-a-5", "MIG-a-6", "MIG-a-7"}},
		{name: "other parent is isolated", parent: "GPU-b", gi: allInstances, ci: allInstances, xid: 79, want: []string{"MIG-b-1"}},
		{name: "unknown parent is ignored", parent: "GPU-unknown", gi: allInstances, ci: allInstances, xid: 79},
		{name: "different compute instance is ignored", parent: "GPU-a", gi: 1, ci: 7, xid: 79},
		{name: "disabled xid is ignored", parent: "GPU-a", gi: allInstances, ci: allInstances, xid: 13},
	}
	for gi := uint32(1); gi <= 7; gi++ {
		tests = append(tests, struct {
			name   string
			parent string
			gi, ci uint32
			xid    uint64
			want   []string
		}{name: fmt.Sprintf("instance %d is selected", gi), parent: "GPU-a", gi: gi, ci: 0, xid: 79,
			want: []string{fmt.Sprintf("MIG-a-%d", gi)}})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envDisableHealthChecks, "")
			t.Setenv(envEnableHealthChecks, "")
			stop := make(chan interface{})
			eventSet := &mock.EventSet{
				FreeFunc: func() nvml.Return { return nvml.SUCCESS },
				WaitFunc: func(uint32) (nvml.EventData, nvml.Return) {
					close(stop)
					return nvml.EventData{
						Device:    &mock.Device{GetUUIDFunc: func() (string, nvml.Return) { return tt.parent, nvml.SUCCESS }},
						EventType: nvml.EventTypeXidCriticalError, EventData: tt.xid,
						GpuInstanceId: tt.gi, ComputeInstanceId: tt.ci,
					}, nvml.SUCCESS
				},
			}
			handles := map[string]nvml.Device{}
			for _, parent := range []string{"GPU-a", "GPU-b"} {
				handles[parent] = &mock.Device{
					GetUUIDFunc:                func() (string, nvml.Return) { return parent, nvml.SUCCESS },
					GetSupportedEventTypesFunc: func() (uint64, nvml.Return) { return nvml.EventTypeXidCriticalError, nvml.SUCCESS },
					RegisterEventsFunc:         func(uint64, nvml.EventSet) nvml.Return { return nvml.SUCCESS },
				}
			}
			devices := Devices{}
			for gi := 1; gi <= 8; gi++ {
				parent, instance := "GPU-a", gi
				if gi == 8 {
					parent, instance = "GPU-b", 1
				}
				id := fmt.Sprintf("MIG-%s-%d", parent[4:], instance)
				handles[id] = &mock.Device{
					GetDeviceHandleFromMigDeviceHandleFunc: func() (nvml.Device, nvml.Return) { return handles[parent], nvml.SUCCESS },
					GetGpuInstanceIdFunc:                   func() (int, nvml.Return) { return instance, nvml.SUCCESS },
					GetComputeInstanceIdFunc:               func() (int, nvml.Return) { return 0, nvml.SUCCESS },
				}
				devices[id] = &Device{Device: kubeletdevicepluginv1beta1.Device{ID: id, Health: kubeletdevicepluginv1beta1.Healthy}, Index: fmt.Sprintf("0:%d", instance)}
			}
			r := &nvmlResourceManager{nvml: &mock.Interface{
				InitFunc:           func() nvml.Return { return nvml.SUCCESS },
				ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
				EventSetCreateFunc: func() (nvml.EventSet, nvml.Return) { return eventSet, nvml.SUCCESS },
				DeviceGetHandleByUUIDFunc: func(id string) (nvml.Device, nvml.Return) {
					return handles[id], nvml.SUCCESS
				},
			}}
			unhealthy := make(chan *Device, len(devices))
			require.NoError(t, r.checkHealth(stop, devices, unhealthy, make(chan bool)))
			var got []string
			for len(unhealthy) > 0 {
				got = append(got, (<-unhealthy).ID)
			}
			require.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestCheckHealthReplicaFanout(t *testing.T) {
	t.Setenv(envDisableHealthChecks, "")
	t.Setenv(envEnableHealthChecks, "")
	stop := make(chan interface{})
	parent := &mock.Device{
		GetUUIDFunc:                func() (string, nvml.Return) { return "GPU-a", nvml.SUCCESS },
		GetSupportedEventTypesFunc: func() (uint64, nvml.Return) { return nvml.EventTypeXidCriticalError, nvml.SUCCESS },
		RegisterEventsFunc:         func(uint64, nvml.EventSet) nvml.Return { return nvml.SUCCESS },
	}
	eventSet := &mock.EventSet{
		FreeFunc: func() nvml.Return { return nvml.SUCCESS },
		WaitFunc: func(uint32) (nvml.EventData, nvml.Return) {
			close(stop)
			return nvml.EventData{Device: parent, EventType: nvml.EventTypeXidCriticalError, EventData: 79,
				GpuInstanceId: 0xFFFFFFFF, ComputeInstanceId: 0xFFFFFFFF}, nvml.SUCCESS
		},
	}
	r := &nvmlResourceManager{nvml: &mock.Interface{
		InitFunc:                  func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:              func() nvml.Return { return nvml.SUCCESS },
		EventSetCreateFunc:        func() (nvml.EventSet, nvml.Return) { return eventSet, nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) { return parent, nvml.SUCCESS },
	}}
	devices := Devices{}
	var want []string
	for replica := 0; replica < 4; replica++ {
		id := string(NewAnnotatedID("GPU-a", replica))
		devices[id] = &Device{Device: kubeletdevicepluginv1beta1.Device{ID: id, Health: kubeletdevicepluginv1beta1.Healthy}, Index: "0", Replicas: 4}
		want = append(want, id)
	}
	unhealthy := make(chan *Device, len(devices))
	require.NoError(t, r.checkHealth(stop, devices, unhealthy, make(chan bool)))
	var got []string
	for len(unhealthy) > 0 {
		got = append(got, (<-unhealthy).ID)
	}
	require.ElementsMatch(t, want, got)
}
