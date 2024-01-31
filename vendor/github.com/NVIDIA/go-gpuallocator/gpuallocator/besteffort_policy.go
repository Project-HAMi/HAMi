// Copyright (c) 2019, NVIDIA CORPORATION. All rights reserved.

package gpuallocator

import (
	"fmt"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
)

type bestEffortPolicy struct{}

// NewBestEffortPolicy creates a new BestEffortPolicy.
func NewBestEffortPolicy() Policy {
	return &bestEffortPolicy{}
}

//  Allocate finds the best set of 'size' GPUs to allocate from a list of
//  available GPU devices and returns them. The algorithm is designed to
//  ensure that a list of 'required' GPU devices is present in the final
//  allocation.
//
//  This algorithm considers all possible sets of GPUs of size 'size'.
//  However, it does not settle for the greedy solution of looking for the
//  single set of size 'size' with the highest score. Instead, it looks for a
//  solution that maximizes the total score when dividing up all available
//  GPUs on the node into sets of size 'size' and then summing their
//  individual scores. It then returns the set of GPUs from that grouping
//  with the highest individual score.
//
//  Such a solution is necessary in the general case because of the
//  non-hierarchical nature of the various links that influence the score
//  calculated for each pair of GPUs.
func (p *bestEffortPolicy) Allocate(available []*Device, required []*Device, size int) []*Device {
	if size <= 0 {
		return []*Device{}
	}

	if len(available) < size {
		return []*Device{}
	}

	if len(required) > size {
		return []*Device{}
	}

	// Find the highest scoring GPU partition with sets of of size 'size'.
	// Don't consider partitions that don't have at least one set that contains
	// all of the GPUs 'required' by the allocation.
	bestPartition := [][]*Device(nil)
	bestScore := 0
	iterateGPUPartitions(available, size, func(candidate [][]*Device) {
		if !gpuPartitionContainsSetWithAll(candidate, required) {
			return
		}
		score := calculateGPUPartitionScore(candidate)
		if score > bestScore || bestPartition == nil {
			bestPartition = candidate
			bestScore = score
		}
	})

	// Filter the 'bestPartition' to only include sets containing all of the
	// 'required' devices (which may be nil so all sets will be valid).
	filteredBestPartition := [][]*Device{}
	for _, set := range bestPartition {
		if gpuSetContainsAll(set, required) {
			filteredBestPartition = append(filteredBestPartition, set)
		}
	}

	if len(filteredBestPartition) == 0 {
		return []*Device{}
	}

	// Find the highest scoring GPU set in the highest scoring GPU partition.
	bestSet := filteredBestPartition[0]
	bestScore = calculateGPUSetScore(bestSet)
	for i := 1; i < len(filteredBestPartition); i++ {
		score := calculateGPUSetScore(filteredBestPartition[i])
		if score > bestScore {
			bestSet = filteredBestPartition[i]
			bestScore = score
		}
	}

	// Return the highest scoring GPU set.
	return bestSet
}

// Check to see if a specific GPU is contained in a GPU set.
func gpuSetContains(gpuSet []*Device, gpu *Device) bool {
	for i := range gpuSet {
		if gpuSet[i] == gpu {
			return true
		}
	}
	return false
}

// Check to see if an entire subset of GPUs is contained in a GPU set.
func gpuSetContainsAll(gpuSet []*Device, gpuSubset []*Device) bool {
	for _, gpu := range gpuSubset {
		if !gpuSetContains(gpuSet, gpu) {
			return false
		}
	}
	return true
}

// Check to see if 'gpuPartition' has at least one set containing all 'gpuSubset' devices and no padding.
func gpuPartitionContainsSetWithAll(gpuPartition [][]*Device, gpuSubset []*Device) bool {
	for _, gpuSet := range gpuPartition {
		if gpuSetContainsAll(gpuSet, gpuSubset) && gpuSetCountPadding(gpuSet) == 0 {
			return true
		}
	}
	return false
}

