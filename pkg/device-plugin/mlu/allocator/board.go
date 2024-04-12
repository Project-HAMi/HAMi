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
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cndev"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cntopo"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

type boardAllocator struct {
	policy string
	cntopo cntopo.Cntopo
	devs   map[string]*cndev.Device
	groups [][]uint
}

func NewBoardAllocator(policy string, devs map[string]*cndev.Device) Allocator {
	return &boardAllocator{
		policy: policy,
		cntopo: cntopo.New(),
		devs:   devs,
		groups: getCPUGroups(),
	}
}

func (a *boardAllocator) Allocate(available []uint, required []uint, size int) ([]uint, error) {

	rings, err := a.cntopo.GetRings(available, size)
	if err != nil {
		return nil, err
	}
	sort.Slice(rings, func(i int, j int) bool {
		return rings[i].NonConflictRingNum > rings[j].NonConflictRingNum
	})

	boards := splitByBoards(available, a.devs)
	groups, err := a.filterAvaliableDevsByGroup(available)
	if err != nil {
		log.Printf("failed to filter %v by group %v, ignore when allocating", available, a.groups)
	}

	log.Printf("available devs filtered by group: %v", groups)

	if len(rings) == 0 {
		log.Println("found no rings")
		if a.policy != util.BestEffort && !a.sizeAlwaysFailsToFormRing(size) {
			return nil, fmt.Errorf("mode %s found no rings for size %d", a.policy, size)
		}

		needed := size
		allocated := []uint{}

		allocateRemainingFrom := func(devices []uint) bool {
			for _, device := range devices {
				if contains(allocated, device) {
					continue
				}
				allocated = append(allocated, device)
				needed--
				if needed == 0 {
					return true
				}
			}
			return false
		}
		if groups == nil {
			for _, board := range boards {
				if allocateRemainingFrom(board) {
					return allocated, nil
				}
			}
		}
		for _, group := range groups {
			for _, board := range boards {
				if containsAll(group, board) {
					if allocateRemainingFrom(board) {
						return allocated, nil
					}
				}
			}
		}
		if allocateRemainingFrom(available) {
			return allocated, nil
		}
		return nil, errors.New("allocated from all available devices, should not be here")
	}

	if a.policy == util.Restricted && size == 2 && rings[0].NonConflictRingNum < 2 {
		return nil, fmt.Errorf("mode %s, max non-conflict ring num %d", a.policy, rings[0].NonConflictRingNum)
	}

	candidates := rings
	for i, ring := range rings {
		if ring.NonConflictRingNum < rings[0].NonConflictRingNum {
			candidates = rings[0:i]
			break
		}
	}

	for _, group := range groups {
		for _, candidate := range candidates {
			if containsAll(group, candidate.Ordinals) {
				return candidate.Ordinals, nil
			}
		}
	}

	return candidates[0].Ordinals, nil

}

func (a *boardAllocator) filterAvaliableDevsByGroup(available []uint) ([][]uint, error) {
	if len(a.groups) != 2 {
		return nil, fmt.Errorf("allocator groups is %v", a.groups)
	}
	group0 := []uint{}
	group1 := []uint{}
	for _, dev := range available {
		if contains(a.groups[0], dev) {
			group0 = append(group0, dev)
			continue
		}
		if contains(a.groups[1], dev) {
			group1 = append(group1, dev)
			continue
		}
		return nil, fmt.Errorf("dev %d not in groups %v", available, a.groups)
	}
	if len(group0) > len(group1) {
		group0, group1 = group1, group0
	}
	return [][]uint{group0, group1}, nil
}

func (a *boardAllocator) sizeAlwaysFailsToFormRing(size int) bool {
	if size > 8 || size <= 1 {
		return true
	}
	if size%2 == 1 {
		return true
	}
	return false
}

func splitByBoards(available []uint, devs map[string]*cndev.Device) [][]uint {
	boards := make(map[string][]uint)
	for _, dev := range devs {
		if !contains(available, dev.Slot) {
			continue
		}
		boards[dev.SN] = append(boards[dev.SN], dev.Slot)
	}
	log.Printf("available devices separated by board %v", boards)
	res := [][]uint{}
	for _, board := range boards {
		res = append(res, board)
	}
	sort.Slice(res, func(i, j int) bool {
		return len(res[i]) < len(res[j])
	})
	log.Printf("sorted available devices seperated by board %v", res)
	return res
}

func getCPUGroups() [][]uint {
	groups, err := cndev.GetMLULinkGroups()
	if err != nil {
		log.Printf("failed to get CPU groups, err %v", err)
		return nil
	}
	if len(groups) != 2 || len(groups[0]) != 8 {
		log.Printf("unexpected groups: %v", groups)
		return nil
	}
	return groups
}
