// Copyright 2021 Cambricon, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package allocator

import (
	"sort"

	"4pd.io/k8s-vgpu/pkg/device-plugin/mlu/cndev"
	"4pd.io/k8s-vgpu/pkg/device-plugin/mlu/cntopo"
	"4pd.io/k8s-vgpu/pkg/util"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Spider Allocator", func() {

	Context("Test allocate", func() {
		var (
			devsInfo  map[string]*cndev.Device
			allocator *spiderAllocator
		)

		BeforeEach(func() {
			devsInfo = map[string]*cndev.Device{
				"MLU-0": {
					UUID:        "MLU-0",
					Slot:        0,
					MotherBoard: "mb-0",
				},
				"MLU-1": {
					UUID:        "MLU-1",
					Slot:        1,
					MotherBoard: "mb-0",
				},
				"MLU-2": {
					UUID:        "MLU-2",
					Slot:        2,
					MotherBoard: "mb-0",
				},
				"MLU-3": {
					UUID:        "MLU-3",
					Slot:        3,
					MotherBoard: "mb-0",
				},
				"MLU-4": {
					UUID:        "MLU-4",
					Slot:        4,
					MotherBoard: "mb-1",
				},
				"MLU-5": {
					UUID:        "MLU-5",
					Slot:        5,
					MotherBoard: "mb-1",
				},
				"MLU-6": {
					UUID:        "MLU-6",
					Slot:        6,
					MotherBoard: "mb-1",
				},
				"MLU-7": {
					UUID:        "MLU-7",
					Slot:        7,
					MotherBoard: "mb-1",
				},
			}
		})

		DescribeTable("Allocation Devices",
			func(policy string, available []uint, size int, rings []cntopo.Ring, expected []uint) {
				allocator = &spiderAllocator{
					policy: policy,
					cntopo: cntopoMock,
					devs:   devsInfo,
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
			Entry("1",
				util.BestEffort,
				[]uint{0, 1, 5},
				1,
				[]cntopo.Ring{},
				[]uint{5},
			),
			Entry("2",
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
			Entry("3",
				util.BestEffort,
				[]uint{0, 1, 2, 3, 4, 6},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 2},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 2},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 3},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{2, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{4, 6},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{2, 6},
						NonConflictRingNum: 1,
					},
				},
				[]uint{4, 6},
			),
			Entry("4",
				util.BestEffort,
				[]uint{0, 1, 4, 6},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{4, 6},
						NonConflictRingNum: 2,
					},
				},
				[]uint{4, 6},
			),
			Entry("5",
				util.BestEffort,
				[]uint{0, 5},
				2,
				[]cntopo.Ring{},
				[]uint{0, 5},
			),
			Entry("6",
				util.BestEffort,
				[]uint{0, 1, 2, 4},
				3,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1, 2},
						NonConflictRingNum: 2,
					},
				},
				[]uint{0, 1, 2},
			),
			Entry("7",
				util.BestEffort,
				[]uint{0, 1, 2, 4, 5, 6, 7},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1, 4, 5},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 2, 4, 6},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{1, 2, 5, 6},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{4, 5, 6, 7},
						NonConflictRingNum: 4,
					},
				},
				[]uint{4, 5, 6, 7},
			),
			Entry("8",
				util.BestEffort,
				[]uint{0, 2, 3, 4, 7},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 3, 4, 7},
						NonConflictRingNum: 2,
					},
				},
				[]uint{0, 3, 4, 7},
			),
			Entry("9",
				util.BestEffort,
				[]uint{0, 1, 3, 6},
				4,
				[]cntopo.Ring{},
				[]uint{0, 1, 3, 6},
			),
			Entry("10",
				util.BestEffort,
				[]uint{0, 1, 3, 4, 5, 7},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{1, 3, 5, 7},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 1, 4, 5},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 3, 4, 7},
						NonConflictRingNum: 2,
					},
				},
				[]uint{1, 3, 5, 7},
			),
			Entry("11",
				util.Guaranteed,
				[]uint{0, 1, 3, 4, 5, 7},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{1, 3, 5, 7},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 1, 4, 5},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 3, 4, 7},
						NonConflictRingNum: 2,
					},
				},
				[]uint{1, 3, 5, 7},
			),
			Entry("12",
				util.Guaranteed,
				[]uint{0, 1, 3, 6},
				4,
				[]cntopo.Ring{},
				nil,
			),
			Entry("13",
				util.Guaranteed,
				[]uint{0, 1, 2, 4},
				3,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1, 2},
						NonConflictRingNum: 2,
					},
				},
				[]uint{0, 1, 2},
			),
			Entry("14",
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
			Entry("15",
				util.Guaranteed,
				[]uint{0, 5},
				2,
				[]cntopo.Ring{},
				nil,
			),
			Entry("16",
				util.Guaranteed,
				[]uint{0, 1, 2, 3, 4, 6},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 2},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 2},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 3},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{2, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{4, 6},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{2, 6},
						NonConflictRingNum: 1,
					},
				},
				[]uint{4, 6},
			),
			Entry("17",
				util.Guaranteed,
				[]uint{0, 1, 4, 6},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{4, 6},
						NonConflictRingNum: 2,
					},
				},
				[]uint{4, 6},
			),
			Entry("18",
				util.Restricted,
				[]uint{0, 1, 2},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 2},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 2},
						NonConflictRingNum: 2,
					},
				},
				[]uint{0, 2},
			),
			Entry("19",
				util.Restricted,
				[]uint{0, 3, 4},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
				},
				nil,
			),
			Entry("20",
				util.Restricted,
				[]uint{0, 1, 2, 3, 4, 6},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 2},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 2},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{1, 3},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{2, 3},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{4, 6},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{2, 6},
						NonConflictRingNum: 1,
					},
				},
				[]uint{4, 6},
			),
			Entry("21",
				util.Restricted,
				[]uint{0, 2, 4, 5},
				2,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 4},
						NonConflictRingNum: 1,
					},
					{
						Ordinals:           []uint{0, 2},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{4, 5},
						NonConflictRingNum: 1,
					},
				},
				[]uint{0, 2},
			),
			Entry("22",
				util.Restricted,
				[]uint{0, 1, 2, 4, 5, 6, 7},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 1, 4, 5},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{0, 2, 4, 6},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{1, 2, 5, 6},
						NonConflictRingNum: 2,
					},
					{
						Ordinals:           []uint{4, 5, 6, 7},
						NonConflictRingNum: 4,
					},
				},
				[]uint{4, 5, 6, 7},
			),
			Entry("23",
				util.Restricted,
				[]uint{0, 2, 3, 4, 7},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 3, 4, 7},
						NonConflictRingNum: 2,
					},
				},
				nil,
			),
			Entry("24",
				util.Restricted,
				[]uint{0, 2, 4, 6},
				4,
				[]cntopo.Ring{
					{
						Ordinals:           []uint{0, 2, 4, 6},
						NonConflictRingNum: 2,
					},
				},
				nil,
			),
		)
	})
})
