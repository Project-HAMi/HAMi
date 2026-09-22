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

import (
	"strings"
	"testing"
)

// A forged allocation annotation must not hand a container more memory or more
// cores than its own limits ask for, whichever vendor's plugin serves it
// (issue #3041).
func TestValidateContainerAllocation(t *testing.T) {
	const uuid = "GPU-1"
	knownCard := func(asked string) (int32, bool) {
		if asked != uuid {
			return 0, false
		}
		return 24000, true
	}

	tests := []struct {
		name       string
		req        ContainerDeviceRequest
		allocated  ContainerDevices
		cardMemory CardMemoryMB
		cores      CoresUnit
		wantErr    bool
	}{
		{
			name:       "within the requested memory",
			req:        ContainerDeviceRequest{Nums: 1, Memreq: 3000},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 3000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
		},
		{
			name:       "more memory than requested",
			req:        ContainerDeviceRequest{Nums: 1, Memreq: 3000},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 20000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
			wantErr:    true,
		},
		{
			name:       "within the requested cores",
			req:        ContainerDeviceRequest{Nums: 1, Coresreq: 50},
			allocated:  ContainerDevices{{UUID: uuid, Usedcores: 50}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
		},
		{
			name:       "more cores than requested",
			req:        ContainerDeviceRequest{Nums: 1, Coresreq: 10},
			allocated:  ContainerDevices{{UUID: uuid, Usedcores: 100}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
			wantErr:    true,
		},
		{
			name:       "within the requested percentage",
			req:        ContainerDeviceRequest{Nums: 1, MemPercentagereq: 50},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 12000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
		},
		{
			name:       "more than the requested percentage",
			req:        ContainerDeviceRequest{Nums: 1, MemPercentagereq: 50},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 20000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
			wantErr:    true,
		},
		{
			name:       "percentage on a card the plugin does not know",
			req:        ContainerDeviceRequest{Nums: 1, MemPercentagereq: 50},
			allocated:  ContainerDevices{{UUID: "GPU-other", Usedmem: 20000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
		},
		{
			// Both were asked for, and every backend sizes the slice from
			// Memreq while it is set. Measuring against the percentage instead
			// would reject what the scheduler allocated.
			name:       "both memory requests set, the absolute one is larger",
			req:        ContainerDeviceRequest{Nums: 1, Memreq: 20000, MemPercentagereq: 10},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 20000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
		},
		{
			name:       "both memory requests set, over the absolute one",
			req:        ContainerDeviceRequest{Nums: 1, Memreq: 20000, MemPercentagereq: 10},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 21000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
			wantErr:    true,
		},
		{
			name:      "percentage with no card memory to size it against",
			req:       ContainerDeviceRequest{Nums: 1, MemPercentagereq: 50},
			allocated: ContainerDevices{{UUID: uuid, Usedmem: 20000}},
			cores:     CoresInRequestUnits,
		},
		{
			name:       "memory left to the default",
			req:        ContainerDeviceRequest{Nums: 1},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 24000}},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
		},
		{
			name: "the second of two devices is over",
			req:  ContainerDeviceRequest{Nums: 2, Memreq: 3000},
			allocated: ContainerDevices{
				{UUID: uuid, Usedmem: 3000},
				{UUID: uuid, Usedmem: 9000},
			},
			cardMemory: knownCard,
			cores:      CoresInRequestUnits,
			wantErr:    true,
		},
		{
			// AMD books Usedcores as the compute units the percentage resolved
			// to, and AWS Neuron books a core bitmask. Neither is the request's
			// unit, so reading them as one rejects allocations the scheduler
			// made. Memory is still in MB and still checked.
			name:       "vendor encoded cores are left alone",
			req:        ContainerDeviceRequest{Nums: 1, Memreq: 3000, Coresreq: 50},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 3000, Usedcores: 152}},
			cardMemory: knownCard,
			cores:      CoresVendorEncoded,
		},
		{
			name:       "vendor encoded cores still do not excuse the memory",
			req:        ContainerDeviceRequest{Nums: 1, Memreq: 3000, Coresreq: 50},
			allocated:  ContainerDevices{{UUID: uuid, Usedmem: 9000, Usedcores: 152}},
			cardMemory: knownCard,
			cores:      CoresVendorEncoded,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContainerAllocation("main", tt.req, tt.allocated, tt.cardMemory, tt.cores)
			if tt.wantErr {
				if err == nil {
					t.Fatal("the allocation was accepted")
				}
				if !strings.Contains(err.Error(), "main") {
					t.Errorf("error %q does not name the container", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("the allocation was rejected: %v", err)
			}
		})
	}
}
