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
	"flag"
	"strconv"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/kunlun"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
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

func TestScoreNodeQuotaPreservesInitContainerPositions(t *testing.T) {
	const namespace = "score-init-position-quota"
	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "gpu-memory", Namespace: namespace},
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			"limits.hami.io/gpumem": resource.MustParse("24000"),
		}},
	}
	manager := device.NewQuotaManager()
	manager.AddQuota(quota)
	t.Cleanup(func() { manager.DelQuota(quota) })

	gpuContainer := func(name string, percentage int64, sidecar bool) corev1.Container {
		container := corev1.Container{
			Name: name,
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"hami.io/gpu":               resource.MustParse("1"),
				"hami.io/gpumem-percentage": *resource.NewQuantity(percentage, resource.DecimalSI),
			}},
		}
		if sidecar {
			always := corev1.ContainerRestartPolicyAlways
			container.RestartPolicy = &always
		}
		return container
	}

	tests := []struct {
		name   string
		inits  []corev1.Container
		apps   []corev1.Container
		fits   bool
		usedMB int32
	}{
		{name: "no init over quota", apps: []corev1.Container{gpuContainer("app-a", 40, false), gpuContainer("app-b", 40, false)}},
		{name: "non-GPU init does not hide an app", inits: []corev1.Container{{Name: "setup"}}, apps: []corev1.Container{gpuContainer("app-a", 40, false), gpuContainer("app-b", 40, false)}},
		{name: "GPU init does not hide an app", inits: []corev1.Container{gpuContainer("init", 30, false)}, apps: []corev1.Container{gpuContainer("app-a", 40, false), gpuContainer("app-b", 40, false)}},
		{name: "native sidecar and app are concurrent", inits: []corev1.Container{gpuContainer("sidecar", 40, true)}, apps: []corev1.Container{gpuContainer("app", 40, false)}},
		{name: "sequential GPU init and app reuse", inits: []corev1.Container{gpuContainer("init", 50, false)}, apps: []corev1.Container{gpuContainer("app", 40, false)}, fits: true, usedMB: 20000},
		{name: "non-GPU init leaves valid apps unchanged", inits: []corev1.Container{{Name: "setup"}}, apps: []corev1.Container{gpuContainer("app-a", 20, false), gpuContainer("app-b", 20, false)}, fits: true, usedMB: 16000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "quota-test", Namespace: namespace},
				Spec:       corev1.PodSpec{InitContainers: tc.inits, Containers: tc.apps},
			}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-node"}}
			usage := &NodeUsage{
				Node:     node,
				NodeInfo: &device.NodeInfo{ID: node.Name, Node: node},
				Devices: policy.DeviceUsageList{DeviceLists: []*policy.DeviceListsScore{{
					Device: &device.DeviceUsage{ID: "gpu-0", Type: nvidia.NvidiaGPUDevice, Health: true, Count: 10, Totalmem: 40000, Totalcore: 100},
				}}},
			}
			nodes := map[string]*NodeUsage{node.Name: usage}
			failedNodes := map[string]string{}
			got, err := (&Scheduler{}).calcScoreWithOptions(&nodes, device.Resourcereqs(pod), pod, failedNodes, false, false)
			assert.NilError(t, err)
			if !tc.fits {
				assert.Equal(t, len(got.NodeList), 0)
				assert.Assert(t, strings.Contains(failedNodes[node.Name], common.ResourceQuotaNotFit), "failure reason: %q", failedNodes[node.Name])
				assert.Equal(t, usage.Devices.DeviceLists[0].Device.Usedmem, int32(0))
				return
			}
			assert.Equal(t, len(got.NodeList), 1)
			assert.Equal(t, len(got.NodeList[0].Devices[nvidia.NvidiaGPUDevice]), len(tc.inits)+len(tc.apps))
			assert.Equal(t, usage.Devices.DeviceLists[0].Device.Usedmem, tc.usedMB)
		})
	}
}

