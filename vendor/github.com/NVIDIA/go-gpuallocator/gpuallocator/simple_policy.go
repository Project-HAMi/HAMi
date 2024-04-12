// Copyright (c) 2019, NVIDIA CORPORATION. All rights reserved.

package gpuallocator

type simplePolicy struct{}

// NewSimplePolicy creates a new SimplePolicy.
func NewSimplePolicy() Policy {
	return &simplePolicy{}
}

// Allocate GPUs following a simple policy.
func (p *simplePolicy) Allocate(available []*Device, required []*Device, size int) []*Device {
	if size <= 0 {
		return []*Device{}
	}

	if len(available) < size {
		return []*Device{}
	}

	if len(required) > size {
		return []*Device{}
	}

	availableSet := NewDeviceSet(available...)
	if !availableSet.ContainsAll(required) {
		return []*Device{}
	}
	availableSet.Delete(required...)

	allocated := append([]*Device{}, required...)
	allocated = append(allocated, availableSet.SortedSlice()[:size-len(allocated)]...)
	return allocated
}
