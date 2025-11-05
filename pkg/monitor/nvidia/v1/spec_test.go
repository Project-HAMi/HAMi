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

package v1

import (
	"testing"

	"gotest.tools/v3/assert"
)

func Test_DeviceMax(t *testing.T) {
	tests := []struct {
		name string
		args *Spec
		want int
	}{
		{
			name: "device max is 8",
			args: &Spec{
				sr: &sharedRegionT{
					num: 8,
				},
			},
			want: maxDevices,
		},
		{
			name: "device max is 16",
			args: &Spec{
				sr: &sharedRegionT{
					num: 16,
				},
			},
			want: maxDevices,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := Spec{}
			result := s.DeviceMax()
			if result != test.want {
				t.Errorf("DeviceMax is %d, want is %d", result, test.want)
			}
		})
	}
}

func Test_DeviceNum(t *testing.T) {
	tests := []struct {
		name string
		args *Spec
		want int
	}{
		{
			name: "device num is 2",
			args: &Spec{
				sr: &sharedRegionT{
					num: 2,
				},
			},
			want: int(2),
		},
		{
			name: "device num is 4",
			args: &Spec{
				sr: &sharedRegionT{
					num: 4,
				},
			},
			want: int(4),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := Spec{
				sr: &sharedRegionT{
					num: test.args.sr.num,
				},
			}
			result := s.DeviceNum()
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_DeviceMemoryContextSize(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want uint64
	}{
		{
			name: "device memory context size for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(0),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										contextSize: 100,
									},
									{
										contextSize: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										contextSize: 100,
									},
									{
										contextSize: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(200),
		},
		{
			name: "device memory context size for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(1),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										contextSize: 100,
									},
									{
										contextSize: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										contextSize: 100,
									},
									{
										contextSize: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(400),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceMemoryContextSize(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_DeviceMemoryModuleSize(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want uint64
	}{
		{
			name: "device memory module size for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(0),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										moduleSize: 100,
									},
									{
										moduleSize: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										moduleSize: 100,
									},
									{
										moduleSize: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(200),
		},
		{
			name: "device memory module size for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(1),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										moduleSize: 100,
									},
									{
										moduleSize: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										moduleSize: 100,
									},
									{
										moduleSize: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(400),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceMemoryModuleSize(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_DeviceMemoryBufferSize(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want uint64
	}{
		{
			name: "device memory buffer size for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(0),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										bufferSize: 100,
									},
									{
										bufferSize: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										bufferSize: 100,
									},
									{
										bufferSize: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(200),
		},
		{
			name: "device memory module size for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(1),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										bufferSize: 100,
									},
									{
										bufferSize: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										bufferSize: 100,
									},
									{
										bufferSize: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(400),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceMemoryBufferSize(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_DeviceMemoryOffset(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want uint64
	}{
		{
			name: "device memory offset for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(0),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										offset: 100,
									},
									{
										offset: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										offset: 100,
									},
									{
										offset: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(200),
		},
		{
			name: "device memory offset for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(1),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										offset: 100,
									},
									{
										offset: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										offset: 100,
									},
									{
										offset: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(400),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceMemoryOffset(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_DeviceMemoryTotal(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want uint64
	}{
		{
			name: "device memory total for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(0),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										total: 100,
									},
									{
										total: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										total: 100,
									},
									{
										total: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(200),
		},
		{
			name: "device memory total for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(1),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								used: [16]deviceMemory{
									{
										total: 100,
									},
									{
										total: 200,
									},
								},
							},
							{
								used: [16]deviceMemory{
									{
										total: 100,
									},
									{
										total: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(400),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceMemoryTotal(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_DeviceSmUtil(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want uint64
	}{
		{
			name: "device sm util for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(0),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								deviceUtil: [16]deviceUtilization{
									{
										smUtil: 100,
									},
									{
										smUtil: 200,
									},
								},
							},
							{
								deviceUtil: [16]deviceUtilization{
									{
										smUtil: 100,
									},
									{
										smUtil: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(200),
		},
		{
			name: "device sm util for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: int(1),
				spec: &Spec{
					sr: &sharedRegionT{
						procnum: 2,
						procs: [1024]shrregProcSlotT{
							{
								deviceUtil: [16]deviceUtilization{
									{
										smUtil: 100,
									},
									{
										smUtil: 200,
									},
								},
							},
							{
								deviceUtil: [16]deviceUtilization{
									{
										smUtil: 100,
									},
									{
										smUtil: 200,
									},
								},
							},
						},
					},
				},
			},
			want: uint64(400),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceSmUtil(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_SetDeviceSmLimit(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			l    uint64
			spec *Spec
		}
		want [16]uint64
	}{
		{
			name: "set device sm limit to 300",
			args: struct {
				l    uint64
				spec *Spec
			}{
				l: uint64(300),
				spec: &Spec{
					sr: &sharedRegionT{
						num:     2,
						smLimit: [16]uint64{},
					},
				},
			},
			want: [16]uint64{300, 300},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			s.SetDeviceSmLimit(test.args.l)
			result := test.args.spec.sr.smLimit
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_IsValidUUID(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want bool
	}{
		{
			name: "set valid uuid",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: 0,
				spec: &Spec{
					sr: &sharedRegionT{
						uuids: [16]uuid{
							{
								uuid: [96]byte{
									1,
								},
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "set invalid uuid",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: 0,
				spec: &Spec{
					sr: &sharedRegionT{
						uuids: [16]uuid{
							{
								uuid: [96]byte{
									0,
								},
							},
						},
					},
				},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.IsValidUUID(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_DeviceUUID(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want string
	}{
		{
			name: "device uuid for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: 0,
				spec: &Spec{
					sr: &sharedRegionT{
						uuids: [16]uuid{
							{
								uuid: [96]byte{
									'a', '1', 'b', '2',
								},
							},
						},
					},
				},
			},
			want: "a1b2",
		},
		{
			name: "device uuid for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: 1,
				spec: &Spec{
					sr: &sharedRegionT{
						uuids: [16]uuid{
							{
								uuid: [96]byte{
									'a', '1', 'b', '2',
								},
							},
							{
								uuid: [96]byte{
									'c', '3', 'd', '4',
								},
							},
						},
					},
				},
			},
			want: "c3d4",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceUUID(test.args.idx)
			assert.Equal(t, result[:4], test.want)
		})
	}
}

func Test_DeviceMemoryLimit(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			idx  int
			spec *Spec
		}
		want uint64
	}{
		{
			name: "device memory limit for idx 0",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: 0,
				spec: &Spec{
					sr: &sharedRegionT{
						limit: [16]uint64{
							100,
						},
					},
				},
			},
			want: uint64(100),
		},
		{
			name: "device memory limit for idx 1",
			args: struct {
				idx  int
				spec *Spec
			}{
				idx: 1,
				spec: &Spec{
					sr: &sharedRegionT{
						limit: [16]uint64{
							100, 200,
						},
					},
				},
			},
			want: uint64(200),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			result := s.DeviceMemoryLimit(test.args.idx)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_SetDeviceMemoryLimit(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			l    uint64
			spec *Spec
		}
		want [16]uint64
	}{
		{
			name: "set device memory limit to 1024",
			args: struct {
				l    uint64
				spec *Spec
			}{
				l: uint64(1024),
				spec: &Spec{
					sr: &sharedRegionT{
						num: 1,
					},
				},
			},
			want: [16]uint64{1024},
		},
		{
			name: "set device memory limit to 2048",
			args: struct {
				l    uint64
				spec *Spec
			}{
				l: uint64(2048),
				spec: &Spec{
					sr: &sharedRegionT{
						num: 2,
					},
				},
			},
			want: [16]uint64{2048, 2048},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			s.SetDeviceMemoryLimit(test.args.l)
			result := test.args.spec.sr.limit
			assert.DeepEqual(t, result, test.want)
		})
	}
}

func Test_LastKernelTime(t *testing.T) {
	tests := []struct {
		name string
		args *Spec
		want int64
	}{
		{
			name: "last kernel time",
			args: &Spec{
				sr: &sharedRegionT{
					lastKernelTime: int64(1234),
				},
			},
			want: int64(1234),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args
			result := s.LastKernelTime()
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GetPriority(t *testing.T) {
	tests := []struct {
		name string
		args Spec
		want int
	}{
		{
			name: "get priority",
			args: Spec{
				sr: &sharedRegionT{
					priority: int32(1),
				},
			},
			want: int(1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args
			result := s.GetPriority()
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GetRecentKernel(t *testing.T) {
	tests := []struct {
		name string
		args Spec
		want int32
	}{
		{
			name: "get recent kernel",
			args: Spec{
				sr: &sharedRegionT{
					recentKernel: int32(1234),
				},
			},
			want: int32(1234),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args
			result := s.GetRecentKernel()
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_SetRecentKernel(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			v    int32
			spec Spec
		}
		want int32
	}{
		{
			name: "get recent kernel",
			args: struct {
				v    int32
				spec Spec
			}{
				v: int32(1111),
				spec: Spec{
					sr: &sharedRegionT{},
				},
			},
			want: int32(1111),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := test.args.spec
			s.SetRecentKernel(test.args.v)
			result := test.args.spec.sr.recentKernel
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_GetUtilizationSwitch(t *testing.T) {
	tests := []struct {
		name string
		args Spec
		want int32
	}{
		{
			name: "get utilization switch",
			args: Spec{
				sr: &sharedRegionT{
					utilizationSwitch: int32(1234),
				},
			},
			want: int32(1234),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.args.GetUtilizationSwitch()
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_SetUtilizationSwitch(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			v    int32
			spec Spec
		}
		want int32
	}{
		{
			name: "set utilization switch",
			args: struct {
				v    int32
				spec Spec
			}{
				v: int32(3333),
				spec: Spec{
					sr: &sharedRegionT{},
				},
			},
			want: int32(3333),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.args.spec.SetUtilizationSwitch(test.args.v)
			result := test.args.spec.sr.utilizationSwitch
			assert.Equal(t, result, test.want)
		})
	}
}
