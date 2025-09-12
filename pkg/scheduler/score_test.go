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
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/kunlun"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestMain(m *testing.M) {
	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
		},
	}

	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		klog.Fatalf("Failed to initialize devices with config: %v", err)
	}
	m.Run()
}

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
	/*
		Uncomment this line if you're running this single test.
		If you're running `make test`, keep this commented out, as there's another test
		(pkg/k8sutil/pod_test.go) that may cause a DATA RACE when calling device.InitDevices().
	*/
	//device.InitDevices()

	tests := []struct {
		name string
		args struct {
			nodes *map[string]*NodeUsage
			nums  device.PodDeviceRequests
			annos map[string]string
			task  *corev1.Pod
		}
		wants struct {
			want        *policy.NodeScoreList
			failedNodes map[string]string
			err         error
		}
	}{
		{
			name: "one node one device one pod one container use one device.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 0,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 15,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 0,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 7.5,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 0,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 15,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 0,
						},
						{
							NodeID: "node2",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 0,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 15,
						},
						{
							NodeID: "node2",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
							Score: 0,
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
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.NodeSchedulerPolicyBinpack.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
									{},
								},
							},
							Score: 0,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node one device one pod two container, the second container use device",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.NodeSchedulerPolicyBinpack.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{},
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums:             1,
							Type:             nvidia.NvidiaGPUDevice,
							Memreq:           1000,
							MemPercentagereq: 101,
							Coresreq:         30,
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
								Name:      "gpu-burn2",
								Image:     "chrstnhntschl/gpu_burn",
								Args:      []string{"6000"},
								Resources: corev1.ResourceRequirements{},
							},
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
						},
					},
				},
			},
			wants: struct {
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
									{},
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
							Score: 0,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node one device one pod with three containers, middle container uses one device.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{},
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
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
								Name:      "gpu-burn1",
								Image:     "chrstnhntschl/gpu_burn",
								Args:      []string{"6000"},
								Resources: corev1.ResourceRequirements{},
							},
							{
								Name:  "gpu-burn2",
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
								Name:      "gpu-burn3",
								Image:     "chrstnhntschl/gpu_burn",
								Args:      []string{"6000"},
								Resources: corev1.ResourceRequirements{},
							},
						},
					},
				},
			},
			wants: struct {
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
									{},
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
									{},
								},
							},
							Score: 0,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node two device one pod two containers use two device with spread,should not in same device.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
									Device: &device.DeviceUsage{
										ID:        "uuid2",
										Index:     1,
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   8000,
							Coresreq: 30,
						},
					},
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
										"hami.io/gpumem":   *resource.NewQuantity(8000, resource.BinarySI),
									},
								},
							},
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
						},
					},
				},
			},
			wants: struct {
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
									{
										{
											Idx:       1,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   8000,
										},
									},
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
							Score: 0,
						},
					},
				},
				err: nil,
			},
		},
		{
			name: "one node one device one pod one container use one device and not enough resource,node should be failed.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  50, // not enough mem
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy:   util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{},
				},
				failedNodes: map[string]string{
					"node1": nodeUnfitPod,
				},
				err: nil,
			},
		},
		{
			name: "race condition test for failedNodes map with two nodes failing",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  50, // not enough mem
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
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
										ID:        "uuid2",
										Index:     0,
										Used:      0,
										Count:     10,
										Usedmem:   0,
										Totalmem:  50, // not enough mem
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
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
						Name: "test-race",
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
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy:   util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{},
				},
				failedNodes: map[string]string{
					"node1": nodeUnfitPod,
					"node2": nodeUnfitPod,
				},
				err: nil,
			},
		},
		{
			name: "one node one device one pod one container use one device for kunlun.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid2",
										Index:     1,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid3",
										Index:     2,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid4",
										Index:     3,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid5",
										Index:     4,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid6",
										Index:     5,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid7",
										Index:     6,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid8",
										Index:     7,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
					"node2": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid2",
										Index:     1,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid3",
										Index:     2,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid4",
										Index:     3,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid5",
										Index:     4,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid6",
										Index:     5,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid7",
										Index:     6,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid8",
										Index:     7,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
							Nums:     1,
							Type:     kunlun.KunlunGPUDevice,
							Memreq:   0,
							Coresreq: 0,
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
								Name:  "xpu",
								Image: "chrstnhntschl/xpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"kunlunxin.com/xpu": *resource.NewQuantity(1, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"kunlun": device.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      kunlun.KunlunGPUDevice,
											Usedcores: 100,
											Usedmem:   98304,
										},
									},
								},
							},
							Score: 0,
						},
						{
							NodeID: "node2",
							Devices: device.PodDevices{
								"kunlun": device.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      kunlun.KunlunGPUDevice,
											Usedcores: 100,
											Usedmem:   98304,
										},
									},
								},
							},
							Score: 1006.25,
						},
					},
				},
				failedNodes: map[string]string{},
				err:         nil,
			},
		},
		{
			name: "two node eight device one pod two container use one device for kunlun.",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid2",
										Index:     1,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid3",
										Index:     2,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid4",
										Index:     3,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid5",
										Index:     4,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid6",
										Index:     5,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid7",
										Index:     6,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid8",
										Index:     7,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
					"node2": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
										ID:        "uuid1",
										Index:     0,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid2",
										Index:     1,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid3",
										Index:     2,
										Used:      0,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid4",
										Index:     3,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid5",
										Index:     4,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid6",
										Index:     5,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid7",
										Index:     6,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid8",
										Index:     7,
										Used:      1,
										Count:     1,
										Usedmem:   0,
										Totalmem:  98304, // not enough mem
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      kunlun.KunlunGPUDevice,
										Health:    true,
									},
									Score: 0,
								},
							},
						},
					},
				},
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
							Nums:     1,
							Type:     kunlun.KunlunGPUDevice,
							Memreq:   0,
							Coresreq: 0,
						},
					},
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
							Nums:     1,
							Type:     kunlun.KunlunGPUDevice,
							Memreq:   0,
							Coresreq: 0,
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
								Name:  "xpu",
								Image: "chrstnhntschl/xpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"kunlunxin.com/xpu": *resource.NewQuantity(1, resource.BinarySI),
									},
								},
							},
							{
								Name:  "xpu",
								Image: "chrstnhntschl/xpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"kunlunxin.com/xpu": *resource.NewQuantity(1, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"kunlun": device.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      kunlun.KunlunGPUDevice,
											Usedcores: 100,
											Usedmem:   98304,
										},
									},
									{
										{
											Idx:       1,
											UUID:      "uuid2",
											Type:      kunlun.KunlunGPUDevice,
											Usedcores: 100,
											Usedmem:   98304,
										},
									},
								},
							},
							Score: 1000,
						},
						{
							NodeID: "node2",
							Devices: device.PodDevices{
								"kunlun": device.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      kunlun.KunlunGPUDevice,
											Usedcores: 100,
											Usedmem:   98304,
										},
									},
									{
										{
											Idx:       1,
											UUID:      "uuid2",
											Type:      kunlun.KunlunGPUDevice,
											Usedcores: 100,
											Usedmem:   98304,
										},
									},
								},
							},
							Score: 2006.25,
						},
					},
				},
				failedNodes: map[string]string{},
				err:         nil,
			},
		},
		{
			name: "two node per node having one device one pod two container use one device",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  device.PodDeviceRequests
				annos map[string]string
				task  *corev1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
						Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
						Devices: policy.DeviceUsageList{
							Policy: util.GPUSchedulerPolicySpread.String(),
							DeviceLists: []*policy.DeviceListsScore{
								{
									Device: &device.DeviceUsage{
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
				nums: device.PodDeviceRequests{
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
					{
						"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 40,
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
							{
								Name:  "gpu-burn1",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(40, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
			},
			wants: struct {
				want        *policy.NodeScoreList
				failedNodes map[string]string
				err         error
			}{
				want: &policy.NodeScoreList{
					Policy: util.NodeSchedulerPolicyBinpack.String(),
					NodeList: []*policy.NodeScore{
						{
							NodeID: "node1",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
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
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 40,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 0,
						},
						{
							NodeID: "node2",
							Devices: device.PodDevices{
								"NVIDIA": device.PodSingleDevice{
									{
										{
											Idx:       0,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
									{
										{
											Idx:       0,
											UUID:      "uuid2",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 40,
											Usedmem:   1000,
										},
									},
								},
							},
							Score: 0,
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
			for nodeName, nodeUsage := range *(test.args.nodes) {
				devices := []device.DeviceInfo{}
				for _, devinstance := range nodeUsage.Devices.DeviceLists {
					devices = append(devices, device.DeviceInfo{
						ID: devinstance.Device.ID,
					})
				}
				s.addNode(nodeName, &device.NodeInfo{ID: nodeName, Node: nodeUsage.Node, Devices: devices})
			}
			failedNodes := map[string]string{}
			got, gotErr := s.calcScore(test.args.nodes, test.args.nums, test.args.task, failedNodes)
			assert.DeepEqual(t, test.wants.err, gotErr)
			wantMap := make(map[string]*policy.NodeScore)
			for index, node := range (*(test.wants.want)).NodeList {
				wantMap[node.NodeID] = (*(test.wants.want)).NodeList[index]
			}
			if gotErr == nil && len(got.NodeList) == 0 && len(failedNodes) == 0 {
				t.Fatal("empty error and empty result")
			}
			if len(failedNodes) != 0 {
				assert.DeepEqual(t, test.wants.failedNodes, failedNodes)
				return
			}
			for i := range got.Len() {
				gotI := (*(got)).NodeList[i]
				wantI := wantMap[gotI.NodeID]
				assert.DeepEqual(t, wantI.NodeID, gotI.NodeID)
				assert.DeepEqual(t, wantI.Devices, gotI.Devices)
				assert.DeepEqual(t, wantI.Score, gotI.Score)
			}
		})
	}
}

func Test_fitInCertainDevice(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			node      *NodeUsage
			request   device.ContainerDeviceRequest
			pod       *corev1.Pod
			allocated *device.PodDevices
		}
		want1 bool
		want2 map[string]device.ContainerDevices
		want3 map[string]int
	}{
		{
			name: "allocated device",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(1),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: true,
			want2: map[string]device.ContainerDevices{
				"NVIDIA": {
					{
						Usedcores: int32(1),
						Usedmem:   int32(1024),
						Type:      nvidia.NvidiaGPUDevice,
						UUID:      "test-0",
					},
				},
			},
		},
		{
			name: "card type don't match",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             "test",
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(1),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{cardTypeMismatch: 1},
		},
		{
			name: "device count less than device used",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(5),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(1),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{cardTimeSlicingExhausted: 1},
		},
		{
			name: "core limit exceed 100",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(200),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{cardInsufficientCore: 1},
		},
		{
			name: "card insufficient remaining memory",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8000),
									Usedmem:   int32(8000),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(0),
					MemPercentagereq: int32(100),
					Coresreq:         int32(100),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{cardInsufficientMemory: 1},
		},
		{
			name: "the container wants exclusive access to an entire card, but the card is already in use",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(0),
									Totalcore: int32(100),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(100),
					MemPercentagereq: int32(100),
					Coresreq:         int32(100),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{exclusiveDeviceAllocateConflict: 1},
		},
		{
			name: "can't allocate core=0 job to an already full GPU",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(1),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(0),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{cardComputeUnitsExhausted: 1},
		},
		{
			name: "mode is mig",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Mode:      "mig",
									MigUsage: device.MigInUse{
										Index: int32(1),
										UsageList: device.MIGS{
											{
												Name:   "test6",
												Memory: int32(2048),
												InUse:  false,
											},
										},
									},
									Health: true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(2),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(1),
				},
				pod:       &corev1.Pod{},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{
				"NVIDIA": {
					{
						UUID:      "test-0",
						Type:      nvidia.NvidiaGPUDevice,
						Usedcores: int32(1),
						Usedmem:   int32(1024),
					},
				},
			},
			want3: map[string]int{cardNotFoundCustomFilterRule: 1, allocatedCardsInsufficientRequest: 1},
		},
		{
			name: "card uuid don't match",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-0",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(1),
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							nvidia.GPUUseUUID: "abc",
						},
					},
				},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{cardUUIDMismatch: 1},
		},
		{
			name: "numa not fit",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{Device: makeDevice("test-0", 0, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 4)},
							{Device: makeDevice("test-1", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 4)},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(2),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(1),
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							nvidia.GPUInUse: "NVIDIA",
							nvidia.NumaBind: "true",
						},
					},
				},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{
				"NVIDIA": {
					{
						UUID:      "test-0",
						Type:      nvidia.NvidiaGPUDevice,
						Usedcores: int32(1),
						Usedmem:   int32(1024),
					},
				},
			},
			want3: map[string]int{numaNotFit: 1, allocatedCardsInsufficientRequest: 1},
		},
		{
			name: "test device kind of not fit reason",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				pod       *corev1.Pod
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							// test CardTypeMismatch
							{Device: makeDevice("a", 0, hygon.HygonDCUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("f", 0, metax.MetaxGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							// test CardUUIDMismatch
							{Device: makeDevice("b", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("q", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("i", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							// test CardTimeSlicingExhausted
							{Device: makeDevice("c", 1, nvidia.NvidiaGPUDevice, 4, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("j", 1, nvidia.NvidiaGPUDevice, 4, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("u", 1, nvidia.NvidiaGPUDevice, 4, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("l", 1, nvidia.NvidiaGPUDevice, 4, 4, 8192, 2048, 1, 100)},
							// test CardInsufficientMemory
							{Device: makeDevice("d", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 8048, 1, 100)},
							{Device: makeDevice("m", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 8048, 1, 100)},
							// test CardInsufficientCore
							{Device: makeDevice("e", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 90, 100)},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(2),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(20),
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{nvidia.GPUUseUUID: "a,f,c,d,e,g,h,j,l,u,m"},
					},
				},
				allocated: &device.PodDevices{},
			},
			want1: false,
			want2: map[string]device.ContainerDevices{},
			want3: map[string]int{cardUUIDMismatch: 3, cardTimeSlicingExhausted: 4,
				cardInsufficientMemory: 2, cardInsufficientCore: 1},
		},
	}
	for _, test := range tests {

		t.Run(test.name, func(t *testing.T) {
			gpuDevices := &nvidia.NvidiaGPUDevices{}

			result1, result2, result3 := gpuDevices.Fit(getNodeResources(*test.args.node, nvidia.NvidiaGPUDevice), test.args.request, test.args.pod, &device.NodeInfo{}, test.args.allocated)
			assert.DeepEqual(t, result1, test.want1)
			assert.DeepEqual(t, result2, test.want2)
			assert.DeepEqual(t, convertReasonToMap(result3), test.want3)
		})
	}
}

func makeDevice(id string, numa int, Type string, used, count, totalmem, usedmem, usedcores, totalcore int) *device.DeviceUsage {
	return &device.DeviceUsage{
		ID:        id,
		Numa:      numa,
		Type:      Type,
		Used:      int32(used),
		Count:     int32(count),
		Totalmem:  int32(totalmem),
		Usedmem:   int32(usedmem),
		Usedcores: int32(usedcores),
		Totalcore: int32(totalcore),
		Health:    true,
	}
}

// convertReasonToMap converts a string in a specific format to a map.
// The input string should be in the format "cnt/total reason1, cnt/total reason2, ...".
// This function parses the string and returns a map where the key is the reason and the value is the corresponding count.
func convertReasonToMap(reason string) map[string]int {
	var reasonMap map[string]int
	for r := range strings.SplitSeq(reason, ", ") {
		parts := strings.SplitN(r, " ", 2)
		if len(parts) != 2 {
			continue
		}
		countParts := strings.SplitN(parts[0], "/", 2)
		if len(countParts) != 2 {
			continue
		}
		cnt, err := strconv.Atoi(countParts[0])
		if err != nil {
			continue
		}
		if reasonMap == nil {
			reasonMap = make(map[string]int)
		}
		reasonMap[parts[1]] = cnt
	}
	return reasonMap
}

func Test_fitInDevices(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			node     NodeUsage
			requests device.ContainerDeviceRequests
			annos    map[string]string
			pod      *corev1.Pod
			devinput *device.PodDevices
		}
		want1 bool
		want2 string
	}{
		{
			name: "all device score for one node",
			args: struct {
				node     NodeUsage
				requests device.ContainerDeviceRequests
				annos    map[string]string
				pod      *corev1.Pod
				devinput *device.PodDevices
			}{
				node: NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-1",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
							{
								Device: &device.DeviceUsage{
									ID:        "test-2",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				requests: device.ContainerDeviceRequests{
					"test-2": {
						Nums:             int32(1),
						Type:             nvidia.NvidiaGPUDevice,
						Memreq:           int32(1024),
						MemPercentagereq: int32(100),
						Coresreq:         int32(1),
					},
				},
				annos:    map[string]string{},
				pod:      &corev1.Pod{},
				devinput: &device.PodDevices{},
			},
			want1: true,
			want2: "",
		},
		{
			name: "request devices nums cannot exceed the total number of devices on the node",
			args: struct {
				node     NodeUsage
				requests device.ContainerDeviceRequests
				annos    map[string]string
				pod      *corev1.Pod
				devinput *device.PodDevices
			}{
				node: NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-1",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				requests: device.ContainerDeviceRequests{
					"test-1": {
						Nums:             int32(2),
						Type:             nvidia.NvidiaGPUDevice,
						Memreq:           int32(1024),
						MemPercentagereq: int32(100),
						Coresreq:         int32(1),
					},
				},
				annos:    map[string]string{},
				pod:      &corev1.Pod{},
				devinput: &device.PodDevices{},
			},
			want1: false,
			want2: "NodeInsufficientDevice",
		},
		{
			name: "device type the different from request type",
			args: struct {
				node     NodeUsage
				requests device.ContainerDeviceRequests
				annos    map[string]string
				pod      *corev1.Pod
				devinput *device.PodDevices
			}{
				node: NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{
								Device: &device.DeviceUsage{
									ID:        "test-2",
									Numa:      int(1),
									Type:      nvidia.NvidiaGPUDevice,
									Used:      int32(1),
									Count:     int32(4),
									Totalmem:  int32(8192),
									Usedmem:   int32(2048),
									Usedcores: int32(1),
									Totalcore: int32(4),
									Health:    true,
								},
							},
						},
					},
				},
				requests: device.ContainerDeviceRequests{
					"test-1": {
						Nums:             int32(1),
						Type:             "test",
						Memreq:           int32(1024),
						MemPercentagereq: int32(100),
						Coresreq:         int32(1),
					},
				},
				annos:    map[string]string{},
				pod:      &corev1.Pod{},
				devinput: &device.PodDevices{},
			},
			want1: false,
			want2: "Device type not found",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			viewStatus(test.args.node)
			result1, result2 := fitInDevices(&test.args.node, test.args.requests, test.args.pod, nil, test.args.devinput)
			assert.DeepEqual(t, result1, test.want1)
			assert.DeepEqual(t, result2, test.want2)
		})
	}
}

func Test_Nvidia_GPU_Topology(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			node      *NodeUsage
			request   device.ContainerDeviceRequest
			annos     map[string]string
			pod       *corev1.Pod
			nodeInfo  *device.NodeInfo
			allocated *device.PodDevices
		}
		want1 bool
		want2 map[string]device.ContainerDevices
		want3 string
	}{
		{
			name: "test nvidia gpu topology-aware",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				annos     map[string]string
				pod       *corev1.Pod
				nodeInfo  *device.NodeInfo
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{Device: makeDevice("a", 0, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("b", 0, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("c", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("d", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("e", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("f", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(3),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(20),
				},
				annos: map[string]string{},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String(),
						},
					},
				},
				nodeInfo: &device.NodeInfo{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: []device.DeviceInfo{
						{ID: "a", DevicePairScore: device.DevicePairScore{ID: "a", Scores: map[string]int{"b": 1, "c": 1, "d": 1, "e": 1, "f": 100}}},
						{ID: "b", DevicePairScore: device.DevicePairScore{ID: "b", Scores: map[string]int{"a": 1, "c": 1, "d": 1, "e": 1, "f": 1}}},
						{ID: "c", DevicePairScore: device.DevicePairScore{ID: "c", Scores: map[string]int{"a": 1, "b": 1, "d": 1, "e": 1, "f": 100}}},
						{ID: "d", DevicePairScore: device.DevicePairScore{ID: "d", Scores: map[string]int{"a": 1, "b": 1, "c": 1, "e": 1, "f": 1}}},
						{ID: "e", DevicePairScore: device.DevicePairScore{ID: "e", Scores: map[string]int{"a": 1, "b": 1, "c": 1, "d": 1, "f": 1}}},
						{ID: "f", DevicePairScore: device.DevicePairScore{ID: "f", Scores: map[string]int{"a": 100, "b": 1, "c": 100, "d": 1, "e": 1}}},
					},
				},
				allocated: &device.PodDevices{},
			},
			want1: true,
			want2: map[string]device.ContainerDevices{
				"NVIDIA": {
					{UUID: "f", Type: "NVIDIA", Usedmem: 1024, Usedcores: 20},
					{UUID: "c", Type: "NVIDIA", Usedmem: 1024, Usedcores: 20},
					{UUID: "a", Type: "NVIDIA", Usedmem: 1024, Usedcores: 20},
				},
			},
			want3: "",
		},
		{
			name: "test Single Card Topology ",
			args: struct {
				node      *NodeUsage
				request   device.ContainerDeviceRequest
				annos     map[string]string
				pod       *corev1.Pod
				nodeInfo  *device.NodeInfo
				allocated *device.PodDevices
			}{
				node: &NodeUsage{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: policy.DeviceUsageList{
						DeviceLists: []*policy.DeviceListsScore{
							{Device: makeDevice("a", 0, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("b", 0, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("c", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("d", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("e", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
							{Device: makeDevice("f", 1, nvidia.NvidiaGPUDevice, 1, 4, 8192, 2048, 1, 100)},
						},
					},
				},
				request: device.ContainerDeviceRequest{
					Nums:             int32(1),
					Type:             nvidia.NvidiaGPUDevice,
					Memreq:           int32(1024),
					MemPercentagereq: int32(100),
					Coresreq:         int32(20),
				},
				annos: map[string]string{},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String(),
						},
					},
				},
				nodeInfo: &device.NodeInfo{
					Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
					Devices: []device.DeviceInfo{
						{ID: "a", DevicePairScore: device.DevicePairScore{ID: "a", Scores: map[string]int{"b": 100, "c": 100, "d": 100, "e": 100, "f": 1}}},
						{ID: "b", DevicePairScore: device.DevicePairScore{ID: "b", Scores: map[string]int{"a": 100, "c": 100, "d": 100, "e": 100, "f": 1}}},
						{ID: "c", DevicePairScore: device.DevicePairScore{ID: "c", Scores: map[string]int{"a": 100, "b": 100, "d": 100, "e": 100, "f": 1}}},
						{ID: "d", DevicePairScore: device.DevicePairScore{ID: "d", Scores: map[string]int{"a": 100, "b": 100, "c": 100, "e": 100, "f": 1}}},
						{ID: "e", DevicePairScore: device.DevicePairScore{ID: "e", Scores: map[string]int{"a": 100, "b": 100, "c": 100, "d": 100, "f": 1}}},
						{ID: "f", DevicePairScore: device.DevicePairScore{ID: "f", Scores: map[string]int{"a": 1, "b": 1, "c": 1, "d": 1, "e": 1}}},
					},
				},
				allocated: &device.PodDevices{},
			},
			want1: true,
			want2: map[string]device.ContainerDevices{
				"NVIDIA": {
					{UUID: "f", Type: "NVIDIA", Usedmem: 1024, Usedcores: 20},
				},
			},
			want3: "",
		},
	}
	for _, test := range tests {

		t.Run(test.name, func(t *testing.T) {
			gpuDevices := &nvidia.NvidiaGPUDevices{}

			result1, result2, result3 := gpuDevices.Fit(getNodeResources(*test.args.node, nvidia.NvidiaGPUDevice), test.args.request, test.args.pod, test.args.nodeInfo, test.args.allocated)
			assert.DeepEqual(t, result1, test.want1)
			assert.DeepEqual(t, result2, test.want2)
			assert.DeepEqual(t, result3, test.want3)
		})
	}
}
