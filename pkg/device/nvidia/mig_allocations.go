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
// The device plugin only realizes this exact hardware placement and fills in
// MigUUID. ContainerIndex and DeviceIndex make repeated allocations on the
// same physical GPU unambiguous without synthesizing logical device IDs.
type MigAllocation struct {
	ContainerIndex int                 `json:"containerIndex"`
	DeviceIndex    int                 `json:"deviceIndex"`
	GPUUUID        string              `json:"gpuUUID"`
	Profile        string              `json:"profile"`
	Placement      device.MigPlacement `json:"placement"`
	MigUUID        string              `json:"migUUID,omitempty"`
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
		key := [2]int{allocation.ContainerIndex, allocation.DeviceIndex}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate MIG allocation for container %d device %d", allocation.ContainerIndex, allocation.DeviceIndex)
		}
		seen[key] = struct{}{}
	}
	return out, nil
}
