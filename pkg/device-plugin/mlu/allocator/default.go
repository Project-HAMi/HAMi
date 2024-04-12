/*
Copyright 2024 The HAMi Authors.

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

package allocator

import (
	"fmt"
	"log"
	"sort"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cndev"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cntopo"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

type defaultAllocator struct {
	policy string
	cntopo cntopo.Cntopo
	devs   map[string]*cndev.Device
}

func NewDefaultAllocator(policy string, devs map[string]*cndev.Device) Allocator {
	return &defaultAllocator{
		policy: policy,
		cntopo: cntopo.New(),
		devs:   devs,
	}
}

func (a *defaultAllocator) Allocate(available []uint, required []uint, size int) ([]uint, error) {

	rings, err := a.cntopo.GetRings(available, size)
	if err != nil {
		return nil, err
	}
	sort.Slice(rings, func(i int, j int) bool {
		return rings[i].NonConflictRingNum > rings[j].NonConflictRingNum
	})

	if len(rings) == 0 {
		log.Println("found no rings")
		if a.policy != util.BestEffort && !a.sizeAlwaysFailsToFormRing(size) {
			return nil, fmt.Errorf("mode %s found no rings", a.policy)
		}
		return available[0:size], nil
	}

	return rings[0].Ordinals, nil
}

func (a *defaultAllocator) sizeAlwaysFailsToFormRing(size int) bool {
	return size%2 == 1
}