// Copy a GPU set and add padding to it.
//
// Pad the list of available GPUs on the node such that the list can be evenly
// partitioned into subsets of size 'size'. This is necessary to ensure that the
// recursive solution does not exit early and actually considers all possible
// sets when comparing scores between them.
func gpuSetCopyAndAddPadding(gpuSet []*Device, size int) []*Device {
	if size <= 0 {
		return []*Device{}
	}

	gpus := append([]*Device{}, gpuSet...)
	for len(gpus)%size != 0 {
		gpus = append(gpus, nil)
	}

	return gpus
}

// Count the amount of padding in the GPU set.
func gpuSetCountPadding(gpuSet []*Device) int {
	count := 0

	for i := range gpuSet {
		if gpuSet[i] == nil {
			count++
		}
	}

	return count
}

// Iterate through all GPU sets of size 'size', applying a callback function to them.
// This function is implemented using an iterative solution for efficiency.
func iterateGPUSets(devices []*Device, size int, callback func([]*Device)) {
	if size <= 0 {
		return
	}

	if size > len(devices) {
		return
	}

	// The logic below is a simple unrolling of the recursive loops:
	//
	// n := len(devices)
	// for i := 0; i < n; i++
	//     for j := i+1; j < n; j++
	//         for k := j+1; k < n; k++
	//             ...
	//             for z := y+1; z < n; z++
	//                 callback({devices[i], devices[j], devices[k], ..., devices[z]})
	//
	// Where 'size' represents how many logical 'for' loops there are, 'level'
	// represents how many 'for' loops deep we are, 'indices' holds the loop
	// index at each level, and 'set' builds out the list of devices to pass to
	// the callback each time the bottom most level is reached.
	level := 0
	indices := make([]int, size)
	set := make([]*Device, size)

	for {
		if indices[level] == len(devices) {
			if level == 0 {
				break
			}

			level--
			indices[level]++
			continue
		}

		set[level] = devices[indices[level]]

		if level < (size - 1) {
			level++
			indices[level] = indices[level-1] + 1
			continue
		}

		callback(set)
		indices[level]++
	}
}

// Iterate through all possible partitions of the available GPU devices into
// sets of size 'size'. This function walks recursively through each possible
// partition and applies a callback function to it.
func iterateGPUPartitions(devices []*Device, size int, callback func([][]*Device)) {
	if size <= 0 {
		return
	}

	if size > len(devices) {
		return
	}

	// Optimize for the case when size == 1.
	if size == 1 {
		for _, device := range devices {
			callback([][]*Device{[]*Device{device}})
		}
		return
	}

	// Otherwise, pad the list of available GPUs on the node such that the list
	// can be evenly partitioned into subsets of size 'size'. This is necessary
	// to ensure that the recursive solution does not exit early and actually
	// considers all possible sets when comparing scores between them. We use
	// the amount of expected padding to prune the search space of possible
	// partitions as described in the comments below.
	devices = gpuSetCopyAndAddPadding(devices, size)
	padding := gpuSetCountPadding(devices)

	// We wrap the recursive call to make use of an 'accum' variable to
	// build out each partition as the recursion progresses.
	var iterate func(devices []*Device, size int, accum [][]*Device)
	iterate = func(devices []*Device, size int, accum [][]*Device) {
		// Padding should ensure that his never happens.
		if size > len(devices) {
			panic("Internal error in best effort allocation policy")
		}

		// Base case once we've reached 'size' number of devices.
		if size == len(devices) {
			callback(append(accum, devices))
			return
		}

		// For all other sizes and device lengths ...
		//
		// The code below is optimized to avoid considering duplicate
		// partitions, e.g. [[0,1],[2,3]] and [[2,3],[0,1]].
		//
		// It does this by not directly calling
		//     iterateGPUSets(devices, size, func(set []*Device)
		// to iterate over all possible GPU sets of size 'size' in 'devices'.
		//
		// Instead, it pulls out device[0], calls
		//     iterateGPUSets(devices[1:], size-1, func(set []*Device)
		// and adds device[0] back into each resulting set of size 'size-1'.
		//
		// This ensures that the _first_ device index of each set in a
		// partition is in increaing order, e.g. [[0...], [4...], [7...]] and
		// never [[0...], [7...], [4...].
		iterateGPUSets(devices[1:], size-1, func(set []*Device) {
			set = append([]*Device{devices[0]}, set...)

			// Only consider sets that either contain the full padding or no
			// padding at all. This helps us avoid situations, such as considering
			// the set '[[0 1 2 3 <nil>], [4 5 6 7 <nil>]]' as a candidate for
			// allocating 5 GPUs from a set of 8.
			p := gpuSetCountPadding(set)
			if !(p == 0 || p == padding) {
				return
			}

			remaining := []*Device{}
			for _, gpu := range devices {
				if !gpuSetContains(set, gpu) {
					remaining = append(remaining, gpu)
				}
			}

			iterate(remaining, size, append(accum, set))
		})
	}

	iterate(devices, size, [][]*Device{})
}

