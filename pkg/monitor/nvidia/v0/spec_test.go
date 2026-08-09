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

package v0

import (
	"reflect"
	"testing"
)

type specTest struct {
	name     string
	spec     *Spec
	input    int
	expected any
}

func TestSpec_DeviceMax(t *testing.T) {
	tests := []specTest{
		{name: "max devices is 8", spec: &Spec{sr: &sharedRegionT{num: 8}}, expected: 16},
		{name: "max devices is 16", spec: &Spec{sr: &sharedRegionT{num: 16}}, expected: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceMax()
			if actual != tt.expected {
				t.Errorf("DeviceMax() = %d, want %d", actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceNum(t *testing.T) {
	tests := []specTest{
		{name: "device num is 4", spec: &Spec{sr: &sharedRegionT{num: 4}}, expected: 4},
		{name: "device num is 8", spec: &Spec{sr: &sharedRegionT{num: 8}}, expected: 8},
		{name: "num larger than maxDevices is clamped", spec: &Spec{sr: &sharedRegionT{num: 9999}}, expected: maxDevices},
		{name: "high-bit uint64 num is clamped not negative", spec: &Spec{sr: &sharedRegionT{num: 0x8000000000000001}}, expected: maxDevices},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceNum()
			if actual != tt.expected {
				t.Errorf("DeviceNum() = %d, want %d", actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceMemoryContextSize(t *testing.T) {
	tests := []specTest{
		{
			name: "device memory context size for index 1",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{contextSize: 100}, {contextSize: 200}}},
					{status: 1, used: [16]deviceMemory{{contextSize: 300}, {contextSize: 400}}},
				},
			}},
			input:    1,
			expected: uint64(600),
		},
		{
			name: "device memory context size for index 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{contextSize: 100}, {contextSize: 200}}},
					{status: 1, used: [16]deviceMemory{{contextSize: 300}, {contextSize: 400}}},
				},
			}},
			input:    0,
			expected: uint64(400),
		},
		{
			name: "stale proc slot beyond procnum is ignored",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{contextSize: 100}, {contextSize: 200}}},
					{status: 1, used: [16]deviceMemory{{contextSize: 300}, {contextSize: 400}}},
				},
			}},
			input:    1,
			expected: uint64(200),
		},
		{
			name: "dead slot within procnum is excluded",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{contextSize: 200}}},
					{status: 0, used: [16]deviceMemory{{contextSize: 999}}},
				},
			}},
			input:    0,
			expected: uint64(200),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceMemoryContextSize(tt.input)
			if actual != tt.expected {
				t.Errorf("DeviceMemoryContextSize(%d) = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceMemoryModuleSize(t *testing.T) {
	tests := []specTest{
		{
			name: "device memory module size for index 1",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{moduleSize: 100}, {moduleSize: 200}}},
					{status: 1, used: [16]deviceMemory{{moduleSize: 300}, {moduleSize: 400}}},
				},
			}},
			input:    1,
			expected: uint64(600),
		},
		{
			name: "device memory module size for index 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{moduleSize: 100}, {moduleSize: 200}}},
					{status: 1, used: [16]deviceMemory{{moduleSize: 300}, {moduleSize: 400}}},
				},
			}},
			input:    0,
			expected: uint64(400),
		},
		{
			name: "stale proc slot beyond procnum is ignored",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{moduleSize: 100}, {moduleSize: 200}}},
					{status: 1, used: [16]deviceMemory{{moduleSize: 300}, {moduleSize: 400}}},
				},
			}},
			input:    1,
			expected: uint64(200),
		},
		{
			name: "dead slot within procnum is excluded",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{moduleSize: 150}}},
					{status: 0, used: [16]deviceMemory{{moduleSize: 999}}},
				},
			}},
			input:    0,
			expected: uint64(150),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceMemoryModuleSize(tt.input)
			if actual != tt.expected {
				t.Errorf("DeviceMemoryModuleSize(%d) = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceMemoryBufferSize(t *testing.T) {
	tests := []specTest{
		{
			name: "device memory buffer size for index 1",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{bufferSize: 100}, {bufferSize: 200}}},
					{status: 1, used: [16]deviceMemory{{bufferSize: 300}, {bufferSize: 400}}},
				},
			}},
			input:    1,
			expected: uint64(600),
		},
		{
			name: "device memory buffer size for index 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{bufferSize: 100}, {bufferSize: 200}}},
					{status: 1, used: [16]deviceMemory{{bufferSize: 300}, {bufferSize: 400}}},
				},
			}},
			input:    0,
			expected: uint64(400),
		},
		{
			name: "stale proc slot beyond procnum is ignored",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{bufferSize: 100}, {bufferSize: 200}}},
					{status: 1, used: [16]deviceMemory{{bufferSize: 300}, {bufferSize: 400}}},
				},
			}},
			input:    1,
			expected: uint64(200),
		},
		{
			name: "dead slot within procnum is excluded",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{bufferSize: 400}}},
					{status: 0, used: [16]deviceMemory{{bufferSize: 999}}},
				},
			}},
			input:    0,
			expected: uint64(400),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceMemoryBufferSize(tt.input)
			if actual != tt.expected {
				t.Errorf("DeviceMemoryBufferSize(%d) = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceMemoryOffset(t *testing.T) {
	tests := []specTest{
		{
			name: "device memory offset for index 1",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{offset: 100}, {offset: 200}}},
					{status: 1, used: [16]deviceMemory{{offset: 300}, {offset: 400}}},
				},
			}},
			input:    1,
			expected: uint64(600),
		},
		{
			name: "device memory offset for index 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{offset: 100}, {offset: 200}}},
					{status: 1, used: [16]deviceMemory{{offset: 300}, {offset: 400}}},
				},
			}},
			input:    0,
			expected: uint64(400),
		},
		{
			name: "stale proc slot beyond procnum is ignored",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{offset: 100}, {offset: 200}}},
					{status: 1, used: [16]deviceMemory{{offset: 300}, {offset: 400}}},
				},
			}},
			input:    1,
			expected: uint64(200),
		},
		{
			name: "dead slot within procnum is excluded",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{offset: 100}}},
					{status: 0, used: [16]deviceMemory{{offset: 999}}},
				},
			}},
			input:    0,
			expected: uint64(100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceMemoryOffset(tt.input)
			if actual != tt.expected {
				t.Errorf("DeviceMemoryOffset(%d) = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceMemoryTotal(t *testing.T) {
	tests := []specTest{
		{
			name: "device memory total for index 1",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{total: 100}, {total: 200}}},
					{status: 1, used: [16]deviceMemory{{total: 300}, {total: 400}}},
				},
			}},
			input:    1,
			expected: uint64(600),
		},
		{
			name: "device memory total for index 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{total: 100}, {total: 200}}},
					{status: 1, used: [16]deviceMemory{{total: 300}, {total: 400}}},
				},
			}},
			input:    0,
			expected: uint64(400),
		},
		{
			name: "stale proc slot beyond procnum is ignored",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{total: 100}, {total: 200}}},
					{status: 1, used: [16]deviceMemory{{total: 300}, {total: 400}}},
				},
			}},
			input:    1,
			expected: uint64(200),
		},
		{
			name: "dead slot within procnum is excluded",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{total: 512}}},
					{status: 0, used: [16]deviceMemory{{total: 999}}},
				},
			}},
			input:    0,
			expected: uint64(512),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceMemoryTotal(tt.input)
			if actual != tt.expected {
				t.Errorf("DeviceMemoryTotal(%d) = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceSmUtil(t *testing.T) {
	tests := []specTest{
		{
			name: "device sm util for index 1",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, deviceUtil: [16]deviceUtilization{{smUtil: 100}, {smUtil: 200}}},
					{status: 1, deviceUtil: [16]deviceUtilization{{smUtil: 300}, {smUtil: 400}}},
				},
			}},
			input:    1,
			expected: uint64(600),
		},
		{
			name: "device sm util for index 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, deviceUtil: [16]deviceUtilization{{smUtil: 100}, {smUtil: 200}}},
					{status: 1, deviceUtil: [16]deviceUtilization{{smUtil: 300}, {smUtil: 400}}},
				},
			}},
			input:    0,
			expected: uint64(400),
		},
		{
			name: "stale proc slot beyond procnum is ignored",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, deviceUtil: [16]deviceUtilization{{smUtil: 100}, {smUtil: 200}}},
					{status: 1, deviceUtil: [16]deviceUtilization{{smUtil: 300}, {smUtil: 400}}},
				},
			}},
			input:    1,
			expected: uint64(200),
		},
		{
			name: "dead slot within procnum is excluded",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, deviceUtil: [16]deviceUtilization{{smUtil: 60}}},
					{status: 0, deviceUtil: [16]deviceUtilization{{smUtil: 999}}},
				},
			}},
			input:    0,
			expected: uint64(60),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceSmUtil(tt.input)
			if actual != tt.expected {
				t.Errorf("DeviceSmUtil(%d) = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_CorruptProcnumIsClamped(t *testing.T) {
	tests := []struct {
		name     string
		procnum  int32
		expected uint64
	}{
		// Negative procnum clamps to 0 active slots, so nothing is summed.
		{name: "negative procnum", procnum: -5, expected: 0},
		// procnum larger than the backing array clamps to its length; only the
		// single populated slot contributes.
		{name: "procnum over backing array", procnum: 2000, expected: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := &sharedRegionT{num: 2, procnum: tt.procnum}
			sr.procs[0].status = 1
			sr.procs[0].used[0].total = 100
			sr.procs[0].deviceUtil[0].smUtil = 100
			s := Spec{sr: sr}
			// A corrupt procnum must never panic the slice bound.
			if got := s.DeviceMemoryTotal(0); got != tt.expected {
				t.Errorf("DeviceMemoryTotal(0) = %d, want %d", got, tt.expected)
			}
			if got := s.DeviceSmUtil(0); got != tt.expected {
				t.Errorf("DeviceSmUtil(0) = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceProcessCount(t *testing.T) {
	tests := []struct {
		name      string
		spec      *Spec
		deviceIdx int
		want      int
	}{
		{
			name: "no active processes",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 0},
					{status: 0},
				},
			}},
			deviceIdx: 0,
			want:      0,
		},
		{
			name: "one active process with memory on device 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{total: 512 * 1024 * 1024}}},
				},
			}},
			deviceIdx: 0,
			want:      1,
		},
		{
			name: "process active on device 1 only is not counted for device 0",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{}, {total: 256 * 1024 * 1024}}},
				},
			}},
			deviceIdx: 0,
			want:      0,
		},
		{
			name: "dead slot within procnum is excluded",
			spec: &Spec{sr: &sharedRegionT{
				num:     2,
				procnum: 2,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{total: 128 * 1024 * 1024}}},
					{status: 0, used: [16]deviceMemory{{total: 999 * 1024 * 1024}}},
				},
			}},
			deviceIdx: 0,
			want:      1,
		},
		{
			name: "corrupt procnum is clamped and does not panic",
			spec: &Spec{sr: func() *sharedRegionT {
				sr := &sharedRegionT{num: 1, procnum: -99}
				sr.procs[0].status = 1
				sr.procs[0].used[0].total = 64 * 1024 * 1024
				return sr
			}()},
			deviceIdx: 0,
			want:      0,
		},
		{
			name: "context-only slot is counted (total not yet written)",
			spec: &Spec{sr: &sharedRegionT{
				num:     1,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{contextSize: 32 * 1024 * 1024}}},
				},
			}},
			deviceIdx: 0,
			want:      1,
		},
		{
			name: "module-only slot is counted (total not yet written)",
			spec: &Spec{sr: &sharedRegionT{
				num:     1,
				procnum: 1,
				procs: [1024]shrregProcSlotT{
					{status: 1, used: [16]deviceMemory{{moduleSize: 16 * 1024 * 1024}}},
				},
			}},
			deviceIdx: 0,
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.DeviceProcessCount(tt.deviceIdx)
			if got != tt.want {
				t.Errorf("DeviceProcessCount(%d) = %d, want %d", tt.deviceIdx, got, tt.want)
			}
		})
	}
}

