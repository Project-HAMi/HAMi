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
	"sort"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cndev"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cntopo"
	"github.com/Project-HAMi/HAMi/pkg/util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Board Allocator", func() {

	Context("Test allocate", func() {
		var (
			devsInfo  map[string]*cndev.Device
			allocator *boardAllocator
		)

		BeforeEach(func() {
			devsInfo = map[string]*cndev.Device{
				"MLU-0": {
					UUID: "MLU-0",
					Slot: 0,
					SN:   "sn-0",
				},
				"MLU-1": {
					UUID: "MLU-1",
					Slot: 1,
					SN:   "sn-0",
				},
				"MLU-2": {
					UUID: "MLU-2",
					Slot: 2,
					SN:   "sn-1",
				},
				"MLU-3": {
					UUID: "MLU-3",
					Slot: 3,
					SN:   "sn-1",
				},
				"MLU-4": {
					UUID: "MLU-4",
					Slot: 4,
					SN:   "sn-2",
				},
				"MLU-5": {
					UUID: "MLU-5",
					Slot: 5,
					SN:   "sn-2",
				},
				"MLU-6": {
					UUID: "MLU-6",
					Slot: 6,
					SN:   "sn-3",
				},
				"MLU-7": {
					UUID: "MLU-7",
					Slot: 7,
					SN:   "sn-3",
				},
				"MLU-8": {
					UUID: "MLU-8",
					Slot: 8,
					SN:   "sn-4",
				},
				"MLU-9": {
					UUID: "MLU-9",
					Slot: 9,
					SN:   "sn-4",
				},
				"MLU-10": {
					UUID: "MLU-10",
					Slot: 10,
					SN:   "sn-5",
				},
				"MLU-11": {
					UUID: "MLU-11",
					Slot: 11,
					SN:   "sn-5",
				},
				"MLU-12": {
					UUID: "MLU-12",
					Slot: 12,
					SN:   "sn-6",
				},
				"MLU-13": {
					UUID: "MLU-13",
					Slot: 13,
					SN:   "sn-6",
				},
				"MLU-14": {
					UUID: "MLU-14",
					Slot: 14,
					SN:   "sn-7",
				},
				"MLU-15": {
					UUID: "MLU-15",
					Slot: 15,
					SN:   "sn-7",
				},
			}
		})

		DescribeTable("Allocation Devices",
			func(policy string, available []uint, size int, rings []cntopo.Ring, expected []uint) {
				allocator = &boardAllocator{
					policy: policy,
					cntopo: cntopoMock,
					devs:   devsInfo,
					groups: [][]uint{{0, 1, 2, 3, 4, 5, 6, 7}, {8, 9, 10, 11, 12, 13, 14, 15}},
				}
				cntopoMock.EXPECT().GetRings(available, size).Times(1).Return(rings, nil)
				got, err := allocator.Allocate(available, nil, size)
				if expected != nil {
					Expect(err).NotTo(HaveOccurred())
					sort.Slice(got, func(i, j int) bool {
						return got[i] < got[j]
					})
					Expect(got).To(Equal(expected))
				} else {
					Expect(err).To(HaveOccurred())
					Expect(got).To(BeNil())
				}
			},
			Entry("Should Succeed Case 1",
				util.BestEffort,
				[]uint{0, 1, 5},
				1,
				[]cntopo.Ring{},
				[]uint{5},
			),
			Entry("Should Succeed Case 2",
				util.BestEffort,
				[]uint{0, 1, 4},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{1, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
				},
				[]uint{0, 1},
			),
			Entry("Should Succeed Case 3",
				util.BestEffort,
				[]uint{0, 1, 2, 3, 14, 15},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 2},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{2, 3},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{14, 15},
						NonConflictRingNum: 2,
					},
				},
				[]uint{14, 15},
			),
			Entry("Should Succeed Case 4",
				util.BestEffort,
				[]uint{0, 1, 2, 3, 14, 11},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 2},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{2, 3},
						NonConflictRingNum: 2,
					},
				},
				[]uint{0, 1},
			),
			Entry("Should Succeed Case 5",
				util.BestEffort,
				[]uint{0, 1, 4, 6},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 6},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{4, 6},
						NonConflictRingNum: 1,
					},
				},
				[]uint{0, 1},
			),
			Entry("Should Succeed Case 6",
				util.BestEffort,
				[]uint{0, 14},
				2,
				[]cntopo.Ring{},
				[]uint{0, 14},
			),
			Entry("Should Succeed Case 7",
				util.BestEffort,
				[]uint{0, 1, 2, 3, 8, 9, 10},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1, 2, 3},
						NonConflictRingNum: 2,
					},
				},
				[]uint{0, 1, 2, 3},
			),
			Entry("Should Succeed Case 8",
				util.BestEffort,
				[]uint{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
				8,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1, 2, 3, 4, 5, 6, 7},
						NonConflictRingNum: 4,
					},
				},
				[]uint{0, 1, 2, 3, 4, 5, 6, 7},
			),
			Entry("Should Succeed Case 9",
				util.Guaranteed,
				[]uint{0, 1, 2, 3, 8, 9, 10},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1, 2, 3},
						NonConflictRingNum: 2,
					},
				},
				[]uint{0, 1, 2, 3},
			),
			Entry("Should Succeed Case 10",
				util.Guaranteed,
				[]uint{0, 1, 3, 6},
				4,
				[]cntopo.Ring{},
				nil,
			),
			Entry("Should Succeed Case 11",
				util.Guaranteed,
				[]uint{0, 1, 4},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{1, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
				},
				[]uint{0, 1},
			),
			Entry("Should Succeed Case 12",
				util.Guaranteed,
				[]uint{0, 7, 15},
				2,
				[]cntopo.Ring{},
				nil,
			),
			Entry("Should Succeed Case 13",
				util.Restricted,
				[]uint{0, 1, 4},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{1, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
				},
				[]uint{0, 1},
			),
			Entry("Should Succeed Case 14",
				util.Restricted,
				[]uint{1, 4},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{1, 4},
						NonConflictRingNum: 1,
					},
				},
				nil,
			),
		)
	})
})
