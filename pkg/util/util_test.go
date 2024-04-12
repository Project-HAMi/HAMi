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

package util

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
)

var inRequestDevices map[string]string

func init() {
	inRequestDevices = make(map[string]string)
	inRequestDevices["NVIDIA"] = "hami.io/vgpu-devices-to-allocate"
}

func TestEmptyContainerDevicesCoding(t *testing.T) {
	cd1 := ContainerDevices{}
	s := EncodeContainerDevices(cd1)
	fmt.Println(s)
	cd2, _ := DecodeContainerDevices(s)
	assert.DeepEqual(t, cd1, cd2)
}

func TestEmptyPodDeviceCoding(t *testing.T) {
	pd1 := PodDevices{}
	s := EncodePodDevices(inRequestDevices, pd1)
	fmt.Println(s)
	pd2, _ := DecodePodDevices(inRequestDevices, s)
	assert.DeepEqual(t, pd1, pd2)
}

func TestPodDevicesCoding(t *testing.T) {
	tests := []struct {
		name string
		args PodDevices
	}{
		{
			name: "one pod one container use zero device",
			args: PodDevices{
				"NVIDIA": PodSingleDevice{},
			},
		},
		{
			name: "one pod one container use one device",
			args: PodDevices{
				"NVIDIA": PodSingleDevice{
					ContainerDevices{
						ContainerDevice{0, "UUID1", "Type1", 1000, 30},
					},
				},
			},
		},
		{
			name: "one pod two container, every container use one device",
			args: PodDevices{
				"NVIDIA": PodSingleDevice{
					ContainerDevices{
						ContainerDevice{0, "UUID1", "Type1", 1000, 30},
					},
					ContainerDevices{
						ContainerDevice{0, "UUID1", "Type1", 1000, 30},
					},
				},
			},
		},
		{
			name: "one pod one container use two devices",
			args: PodDevices{
				"NVIDIA": PodSingleDevice{
					ContainerDevices{
						ContainerDevice{0, "UUID1", "Type1", 1000, 30},
						ContainerDevice{0, "UUID2", "Type1", 1000, 30},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := EncodePodDevices(inRequestDevices, test.args)
			fmt.Println(s)
			got, _ := DecodePodDevices(inRequestDevices, s)
			assert.DeepEqual(t, test.args, got)
		})
	}
}

func Test_DecodePodDevices(t *testing.T) {
	//DecodePodDevices(checklist map[string]string, annos map[string]string) (PodDevices, error)
	InRequestDevices["NVIDIA"] = "hami.io/vgpu-devices-to-allocate"
	SupportDevices["NVIDIA"] = "hami.io/vgpu-devices-allocated"
	tests := []struct {
		name string
		args struct {
			checklist map[string]string
			annos     map[string]string
		}
		want    PodDevices
		wantErr error
	}{
		{
			name: "annos len is 0",
			args: struct {
				checklist map[string]string
				annos     map[string]string
			}{
				checklist: map[string]string{},
				annos:     make(map[string]string),
			},
			want:    PodDevices{},
			wantErr: nil,
		},
		{
			name: "annos having two device",
			args: struct {
				checklist map[string]string
				annos     map[string]string
			}{
				checklist: InRequestDevices,
				annos: map[string]string{
					InRequestDevices["NVIDIA"]: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76,NVIDIA,500,3:;GPU-ebe7c3f7-303d-558d-435e-99a160631fe4,NVIDIA,500,3:;",
					SupportDevices["NVIDIA"]:   "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76,NVIDIA,500,3:;GPU-ebe7c3f7-303d-558d-435e-99a160631fe4,NVIDIA,500,3:;",
				},
			},
			want: PodDevices{
				"NVIDIA": {
					{
						{
							UUID:      "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
							Type:      "NVIDIA",
							Usedmem:   500,
							Usedcores: 3,
						},
					},
					{
						{
							UUID:      "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4",
							Type:      "NVIDIA",
							Usedmem:   500,
							Usedcores: 3,
						},
					},
				},
			},
			wantErr: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := DecodePodDevices(test.args.checklist, test.args.annos)
			assert.DeepEqual(t, test.wantErr, gotErr)
			assert.DeepEqual(t, test.want, got)
		})
	}
}