// Calculate a "link" score for a pair of GPUs.
// The score is based on the "closeness" of the two GPUs in relation to one
// another in terms of the communication links they have with another, as well
// as the PCIe hierarchy they are in. GPUs connected by an NVLINK receive 100
// points for each link connecting them. GPUs in the PCIe hierarchy receive
// points relative to how close they are to one another.
func calculateGPUPairScore(gpu0 *Device, gpu1 *Device) int {
	if gpu0 == nil || gpu1 == nil {
		return 0
	}

	if gpu0 == gpu1 {
		return 0
	}

	if len(gpu0.Links[gpu1.Index]) != len(gpu1.Links[gpu0.Index]) {
		err := fmt.Errorf("Internal error in bestEffort GPU allocator: all P2PLinks between 2 GPUs should be bidirectional")
		panic(err)
	}

	score := 0

	for _, link := range gpu0.Links[gpu1.Index] {
		switch link.Type {
		case nvml.P2PLinkCrossCPU:
			score += 10
		case nvml.P2PLinkSameCPU:
			score += 20
		case nvml.P2PLinkHostBridge:
			score += 30
		case nvml.P2PLinkMultiSwitch:
			score += 40
		case nvml.P2PLinkSingleSwitch:
			score += 50
		case nvml.P2PLinkSameBoard:
			score += 60
		case nvml.SingleNVLINKLink:
			score += 100
		case nvml.TwoNVLINKLinks:
			score += 200
		case nvml.ThreeNVLINKLinks:
			score += 300
		case nvml.FourNVLINKLinks:
			score += 400
		case nvml.FiveNVLINKLinks:
			score += 500
		case nvml.SixNVLINKLinks:
			score += 600
		case nvml.SevenNVLINKLinks:
			score += 700
		case nvml.EightNVLINKLinks:
			score += 800
		case nvml.NineNVLINKLinks:
			score += 900
		case nvml.TenNVLINKLinks:
			score += 1000
		case nvml.ElevenNVLINKLinks:
			score += 1100
		case nvml.TwelveNVLINKLinks:
			score += 1200
		}
	}

	return score
}

// Get the total score of a set of GPUs. The score is calculated as the sum of
// the scores calculated for each pair of GPUs in the set.
func calculateGPUSetScore(gpuSet []*Device) int {
	score := 0

	iterateGPUSets(gpuSet, 2, func(gpus []*Device) {
		score += calculateGPUPairScore(gpus[0], gpus[1])
	})

	return score
}

// Get the total score of a GPU partition. The score is calculated as the sum
// of the scores calculated for each set of GPUs within the partition.
func calculateGPUPartitionScore(gpuPartition [][]*Device) int {
	score := 0

	for _, gpuSet := range gpuPartition {
		score += calculateGPUSetScore(gpuSet)
	}

	return score
}
