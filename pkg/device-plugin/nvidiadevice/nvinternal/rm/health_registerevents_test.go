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
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	mock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// A device that cannot register events is not faulty, so it must not be marked unhealthy.
func TestCheckHealthRegisterEvents(t *testing.T) {
	registerRet := map[string]nvml.Return{
		"GPU-unsupported": nvml.ERROR_NOT_SUPPORTED,
		"GPU-failing":     nvml.ERROR_UNKNOWN,
		"GPU-ok":          nvml.SUCCESS,
	}

	// checkHealth only waits for events once every device has been registered.
	var once sync.Once
	registered := make(chan struct{})
	eventSet := &mock.EventSet{
		FreeFunc: func() nvml.Return { return nvml.SUCCESS },
		WaitFunc: func(uint32) (nvml.EventData, nvml.Return) {
			once.Do(func() { close(registered) })
			time.Sleep(time.Millisecond)
			return nvml.EventData{}, nvml.ERROR_TIMEOUT
		},
	}
	r := &nvmlResourceManager{nvml: &mock.Interface{
		InitFunc:           func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		EventSetCreateFunc: func() (nvml.EventSet, nvml.Return) { return eventSet, nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc: func(uuid string) (nvml.Device, nvml.Return) {
			return &mock.Device{
				GetSupportedEventTypesFunc: func() (uint64, nvml.Return) {
					return uint64(nvml.EventTypeXidCriticalError), nvml.SUCCESS
				},
				RegisterEventsFunc: func(uint64, nvml.EventSet) nvml.Return { return registerRet[uuid] },
			}, nvml.SUCCESS
		},
	}}

	devices := Devices{}
	for uuid := range registerRet {
		devices[uuid] = &Device{Device: kubeletdevicepluginv1beta1.Device{
			ID: uuid, Health: kubeletdevicepluginv1beta1.Healthy}}
	}

	stop := make(chan interface{})
	unhealthy := make(chan *Device, len(devices))
	done := make(chan error, 1)
	go func() { done <- r.checkHealth(stop, devices, unhealthy, make(chan bool)) }()

	select {
	case <-registered:
	case <-time.After(10 * time.Second):
		t.Fatal("checkHealth did not finish registering devices")
	}
	close(stop)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("checkHealth did not return")
	}

	marked := map[string]bool{}
	for len(unhealthy) > 0 {
		marked[(<-unhealthy).ID] = true
	}
	require.False(t, marked["GPU-unsupported"], "ERROR_NOT_SUPPORTED means the device cannot be health checked, not that it is unhealthy")
	require.True(t, marked["GPU-failing"], "any other registration error must still mark the device unhealthy")
	require.False(t, marked["GPU-ok"], "a device that registered fine must stay healthy")
}