func TestScoreNodeRegularMthreadsInitDoesNotConstrainAppPlacement(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mthreads-init", Namespace: "default"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "app"}},
		},
	}
	requests := device.PodDeviceRequests{
		{mthreads.MthreadsGPUDevice: {Nums: 1, Type: mthreads.MthreadsGPUDevice, Memreq: 8000, Coresreq: 10}},
		{mthreads.MthreadsGPUDevice: {Nums: 1, Type: mthreads.MthreadsGPUDevice, Memreq: 2000, Coresreq: 50}},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "mthreads-node"}}
	usage := &NodeUsage{
		Node:     node,
		NodeInfo: &device.NodeInfo{ID: node.Name, Node: node},
		Devices: policy.DeviceUsageList{DeviceLists: []*policy.DeviceListsScore{
			{Device: &device.DeviceUsage{ID: "gpu-a", Type: mthreads.MthreadsGPUDevice, Health: true, Count: 10, Totalmem: 4000, Totalcore: 100}},
			{Device: &device.DeviceUsage{ID: "gpu-b", Type: mthreads.MthreadsGPUDevice, Health: true, Count: 10, Totalmem: 10000, Totalcore: 20}},
		}},
	}
	nodes := map[string]*NodeUsage{node.Name: usage}
	failedNodes := map[string]string{}

	got, err := (&Scheduler{}).calcScoreWithOptions(&nodes, requests, pod, failedNodes, false, false)
	assert.NilError(t, err)
	assert.Equal(t, len(failedNodes), 0)
	assert.Equal(t, len(got.NodeList), 1)
	allocations := got.NodeList[0].Devices[mthreads.MthreadsGPUDevice]
	assert.Equal(t, len(allocations), 2)
	assert.Equal(t, allocations[0][0].UUID, "gpu-b")
	assert.Equal(t, allocations[1][0].UUID, "gpu-a")
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
			name: "init container requests more memory than any single device has (should filter node)",
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
										Usedmem:   7500, // Only 500 available (8000-7500)
										Totalmem:  8000,
										Totalcore: 100,
										Usedcores: 0,
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
								},
								{
									Device: &device.DeviceUsage{
										ID:        "uuid2",
										Index:     1,
										Used:      0,
										Count:     10,
										Usedmem:   7500, // Only 500 available (8000-7500)
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
					// Index 0: InitContainer requests 4000 (more than any single device has)
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   4000, // Each device only has 500 available
							Coresreq: 30,
						},
					},
					// Index 1: App container requests only 100
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   100,
							Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-init-large-mem",
					},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:  "init-large",
								Image: "busybox",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(4000, resource.BinarySI),
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "app-small",
								Image: "busybox",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(100, resource.BinarySI),
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
					NodeList: []*policy.NodeScore{}, // Node should be filtered out
				},
				failedNodes: map[string]string{
					"node1": "2/2 CardInsufficientMemory",
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
					"node1": "1/1 CardInsufficientMemory",
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
					"node1": "1/1 CardInsufficientMemory",
					"node2": "1/1 CardInsufficientMemory",
				},
				err: nil,
			},
		},
		{
			name: "two init containers, first has no device request, second does - tests slot alignment (issue #1667)",
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
					// Index 0: InitContainer 0 - no device request (e.g. a setup/download step)
					{},
					// Index 1: InitContainer 1 - requests a device
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
					// Index 2: App container - requests a device
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
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
						Name: "test-init-heterogeneous",
					},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:      "init-no-device",
								Image:     "busybox",
								Resources: corev1.ResourceRequirements{},
							},
							{
								Name:  "init-gpu-check",
								Image: "chrstnhntschl/gpu_burn",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "app-gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
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
									// Index 0: init-no-device -> untouched placeholder slot
									{},
									// Index 1: init-gpu-check -> real allocation
									{
										{
											Idx:       0,
											UUID:      "uuid1",
											Type:      nvidia.NvidiaGPUDevice,
											Usedcores: 30,
											Usedmem:   1000,
										},
									},
									// Index 2: app-gpu-burn -> real allocation
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
			name: "one node two devices, pod has one init container (uses 1 device) and one regular container (uses 1 device) - tests InitContainer capacity reset",
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
								},
							},
						},
					},
				},
				nums: device.PodDeviceRequests{
					// Index 0: InitContainer Request
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
					// Index 1: Regular Container Request
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
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
						Name: "test-init-container",
					},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:  "init-gpu-burn",
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
						Containers: []corev1.Container{
							{
								Name:  "app-gpu-burn",
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
									// Index 1: Regular Container allocated normally.
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
			name: "init container requests MORE than app container - needsInitClone=true path",
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
										ID: "uuid1", Index: 0, Used: 0, Count: 10,
										Usedmem: 0, Totalmem: 8000, Totalcore: 100,
										Usedcores: 0, Numa: 0,
										Type: nvidia.NvidiaGPUDevice, Health: true,
									},
								},
								{
									Device: &device.DeviceUsage{
										ID: "uuid2", Index: 0, Used: 0, Count: 10,
										Usedmem: 0, Totalmem: 8000, Totalcore: 100,
										Usedcores: 0, Numa: 0,
										Type: nvidia.NvidiaGPUDevice, Health: true,
									},
								},
							},
						},
					},
				},
				nums: device.PodDeviceRequests{
					// Index 0: InitContainer requests 2 GPUs (MORE than app)
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums: 2, Type: nvidia.NvidiaGPUDevice,
							Memreq: 1000, Coresreq: 30,
						},
					},
					// Index 1: App container requests only 1 GPU (LESS than init)
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums: 1, Type: nvidia.NvidiaGPUDevice,
							Memreq: 1000, Coresreq: 30,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "test-init-bigger"},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:  "init-heavy",
								Image: "chrstnhntschl/gpu_burn",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(2, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "app-light",
								Image: "chrstnhntschl/gpu_burn",
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
									// Init container allocated 2 GPUs from fresh snapshot
									{
										{Idx: 0, UUID: "uuid2", Type: nvidia.NvidiaGPUDevice, Usedcores: 30, Usedmem: 1000},
										{Idx: 0, UUID: "uuid1", Type: nvidia.NvidiaGPUDevice, Usedcores: 30, Usedmem: 1000},
									},
									// App container allocated 1 GPU from its own fresh pool
									{
										{Idx: 0, UUID: "uuid2", Type: nvidia.NvidiaGPUDevice, Usedcores: 30, Usedmem: 1000},
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
			name: "init container requests more cores than any single device has (should filter node)",
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
										Usedcores: 80, // only 20 cores free
										Numa:      0,
										Type:      nvidia.NvidiaGPUDevice,
										Health:    true,
									},
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
										Usedcores: 80, // same, only 20 cores free
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
					// Index 0: InitContainer requests 30 cores (more than any single device's 20 free)
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 30,
						},
					},
					// Index 1: App container requests 10 cores (fits, but init should block the node)
					{
						nvidia.NvidiaGPUDevice: device.ContainerDeviceRequest{
							Nums:     1,
							Type:     nvidia.NvidiaGPUDevice,
							Memreq:   1000,
							Coresreq: 10,
						},
					},
				},
				annos: make(map[string]string),
				task: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "test-init-large-cores"},
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{
							{
								Name:  "init-core-heavy",
								Image: "busybox",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "app-light",
								Image: "busybox",
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(10, resource.BinarySI),
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
					"node1": "2/2 CardInsufficientCore",
				},
				err: nil,
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
				devices := map[string][]device.DeviceInfo{}
				for _, devinstance := range nodeUsage.Devices.DeviceLists {
					devices["NVIDIA"] = append(devices["NVIDIA"], device.DeviceInfo{
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
			want3: map[string]int{common.CardTypeMismatch: 1},
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
			want3: map[string]int{common.CardTimeSlicingExhausted: 1},
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
			want3: map[string]int{common.CardInsufficientCore: 1},
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
			want3: map[string]int{common.CardInsufficientMemory: 1},
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
			want3: map[string]int{common.ExclusiveDeviceAllocateConflict: 1},
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
			want3: map[string]int{common.CardComputeUnitsExhausted: 1},
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
									MigProfiles: []device.MigProfile{{
										Name: "1g.test", MemoryMB: 2048, Core: 1,
										Placements: []device.MigPlacement{{Start: 0, Size: 1}},
									}},
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
						Usedmem:   int32(2048),
					},
				},
			},
			want3: map[string]int{common.CardMigTopologyInfeasible: 1, common.AllocatedCardsInsufficientRequest: 1},
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
			want3: map[string]int{common.CardUUIDMismatch: 1},
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
			want3: map[string]int{common.NumaNotFit: 1, common.AllocatedCardsInsufficientRequest: 1},
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
							{Device: makeDevice("a", 0, hygon.HygonHCUDevice, 1, 4, 8192, 2048, 1, 100)},
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
			want3: map[string]int{common.CardUUIDMismatch: 3, common.CardTimeSlicingExhausted: 4,
				common.CardInsufficientMemory: 2, common.CardInsufficientCore: 1},
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
			// One of the two requested GPUs is present on the node.
			want2: common.GenReason(map[string]int{common.NodeInsufficientDevice: 1}, 2),
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
			result1, result2 := fitInDevices(&test.args.node, test.args.requests, test.args.pod, nil, test.args.devinput, util.DefaultDeviceScoringWeights())
			assert.DeepEqual(t, result1, test.want1)
			assert.DeepEqual(t, result2, test.want2)
		})
	}
}

