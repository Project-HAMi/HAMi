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

package policy

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// The NVIDIA device plugin registers a card's model name as its device type
// ("NVIDIA A100-SXM4-40GB"), and Cambricon takes the type off the node's Model
// label ("MLU370-X8"), while the request generated for a container carries the
// vendor common word ("NVIDIA", "MLU"). ComputeScore must still add the pending
// request to the device score; otherwise cards are ranked by their current
// utilisation instead of their utilisation after placement.
func TestComputeScoreCountsRequestForModelNamedDeviceTypes(t *testing.T) {
	tests := []struct {
		name        string
		deviceType  string
		requestType string
	}{
		{name: "nvidia model name", deviceType: "NVIDIA A100-SXM4-40GB", requestType: "NVIDIA"},
		{name: "cambricon model name", deviceType: "MLU370-X8", requestType: "MLU"},
		{name: "type equal to the common word", deviceType: "NVIDIA", requestType: "NVIDIA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &DeviceListsScore{Device: &device.DeviceUsage{
				ID:        "card-0",
				Type:      tt.deviceType,
				Count:     10,
				Totalcore: 100,
				Totalmem:  40960,
			}}
			requests := device.ContainerDeviceRequests{
				tt.requestType: {
					Nums:             1,
					Type:             tt.requestType,
					Memreq:           20480,
					MemPercentagereq: 101,
					Coresreq:         30,
				},
			}
			ds.ComputeScore(requests, util.DefaultDeviceScoringWeights())

			// slots 1/10 + cores 30/100 + memory 20480/40960 = 0.9, scaled by util.Weight.
			want := float32(util.Weight) * 0.9
			if ds.Score != want {
				t.Fatalf("ComputeScore() = %v, want %v (request was dropped from the score)", ds.Score, want)
			}
		})
	}
}

// A request for a different vendor must not contribute to this device's score.
func TestComputeScoreIgnoresOtherVendorRequests(t *testing.T) {
	ds := &DeviceListsScore{Device: &device.DeviceUsage{
		ID:        "card-0",
		Type:      "NVIDIA A100-SXM4-40GB",
		Count:     10,
		Totalcore: 100,
		Totalmem:  40960,
	}}
	requests := device.ContainerDeviceRequests{
		"MLU": {Nums: 1, Type: "MLU", Memreq: 20480, MemPercentagereq: 101, Coresreq: 30},
	}
	ds.ComputeScore(requests, util.DefaultDeviceScoringWeights())
	if ds.Score != 0 {
		t.Fatalf("ComputeScore() = %v, want 0 for a request of another vendor", ds.Score)
	}
}

// Registered device types nest: the chart ships both "Ascend910B4" and
// "Ascend910B4-1" as Ascend common words, and the longer one contains the
// shorter. Matching on a substring means a pod requesting both types would see
// both requests land on one device unless the score picks a single type, which
// comparing types for equality used to guarantee.
func TestComputeScoreScoresNestedDeviceTypesOnce(t *testing.T) {
	requests := device.ContainerDeviceRequests{
		"Ascend910B4": {
			Nums: 1, Type: "Ascend910B4",
			Memreq: 8192, MemPercentagereq: 101, Coresreq: 10,
		},
		"Ascend910B4-1": {
			Nums: 1, Type: "Ascend910B4-1",
			Memreq: 4096, MemPercentagereq: 101, Coresreq: 20,
		},
	}

	// The more specific type wins on the device that carries it.
	specific := &DeviceListsScore{Device: &device.DeviceUsage{
		ID: "npu-0", Type: "Ascend910B4-1",
		Count: 10, Totalcore: 100, Totalmem: 40960,
	}}
	specific.ComputeScore(requests, util.DefaultDeviceScoringWeights())
	// slots 1/10 + cores 20/100 + memory 4096/40960 = 0.4
	if want := float32(util.Weight) * 0.4; specific.Score != want {
		t.Errorf("Ascend910B4-1 device: ComputeScore() = %v, want %v (both requests scored?)", specific.Score, want)
	}

	// A device carrying only the shorter type is unaffected by the longer one.
	broad := &DeviceListsScore{Device: &device.DeviceUsage{
		ID: "npu-1", Type: "Ascend910B4",
		Count: 10, Totalcore: 100, Totalmem: 40960,
	}}
	broad.ComputeScore(requests, util.DefaultDeviceScoringWeights())
	// slots 1/10 + cores 10/100 + memory 8192/40960 = 0.4
	if want := float32(util.Weight) * 0.4; broad.Score != want {
		t.Errorf("Ascend910B4 device: ComputeScore() = %v, want %v", broad.Score, want)
	}
}

// Two containers requesting the same device type must still accumulate onto one
// device; restricting the score to a single *type* must not restrict it to a
// single request.
func TestComputeScoreStillAggregatesRequestsOfOneType(t *testing.T) {
	ds := &DeviceListsScore{Device: &device.DeviceUsage{
		ID: "gpu-0", Type: "NVIDIA A100-SXM4-40GB",
		Count: 10, Totalcore: 100, Totalmem: 40960,
	}}
	requests := device.ContainerDeviceRequests{
		"container1": {Nums: 1, Type: "NVIDIA", Memreq: 4096, MemPercentagereq: 101, Coresreq: 10},
		"container2": {Nums: 1, Type: "NVIDIA", Memreq: 8192, MemPercentagereq: 101, Coresreq: 20},
	}
	ds.ComputeScore(requests, util.DefaultDeviceScoringWeights())
	// slots 2/10 + cores 30/100 + memory 12288/40960 = 0.8
	if want := float32(util.Weight) * 0.8; ds.Score != want {
		t.Errorf("ComputeScore() = %v, want %v", ds.Score, want)
	}
}