func TestDeviceMemoryLimit(t *testing.T) {
	testCases := []struct {
		name          string
		idx           int
		expectedLimit uint64
	}{
		{"Test index 0", 0, 1024},
		{"Test index 1", 1, 2048},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			mockSR := &sharedRegionT{
				limit: [16]uint64{1024, 2048, 3072, 4096},
			}

			s := Spec{
				sr: mockSR,
			}

			limit := s.DeviceMemoryLimit(tc.idx)

			if limit != tc.expectedLimit {
				t.Errorf("DeviceMemoryLimit(%d) = %d; want %d", tc.idx, limit, tc.expectedLimit)
			}
		})
	}
}

func TestSpec_SetDeviceSmLimit(t *testing.T) {
	tests := []specTest{
		{
			name:     "set device sm limit to 1000",
			spec:     &Spec{sr: &sharedRegionT{num: 2}},
			input:    1000,
			expected: [16]uint64{1000, 1000},
		},
		{
			name:     "set device sm limit to 2000",
			spec:     &Spec{sr: &sharedRegionT{num: 3}},
			input:    2000,
			expected: [16]uint64{2000, 2000, 2000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.spec.SetDeviceSmLimit(uint64(tt.input))
			actual := tt.spec.sr.smLimit
			expected, ok := tt.expected.([16]uint64)
			if !ok {
				t.Errorf("TestSpec_SetDeviceSmLimit: type assertion failed for expected value")
			}
			for i := range int(tt.spec.sr.num) {
				if actual[i] != expected[i] {
					t.Errorf("SetDeviceSmLimit(%d) failed: actual[%d] = %d, want %d", tt.input, i, actual[i], expected[i])
				}
			}
		})
	}

	t.Run("num larger than maxDevices does not panic", func(t *testing.T) {
		s := &Spec{sr: &sharedRegionT{num: 9999}}
		s.SetDeviceSmLimit(500)
		want := [16]uint64{500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500}
		if s.sr.smLimit != want {
			t.Errorf("SetDeviceSmLimit with oversized num: got %v, want %v", s.sr.smLimit, want)
		}
	})
}

