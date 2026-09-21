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

package device

import "fmt"

// CardMemoryMB reports the memory a device plugin published for a card, and
// whether that card is known to it. A percentage request is sized against this
// number, so a card the plugin does not know leaves such a request unbounded.
type CardMemoryMB func(uuid string) (int32, bool)

// ValidateContainerAllocation rejects an allocation handing the container more
// than its own resource limits ask for (issue #3041). Every device plugin reads
// the allocation from the pod annotation the scheduler writes, and nothing on
// the plugin side proves the scheduler wrote it, so the numbers are compared
// against the container's limits, which cannot be raised once the pod exists.
//
// req is what the device backend derives from the container, and cardMemory
// resolves the published memory of a card for a percentage request. A vendor
// whose slices are charged a rounded-up capacity rather than the raw request,
// as NVIDIA MIG is, has nothing to compare against and should not call this.
func ValidateContainerAllocation(ctrName string, req ContainerDeviceRequest, allocated ContainerDevices, cardMemory CardMemoryMB) error {
	for _, each := range allocated {
		limit, bounded := requestedMemoryMB(req, each.UUID, cardMemory)
		if bounded && each.Usedmem > limit {
			return fmt.Errorf("container %s is allocated %d MB on device %s but requests %d MB",
				ctrName, each.Usedmem, each.UUID, limit)
		}
		if req.Coresreq > 0 && each.Usedcores > req.Coresreq {
			return fmt.Errorf("container %s is allocated %d%% of the cores on device %s but requests %d%%",
				ctrName, each.Usedcores, each.UUID, req.Coresreq)
		}
	}
	return nil
}

// requestedMemoryMB returns the memory the request entitles the container to on
// the named card, and whether the request bounds it at all.
func requestedMemoryMB(req ContainerDeviceRequest, uuid string, cardMemory CardMemoryMB) (int32, bool) {
	// A percentage leaves Memreq at 0 unless both were asked for, in which case
	// the scheduler sized the slice from the percentage as well.
	if req.MemPercentagereq >= 1 && req.MemPercentagereq <= 100 {
		if cardMemory == nil {
			return 0, false
		}
		total, ok := cardMemory(uuid)
		if !ok {
			return 0, false
		}
		return total * req.MemPercentagereq / 100, true
	}
	if req.Memreq > 0 {
		return req.Memreq, true
	}
	return 0, false
}
