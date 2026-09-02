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

package ascend

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// Module pair allocation was introduced for SuperPod environments (#1610), and
// #2005 added the SuperPod gate to MutateAdmission but not to Fit. In split
// mode an odd request therefore passed admission and was then rejected on every
// node, because the pair combination selects whole cards and returns more
// devices than requested.
func TestAscend910C_FitSplitModeOddRequestSchedulable(t *testing.T) {
	dev := &Devices{config: VNPUConfig{CommonWord: Ascend910CType}}
	devices := make([]*device.DeviceUsage, 0, 8)
	for i := range 8 {
		devices = append(devices, &device.DeviceUsage{
			ID:        fmt.Sprintf("npu-%d", i),
			Index:     uint(i),
			Count:     100,
			Totalmem:  65536,
			Totalcore: 100,
			Health:    true,
		})
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", Annotations: map[string]string{}}}
	request := device.ContainerDeviceRequest{Nums: 3, Type: Ascend910CType, Memreq: 16384, Coresreq: 20}

	fit, tmpDevs, reason := dev.Fit(devices, request, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true, reason)
	assert.Equal(t, len(tmpDevs[Ascend910CType]), 3)
}