func TestCalcScoreRejectsInvalidDeviceScoringWeights(t *testing.T) {
	nodes := map[string]*NodeUsage{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		util.DeviceScoringWeightsAnnotationKey: "slot=1,core=-1,memory=3",
	}}}

	result, err := (&Scheduler{}).calcScoreWithOptions(&nodes, nil, pod, map[string]string{}, false, false)

	assert.Assert(t, result == nil)
	assert.ErrorContains(t, err, `"core" weight must not be negative`)
}

// TestCalcScoreRecordsNodeInsufficientDeviceReason covers the rejection
// fitInDevices raises before any device backend runs. It used to be reported as a
// bare constant rather than through GenReason, so common.ParseReason dropped it
// and the pod got no event naming why the node was rejected.
func TestCalcScoreRecordsNodeInsufficientDeviceReason(t *testing.T) {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
	nodes := map[string]*NodeUsage{
		"node1": {
			Node:     node,
			NodeInfo: &device.NodeInfo{ID: node.Name, Node: node},
			Devices: policy.DeviceUsageList{
				Policy: util.GPUSchedulerPolicyBinpack.String(),
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{
						ID: "gpu-a", Index: 0, Type: nvidia.NvidiaGPUDevice, Health: true,
						Count: 10, Totalcore: 100, Totalmem: 100,
					}},
				},
			},
		},
	}
	// The node registers one GPU, so a four-GPU request is rejected by the
	// device-count check rather than by a backend's Fit().
	requests := device.PodDeviceRequests{
		{
			"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
				Nums: 4,
				Type: nvidia.NvidiaGPUDevice,
			},
		},
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "insufficient-devices", Namespace: "default",
	}}

	recorder := record.NewFakeRecorder(10)
	s := &Scheduler{eventRecorder: recorder}
	failedNodes := map[string]string{}

	result, err := s.calcScoreWithOptions(&nodes, requests, pod, failedNodes, true, false)

	assert.NilError(t, err)
	assert.Equal(t, len(result.NodeList), 0)
	// One of the four requested GPUs is present on the node.
	assert.Equal(t, failedNodes["node1"], common.GenReason(map[string]int{common.NodeInsufficientDevice: 1}, 4))

	select {
	case event := <-recorder.Events:
		assert.Assert(t, strings.Contains(event, EventReasonFilteringFailed), "event %q", event)
		assert.Assert(t, strings.Contains(event, common.NodeInsufficientDevice), "event %q", event)
		assert.Assert(t, strings.Contains(event, "node1"), "event %q", event)
	default:
		t.Fatalf("no %s event recorded naming %s", EventReasonFilteringFailed, common.NodeInsufficientDevice)
	}
}

func newDeviceScoringWeightTestNodes() *map[string]*NodeUsage {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
	return &map[string]*NodeUsage{
		"node1": {
			Node:     node,
			NodeInfo: &device.NodeInfo{ID: node.Name, Node: node},
			Devices: policy.DeviceUsageList{
				Policy: util.GPUSchedulerPolicyBinpack.String(),
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{
						ID: "gpu-a", Index: 0, Type: nvidia.NvidiaGPUDevice, Health: true,
						Count: 10, Used: 1, Totalcore: 100, Usedcores: 90, Totalmem: 100, Usedmem: 10,
					}},
					{Device: &device.DeviceUsage{
						ID: "gpu-b", Index: 1, Type: nvidia.NvidiaGPUDevice, Health: true,
						Count: 10, Used: 7, Totalcore: 100, Usedcores: 10, Totalmem: 100, Usedmem: 20,
					}},
				},
			},
		},
	}
}

func TestCalcScoreUsesPodDeviceScoringWeights(t *testing.T) {
	requests := device.PodDeviceRequests{{
		"hami.io/vgpu-devices-to-allocate": {
			Nums: 1, Type: nvidia.NvidiaGPUDevice, MemPercentagereq: 40,
		},
	}}
	selectedDevice := func(t *testing.T, annotations map[string]string) string {
		t.Helper()
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "weighted-score", Namespace: "default", Annotations: annotations,
		}}
		result, err := (&Scheduler{}).calcScoreWithOptions(newDeviceScoringWeightTestNodes(), requests, pod, map[string]string{}, false, false)
		assert.NilError(t, err)
		assert.Equal(t, len(result.NodeList), 1)
		allocated := result.NodeList[0].Devices[nvidia.NvidiaGPUDevice]
		assert.Equal(t, len(allocated), 1)
		assert.Equal(t, len(allocated[0]), 1)
		return allocated[0][0].UUID
	}

	assert.Equal(t, selectedDevice(t, map[string]string{
		util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyBinpack.String(),
	}), "gpu-a")
	assert.Equal(t, selectedDevice(t, map[string]string{
		util.GPUSchedulerPolicyAnnotationKey:   util.GPUSchedulerPolicyBinpack.String(),
		util.DeviceScoringWeightsAnnotationKey: "slot=1,core=1,memory=3",
	}), "gpu-b")
}