func TestSpec_IsValidUUID(t *testing.T) {
	tests := []specTest{
		{
			name:     "valid UUID",
			spec:     &Spec{sr: &sharedRegionT{uuids: [16]uuid{{uuid: [96]byte{1}}}}},
			input:    0,
			expected: true,
		},
		{
			name:     "invalid UUID",
			spec:     &Spec{sr: &sharedRegionT{uuids: [16]uuid{{uuid: [96]byte{0}}}}},
			input:    0,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.IsValidUUID(tt.input)
			if actual != tt.expected {
				t.Errorf("IsValidUUID(%d) = %v, want %v", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_DeviceUUID(t *testing.T) {
	tests := []specTest{
		{
			name:     "device UUID for index 0",
			spec:     &Spec{sr: &sharedRegionT{uuids: [16]uuid{{uuid: [96]byte{'a', 'b', 'c', 'd'}}}}},
			input:    0,
			expected: "abcd",
		},
		{
			name:     "device UUID for index 1",
			spec:     &Spec{sr: &sharedRegionT{uuids: [16]uuid{{uuid: [96]byte{'e', 'f', 'g', 'h'}}, {uuid: [96]byte{'i', 'j', 'k', 'l'}}}}},
			input:    1,
			expected: "ijkl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.DeviceUUID(tt.input)
			if actual[:4] != tt.expected {
				t.Errorf("DeviceUUID(%d) = %s, want %s", tt.input, actual[:4], tt.expected)
			}
		})
	}
}

func TestSpec_GetPriority(t *testing.T) {
	tests := []specTest{
		{
			name: "get priority",
			spec: &Spec{
				sr: &sharedRegionT{priority: 5},
			},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.GetPriority()
			if actual != tt.expected {
				t.Errorf("GetPriority() = %d, want %d", actual, tt.expected)
			}
		})
	}
}

func TestSpec_GetRecentKernel(t *testing.T) {
	tests := []specTest{
		{
			name: "get recent kernel",
			spec: &Spec{
				sr: &sharedRegionT{recentKernel: 12345},
			},
			expected: int32(12345),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.GetRecentKernel()
			if actual != tt.expected {
				t.Errorf("GetRecentKernel() = %d, want %d", actual, tt.expected)
			}
		})
	}
}

func TestSpec_SetRecentKernel(t *testing.T) {
	tests := []specTest{
		{
			name: "set recent kernel",
			spec: &Spec{
				sr: &sharedRegionT{},
			},
			input:    67890,
			expected: int32(67890),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.spec.SetRecentKernel(int32(tt.input))
			actual := tt.spec.GetRecentKernel()
			if actual != tt.expected {
				t.Errorf("SetRecentKernel(%d) failed: actual = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_GetUtilizationSwitch(t *testing.T) {
	tests := []specTest{
		{
			name: "get utilization switch",
			spec: &Spec{
				sr: &sharedRegionT{utilizationSwitch: 1},
			},
			expected: int32(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.GetUtilizationSwitch()
			if actual != tt.expected {
				t.Errorf("GetUtilizationSwitch() = %d, want %d", actual, tt.expected)
			}
		})
	}
}

func TestSpec_SetUtilizationSwitch(t *testing.T) {
	tests := []specTest{
		{
			name: "set utilization switch",
			spec: &Spec{
				sr: &sharedRegionT{},
			},
			input:    2,
			expected: int32(2),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.spec.SetUtilizationSwitch(int32(tt.input))
			actual := tt.spec.GetUtilizationSwitch()
			if actual != tt.expected {
				t.Errorf("SetUtilizationSwitch(%d) failed: actual = %d, want %d", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestSpec_SetDeviceMemoryLimit(t *testing.T) {
	tests := []struct {
		name     string
		spec     *Spec
		input    uint64
		expected []uint64
	}{
		{
			name: "set device memory limit",
			spec: &Spec{
				sr: &sharedRegionT{
					num: 3,
					limit: [16]uint64{
						100, 200, 300,
					},
				},
			},
			input:    500,
			expected: []uint64{500, 500, 500},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.spec.SetDeviceMemoryLimit(tt.input)
			actual := tt.spec.sr.limit[:tt.spec.sr.num]
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("SetDeviceMemoryLimit(%d) failed: actual = %v, want %v", tt.input, actual, tt.expected)
			}
		})
	}

	t.Run("num larger than maxDevices does not panic", func(t *testing.T) {
		s := &Spec{sr: &sharedRegionT{num: 9999}}
		s.SetDeviceMemoryLimit(750)
		want := [16]uint64{750, 750, 750, 750, 750, 750, 750, 750, 750, 750, 750, 750, 750, 750, 750, 750}
		if s.sr.limit != want {
			t.Errorf("SetDeviceMemoryLimit with oversized num: got %v, want %v", s.sr.limit, want)
		}
	})
}

func TestSpec_LastKernelTime(t *testing.T) {
	tests := []specTest{
		{
			name: "last kernel time",
			spec: &Spec{
				sr: &sharedRegionT{},
			},
			expected: int64(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.spec.LastKernelTime()
			if actual != tt.expected {
				t.Errorf("LastKernelTime() = %d, want %d", actual, tt.expected)
			}
		})
	}
}
