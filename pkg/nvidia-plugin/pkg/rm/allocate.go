/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rm

import (
	"fmt"
	"sort"
)

// distributedAlloc returns a list of devices such that any replicated
// devices are distributed across all replicated GPUs equally. It takes into
// account already allocated replicas to ensure a proper balance across them.
func (r *resourceManager) distributedAlloc(available, required []string, size int) ([]string, error) {
	// Get the set of candidate devices as the difference between available and required.
	candidates := r.devices.Subset(available).Difference(r.devices.Subset(required)).GetIDs()
	needed := size - len(required)

	if len(candidates) < needed {
		return nil, fmt.Errorf("not enough available devices to satisfy allocation")
	}

	// For each candidate device, build a mapping of (stripped) device ID to
	// total / available replicas for that device.
	replicas := make(map[string]*struct{ total, available int })
	for _, c := range candidates {
		id := AnnotatedID(c).GetID()
		if _, exists := replicas[id]; !exists {
			replicas[id] = &struct{ total, available int }{}
		}
		replicas[id].available++
	}
	for d := range r.devices {
		id := AnnotatedID(d).GetID()
		if _, exists := replicas[id]; !exists {
			continue
		}
		replicas[id].total++
	}

	// Grab the set of 'needed' devices one-by-one from the candidates list.
	// Before selecting each candidate, first sort the candidate list using the
	// replicas map above. After sorting, the first element in the list will
	// contain the device with the least difference between total and available
	// replications (based on what's already been allocated). Add this device
	// to the list of devices to allocate, remove it from the candidate list,
	// down its available count in the replicas map, and repeat.
	var devices []string
	for i := 0; i < needed; i++ {
		sort.Slice(candidates, func(i, j int) bool {
			iid := AnnotatedID(candidates[i]).GetID()
			jid := AnnotatedID(candidates[j]).GetID()
			idiff := replicas[iid].total - replicas[iid].available
			jdiff := replicas[jid].total - replicas[jid].available
			return idiff < jdiff
		})
		id := AnnotatedID(candidates[0]).GetID()
		replicas[id].available--
		devices = append(devices, candidates[0])
		candidates = candidates[1:]
	}

	// Add the set of required devices to this list and return it.
	devices = append(required, devices...)

	return devices, nil
}