func TestCalcScoreUsesPodDeviceScoringWeightsForInitContainers(t *testing.T) {
	requests := device.PodDeviceRequests{
		{
			"hami.io/vgpu-devices-to-allocate": {
				Nums: 1, Type: nvidia.NvidiaGPUDevice, MemPercentagereq: 40,
			},
		},
		{},
	}
	selectedInitDevice := func(t *testing.T, annotations map[string]string) string {
		t.Helper()
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "weighted-init-score", Namespace: "default", Annotations: annotations,
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{Name: "init"}},
				Containers:     []corev1.Container{{Name: "app"}},
			},
		}
		result, err := (&Scheduler{}).calcScoreWithOptions(newDeviceScoringWeightTestNodes(), requests, pod, map[string]string{}, false, false)
		assert.NilError(t, err)
		assert.Equal(t, len(result.NodeList), 1)
		allocated := result.NodeList[0].Devices[nvidia.NvidiaGPUDevice]
		assert.Equal(t, len(allocated), 2)
		assert.Equal(t, len(allocated[0]), 1)
		assert.Equal(t, len(allocated[1]), 0)
		return allocated[0][0].UUID
	}

	assert.Equal(t, selectedInitDevice(t, map[string]string{
		util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyBinpack.String(),
	}), "gpu-a")
	assert.Equal(t, selectedInitDevice(t, map[string]string{
		util.GPUSchedulerPolicyAnnotationKey:   util.GPUSchedulerPolicyBinpack.String(),
		util.DeviceScoringWeightsAnnotationKey: "slot=1,core=1,memory=3",
	}), "gpu-b")
}

func TestCalcScoreUsesDeviceScoringWeightsAsTopologyTieBreaker(t *testing.T) {
	newNodes := func() *map[string]*NodeUsage {
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
		return &map[string]*NodeUsage{
			"node1": {
				Node: node,
				NodeInfo: &device.NodeInfo{
					ID:   node.Name,
					Node: node,
					Devices: map[string][]device.DeviceInfo{
						nvidia.NvidiaGPUDevice: {
							{ID: "gpu-a", DevicePairScore: device.DevicePairScore{ID: "gpu-a", Scores: map[string]int{"gpu-b": 1}}},
							{ID: "gpu-b", DevicePairScore: device.DevicePairScore{ID: "gpu-b", Scores: map[string]int{"gpu-a": 1}}},
						},
					},
				},
				Devices: policy.DeviceUsageList{
					Policy: util.GPUSchedulerPolicyTopology.String(),
					DeviceLists: []*policy.DeviceListsScore{
						{Device: &device.DeviceUsage{
							ID: "gpu-a", Index: 0, Type: nvidia.NvidiaGPUDevice, Health: true,
							Count: 10, Used: 1, Totalcore: 100, Usedcores: 90, Totalmem: 100, Usedmem: 10,
						}},
						{Device: &device.DeviceUsage{
							ID: "gpu-b", Index: 1, Type: nvidia.NvidiaGPUDevice, Health: true,
							Count: 10, Used: 7, Totalcore: 100, Usedcores: 10, Totalmem: 100, Usedmem: 20,
						}},
					},
				},
			},
		}
	}
	requests := device.PodDeviceRequests{{
		"hami.io/vgpu-devices-to-allocate": {
			Nums: 1, Type: nvidia.NvidiaGPUDevice, MemPercentagereq: 40,
		},
	}}
	selectedDevice := func(t *testing.T, weights string) string {
		t.Helper()
		annotations := map[string]string{
			util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyTopology.String(),
		}
		if weights != "" {
			annotations[util.DeviceScoringWeightsAnnotationKey] = weights
		}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "topology-score-tie", Namespace: "default", Annotations: annotations,
		}}
		result, err := (&Scheduler{}).calcScoreWithOptions(newNodes(), requests, pod, map[string]string{}, false, false)
		assert.NilError(t, err)
		allocated := result.NodeList[0].Devices[nvidia.NvidiaGPUDevice]
		return allocated[0][0].UUID
	}

	// Both devices have equal topology scores, so the existing topology policy
	// retains precedence and uses utilization-score ordering only as a tie-breaker.
	assert.Equal(t, selectedDevice(t, ""), "gpu-b")
	assert.Equal(t, selectedDevice(t, "slot=1,core=1,memory=3"), "gpu-a")
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
					Devices: map[string][]device.DeviceInfo{
						"NVIDIA": {
							{ID: "a", DevicePairScore: device.DevicePairScore{ID: "a", Scores: map[string]int{"b": 1, "c": 1, "d": 1, "e": 1, "f": 100}}},
							{ID: "b", DevicePairScore: device.DevicePairScore{ID: "b", Scores: map[string]int{"a": 1, "c": 1, "d": 1, "e": 1, "f": 1}}},
							{ID: "c", DevicePairScore: device.DevicePairScore{ID: "c", Scores: map[string]int{"a": 1, "b": 1, "d": 1, "e": 1, "f": 100}}},
							{ID: "d", DevicePairScore: device.DevicePairScore{ID: "d", Scores: map[string]int{"a": 1, "b": 1, "c": 1, "e": 1, "f": 1}}},
							{ID: "e", DevicePairScore: device.DevicePairScore{ID: "e", Scores: map[string]int{"a": 1, "b": 1, "c": 1, "d": 1, "f": 1}}},
							{ID: "f", DevicePairScore: device.DevicePairScore{ID: "f", Scores: map[string]int{"a": 100, "b": 1, "c": 100, "d": 1, "e": 1}}},
						}},
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
					Devices: map[string][]device.DeviceInfo{
						"NVIDIA": {
							{ID: "a", DevicePairScore: device.DevicePairScore{ID: "a", Scores: map[string]int{"b": 100, "c": 100, "d": 100, "e": 100, "f": 1}}},
							{ID: "b", DevicePairScore: device.DevicePairScore{ID: "b", Scores: map[string]int{"a": 100, "c": 100, "d": 100, "e": 100, "f": 1}}},
							{ID: "c", DevicePairScore: device.DevicePairScore{ID: "c", Scores: map[string]int{"a": 100, "b": 100, "d": 100, "e": 100, "f": 1}}},
							{ID: "d", DevicePairScore: device.DevicePairScore{ID: "d", Scores: map[string]int{"a": 100, "b": 100, "c": 100, "e": 100, "f": 1}}},
							{ID: "e", DevicePairScore: device.DevicePairScore{ID: "e", Scores: map[string]int{"a": 100, "b": 100, "c": 100, "d": 100, "f": 1}}},
							{ID: "f", DevicePairScore: device.DevicePairScore{ID: "f", Scores: map[string]int{"a": 1, "b": 1, "c": 1, "d": 1, "e": 1}}},
						}},
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

