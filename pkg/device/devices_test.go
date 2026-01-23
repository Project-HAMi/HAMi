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

package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/util"
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

func TestExtractMigTemplatesFromUUID(t *testing.T) {
	testCases := []struct {
		name          string
		uuid          string
		expectedTmpID int
		expectedPos   int
		expectError   bool
	}{
		{
			name:          "Valid UUID",
			uuid:          "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313[7-9]",
			expectedTmpID: 7,
			expectedPos:   9,
			expectError:   false,
		},
		{
			name:          "Invalid UUID format - missing '[' delimiter",
			uuid:          "GPU-936619fc-f6a1-74a8-0bc6-ecf6b32693137-9]",
			expectedTmpID: -1,
			expectedPos:   -1,
			expectError:   true,
		},
		{
			name:          "Invalid UUID format - missing ']' delimiter",
			uuid:          "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313[7-9",
			expectedTmpID: -1,
			expectedPos:   -1,
			expectError:   true,
		},
		{
			name:          "Invalid UUID format - missing '-' delimiter",
			uuid:          "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313[79]",
			expectedTmpID: -1,
			expectedPos:   -1,
			expectError:   true,
		},
		{
			name:          "Invalid template index",
			uuid:          "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313[a-9]",
			expectedTmpID: -1,
			expectedPos:   -1,
			expectError:   true,
		},
		{
			name:          "Invalid position",
			uuid:          "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313[7-b]",
			expectedTmpID: -1,
			expectedPos:   -1,
			expectError:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempid, pos, err := ExtractMigTemplatesFromUUID(tc.uuid)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected an error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Did not expect an error but got: %v", err)
				}
				if tempid != tc.expectedTmpID || pos != tc.expectedPos {
					t.Errorf("Expected %d:%d, got %d:%d", tc.expectedTmpID, tc.expectedPos, tempid, pos)
				}
			}
		})
	}
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
						ContainerDevice{0, "UUID1", "Type1", 1000, 30, nil},
					},
				},
			},
		},
		{
			name: "one pod two container, every container use one device",
			args: PodDevices{
				"NVIDIA": PodSingleDevice{
					ContainerDevices{
						ContainerDevice{0, "UUID1", "Type1", 1000, 30, nil},
					},
					ContainerDevices{
						ContainerDevice{0, "UUID1", "Type1", 1000, 30, nil},
					},
				},
			},
		},
		{
			name: "one pod one container use two devices",
			args: PodDevices{
				"NVIDIA": PodSingleDevice{
					ContainerDevices{
						ContainerDevice{0, "UUID1", "Type1", 1000, 30, nil},
						ContainerDevice{0, "UUID2", "Type1", 1000, 30, nil},
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

func TestMarshalNodeDevices(t *testing.T) {
	type args struct {
		dlist []*DeviceInfo
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test one",
			args: args{
				dlist: []*DeviceInfo{
					{
						Index:   1,
						ID:      "id-1",
						Count:   1,
						Devmem:  1024,
						Devcore: 10,
						Type:    "type",
						Numa:    0,
						Health:  true,
					},
				},
			},
			want: "[{\"index\":1,\"id\":\"id-1\",\"count\":1,\"devmem\":1024,\"devcore\":10,\"type\":\"type\",\"numa\":0,\"health\":true}]",
		},
		{
			name: "test multiple",
			args: args{
				dlist: []*DeviceInfo{
					{
						Index:   1,
						ID:      "id-1",
						Count:   1,
						Devmem:  1024,
						Devcore: 10,
						Type:    "type",
						Numa:    0,
						Health:  true,
					},
					{
						Index:   2,
						ID:      "id-2",
						Count:   2,
						Devmem:  2048,
						Devcore: 20,
						Type:    "type2",
						Numa:    1,
						Health:  false,
					},
				},
			},
			want: "[{\"index\":1,\"id\":\"id-1\",\"count\":1,\"devmem\":1024,\"devcore\":10,\"type\":\"type\",\"numa\":0,\"health\":true},{\"index\":2,\"id\":\"id-2\",\"count\":2,\"devmem\":2048,\"devcore\":20,\"type\":\"type2\",\"numa\":1,\"health\":false}]",
		},
		{
			name: "test empty",
			args: args{
				dlist: []*DeviceInfo{},
			},
			want: "[]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MarshalNodeDevices(tt.args.dlist)

			var gotDeviceInfo, wantDeviceInfo []*DeviceInfo
			// Compare the JSON contents by unmarshalling both got and want
			err := json.Unmarshal([]byte(got), &gotDeviceInfo)
			assert.NilError(t, err)

			err = json.Unmarshal([]byte(tt.want), &wantDeviceInfo)
			assert.NilError(t, err)

			assert.DeepEqual(t, gotDeviceInfo, wantDeviceInfo)
		})
	}
}

func TestUnMarshalNodeDevices(t *testing.T) {
	type args struct {
		str string
	}
	tests := []struct {
		name    string
		args    args
		want    []*DeviceInfo
		wantErr bool
	}{
		{
			name: "test one",
			args: args{
				str: "[{\"index\":1,\"id\":\"id-1\",\"count\":1,\"devmem\":1024,\"devcore\":10,\"type\":\"type\",\"health\":true}]\n",
			},
			want: []*DeviceInfo{
				{
					Index:   1,
					ID:      "id-1",
					Count:   1,
					Devmem:  1024,
					Devcore: 10,
					Type:    "type",
					Numa:    0,
					Health:  true,
				},
			},
			wantErr: false,
		},
		{
			name: "test two",
			args: args{
				str: "[{\"index\":1,\"id\":\"id-1\",\"count\":1,\"devmem\":1024,\"devcore\":10,\"type\":\"type\",\"health\":true}," +
					"{\"index\":2,\"id\":\"id-2\",\"count\":2,\"devmem\":4096,\"devcore\":20,\"type\":\"type2\",\"health\":false}]",
			},
			want: []*DeviceInfo{
				{
					Index:   1,
					ID:      "id-1",
					Count:   1,
					Devmem:  1024,
					Devcore: 10,
					Type:    "type",
					Numa:    0,
					Health:  true,
				},
				{
					Index:   2,
					ID:      "id-2",
					Count:   2,
					Devmem:  4096,
					Devcore: 20,
					Type:    "type2",
					Numa:    0,
					Health:  false,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnMarshalNodeDevices(tt.args.str)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnMarshalNodeDevices() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.DeepEqual(t, got, tt.want)
		})
	}
}

func Test_DecodeNodeDevices(t *testing.T) {
	tests := []struct {
		name string
		args string
		want struct {
			di  []*DeviceInfo
			err error
		}
	}{
		{
			name: "args is invalid",
			args: "a",
			want: struct {
				di  []*DeviceInfo
				err error
			}{
				di:  []*DeviceInfo{},
				err: errors.New("node annotations not decode successfully"),
			},
		},
		{
			name: "str is old format",
			args: "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4,10,7680,100,NVIDIA-Tesla P4,0,true:",
			want: struct {
				di  []*DeviceInfo
				err error
			}{
				di: []*DeviceInfo{
					{
						ID:      "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4",
						Index:   0,
						Count:   10,
						Devmem:  7680,
						Devcore: 100,
						Type:    "NVIDIA-Tesla P4",
						Mode:    "hami-core",
						Numa:    0,
						Health:  true,
					},
				},
				err: nil,
			},
		},
		{
			name: "str is new format",
			args: "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4,10,7680,100,NVIDIA-Tesla P4,0,true,1,hami-core:",
			want: struct {
				di  []*DeviceInfo
				err error
			}{
				di: []*DeviceInfo{
					{
						ID:      "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4",
						Index:   1,
						Count:   10,
						Devmem:  7680,
						Devcore: 100,
						Type:    "NVIDIA-Tesla P4",
						Mode:    "hami-core",
						Numa:    0,
						Health:  true,
					},
				},
				err: nil,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DecodeNodeDevices(test.args)
			assert.DeepEqual(t, test.want.di, got)
			if err != nil {
				assert.DeepEqual(t, test.want.err.Error(), err.Error())
			}
		})
	}
}

func Test_EncodeNodeDevices(t *testing.T) {
	tests := []struct {
		name string
		args []*DeviceInfo
		want string
	}{
		{
			name: "old format",
			args: []*DeviceInfo{
				{
					ID:      "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4",
					Index:   0,
					Count:   10,
					Devmem:  7680,
					Devcore: 100,
					Type:    "NVIDIA-Tesla P4",
					Numa:    0,
					Mode:    "hami-core",
					Health:  true,
				},
			},
			want: "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4,10,7680,100,NVIDIA-Tesla P4,0,true,0,hami-core:",
		},
		{
			name: "test two",
			args: []*DeviceInfo{
				{
					ID:      "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4",
					Index:   1,
					Count:   10,
					Devmem:  7680,
					Devcore: 100,
					Mode:    "hami-core",
					Type:    "NVIDIA-Tesla P4",
					Numa:    0,
					Health:  true,
				},
			},
			want: "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4,10,7680,100,NVIDIA-Tesla P4,0,true,1,hami-core:",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EncodeNodeDevices(test.args)
			assert.DeepEqual(t, test.want, got)
		})
	}
}

func Test_CheckHealth(t *testing.T) {
	util.HandshakeAnnos["huawei.com/Ascend910"] = "hami.io/node-handshake-ascend"
	tests := []struct {
		name string
		args struct {
			devType string
			n       corev1.Node
		}
		want1 bool
		want2 bool
	}{
		{
			name: "Requesting state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Requesting_2128-12-02 00:00:00",
						},
					},
				},
			},
			want1: true,
			want2: false,
		},
		{
			name: "Deleted state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Deleted",
						},
					},
				},
			},
			want1: true,
			want2: false,
		},
		{
			name: "Unknown state",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Unknown",
						},
					},
				},
			},
			want1: true,
			want2: false,
		},
		{
			name: "Requesting state expired",
			args: struct {
				devType string
				n       corev1.Node
			}{
				devType: "huawei.com/Ascend910",
				n: corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.HandshakeAnnos["huawei.com/Ascend910"]: "Requesting_2024-01-02 00:00:00",
						},
					},
				},
			},
			want1: false,
			want2: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1, result2 := CheckHealth(test.args.devType, &test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
		})
	}
}
func TestDecodeNodeDevices(t *testing.T) {
	pairScore := "[{\"uuid\":\"GPU-0\",\"score\":{\"GPU-1\":50,\"GPU-2\":20,\"GPU-3\":20}}]"
	_, err := DecodePairScores(pairScore)
	assert.NilError(t, err)
}

