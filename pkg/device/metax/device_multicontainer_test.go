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

package metax

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// Metax whole GPUs register with Count 1, so a pod with several containers
// necessarily spreads over distinct cards. The removed same card filter made
// that impossible: the second container was rejected on a fully idle node
// with CardNotFoundCustomFilterRule on every free device.
func Test_Fit_MultiContainerPodSpreadsOverCards(t *testing.T) {
	dev := &MetaxDevices{}
	devices := make([]*device.DeviceUsage, 0, 8)
	for i := range 8 {
		devices = append(devices, &device.DeviceUsage{
			ID:        fmt.Sprintf("node-metax-%d", i),
			Index:     uint(i),
			Count:     1,
			Totalmem:  65536,
			Totalcore: 100,
			Type:      MetaxGPUDevice,
			Health:    true,
		})
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default", Annotations: map[string]string{}}}
	req := device.ContainerDeviceRequest{Nums: 1, Type: MetaxGPUDevice, Memreq: 1024, Coresreq: 10}
	allocated := &device.PodDevices{}

	fit1, tmp1, reason1 := dev.Fit(devices, req, pod, &device.NodeInfo{}, allocated)
	assert.Equal(t, fit1, true, reason1)
	first := tmp1[MetaxGPUDevice][0]
	for _, du := range devices {
		if du.ID == first.UUID {
			assert.NilError(t, dev.AddResourceUsage(pod, du, &first))
		}
	}
	(*allocated)[MetaxGPUDevice] = append((*allocated)[MetaxGPUDevice], tmp1[MetaxGPUDevice])

	fit2, tmp2, reason2 := dev.Fit(devices, req, pod, &device.NodeInfo{}, allocated)
	assert.Equal(t, fit2, true, reason2)
	second := tmp2[MetaxGPUDevice][0]
	assert.Assert(t, first.UUID != second.UUID, "both containers landed on %s", first.UUID)
}