// fitMockDevice is a minimal device.Devices implementation whose Fit always
// succeeds and returns a single device of its own type. Used to exercise
// fitInDevices with more than one device type in a single container.
type fitMockDevice struct {
	typeName string
	uuid     string
}

func (m *fitMockDevice) CommonWord() string { return m.typeName }
func (m *fitMockDevice) MutateAdmission(_ *corev1.Container, _ *corev1.Pod) (bool, error) {
	return false, nil
}
func (m *fitMockDevice) CheckHealth(_ string, _ *corev1.Node) (bool, bool) { return true, true }
func (m *fitMockDevice) NodeCleanUp(_ string) error                        { return nil }
func (m *fitMockDevice) GetResourceNames() device.ResourceNames            { return device.ResourceNames{} }
func (m *fitMockDevice) GetNodeDevices(_ corev1.Node) ([]*device.DeviceInfo, error) {
	return nil, nil
}
func (m *fitMockDevice) LockNode(_ *corev1.Node, _ *corev1.Pod) error        { return nil }
func (m *fitMockDevice) ReleaseNodeLock(_ *corev1.Node, _ *corev1.Pod) error { return nil }
func (m *fitMockDevice) GenerateResourceRequests(_ *corev1.Container) device.ContainerDeviceRequest {
	return device.ContainerDeviceRequest{}
}
func (m *fitMockDevice) PatchAnnotations(_ *corev1.Pod, _ *map[string]string, _ device.PodDevices) map[string]string {
	return nil
}
func (m *fitMockDevice) ScoreNode(_ *corev1.Node, _ device.PodSingleDevice, _ []*device.DeviceUsage, _ string) float32 {
	return 0
}
func (m *fitMockDevice) AddResourceUsage(_ *corev1.Pod, _ *device.DeviceUsage, _ *device.ContainerDevice) error {
	return nil
}
func (m *fitMockDevice) Fit(_ []*device.DeviceUsage, _ device.ContainerDeviceRequest, _ *corev1.Pod, _ *device.NodeInfo, _ *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	return true, map[string]device.ContainerDevices{
		m.typeName: {{UUID: m.uuid, Type: m.typeName}},
	}, ""
}

// Test_fitInDevices_MultiTypePartition guards against cross-device-type
// pollution: when one container requests two device types, each type's entry
// in the output PodDevices must contain only its own device.
func Test_fitInDevices_MultiTypePartition(t *testing.T) {
	oldDevicesMap := device.DevicesMap
	defer func() { device.DevicesMap = oldDevicesMap }()
	device.DevicesMap = map[string]device.Devices{
		"mockA": &fitMockDevice{typeName: "mockA", uuid: "uuid-a"},
		"mockB": &fitMockDevice{typeName: "mockB", uuid: "uuid-b"},
	}

	node := NodeUsage{
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}},
		Devices: policy.DeviceUsageList{
			DeviceLists: []*policy.DeviceListsScore{
				{Device: &device.DeviceUsage{
					ID: "uuid-a", Type: "mockA",
					Count: 4, Used: 0, Totalcore: 100, Usedcores: 0, Totalmem: 8192, Usedmem: 0, Health: true,
				}},
				{Device: &device.DeviceUsage{
					ID: "uuid-b", Type: "mockB",
					Count: 4, Used: 0, Totalcore: 100, Usedcores: 0, Totalmem: 8192, Usedmem: 0, Health: true,
				}},
			},
		},
	}
	requests := device.ContainerDeviceRequests{
		"mockA": {Nums: 1, Type: "mockA", Memreq: 1024, MemPercentagereq: 101, Coresreq: 1},
		"mockB": {Nums: 1, Type: "mockB", Memreq: 1024, MemPercentagereq: 101, Coresreq: 1},
	}
	devinput := &device.PodDevices{}

	viewStatus(node)
	fit, reason := fitInDevices(&node, requests, &corev1.Pod{}, nil, devinput, util.DefaultDeviceScoringWeights())

	assert.Equal(t, fit, true)
	assert.Equal(t, reason, "")

	// One container was processed, so each type has exactly one container entry.
	assert.Equal(t, len((*devinput)["mockA"]), 1)
	assert.Equal(t, len((*devinput)["mockB"]), 1)

	// Each type's entry must contain ONLY its own device (the bug leaks the
	// first-processed type's device into the second-processed type's entry).
	assert.Equal(t, len((*devinput)["mockA"][0]), 1)
	assert.Equal(t, (*devinput)["mockA"][0][0].UUID, "uuid-a")
	assert.Equal(t, len((*devinput)["mockB"][0]), 1)
	assert.Equal(t, (*devinput)["mockB"][0][0].UUID, "uuid-b")
}

