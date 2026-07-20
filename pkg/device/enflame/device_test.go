/*
Copyright 2025 The HAMi Authors.

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

package enflame

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetNodeDevices_DRSAnnotation(t *testing.T) {
	InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	dev := &EnflameDevices{}
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-a",
			Annotations: map[string]string{
				GCUDrsCapacity: `{"devices":[{"index":"0","minor":"0","capacity":6}],"profiles":{"1g.6gb":"0","3g.20gb":"1","6g.40gb":"2"}}`,
			},
		},
	}

	got, err := dev.GetNodeDevices(node)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Type, EnflameVGCUDevice)
	assert.Equal(t, got[0].Devmem, int32(40960))
	assert.Equal(t, got[0].Count, int32(6))
	assert.Equal(t, got[0].CustomInfo["minor"], "0")
}

func Test_checkType(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     device.DeviceUsage
			n     device.ContainerDeviceRequest
		}
		want1 bool
		want2 bool
		want3 bool
	}{
		{
			name: "the same type",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
					Type: EnflameVGCUDevice,
				},
			},
			want1: true,
			want2: true,
			want3: false,
		},
		{
			name: "the different type",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
					Type: "test123",
				},
			},
			want1: false,
			want2: false,
			want3: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := EnflameDevices{}
			result1, result2, result3 := dev.checkType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
			assert.Equal(t, result3, test.want3)
		})
	}
}

func TestGenerateResourceRequests(t *testing.T) {
	InitEnflameDevice(EnflameConfig{
		ResourceNameDRSGCU: "enflame.com/drs-gcu",
		ResourceNameMemory: "enflame.com/gcu-memory",
		ResourceNameCore:   "enflame.com/gcu-core",
	})
	config := EnflameConfig{
		ResourceNameDRSGCU: "enflame.com/drs-gcu",
		ResourceNameMemory: "enflame.com/gcu-memory",
		ResourceNameCore:   "enflame.com/gcu-core",
	}
	dev := InitEnflameDevice(config)
	container := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"enflame.com/drs-gcu": resource.MustParse("3"),
			},
		},
	}
	req := dev.GenerateResourceRequests(container)
	assert.Equal(t, req.Nums, int32(1))
	assert.Equal(t, req.Memreq, int32(3))
	assert.Equal(t, req.MemPercentagereq, enflameRequestModeDirect)
	assert.Equal(t, req.Type, EnflameVGCUDevice)
}

func TestGenerateResourceRequests_ByMemoryCore(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{
		ResourceNameDRSGCU: "enflame.com/drs-gcu",
		ResourceNameMemory: "enflame.com/gcu-memory",
		ResourceNameCore:   "enflame.com/gcu-core",
	})
	container := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"enflame.com/gcu-memory": resource.MustParse("20480"),
				"enflame.com/gcu-core":   resource.MustParse("40"),
			},
		},
	}
	req := dev.GenerateResourceRequests(container)
	assert.Equal(t, req.Nums, int32(1))
	assert.Equal(t, req.Type, EnflameVGCUDevice)
	assert.Equal(t, req.Memreq, int32(20480))
	assert.Equal(t, req.Coresreq, int32(40))
	assert.Equal(t, req.MemPercentagereq, enflameRequestModeBySpec)
}

func TestFit_SelectProfileByRequest(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	devices := []*device.DeviceUsage{
		{
			ID:       "node-a-enflame-drs-0",
			Index:    0,
			Count:    6,
			Used:     0,
			Totalmem: 40960,
			Type:     EnflameVGCUDevice,
			CustomInfo: map[string]any{
				"minor": "0",
				"index": "0",
				"profiles": map[string]string{
					"1g.6gb":  "0",
					"3g.20gb": "1",
					"6g.40gb": "2",
				},
			},
		},
	}
	req := device.ContainerDeviceRequest{
		Nums:   1,
		Type:   EnflameVGCUDevice,
		Memreq: 3,
	}
	fit, result, reason := dev.Fit(devices, req, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, reason, "")
	assert.Equal(t, len(result[EnflameVGCUDevice]), 1)
	assert.Equal(t, result[EnflameVGCUDevice][0].Usedmem, int32(20480))
	assert.Equal(t, result[EnflameVGCUDevice][0].Usedcores, int32(50))
	assert.Equal(t, result[EnflameVGCUDevice][0].CustomInfo["profileName"], "3g.20gb")
	assert.Equal(t, result[EnflameVGCUDevice][0].CustomInfo["profileID"], "1")
}

func TestFit_SelectProfileByMemoryCoreRequest(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	devices := []*device.DeviceUsage{
		{
			ID:       "node-a-enflame-drs-0",
			Index:    0,
			Count:    6,
			Used:     0,
			Totalmem: 40960,
			Type:     EnflameVGCUDevice,
			CustomInfo: map[string]any{
				"minor": "0",
				"index": "0",
				"profiles": map[string]string{
					"1g.6gb":  "0",
					"3g.20gb": "1",
					"6g.40gb": "2",
				},
			},
		},
	}
	req := device.ContainerDeviceRequest{
		Nums:             1,
		Type:             EnflameVGCUDevice,
		Memreq:           20480, // MiB -> 20GB
		Coresreq:         40,    // 40% core requirement
		MemPercentagereq: enflameRequestModeBySpec,
	}
	fit, result, reason := dev.Fit(devices, req, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, true)
	assert.Equal(t, reason, "")
	assert.Equal(t, len(result[EnflameVGCUDevice]), 1)
	assert.Equal(t, result[EnflameVGCUDevice][0].Usedmem, int32(20480))
	assert.Equal(t, result[EnflameVGCUDevice][0].Usedcores, int32(50))
	assert.Equal(t, result[EnflameVGCUDevice][0].CustomInfo["profileName"], "3g.20gb")
	assert.Equal(t, result[EnflameVGCUDevice][0].CustomInfo["profileID"], "1")
}

func TestMutateAdmission_ByMemoryCoreAPI(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{
		ResourceNameDRSGCU: "enflame.com/drs-gcu",
		ResourceNameMemory: "enflame.com/gcu-memory",
		ResourceNameCore:   "enflame.com/gcu-core",
	})
	container := &corev1.Container{
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"enflame.com/gcu-memory": resource.MustParse("20480"),
				"enflame.com/gcu-core":   resource.MustParse("50"),
			},
		},
	}
	found, err := dev.MutateAdmission(container, &corev1.Pod{})
	assert.NilError(t, err)
	assert.Equal(t, found, true)
}

func TestFit_ProfileNotFound(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	devices := []*device.DeviceUsage{
		{
			ID:       "node-a-enflame-drs-0",
			Index:    0,
			Count:    6,
			Used:     0,
			Totalmem: 40960,
			Type:     EnflameVGCUDevice,
			CustomInfo: map[string]any{
				"minor": "0",
				"index": "0",
				"profiles": map[string]string{
					"1g.6gb": "0",
				},
			},
		},
	}
	req := device.ContainerDeviceRequest{
		Nums:   1,
		Type:   EnflameVGCUDevice,
		Memreq: 3,
	}
	fit, _, reason := dev.Fit(devices, req, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Assert(t, strings.Contains(reason, "ModeNotFit"))
}

func TestFit_MutexRejectsUsedDevice(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	devices := []*device.DeviceUsage{
		{
			ID:       "node-a-enflame-drs-0",
			Index:    0,
			Count:    2,
			Used:     1,
			Totalmem: 40960,
			Type:     EnflameVGCUDevice,
			CustomInfo: map[string]any{
				"minor": "0",
				"index": "0",
				"profiles": map[string]string{
					"1g.6gb":  "0",
					"3g.20gb": "1",
					"6g.40gb": "2",
				},
			},
		},
	}
	req := device.ContainerDeviceRequest{
		Nums:   1,
		Type:   EnflameVGCUDevice,
		Memreq: 3,
	}
	annos := map[string]string{"hami.io/gpu-scheduler-policy": "mutex"}
	fit, _, reason := dev.Fit(devices, req, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: annos}}, &device.NodeInfo{}, &device.PodDevices{})
	assert.Equal(t, fit, false)
	assert.Equal(t, reason, "1/1 ExclusiveDeviceAllocateConflict")
}

func TestPatchAnnotations_DRSFields(t *testing.T) {
	dev := InitEnflameDevice(EnflameConfig{ResourceNameDRSGCU: "enflame.com/drs-gcu"})
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "pod-gcu-example1"},
			},
		},
	}
	annoInput := map[string]string{}
	podDevices := device.PodDevices{
		EnflameVGCUDevice: {
			{
				{
					Idx:       0,
					UUID:      "node-a-enflame-drs-0",
					Type:      EnflameVGCUDevice,
					Usedmem:   20480,
					Usedcores: 50,
					CustomInfo: map[string]any{
						"minor":       "0",
						"index":       "0",
						"profileName": "3g.20gb",
						"profileID":   "1",
						"drsSlice":    3,
					},
				},
			},
		},
	}

	got := dev.PatchAnnotations(pod, &annoInput, podDevices)
	assert.Assert(t, strings.Contains(got[device.SupportDevices[EnflameVGCUDevice]], ",20480,50"))
	assert.Equal(t, got[PodHasAssignedGCU], "false")
	assert.Equal(t, got[PodAssignedGCUIdx], "0")
	assert.Equal(t, got[PodAssignedGCUMin], "0")
	assert.Equal(t, got[PodRequestGCUSize], "3")
	assert.Assert(t, got[PodAssignedGCUTime] != "")
	assert.Assert(t, got[AssignedContainers] != "")

	assigned := map[string]assignedContainerInfo{}
	err := json.Unmarshal([]byte(got[AssignedContainers]), &assigned)
	assert.NilError(t, err)
	assert.Equal(t, assigned["pod-gcu-example1"].Allocated, false)
	assert.Equal(t, assigned["pod-gcu-example1"].Request, int32(3))
	assert.Equal(t, assigned["pod-gcu-example1"].ProfileName, "3g.20gb")
	assert.Equal(t, assigned["pod-gcu-example1"].ProfileID, "1")
}
