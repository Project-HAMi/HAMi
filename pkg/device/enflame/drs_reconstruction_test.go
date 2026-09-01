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

package enflame

import (
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

// drsCard returns a DRS card with 6 slices and a 3-profile menu. Totalmem and
// Totalcore are parameterised so callers pick which constraint is binding.
func drsCard(totalmem, totalcore int32) *device.DeviceUsage {
	return &device.DeviceUsage{
		ID:        "GPU-enflame-drs-0",
		Index:     0,
		Count:     6,
		Used:      0,
		Totalmem:  totalmem,
		Totalcore: totalcore,
		Type:      EnflameVGCUDevice,
		Health:    true,
		CustomInfo: map[string]any{
			"minor": "0",
			"index": "0",
			"profiles": map[string]string{
				"1g.6gb":  "0",
				"3g.20gb": "1",
				"6g.40gb": "2",
			},
		},
	}
}

// TestDRSSlice_SlotCountSurvivesReconstruction checks that the slice count a
// live allocation consumes is recovered from the pod annotation.
func TestDRSSlice_SlotCountSurvivesReconstruction(t *testing.T) {
	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	enf := &EnflameDevices{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	// Memreq 3 selects the 3-slice "3g.20gb" profile.
	req := device.ContainerDeviceRequest{Nums: 1, Type: EnflameVGCUDevice, Memreq: 3}

	fit, result, reason := enf.Fit([]*device.DeviceUsage{drsCard(40960, 100)}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, reason, "")
	ctrDevs := result[EnflameVGCUDevice]
	assert.Equal(t, len(ctrDevs), 1)
	assert.Equal(t, ctrDevs[0].Slots, int32(3))

	// Live path: AddResourceUsage consumes profile.Size slices.
	liveCard := drsCard(40960, 100)
	assert.NilError(t, enf.AddResourceUsage(pod, liveCard, &ctrDevs[0]))
	assert.Equal(t, liveCard.Used, int32(3))

	// Reconstruction path: persist to the annotation and read it back the way
	// getNodesUsage does.
	encoded := device.EncodeContainerDevices(ctrDevs)
	decoded, err := device.DecodeContainerDevices(encoded)
	assert.NilError(t, err)
	assert.Equal(t, len(decoded), 1)
	assert.Equal(t, max(decoded[0].Slots, 1), liveCard.Used)
}

// TestDRSSlice_PartialCapacityRejectsFullProfile checks that a profile only
// fits when every slice it needs is free, not just one.
func TestDRSSlice_PartialCapacityRejectsFullProfile(t *testing.T) {
	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	enf := &EnflameDevices{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	// Totalcore 0 skips the core guard, so slices are the binding constraint.
	card := drsCard(400000, 0)
	card.Used = 4

	// 2 of 6 slices are free, so the 3 slice "3g.20gb" profile must not fit.
	threeSlices := device.ContainerDeviceRequest{Nums: 1, Type: EnflameVGCUDevice, Memreq: 3}
	fit, _, reason := enf.Fit([]*device.DeviceUsage{card}, threeSlices, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, common.CardTimeSlicingExhausted))

	// The 1 slice "1g.6gb" profile still fits in the remaining capacity.
	oneSlice := device.ContainerDeviceRequest{Nums: 1, Type: EnflameVGCUDevice, Memreq: 1}
	fit, result, reason := enf.Fit([]*device.DeviceUsage{card}, oneSlice, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, reason, "")
	assert.Equal(t, result[EnflameVGCUDevice][0].Slots, int32(1))
}

// TestDRSSlice_SliceCountPrefersSlots checks that Slots is authoritative and
// that CustomInfo only fills in for entries built before Fit recorded Slots.
func TestDRSSlice_SliceCountPrefersSlots(t *testing.T) {
	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	enf := &EnflameDevices{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	tests := []struct {
		name string
		ctr  device.ContainerDevice
		want int32
	}{
		{"slots wins over a stale CustomInfo", device.ContainerDevice{Slots: 3, CustomInfo: map[string]any{"drsSlice": 1}}, 3},
		{"custominfo fills in when slots is unset", device.ContainerDevice{CustomInfo: map[string]any{"drsSlice": 3}}, 3},
		{"neither set counts as one slice", device.ContainerDevice{}, 1},
		{"annotation round trip keeps the count", device.ContainerDevice{Slots: 3}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, sliceCount(&tt.ctr), tt.want)

			// AddResourceUsage and PatchAnnotations must agree on the same entry.
			card := drsCard(400000, 0)
			assert.NilError(t, enf.AddResourceUsage(pod, card, &tt.ctr))
			assert.Equal(t, card.Used, tt.want)

			annos := map[string]string{}
			enf.PatchAnnotations(pod, &annos, device.PodDevices{
				EnflameVGCUDevice: {device.ContainerDevices{tt.ctr}},
			})
			assert.Equal(t, annos[PodRequestGCUSize], strconv.FormatInt(int64(tt.want), 10))
		})
	}
}

// TestDRSSlice_ReconstructionRejectsSliceOversubscription checks that a card
// already full on slices still rejects a new instance after a restart.
func TestDRSSlice_ReconstructionRejectsSliceOversubscription(t *testing.T) {
	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	enf := &EnflameDevices{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
	req := device.ContainerDeviceRequest{Nums: 1, Type: EnflameVGCUDevice, Memreq: 3}

	// Totalcore 0 skips the core guard, so slices are the binding constraint.
	liveCard := drsCard(400000, 0)
	var persisted device.ContainerDevices
	for range 2 {
		fit, result, _ := enf.Fit([]*device.DeviceUsage{liveCard}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
		assert.Equal(t, fit, true)
		ctr := result[EnflameVGCUDevice][0]
		assert.NilError(t, enf.AddResourceUsage(pod, liveCard, &ctr))
		persisted = append(persisted, ctr)
	}
	assert.Equal(t, liveCard.Used, int32(6))

	fitFull, _, _ := enf.Fit([]*device.DeviceUsage{liveCard}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fitFull, false)

	// Restart: rebuild Used from the persisted annotations.
	rebuilt := drsCard(400000, 0)
	for _, ctr := range persisted {
		encoded := device.EncodeContainerDevices(device.ContainerDevices{ctr})
		decoded, err := device.DecodeContainerDevices(encoded)
		assert.NilError(t, err)
		rebuilt.Used += max(decoded[0].Slots, 1)
		rebuilt.Usedmem += decoded[0].Usedmem
		rebuilt.Usedcores += decoded[0].Usedcores
	}
	assert.Equal(t, rebuilt.Used, liveCard.Used)

	fitAfterRestart, _, _ := enf.Fit([]*device.DeviceUsage{rebuilt}, req, pod, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fitAfterRestart, false)
}
