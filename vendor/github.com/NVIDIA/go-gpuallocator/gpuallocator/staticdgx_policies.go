// Copyright (c) 2019, NVIDIA CORPORATION. All rights reserved.

package gpuallocator

// GPUType represents the valid set of GPU
// types a Static DGX policy can be created for.
type GPUType int

// Valid GPUTypes
const (
	GPUTypePascal GPUType = iota // Pascal GPUs
	GPUTypeVolta
)

// Policy Definitions
type staticDGX1PascalPolicy struct{}
type staticDGX1VoltaPolicy struct{}
type staticDGX2VoltaPolicy struct{}

// NewStaticDGX1Policy creates a new StaticDGX1Policy for gpuType.
func NewStaticDGX1Policy(gpuType GPUType) Policy {
	if gpuType == GPUTypePascal {
		return &staticDGX1PascalPolicy{}
	}
	if gpuType == GPUTypeVolta {
		return &staticDGX1VoltaPolicy{}
	}
	return nil
}

// NewStaticDGX2Policy creates a new StaticDGX2Policy.
func NewStaticDGX2Policy() Policy {
	return &staticDGX1VoltaPolicy{}
}

// Allocate GPUs following the Static DGX-1 policy for Pascal GPUs.
func (p *staticDGX1PascalPolicy) Allocate(available []*Device, required []*Device, size int) []*Device {
	if size <= 0 {
		return []*Device{}
	}

	if len(available) < size {
		return []*Device{}
	}

	if len(required) > size {
		return []*Device{}
	}

	validSets := map[int][][]int{
		1: {{0}, {1}, {2}, {3}, {4}, {5}, {6}, {7}},
		2: {{0, 2}, {1, 3}, {4, 6}, {5, 7}},
		4: {{0, 1, 2, 3}, {4, 5, 6, 7}},
		8: {{0, 1, 2, 3, 4, 5, 6, 7}},
	}

	return findGPUSet(available, required, size, validSets[size])
}

// Allocate GPUs following the Static DGX-1 policy for Volta GPUs.
func (p *staticDGX1VoltaPolicy) Allocate(available []*Device, required []*Device, size int) []*Device {
	if size <= 0 {
		return []*Device{}
	}

	if len(available) < size {
		return []*Device{}
	}

	if len(required) > size {
		return []*Device{}
	}

	validSets := map[int][][]int{
		1: {{0}, {1}, {2}, {3}, {4}, {5}, {6}, {7}},
		2: {{0, 3}, {1, 2}, {4, 7}, {5, 6}},
		4: {{0, 1, 2, 3}, {4, 5, 6, 7}},
		8: {{0, 1, 2, 3, 4, 5, 6, 7}},
	}

	return findGPUSet(available, required, size, validSets[size])
}

// Allocate GPUs following the Static DGX-2 policy for Volta GPUs.
func (p *staticDGX2VoltaPolicy) Allocate(available []*Device, required []*Device, size int) []*Device {
	if size <= 0 {
		return []*Device{}
	}

	if len(available) < size {
		return []*Device{}
	}

	if len(required) > size {
		return []*Device{}
	}

	validSets := map[int][][]int{
		1:  {{0}, {1}, {2}, {3}, {4}, {5}, {6}, {7}, {8}, {9}, {10}, {11}, {12}, {13}, {14}, {15}},
		2:  {{0, 1}, {2, 3}, {4, 5}, {6, 7}, {8, 9}, {10, 11}, {12, 13}, {14, 15}},
		4:  {{0, 1, 2, 3}, {4, 5, 6, 7}, {8, 9, 10, 11}, {12, 13, 14, 15}},
		8:  {{0, 1, 2, 3, 4, 5, 6, 7}, {8, 9, 10, 11, 12, 13, 14, 15}},
		16: {{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
	}

	return findGPUSet(available, required, size, validSets[size])
}

// Find a GPU set of size 'size' in the list of devices that is contained in 'validSets'.
// This algorithm makes sure that the set chosen contains all of the devices in 'required'.
func findGPUSet(available []*Device, required []*Device, size int, validSets [][]int) []*Device {
	// Make sure that the required set of devices are actually available.
	availableSet := NewDeviceSet(available...)
	if !availableSet.ContainsAll(required) {
		return []*Device{}
	}
	availableSet.Delete(required...)

	// Allocate devices from a valid set
	allocated := []*Device{}
	for _, validSet := range validSets {
		// Make sure all of the required devices are part of the valid set and allocate them
		for _, i := range validSet {
			for _, device := range required {
				if device.Index == i {
					allocated = append(allocated, device)
					break
				}
			}
		}

		if len(allocated) != len(required) {
			allocated = []*Device{}
			continue
		}

		// Allocate the rest of the devices in the valid set if they are available
		for _, i := range validSet {
			for _, device := range availableSet.SortedSlice() {
				if device.Index == i {
					allocated = append(allocated, device)
					break
				}
			}
		}

		if len(allocated) != size {
			allocated = []*Device{}
			continue
		}

		break
	}

	return allocated
}
