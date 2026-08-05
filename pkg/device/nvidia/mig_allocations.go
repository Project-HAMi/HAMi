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

package nvidia

import (
	"encoding/json"
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

const MigAllocationsAnnotation = "hami.io/vgpu-mig-allocations"

const (
	MigProfileCustomInfo   = "migProfile"
	MigPlacementCustomInfo = "migPlacement"
)

// MigAllocation is the complete scheduler reservation for one MIG device.
// The device plugin realizes this exact hardware placement and fills in its
// MIG UUID plus GI/CI runtime identity. ContainerIndex and DeviceIndex make
// repeated allocations on the same physical GPU unambiguous.
type MigAllocation struct {
	ContainerIndex    int                 `json:"containerIndex"`
	DeviceIndex       int                 `json:"deviceIndex"`
	GPUUUID           string              `json:"gpuUUID"`
	Profile           string              `json:"profile"`
	Placement         device.MigPlacement `json:"placement"`
	MigUUID           string              `json:"migUUID,omitempty"`
	GPUInstanceID     *uint32             `json:"gpuInstanceID,omitempty"`
	ComputeInstanceID *uint32             `json:"computeInstanceID,omitempty"`
}

func EncodeMigAllocations(pd device.PodSingleDevice) (string, bool) {
	out := make([]MigAllocation, 0)
	for containerIndex, ctr := range pd {
		for deviceIndex, dev := range ctr {
			if dev.CustomInfo == nil {
				continue
			}
			profile, profileOK := dev.CustomInfo[MigProfileCustomInfo].(string)
			placement, placementOK := dev.CustomInfo[MigPlacementCustomInfo].(device.MigPlacement)
			if !profileOK || profile == "" || !placementOK || placement.Size == 0 {
				continue
			}
			out = append(out, MigAllocation{
				ContainerIndex: containerIndex,
				DeviceIndex:    deviceIndex,
				GPUUUID:        dev.UUID,
				Profile:        profile,
				Placement:      placement,
			})
		}
	}
	if len(out) == 0 {
		return "", false
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func DecodeMigAllocations(raw string) ([]MigAllocation, error) {
	if raw == "" {
		return nil, nil
	}
	var out []MigAllocation
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	seen := make(map[[2]int]struct{}, len(out))
	for i, allocation := range out {
		if allocation.ContainerIndex < 0 || allocation.DeviceIndex < 0 || allocation.GPUUUID == "" || allocation.Profile == "" || allocation.Placement.Size == 0 {
			return nil, fmt.Errorf("MIG allocation %d is incomplete", i)
		}
		runtimeFields := 0
		if allocation.MigUUID != "" {
			runtimeFields++
		}
		if allocation.GPUInstanceID != nil {
			runtimeFields++
		}
		if allocation.ComputeInstanceID != nil {
			runtimeFields++
		}
		if runtimeFields != 0 && runtimeFields != 3 {
			return nil, fmt.Errorf("MIG allocation %d has partial runtime identity", i)
		}
		key := [2]int{allocation.ContainerIndex, allocation.DeviceIndex}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate MIG allocation for container %d device %d", allocation.ContainerIndex, allocation.DeviceIndex)
		}
		seen[key] = struct{}{}
	}
	return out, nil
}