func TestPlatternMIG(t *testing.T) {
	tests := []struct {
		name        string
		n           *MigInUse
		templates   []Geometry
		templateIdx int
		want        *MigInUse
	}{
		{
			name: "empty template",
			n:    &MigInUse{},
			templates: []Geometry{
				{},
			},
			templateIdx: 0,
			want: &MigInUse{
				Index:     0,
				UsageList: nil,
			},
		},
		{
			name: "single template with one count",
			n:    &MigInUse{},
			templates: []Geometry{
				{
					{
						Name:   "1g.5gb",
						Memory: 5,
						Count:  1,
					},
				},
			},
			templateIdx: 0,
			want: &MigInUse{
				Index: 0,
				UsageList: MIGS{
					{
						Name:   "1g.5gb",
						Memory: 5,
						InUse:  false,
					},
				},
			},
		},
		{
			name: "multiple templates with different counts",
			n:    &MigInUse{},
			templates: []Geometry{
				{
					{
						Name:   "1g.5gb",
						Memory: 5,
						Count:  2,
					},
					{
						Name:   "2g.10gb",
						Memory: 10,
						Count:  1,
					},
				},
			},
			templateIdx: 0,
			want: &MigInUse{
				Index: 0,
				UsageList: MIGS{
					{
						Name:   "1g.5gb",
						Memory: 5,
						InUse:  false,
					},
					{
						Name:   "1g.5gb",
						Memory: 5,
						InUse:  false,
					},
					{
						Name:   "2g.10gb",
						Memory: 10,
						InUse:  false,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			PlatternMIG(tt.n, tt.templates, tt.templateIdx)
			assert.DeepEqual(t, tt.want, tt.n)
		})
	}
}

func TestGetDevicesUUIDList(t *testing.T) {
	tests := []struct {
		name  string
		infos []*DeviceInfo
		want  []string
	}{
		{
			name:  "empty device list",
			infos: []*DeviceInfo{},
			want:  []string{},
		},
		{
			name: "single device",
			infos: []*DeviceInfo{
				{ID: "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313"},
			},
			want: []string{"GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313"},
		},
		{
			name: "multiple devices",
			infos: []*DeviceInfo{
				{ID: "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313"},
				{ID: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76"},
				{ID: "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4"},
			},
			want: []string{
				"GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313",
				"GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76",
				"GPU-ebe7c3f7-303d-558d-435e-99a160631fe4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetDevicesUUIDList(tt.infos)
			assert.DeepEqual(t, tt.want, got)
		})
	}
}
func TestEncodeContainerDeviceType(t *testing.T) {
	tests := []struct {
		name string
		cd   ContainerDevices
		t    string
		want string
	}{
		{
			name: "empty container devices",
			cd:   ContainerDevices{},
			t:    "NVIDIA",
			want: "",
		},
		{
			name: "single matching device",
			cd: ContainerDevices{
				{UUID: "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313", Type: "NVIDIA", Usedmem: 1000, Usedcores: 10},
			},
			t:    "NVIDIA",
			want: "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313,NVIDIA,1000,10:",
		},
		{
			name: "multiple devices with type match",
			cd: ContainerDevices{
				{UUID: "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313", Type: "NVIDIA", Usedmem: 1000, Usedcores: 10},
				{UUID: "GPU-8dcd427f-483b-b48f-d7e5-75fb19a52b76", Type: "AMD", Usedmem: 2000, Usedcores: 20},
				{UUID: "GPU-ebe7c3f7-303d-558d-435e-99a160631fe4", Type: "NVIDIA", Usedmem: 3000, Usedcores: 30},
			},
			t:    "NVIDIA",
			want: "GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313,NVIDIA,1000,10::GPU-ebe7c3f7-303d-558d-435e-99a160631fe4,NVIDIA,3000,30:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EncodeContainerDeviceType(tt.cd, tt.t)
			assert.Equal(t, got, tt.want)
		})
	}
}

func TestCheckUUID(t *testing.T) {
	GPUUseUUID := "hami.io/gpu-use-uuid"
	GPUNoUseUUID := "hami.io/gpu-no-use-uuid"
	tests := []struct {
		name  string
		annos map[string]string
		id    string
		want  bool
	}{
		{
			name:  "don't set GPUUseUUID and GPUNoUseUUID annotation",
			annos: make(map[string]string),
			id:    "abc",
			want:  true,
		},
		{
			name: "use set GPUUseUUID don't set GPUNoUseUUID annotation,device match",
			annos: map[string]string{
				GPUUseUUID: "abc,123",
			},
			id:   "abc",
			want: true,
		},
		{
			name: "use set GPUUseUUID don't set GPUNoUseUUID annotation,device don't match",
			annos: map[string]string{
				GPUUseUUID: "abc,123",
			},
			id:   "1abc",
			want: false,
		},
		{
			name: "use don't set GPUUseUUID set GPUNoUseUUID annotation,device match",
			annos: map[string]string{
				GPUNoUseUUID: "abc,123",
			},
			id:   "abc",
			want: false,
		},
		{
			name: "use don't set GPUUseUUID set GPUNoUseUUID annotation,device  don't match",
			annos: map[string]string{
				GPUNoUseUUID: "abc,123",
			},
			id:   "1abc",
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CheckUUID(test.annos, test.id, GPUUseUUID, GPUNoUseUUID, "NVIDIA")
			assert.Equal(t, test.want, got)
		})
	}
}