func Test_calcScore_SidecarInitOrdering(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways

	newNodes := func(totalMem int32) *map[string]*NodeUsage {
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
		return &map[string]*NodeUsage{
			"node1": {
				Node:     node,
				NodeInfo: &device.NodeInfo{ID: node.Name, Node: node},
				Devices: policy.DeviceUsageList{
					Policy: util.GPUSchedulerPolicyBinpack.String(),
					DeviceLists: []*policy.DeviceListsScore{
						{Device: &device.DeviceUsage{
							ID: "uuid1", Index: 0, Type: nvidia.NvidiaGPUDevice, Health: true,
							Count: 10, Used: 0, Totalcore: 100, Usedcores: 0,
							Totalmem: totalMem, Usedmem: 0,
						}},
					},
				},
			},
		}
	}

	gpuReq := func(mem int32) map[string]device.ContainerDeviceRequest {
		return map[string]device.ContainerDeviceRequest{
			nvidia.NvidiaGPUDevice: {
				Nums: 1, Type: nvidia.NvidiaGPUDevice, Memreq: mem, Coresreq: 10,
			},
		}
	}
	row := func(mem int32) device.ContainerDevices {
		return device.ContainerDevices{
			{Idx: 0, UUID: "uuid1", Type: nvidia.NvidiaGPUDevice, Usedcores: 10, Usedmem: mem},
		}
	}
	initC := func(name string, sidecar bool) corev1.Container {
		c := corev1.Container{Name: name}
		if sidecar {
			c.RestartPolicy = &always
		}
		return c
	}

	tests := []struct {
		name        string
		totalMem    int32
		inits       []corev1.Container
		requests    device.PodDeviceRequests
		wantDevices device.PodDevices
		wantFailed  map[string]string
		wantUsedmem int32
	}{
		{
			name:     "regular then sidecar never overlap and fit",
			totalMem: 10000,
			inits:    []corev1.Container{initC("reg", false), initC("sc", true)},
			requests: device.PodDeviceRequests{gpuReq(5000), gpuReq(8000), {}},
			wantDevices: device.PodDevices{
				nvidia.NvidiaGPUDevice: device.PodSingleDevice{
					row(5000), // regular init: transient, only in the peak
					row(8000), // sidecar: persists into steady state
					{},        // app container without a GPU request
				},
			},
			wantUsedmem: 8000,
		},
		{
			name:       "sidecar then regular overlap and are rejected",
			totalMem:   10000,
			inits:      []corev1.Container{initC("sc", true), initC("reg", false)},
			requests:   device.PodDeviceRequests{gpuReq(8000), gpuReq(5000), {}},
			wantFailed: map[string]string{"node1": "1/1 CardInsufficientMemory"},
		},
		{
			name:     "interleaved inits use the running sidecar sum",
			totalMem: 7500,
			inits:    []corev1.Container{initC("reg-a", false), initC("sc", true), initC("reg-b", false)},
			requests: device.PodDeviceRequests{gpuReq(5000), gpuReq(3000), gpuReq(4000), gpuReq(1000)},
			wantDevices: device.PodDevices{
				nvidia.NvidiaGPUDevice: device.PodSingleDevice{
					row(5000),
					row(3000),
					row(4000),
					row(1000),
				},
			},
			wantUsedmem: 7000,
		},
		{
			name:       "sidecar plus app steady state is summed",
			totalMem:   7500,
			inits:      []corev1.Container{initC("sc", true)},
			requests:   device.PodDeviceRequests{gpuReq(4000), gpuReq(4000)},
			wantFailed: map[string]string{"node1": "1/1 CardInsufficientMemory"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "sidecar-order", Namespace: "default"},
				Spec: corev1.PodSpec{
					InitContainers: tc.inits,
					Containers:     []corev1.Container{{Name: "app"}},
				},
			}
			failedNodes := map[string]string{}
			nodes := newNodes(tc.totalMem)
			got, err := (&Scheduler{}).calcScoreWithOptions(nodes, tc.requests, pod, failedNodes, false, false)
			assert.NilError(t, err)

			if tc.wantFailed != nil {
				assert.Equal(t, len(got.NodeList), 0)
				assert.DeepEqual(t, tc.wantFailed, failedNodes)
				return
			}
			assert.Equal(t, len(failedNodes), 0)
			assert.Equal(t, len(got.NodeList), 1)
			assert.DeepEqual(t, tc.wantDevices, got.NodeList[0].Devices)

			usage := (*nodes)["node1"].Devices.DeviceLists[0].Device
			assert.Equal(t, usage.Usedmem, tc.wantUsedmem)
		})
	}
}

