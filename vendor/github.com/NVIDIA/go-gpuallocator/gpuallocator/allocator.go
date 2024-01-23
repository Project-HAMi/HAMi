// Copyright (c) 2019, NVIDIA CORPORATION. All rights reserved.

package gpuallocator

import (
	"fmt"
	"runtime"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
)

// Allocator defines the primary object for allocating and freeing the
// available GPUs on a node.
type Allocator struct {
	GPUs []*Device

	policy    Policy
	remaining DeviceSet
	allocated DeviceSet
}

// Policy defines an interface for plugagable allocation policies to be added
// to an Allocator.
type Policy interface {
	// Allocate is meant to do the heavy-lifting of implementing the actual
	// allocation strategy of the policy. It takes a slice of devices to
	// allocate GPUs from, and an amount 'size' to allocate from that slice. It
	// then returns a subset of devices of length 'size'. If the policy is
	// unable to allocate 'size' GPUs from the slice of input devices, it
	// returns an empty slice.
	Allocate(available []*Device, required []*Device, size int) []*Device
}

// NewSimpleAllocator creates a new Allocator using the Simple allocation
// policy
func NewSimpleAllocator() (*Allocator, error) {
	return NewAllocator(NewSimplePolicy())
}

// NewBestEffortAllocator creates a new Allocator using the BestEffort
// allocation policy
func NewBestEffortAllocator() (*Allocator, error) {
	return NewAllocator(NewBestEffortPolicy())
}

// NewAllocator creates a new Allocator using the given allocation policy
func NewAllocator(policy Policy) (*Allocator, error) {
	err := nvml.Init()
	if err != nil {
		return nil, fmt.Errorf("error initializing NVML: %v", err)
	}

	devices, err := NewDevices()
	if err != nil {
		return nil, fmt.Errorf("error enumerating GPU devices: %v", err)
	}

	allocator := newAllocatorFrom(devices, policy)

	runtime.SetFinalizer(allocator, func(allocator *Allocator) {
		// Explicitly ignore any errors from nvml.Shutdown().
		_ = nvml.Shutdown()
	})

	return allocator, nil
}

// newAllocatorFrom creates a new Allocator using the given allocation policy
// using the supplied set of devices.
func newAllocatorFrom(devices []*Device, policy Policy) *Allocator {
	allocator := &Allocator{
		GPUs:      devices,
		policy:    policy,
		remaining: NewDeviceSet(),
		allocated: NewDeviceSet(),
	}
	allocator.remaining.Insert(devices...)
	return allocator
}

// Allocate a set of 'num' GPUs from the allocator.
// If 'num' devices cannot be allocated, return an empty slice.
func (a *Allocator) Allocate(num int) []*Device {
	devices := a.policy.Allocate(a.remaining.SortedSlice(), nil, num)

	err := a.AllocateSpecific(devices...)
	if err != nil {
		err = fmt.Errorf("Internal error while allocating GPUs: %v", err)
		panic(err)
	}

	return devices
}

// AllocateSpecific allocates a specific set of GPUs from the allocator.
// Return an error if any of the specified devices cannot be allocated.
func (a *Allocator) AllocateSpecific(devices ...*Device) error {
	// Make sure we can allocate all of the devices.
	unavailable := []*Device{}
	for _, gpu := range devices {
		if !a.remaining.Contains(gpu) {
			unavailable = append(unavailable, gpu)
		}
	}

	if len(unavailable) != 0 {
		return fmt.Errorf("devices '%v' are unavailable for allocation, available: %v", unavailable, a.remaining.SortedSlice())
	}

	a.allocated.Insert(devices...)
	a.remaining.Delete(devices...)

	return nil
}

// Free a set of GPUs back to the allocator.
func (a *Allocator) Free(devices ...*Device) {
	a.remaining.Insert(devices...)
	a.allocated.Delete(devices...)
}
