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

package scheduler

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// test case matrix
/**
| node num | per node device | pod use device | device having use | score |
|----------|-----------------|----------------|-------------------|-------|
| 1 node   | 1 device        | 1 device       | no                | 5.25     |
| 1 node   | 1 device        | 1 device       | 50% core, 50% mem | 20.25     |
| 1 node   | 2 device        | 1 device       | no                | 2.625     |
| 1 node   | 2 device        | 1 device       | 50% core, 50% mem | 10.125     |
| 1 node   | 2 device        | 2 device       | no                | 5.25     |
| 1 node   | 2 device        | 2 device       | 50% core, 50% mem | 20.25     |
| 2 node   | 1 device        | 1 device       | no                | 5.25   |
| 2 node   | 1 device        | 1 device       | node1-device1: 50% core, 50% mem, node2-device1: 0% core, 0% mem  | node1: 5.25 node2: 5.25 |
| 2 node   | 2 device        | 1 device       | no                | 1,1   |
| 2 node   | 1 device        | 1 device       | node1-device1: 50% core, 50% mem, node2-device1: 0% core, 0% mem  | node1: 20.25 node2: 5.25 |
test case matrix.
*/
func Test_calcScore(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			nodes *map[string]*NodeUsage
			nums  util.PodDeviceRequests
			annos map[string]string
			task  *corev1.Pod
		}
		wants struct {
			want *policy.NodeScoreList
			err  error
		}
	}{
		{
			name: "one node one device one pod one container use one device.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 5.25,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node one device one pod one container use one device,but this device before having use.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      5,
										Count:     10,
										Usedmem:   4000,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 50,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 20.25,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node two device one pod one container use one device",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &util.DeviceUsage{
										ID:        "uuid2",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 2.625,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node two device one pod one container use one device,but having use 50%",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &util.DeviceUsage{
										ID:        "uuid2",
										Index:     0,
										Used:      5,
										Count:     10,
										Usedmem:   4000,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 50,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 10.125,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node two device one pod one container use two device",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &util.DeviceUsage{
										ID:        "uuid2",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     2,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(2, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 5.25,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node two device one pod one container use two device,but this two device before having use.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      5,
										Count:     10,
										Usedmem:   4000,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 50,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &util.DeviceUsage{
										ID:        "uuid2",
										Index:     0,
										Used:      5,
										Count:     10,
										Usedmem:   4000,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 50,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     2,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(2, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 20.25,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "two node per node having one device one pod one container use one device",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
					"node2": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid2",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 5.25,
						},
						{
							NodeID: "node2",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 5.25,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "two node per node having one device one pod one container use one device,one device having use 50%",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      5,
										Count:     10,
										Usedmem:   4000,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 50,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
					"node2": {
						Devices: policy.DeviceUsageList{
							Policy: policy.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid2",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": util.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 20.25,
						},
						{
							NodeID: "node2",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 5.25,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node two device one pod two container use two device",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: policy.DeviceUsageList{
							Policy: policy.NodeSchedulerPolicyBinpack.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &util.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
								},
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						nvidia.NvidiaGPUDevice: util.ContainerDeviceRequest{
							Nums:             1,
							Type:             nvidia.NvidiaGPUDevice,
							Memreq:           1000,
							MemPercentagereq: 101,
							Coresreq:         30,
						},
					},
					{},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn1",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
							{
								Name:      "gpu-burn2",
								Image:     "chrstnhntschl/gpu_burn",
								Args:      []string{"6000"},
								Resources: corev1.ResourceRequirements{},
							},
						},
					},
				},
			},
			wants: struct {
				want *policy.NodeScoreList
				err  error
			}{
				want: &policy.NodeScoreList{
					Policy: policy.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: util.PodDevices{
								"NVIDIA": util.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
									{
										{
											Idx:       0,
											UUID:      "",
											Type:      "",
											Usedcores: 0,
											Usedmem:   0,
										},
									},
								},
							},
							Score: 5.25,
						},
					},
				},
				err: nil,
			},
		},
	}
	s := NewScheduler()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := s.calcScore(test.args.nodes, test.args.nums, test.args.annos, test.args.task)
			assert.DeepEqual(t, test.wants.err, gotErr)
			wantMap := make(map[string]*policy.NodeScore)
			for index, node := range (*(test.wants.want)).NodeList {
				wantMap[node.NodeID] = (*(test.wants.want)).NodeList[index]
			}
			for i := 0; i < got.Len(); i++ {
				gotI := (*(got)).NodeList[i]
				wantI := wantMap[gotI.NodeID]
				assert.DeepEqual(t, wantI.NodeID, gotI.NodeID)
				assert.DeepEqual(t, wantI.Devices, gotI.Devices)
				assert.DeepEqual(t, wantI.Score, gotI.Score)
			}
		})
	}
}