// Test_calcScore_AscendHamiCoreOversellConcurrency covers the reported bug that a
// pod is judged against an allocation history rather than its concurrent usage.
// Kubernetes runs ordinary init containers one at a time, while sidecar init
// containers keep running for the whole pod lifetime, and the two differ when a
// card's exclusivity is decided.
func Test_calcScore_AscendHamiCoreOversellConcurrency(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	fs := flag.NewFlagSet("ascend-oversell-concurrency", flag.ContinueOnError)
	ascend.ParseConfig(fs)
	if err := fs.Parse([]string{"--enable-ascend=true"}); err != nil {
		t.Fatalf("failed to enable the ascend backend: %v", err)
	}
	t.Cleanup(func() {
		restore := flag.NewFlagSet("ascend-oversell-concurrency-restore", flag.ContinueOnError)
		ascend.ParseConfig(restore)
		_ = restore.Parse([]string{"--enable-ascend=false"})
	})

	sConfig := &config.Config{
		VNPUs: ascend.VNPUs{
			HamiVnpuCore: true,
			Configs: []ascend.VNPUConfig{{
				CommonWord:         "Ascend910B3",
				ChipName:           "910B3",
				ResourceName:       "huawei.com/Ascend910B3",
				ResourceMemoryName: "huawei.com/Ascend910B3-memory",
				ResourceCoreName:   "huawei.com/Ascend910B3-core",
				MemoryAllocatable:  65536,
				MemoryFactor:       1,
			}},
		},
	}
	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		t.Fatalf("failed to initialize devices: %v", err)
	}
	if _, ok := device.GetDevices()["Ascend910B3"]; !ok {
		t.Fatal("the ascend backend was not registered, the test would pass vacuously")
	}

	always := corev1.ContainerRestartPolicyAlways

	newOversoldNode := func() *map[string]*NodeUsage {
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			Annotations: map[string]string{
				ascend.VNPUNodeSelectorAnnotation: "true",
			},
		}}
		return &map[string]*NodeUsage{
			"node1": {
				Node:     node,
				NodeInfo: &device.NodeInfo{ID: node.Name, Node: node},
				Devices: policy.DeviceUsageList{
					Policy: util.GPUSchedulerPolicyBinpack.String(),
					DeviceLists: []*policy.DeviceListsScore{
						{Device: &device.DeviceUsage{
							ID: "dev-0", Index: 0, Type: "Ascend910B3", Health: true,
							Count: 8, Used: 0,
							Totalcore: 150, Usedcores: 0,
							Totalmem: 65536, Usedmem: 0,
						}},
					},
				},
			},
		}
	}

	occupyWithTenant := func(cores int32) *map[string]*NodeUsage {
		nodes := newOversoldNode()
		tenant := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: "default", UID: types.UID("tenant-uid")},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		}
		for _, dl := range (*nodes)["node1"].Devices.DeviceLists {
			dl.Device.Used = 1
			dl.Device.Usedcores = cores
			dl.Device.Usedmem = 8192
			dl.Device.PodInfos = []*device.PodInfo{{
				Pod:    tenant,
				NodeID: "node1",
				Devices: device.PodDevices{
					"Ascend910B3": device.PodSingleDevice{
						{{UUID: "dev-0", Type: "Ascend910B3", Usedmem: 8192, Usedcores: cores}},
					},
				},
			}}
		}
		return nodes
	}

	ascendReq := func(cores int32, mem int32) device.ContainerDeviceRequests {
		return device.ContainerDeviceRequests{
			"Ascend910B3": {
				Nums: 1, Type: "Ascend910B3",
				Memreq: mem, MemPercentagereq: 101, Coresreq: cores,
			},
		}
	}

	hamiCorePod := func(name string, initCtrs, ctrs []corev1.Container) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "default",
				UID: types.UID(name + "-uid"),
				Annotations: map[string]string{
					util.GPUSchedulerPolicyAnnotationKey:  util.GPUSchedulerPolicyBinpack.String(),
					util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicyBinpack.String(),
					ascend.VNPUModeAnnotation:             ascend.VNPUModeHamiCore,
				},
			},
			Spec: corev1.PodSpec{InitContainers: initCtrs, Containers: ctrs},
		}
	}

	plain := func(name string, sidecar bool) corev1.Container {
		c := corev1.Container{Name: name}
		if sidecar {
			c.RestartPolicy = &always
		}
		return c
	}

	t.Run("sequential ordinary init containers do not block the pod", func(t *testing.T) {
		pod := hamiCorePod("seq-init",
			[]corev1.Container{plain("init-a", false), plain("init-b", false)},
			[]corev1.Container{plain("app", false)})
		reqs := device.PodDeviceRequests{
			ascendReq(60, 8192),
			ascendReq(60, 8192),
			ascendReq(50, 8192),
		}
		nodes := newOversoldNode()
		failedNodes := map[string]string{}
		got, err := (&Scheduler{}).calcScoreWithOptions(nodes, reqs, pod, failedNodes, false, false)
		assert.NilError(t, err)
		assert.Equal(t, len(failedNodes), 0)
		if len(got.NodeList) != 1 {
			t.Fatalf("expected the pod to be admitted, got %d nodes", len(got.NodeList))
		}
	})

	t.Run("sequential inits with another tenant are still admitted", func(t *testing.T) {
		// Tenant 50 plus one live init of 60 is 110, which fits 150. Summing
		// both sequential inits would look like 120 and trip exclusivity.
		pod := hamiCorePod("seq-init-shared",
			[]corev1.Container{plain("init-a", false), plain("init-b", false)},
			[]corev1.Container{plain("app", false)})
		reqs := device.PodDeviceRequests{
			ascendReq(60, 8192),
			ascendReq(60, 8192),
			ascendReq(50, 8192),
		}
		failedNodes := map[string]string{}
		got, err := (&Scheduler{}).calcScoreWithOptions(occupyWithTenant(50), reqs, pod, failedNodes, false, false)
		assert.NilError(t, err)
		assert.Equal(t, len(failedNodes), 0)
		if len(got.NodeList) != 1 {
			t.Fatalf("sequential inits must not be treated as a 100-core occupant, got failed=%v", failedNodes)
		}
	})

	t.Run("a sidecar and its app container are admitted", func(t *testing.T) {
		pod := hamiCorePod("sidecar-app",
			[]corev1.Container{plain("sc", true)},
			[]corev1.Container{plain("app", false)})
		reqs := device.PodDeviceRequests{
			ascendReq(60, 8192),
			ascendReq(50, 8192),
		}
		nodes := newOversoldNode()
		failedNodes := map[string]string{}
		got, err := (&Scheduler{}).calcScoreWithOptions(nodes, reqs, pod, failedNodes, false, false)
		assert.NilError(t, err)
		assert.Equal(t, len(failedNodes), 0)
		if len(got.NodeList) != 1 {
			t.Fatalf("expected the pod to be admitted, got %d nodes", len(got.NodeList))
		}
	})

	t.Run("sidecar plus app totaling 100 refuses another tenant", func(t *testing.T) {
		// App-phase allocated used to omit the sidecar (it stayed in initAllocs),
		// so 50+50 was admitted beside an existing tenant. The sidecar is still
		// running, so this pod reaches the exclusive base.
		pod := hamiCorePod("sidecar-exclusive",
			[]corev1.Container{plain("sc", true)},
			[]corev1.Container{plain("app", false)})
		reqs := device.PodDeviceRequests{
			ascendReq(50, 8192),
			ascendReq(50, 8192),
		}
		failedNodes := map[string]string{}
		got, err := (&Scheduler{}).calcScoreWithOptions(occupyWithTenant(50), reqs, pod, failedNodes, false, false)
		assert.NilError(t, err)
		if len(got.NodeList) != 0 {
			t.Fatalf("sidecar 50 + app 50 must not share with another tenant")
		}
		if len(failedNodes) == 0 {
			t.Fatalf("expected a recorded rejection reason")
		}
	})
}

