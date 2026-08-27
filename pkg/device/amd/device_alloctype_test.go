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

package amd

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// The registered device type is taken verbatim from the external
// hami.io/node-amd-register annotation and can be empty. Since
// DecodeContainerDevices rejects an empty type, an allocation carrying it
// would be unreadable to the scheduler and the pod's usage would be dropped.
func Test_Fit_EmptyRegisteredTypeStaysDecodable(t *testing.T) {
	dev := &AMDDevices{}
	devices := []*device.DeviceUsage{{
		ID:        "GPU-amd-0",
		Index:     0,
		Count:     10,
		Totalmem:  8192,
		Totalcore: 100,
		Type:      "",
		Health:    true,
	}}
	request := device.ContainerDeviceRequest{
		Nums:     1,
		Type:     AMDDevice,
		Memreq:   4096,
		Coresreq: 100,
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	fit, tmpDevs, reason := dev.Fit(devices, request, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, tmpDevs[AMDDevice][0].Type, AMDDevice)

	encoded := device.EncodeContainerDevices(tmpDevs[AMDDevice])
	decoded, err := device.DecodeContainerDevices(encoded)
	assert.NilError(t, err)
	assert.Equal(t, decoded[0].UUID, "GPU-amd-0")
	assert.Equal(t, decoded[0].Type, AMDDevice)
}

// A non empty registered product type is still passed through unchanged.
func Test_Fit_RegisteredProductTypePreserved(t *testing.T) {
	dev := &AMDDevices{}
	devices := []*device.DeviceUsage{{
		ID:        "GPU-amd-0",
		Index:     0,
		Count:     10,
		Totalmem:  8192,
		Totalcore: 100,
		Type:      "AMD Instinct MI300X",
		Health:    true,
	}}
	request := device.ContainerDeviceRequest{
		Nums:     1,
		Type:     AMDDevice,
		Memreq:   4096,
		Coresreq: 100,
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	fit, tmpDevs, reason := dev.Fit(devices, request, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, tmpDevs[AMDDevice][0].Type, "AMD Instinct MI300X")
}
