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

package nvidia

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
)

// Test_calculateGPUPairScore_asymmetric covers the one-sided NVLink case identified in
// the bug investigation: dev0 has both a PCIe topology entry and an NVLink entry toward
// dev1, but dev1 only has a PCIe topology entry toward dev0. This reflects a real hardware
// or driver state where NVLink state is enabled on only one side.
func Test_calculateGPUPairScore_asymmetric(t *testing.T) {
	dev0 := &Device{
		Index:       0,
		nvlibDevice: nvlibDevice{UUID: "gpu0"},
		Links: map[int][]P2PLink{
			1: {
				{Type: P2PLinkSameCPU},  // PCIe topology entry
				{Type: FourNVLINKLinks}, // NVLink entry — only present on this side
			},
		},
	}
	dev1 := &Device{
		Index:       1,
		nvlibDevice: nvlibDevice{UUID: "gpu1"},
		Links: map[int][]P2PLink{
			0: {
				{Type: P2PLinkSameCPU}, // PCIe topology entry only — NVLink absent
			},
		},
	}

	var score int
	var asymmetric bool
	assert.NilError(t, func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("unexpected panic: %v", r)
			}
		}()
		score, asymmetric = calculateGPUPairScore(dev0, dev1)
		return nil
	}())

	assert.Equal(t, score, 0, "asymmetric pair must score 0")
	assert.Equal(t, asymmetric, true, "asymmetric flag must be set")
}

func Test_CalculateGPUScore(t *testing.T) {
	score, _, err := CalculateGPUScore([]string{"GPU-ebe7c3f7-303d-558d-435e-99a160631fe4"})
	t.Log(score)
	t.Log(err)
}

//	Test_calculateGPUScore.
//	      gpu_0 gpu_1 gpu_2 gpu_3.
//
// gpu_0   N-A   NV1   NV1   NV2.
// gpu_1   NV1   N-A   NV2   NV1.
// gpu_2   NV1   NV2   N-A   NV2.
// gpu_3   NV2   NV1   NV2   N-A.
func Test_calculateGPUScore(t *testing.T) {
	tests := []struct {
		name string
		args []*Device
		want ListDeviceScore
	}{
		{
			name: "nvlink test",
			args: []*Device{
				{
					Index: 0,
					nvlibDevice: nvlibDevice{
						UUID: "gpu0",
					},
					Links: map[int][]P2PLink{
						1: {{Type: SingleNVLINKLink}},
						2: {{Type: SingleNVLINKLink}},
						3: {{Type: TwoNVLINKLinks}},
					},
				},
				{
					Index: 1,
					nvlibDevice: nvlibDevice{
						UUID: "gpu1",
					},
					Links: map[int][]P2PLink{
						0: {{Type: SingleNVLINKLink}},
						2: {{Type: TwoNVLINKLinks}},
						3: {{Type: SingleNVLINKLink}},
					},
				},
				{
					Index: 2,
					nvlibDevice: nvlibDevice{
						UUID: "gpu2",
					},
					Links: map[int][]P2PLink{
						0: {{Type: SingleNVLINKLink}},
						1: {{Type: TwoNVLINKLinks}},
						3: {{Type: TwoNVLINKLinks}},
					},
				},
				{
					Index: 3,
					nvlibDevice: nvlibDevice{
						UUID: "gpu3",
					},
					Links: map[int][]P2PLink{
						0: {{Type: TwoNVLINKLinks}},
						1: {{Type: SingleNVLINKLink}},
						2: {{Type: TwoNVLINKLinks}},
					},
				},
			},
			want: ListDeviceScore{
				{
					UUID:  "gpu0",
					Score: map[string]int{"gpu1": 100, "gpu2": 100, "gpu3": 200},
				},
				{
					UUID:  "gpu1",
					Score: map[string]int{"gpu0": 100, "gpu2": 200, "gpu3": 100},
				},
				{
					UUID:  "gpu2",
					Score: map[string]int{"gpu0": 100, "gpu1": 200, "gpu3": 200},
				},
				{
					UUID:  "gpu3",
					Score: map[string]int{"gpu0": 200, "gpu1": 100, "gpu2": 200},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scoreList := make(ListDeviceScore, len(test.args))
			for i, gpuI := range test.args {
				score := make(map[string]int)
				for j, gpuJ := range test.args {
					if i == j {
						continue
					}
					s, _ := calculateGPUPairScore(gpuI, gpuJ)
					score[gpuJ.UUID] = s
				}
				scoreList[i] = DeviceScore{
					UUID:  gpuI.UUID,
					Score: score,
				}
			}
			t.Log(scoreList)
			assert.DeepEqual(t, scoreList, test.want)
		})
	}
}