// Test_calcScore_AllocationRowsStayInLockstep pins the invariant the ascend
// hami-core exclusivity check relies on: the scheduler records exactly one row
// per pod container for every device type, so all types have the same number of
// rows and that number is the index of the container being fitted.
func Test_calcScore_AllocationRowsStayInLockstep(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	fs := flag.NewFlagSet("ascend-row-lockstep", flag.ContinueOnError)
	ascend.ParseConfig(fs)
	if err := fs.Parse([]string{"--enable-ascend=true"}); err != nil {
		t.Fatalf("failed to enable the ascend backend: %v", err)
	}
	t.Cleanup(func() {
		restore := flag.NewFlagSet("ascend-row-lockstep-restore", flag.ContinueOnError)
		ascend.ParseConfig(restore)
		_ = restore.Parse([]string{"--enable-ascend=false"})
	})

	sConfig := &config.Config{
		VNPUs: ascend.VNPUs{
			HamiVnpuCore: true,
			Configs: []ascend.VNPUConfig{
				{
					CommonWord:         "Ascend910B3",
					ChipName:           "910B3",
					ResourceName:       "huawei.com/Ascend910B3",
					ResourceMemoryName: "huawei.com/Ascend910B3-memory",
					ResourceCoreName:   "huawei.com/Ascend910B3-core",
					MemoryAllocatable:  65536,
					MemoryFactor:       1,
				},
				{
					CommonWord:         "Ascend910B4",
					ChipName:           "910B4",
					ResourceName:       "huawei.com/Ascend910B4",
					ResourceMemoryName: "huawei.com/Ascend910B4-memory",
					ResourceCoreName:   "huawei.com/Ascend910B4-core",
					MemoryAllocatable:  65536,
					MemoryFactor:       1,
				},
			},
		},
	}
	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		t.Fatalf("failed to initialize devices: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "lockstep", Namespace: "default",
			UID: types.UID("lockstep-uid"),
			Annotations: map[string]string{
				util.GPUSchedulerPolicyAnnotationKey:  util.GPUSchedulerPolicyBinpack.String(),
				util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicyBinpack.String(),
				ascend.VNPUModeAnnotation:             ascend.VNPUModeHamiCore,
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "app"}},
		},
	}
	reqs := device.PodDeviceRequests{
		{"Ascend910B3": {Nums: 1, Type: "Ascend910B3", Memreq: 8192, MemPercentagereq: 101, Coresreq: 20}},
		{"Ascend910B4": {Nums: 1, Type: "Ascend910B4", Memreq: 8192, MemPercentagereq: 101, Coresreq: 20}},
	}

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:        "node1",
		Annotations: map[string]string{ascend.VNPUNodeSelectorAnnotation: "true"},
	}}
	var lists []*policy.DeviceListsScore
	for _, typ := range []string{"Ascend910B3", "Ascend910B4"} {
		lists = append(lists, &policy.DeviceListsScore{Device: &device.DeviceUsage{
			ID: "dev-" + typ, Index: 0, Type: typ, Health: true,
			Count: 8, Totalcore: 150, Totalmem: 65536,
		}})
	}
	nodes := map[string]*NodeUsage{
		"node1": {
			Node:     node,
			NodeInfo: &device.NodeInfo{ID: "node1", Node: node},
			Devices:  policy.DeviceUsageList{Policy: util.GPUSchedulerPolicyBinpack.String(), DeviceLists: lists},
		},
	}

	failedNodes := map[string]string{}
	got, err := (&Scheduler{}).calcScoreWithOptions(&nodes, reqs, pod, failedNodes, false, false)
	assert.NilError(t, err)
	assert.Equal(t, len(failedNodes), 0)
	if len(got.NodeList) != 1 {
		t.Fatalf("expected the pod to be admitted, got %d nodes", len(got.NodeList))
	}

	wantRows := len(pod.Spec.InitContainers) + len(pod.Spec.Containers)
	for typ, rows := range got.NodeList[0].Devices {
		if len(rows) != wantRows {
			t.Fatalf("device type %s recorded %d rows, want %d (one per container); "+
				"pkg/device/ascend derives the container index from this row count",
				typ, len(rows), wantRows)
		}
	}
}

// mixedCapacityNode holds two NVIDIA cards of different memory capacity,
// registered the way the device plugin registers them: the device type is the
// card's model name, and the vendor is the "NVIDIA" common word the request
// carries. It goes through buildNodeUsage so the vendor is carried into
// DeviceUsage the same way the scheduler does it.
func mixedCapacityNode(task *corev1.Pod) *NodeUsage {
	nodeInfo := &device.NodeInfo{
		ID:   "mixed-node",
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "mixed-node"}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "gpu-40g", Index: 0, Count: 10, Devmem: 40960, Devcore: 100, Numa: 0,
				Type: "NVIDIA A100-SXM4-40GB", DeviceVendor: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "gpu-80g", Index: 1, Count: 10, Devmem: 81920, Devcore: 100, Numa: 0,
				Type: "NVIDIA A100-SXM4-80GB", DeviceVendor: nvidia.NvidiaGPUDevice, Health: true},
		}},
	}
	node := buildNodeUsage(nodeInfo, task)
	for _, deviceList := range node.Devices.DeviceLists {
		if deviceList.Device.ID == "gpu-80g" {
			deviceList.Device.Used = 1
			deviceList.Device.Usedcores = 5
			deviceList.Device.Usedmem = 4096
		}
	}
	return node
}

// Binpack must rank cards by their utilisation *after* the pending request is
// placed. On a node whose cards differ in capacity, scoring on current usage
// alone sends the pod to the 80GB card (0.20 used today vs 0.00) even though
// the 40GB card is the tighter fit for a 20GB request (0.90 vs 0.85 after
// placement) and keeping the large card free is the point of binpack.
func TestBinpackPacksSmallerCardOnMixedCapacityNode(t *testing.T) {
	task := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:        "trainer",
		Namespace:   "default",
		Annotations: map[string]string{util.GPUSchedulerPolicyAnnotationKey: util.GPUSchedulerPolicyBinpack.String()},
	}}
	node := mixedCapacityNode(task)
	requests := device.ContainerDeviceRequests{
		nvidia.NvidiaGPUDevice: {
			Nums:             1,
			Type:             nvidia.NvidiaGPUDevice,
			Memreq:           20480,
			MemPercentagereq: 101,
			Coresreq:         30,
		},
	}

	devinput := &device.PodDevices{}
	fit, reason := fitInDevices(node, requests, task, nil, devinput, util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "expected the pod to fit; reason=%s", reason)

	containers := (*devinput)[nvidia.NvidiaGPUDevice]
	assert.Equal(t, len(containers), 1)
	assert.Equal(t, len(containers[0]), 1)
	assert.Equal(t, containers[0][0].UUID, "gpu-40g",
		"binpack placed the pod by current usage instead of usage after placement")
}
