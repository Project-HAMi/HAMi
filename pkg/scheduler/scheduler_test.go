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
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	nodelockutil "github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

func Test_getNodesUsage(t *testing.T) {
	nodeMage := newNodeManager()
	nodeMage.addNode("node1", &device.NodeInfo{
		ID: "node1",
		Node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Mode:    "hami",
				Health:  true,
			},
				{
					ID:      "GPU1",
					Index:   1,
					Count:   10,
					Devmem:  1024,
					Devcore: 100,
					Numa:    1,
					Mode:    "hami",
					Health:  true,
				}},
		},
	})
	podDevces := device.PodDevices{
		"NVIDIA": device.PodSingleDevice{
			[]device.ContainerDevice{
				{
					Idx:       0,
					UUID:      "GPU0",
					Usedmem:   100,
					Usedcores: 10,
				},
			},
		},
	}
	podMap := device.NewPodManager()
	podMap.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "1111",
			Name:      "test1",
			Namespace: "default",
		},
	}, "node1", podDevces)
	podMap.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "2222",
			Name:      "test2",
			Namespace: "default",
		},
	}, "node1", podDevces)
	s := Scheduler{
		nodeManager: nodeMage,
		podManager:  podMap,
	}
	nodes := make([]string, 0)
	nodes = append(nodes, "node1")
	cachenodeMap, _, _, err := s.getNodesUsage(&nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(*cachenodeMap), 1)
	v, ok := (*cachenodeMap)["node1"]
	assert.Equal(t, ok, true)
	assert.Equal(t, len(v.Devices.DeviceLists), 2)
	assert.Equal(t, v.Devices.DeviceLists[0].Device.Used, int32(2))
	assert.Equal(t, v.Devices.DeviceLists[0].Device.Usedmem, int32(200))
	assert.Equal(t, v.Devices.DeviceLists[0].Device.Usedcores, int32(20))
}

func Test_getNodesUsage_StaleMigIndexDoesNotPanic(t *testing.T) {
	nodeMage := newNodeManager()
	nodeMage.addNode("node1", &device.NodeInfo{
		ID: "node1",
		Node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Mode:    "mig",
				Health:  true,
				MIGTemplate: []device.Geometry{
					{{Name: "1g.5gb", Memory: 5, Count: 1}},
				},
			}},
		},
	})
	// tmpIdx=99 is far past len(MIGTemplate)==1: a stale/corrupt annotation
	// must not panic PlatternMIG or the UsageList index write.
	podDevces := device.PodDevices{
		"NVIDIA": device.PodSingleDevice{
			[]device.ContainerDevice{
				{
					Idx:       0,
					UUID:      "GPU0[99-0]",
					Usedmem:   100,
					Usedcores: 10,
				},
			},
		},
	}
	podMap := device.NewPodManager()
	podMap.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "1111",
			Name:      "test1",
			Namespace: "default",
		},
	}, "node1", podDevces)
	s := Scheduler{
		nodeManager: nodeMage,
		podManager:  podMap,
	}
	nodes := []string{"node1"}
	cachenodeMap, _, _, err := s.getNodesUsage(&nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := (*cachenodeMap)["node1"]
	assert.Assert(t, ok)
	assert.Equal(t, int32(1), v.Devices.DeviceLists[0].Device.Used)
	assert.Equal(t, 0, len(v.Devices.DeviceLists[0].Device.MigUsage.UsageList))
}

func Test_getNodesUsage_UnparsableMigUUIDDoesNotPanic(t *testing.T) {
	nodeMage := newNodeManager()
	nodeMage.addNode("node1", &device.NodeInfo{
		ID: "node1",
		Node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Mode:    "mig",
				Health:  true,
				MIGTemplate: []device.Geometry{
					{{Name: "1g.5gb", Memory: 5, Count: 1}},
				},
			}},
		},
	})
	// "abc" fails strconv.Atoi inside ExtractMigTemplatesFromUUID: exercises
	// the parse-error branch, distinct from the out-of-range branch above.
	podMap := device.NewPodManager()
	podMap.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "1111", Name: "test1", Namespace: "default"},
	}, "node1", device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			[]device.ContainerDevice{{Idx: 0, UUID: "GPU0[abc-0]", Usedmem: 100, Usedcores: 10}},
		},
	})
	s := Scheduler{
		nodeManager: nodeMage,
		podManager:  podMap,
	}
	nodes := []string{"node1"}
	cachenodeMap, _, _, err := s.getNodesUsage(&nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := (*cachenodeMap)["node1"]
	assert.Assert(t, ok)
	assert.Equal(t, int32(1), v.Devices.DeviceLists[0].Device.Used)
	assert.Equal(t, 0, len(v.Devices.DeviceLists[0].Device.MigUsage.UsageList))
}

func Test_getNodesUsage_OutOfRangeMigInstanceSkipped(t *testing.T) {
	nodeMage := newNodeManager()
	nodeMage.addNode("node1", &device.NodeInfo{
		ID: "node1",
		Node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Mode:    "mig",
				Health:  true,
				MIGTemplate: []device.Geometry{
					{{Name: "1g.5gb", Memory: 5, Count: 1}},
				},
			}},
		},
	})
	// Template index 0 is valid and populates a 1-entry UsageList, but the
	// instance position "5" is past its end: exercises the instance bounds
	// check separately from the template bounds check above.
	podMap := device.NewPodManager()
	podMap.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "1111", Name: "test1", Namespace: "default"},
	}, "node1", device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			[]device.ContainerDevice{{Idx: 0, UUID: "GPU0[0-5]", Usedmem: 100, Usedcores: 10}},
		},
	})
	s := Scheduler{
		nodeManager: nodeMage,
		podManager:  podMap,
	}
	nodes := []string{"node1"}
	cachenodeMap, _, _, err := s.getNodesUsage(&nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := (*cachenodeMap)["node1"]
	assert.Assert(t, ok)
	assert.Equal(t, int32(1), v.Devices.DeviceLists[0].Device.Used)
	assert.Equal(t, 1, len(v.Devices.DeviceLists[0].Device.MigUsage.UsageList))
	assert.Assert(t, !v.Devices.DeviceLists[0].Device.MigUsage.UsageList[0].InUse)
}

func Test_getNodesUsage_MismatchedMigIndexSkipped(t *testing.T) {
	nodeMage := newNodeManager()
	nodeMage.addNode("node1", &device.NodeInfo{
		ID: "node1",
		Node: &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
			},
		},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Mode:    "mig",
				Health:  true,
				MIGTemplate: []device.Geometry{
					{{Name: "1g.5gb", Memory: 5, Count: 1}},
					{{Name: "2g.10gb", Memory: 10, Count: 1}},
				},
			}},
		},
	})
	// Two pods on the same device disagree on which geometry (template index)
	// is active. Whichever is processed first wins; the other must be
	// skipped rather than writing its Instance into the wrong UsageList.
	podMap := device.NewPodManager()
	podMap.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "1111", Name: "test1", Namespace: "default"},
	}, "node1", device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			[]device.ContainerDevice{{Idx: 0, UUID: "GPU0[0-0]", Usedmem: 100, Usedcores: 10}},
		},
	})
	podMap.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "2222", Name: "test2", Namespace: "default"},
	}, "node1", device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			[]device.ContainerDevice{{Idx: 0, UUID: "GPU0[1-0]", Usedmem: 100, Usedcores: 10}},
		},
	})
	s := Scheduler{
		nodeManager: nodeMage,
		podManager:  podMap,
	}
	nodes := []string{"node1"}
	cachenodeMap, _, _, err := s.getNodesUsage(&nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := (*cachenodeMap)["node1"]
	assert.Assert(t, ok)
	dev := v.Devices.DeviceLists[0].Device
	assert.Equal(t, int32(2), dev.Used)
	assert.Equal(t, 1, len(dev.MigUsage.UsageList))
	assert.Assert(t, dev.MigUsage.UsageList[0].InUse)
}

// test case matrix
/**
| pod name     | node name|  pod status | annotations                 |             result                  |
|--------------|----------|-------------|---------------------------- |-------------------------------------|
| test-pod-1   | node11   |  Succeeded  |  hami.io/bind-phase:success |  node11:{TotalPod:1,UseDevicePod:1} |
| test-pod-2   | node12   |  Running    |  none                       |  node12:{TotalPod:0;UseDevicePod:0} |
| test-pod-3   | node13   |  Succeeded  |  none                       |  node13:{TotalPod:1;UseDevicePod:0} |
test case matrix.
*/

func Test_getPodUsage(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour*1)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	tests := []struct {
		name    string
		pods    []*corev1.Pod
		want    map[string]device.PodUseDeviceStat
		wantErr error
	}{
		{
			name: "One pod running",
			pods: []*corev1.Pod{
				{
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-2",
						Namespace: "default",
						UID:       "uuid12",
					},
					Spec: corev1.PodSpec{
						NodeName: "node12",
					},
				},
			},
			want: map[string]device.PodUseDeviceStat{
				"node12": {
					TotalPod:     0, // Running pod does not count
					UseDevicePod: 0,
				},
			},
		},
		{
			name: "one pod succeeded,no annotation",
			pods: []*corev1.Pod{
				{
					Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-3",
						Namespace: "default",
						UID:       "uuid14",
					},
					Spec: corev1.PodSpec{
						NodeName: "node13",
					},
				},
			},
			want: map[string]device.PodUseDeviceStat{
				"node13": {
					TotalPod:     1,
					UseDevicePod: 0, // No annotation
				},
			},
		},
		{
			name: "All pods succeeded with device bind success",
			pods: []*corev1.Pod{
				{
					Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-pod-1",
						Namespace:   "default",
						UID:         "uuid11",
						Annotations: map[string]string{util.DeviceBindPhase: util.DeviceBindSuccess},
					},
					Spec: corev1.PodSpec{
						NodeName: "node11",
					},
				},
			},
			want: map[string]device.PodUseDeviceStat{
				"node11": {
					TotalPod:     1,
					UseDevicePod: 1,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, pod := range test.pods {
				client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
				s.podManager.AddPod(pod, pod.Spec.NodeName, device.PodDevices{})
			}

			result, err := s.getPodUsage()
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, test.want[test.pods[0].Namespace], result[test.pods[0].Namespace])
		})
	}
}

// test case matrix
/**
| node policy|  gpu policy| node num | per node device | pod use device | device use info           | result       |
|------------|------------|----------|-----------------|----------------|---------------------------|--------------|
| binpack    |  binpack	  | 2        |  2              |  1             ｜device1: 25%,device4: 75% ｜ node2-device4|
| binpack    |  spread	  | 2        |  2              |  1             ｜device1: 25%,device4: 75% ｜ node2-device3|
| spread     |  binpack	  | 2        |  2              |  1             ｜device1: 25%,device4: 75% ｜ node1-device1|
| spread     |  spread	  | 2        |  2              |  1             ｜device1: 25%,device4: 75% ｜ node1-device2|
test case matrix.
*/
func Test_Filter(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour*1)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informer := informerFactory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAddPod,
		UpdateFunc: s.onUpdatePod,
		DeleteFunc: s.onDelPod,
	})
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)
	s.addAllEventHandlers()
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
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod1",
			UID:  "uuid1",
			Annotations: map[string]string{
				util.DeviceBindPhase: util.DeviceBindSuccess,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node1",
			Containers: []corev1.Container{
				{
					Name:  "gpu-burn",
					Image: "chrstnhntschl/gpu_burn",
					Args:  []string{"6000"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
							"hami.io/gpucores": *resource.NewQuantity(25, resource.BinarySI),
							"hami.io/gpumem":   *resource.NewQuantity(2000, resource.BinarySI),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod2",
			UID:  "uuid2",
			Annotations: map[string]string{
				util.DeviceBindPhase: util.DeviceBindSuccess,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node2",
			Containers: []corev1.Container{
				{
					Name:  "gpu-burn",
					Image: "chrstnhntschl/gpu_burn",
					Args:  []string{"6000"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
							"hami.io/gpucores": *resource.NewQuantity(75, resource.BinarySI),
							"hami.io/gpumem":   *resource.NewQuantity(6000, resource.BinarySI),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod3",
			UID:  "uuid3",
		},
		Spec: corev1.PodSpec{
			NodeName: "node2",
			Containers: []corev1.Container{
				{
					Name:  "gpu-burn",
					Image: "chrstnhntschl/gpu_burn",
					Args:  []string{"6000"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}
	client.KubeClient.CoreV1().Pods(pod1.Namespace).Create(context.Background(), pod1, metav1.CreateOptions{})
	client.KubeClient.CoreV1().Pods(pod2.Namespace).Create(context.Background(), pod2, metav1.CreateOptions{})
	client.KubeClient.CoreV1().Pods(pod3.Namespace).Create(context.Background(), pod3, metav1.CreateOptions{})

	initNode := func() {
		nodes, _ := s.ListNodes()
		for index := range nodes {
			s.rmNodeDevices(index, nvidia.NvidiaGPUDevice)
		}
		pods, _ := s.podManager.ListPodsUID()
		for index := range pods {
			s.podManager.DelPod(pods[index])
		}

		s.addNode("node1", &device.NodeInfo{
			ID:   "node1",
			Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
			Devices: map[string][]device.DeviceInfo{
				nvidia.NvidiaGPUDevice: {{
					ID:           "device1",
					Index:        0,
					Count:        10,
					Devmem:       8000,
					Devcore:      100,
					Numa:         0,
					Mode:         "hami",
					Type:         nvidia.NvidiaGPUDevice,
					Health:       true,
					DeviceVendor: nvidia.NvidiaGPUDevice,
				},
					{
						ID:           "device2",
						Index:        1,
						Count:        10,
						Devmem:       8000,
						Devcore:      100,
						Numa:         0,
						Type:         nvidia.NvidiaGPUDevice,
						Health:       true,
						DeviceVendor: nvidia.NvidiaGPUDevice,
					}},
			},
		})
		s.addNode("node2", &device.NodeInfo{
			ID:   "node2",
			Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			Devices: map[string][]device.DeviceInfo{
				nvidia.NvidiaGPUDevice: {{
					ID:           "device3",
					Index:        0,
					Count:        10,
					Devmem:       8000,
					Devcore:      100,
					Numa:         0,
					Mode:         "hami",
					Type:         nvidia.NvidiaGPUDevice,
					Health:       true,
					DeviceVendor: nvidia.NvidiaGPUDevice,
				},
					{
						ID:           "device4",
						Index:        1,
						Count:        10,
						Devmem:       8000,
						Devcore:      100,
						Numa:         0,
						Type:         nvidia.NvidiaGPUDevice,
						Health:       true,
						DeviceVendor: nvidia.NvidiaGPUDevice,
					}},
			},
		})
		s.podManager.AddPod(pod1, "node1", device.PodDevices{
			nvidia.NvidiaGPUDevice: device.PodSingleDevice{
				{
					{
						Idx:       0,
						UUID:      "device1",
						Type:      nvidia.NvidiaGPUDevice,
						Usedmem:   2000,
						Usedcores: 25,
					},
				},
			},
		})
		s.podManager.AddPod(pod2, "node2", device.PodDevices{
			nvidia.NvidiaGPUDevice: device.PodSingleDevice{
				{
					{
						Idx:       0,
						UUID:      "device4",
						Type:      nvidia.NvidiaGPUDevice,
						Usedmem:   6000,
						Usedcores: 75,
					},
				},
			},
		})
	}

	tests := []struct {
		name                      string
		args                      extenderv1.ExtenderArgs
		want                      *extenderv1.ExtenderFilterResult
		wantPodAnnotationDeviceID string
		wantErr                   error
	}{
		{
			name: "node use binpack gpu use binpack policy",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
						UID:  "test1-uid1",
						Annotations: map[string]string{
							util.GPUSchedulerPolicyAnnotationKey:  util.GPUSchedulerPolicyBinpack.String(),
							util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicyBinpack.String(),
						},
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
										"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1", "node2"},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				NodeNames: &[]string{"node2"},
			},
			wantPodAnnotationDeviceID: "device4",
		},
		{
			name: "node use binpack gpu use spread policy",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test2",
						UID:  "test2-uid2",
						Annotations: map[string]string{
							util.GPUSchedulerPolicyAnnotationKey:  util.GPUSchedulerPolicySpread.String(),
							util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicyBinpack.String(),
						},
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
										"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1", "node2"},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				NodeNames: &[]string{"node2"},
			},
			wantPodAnnotationDeviceID: "device3",
		},
		{
			name: "node use spread gpu use binpack policy",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test3",
						UID:  "test3-uid3",
						Annotations: map[string]string{
							util.GPUSchedulerPolicyAnnotationKey:  util.GPUSchedulerPolicyBinpack.String(),
							util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicySpread.String(),
						},
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
										"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1", "node2"},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				NodeNames: &[]string{"node1"},
			},
			wantPodAnnotationDeviceID: "device1",
		},
		{
			name: "node use spread gpu use spread policy",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test4",
						UID:  "test4-uid4",
						Annotations: map[string]string{
							util.GPUSchedulerPolicyAnnotationKey:  util.GPUSchedulerPolicySpread.String(),
							util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicySpread.String(),
						},
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
										"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1", "node2"},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				NodeNames: &[]string{"node1"},
			},
			wantPodAnnotationDeviceID: "device2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initNode()
			client.KubeClient.CoreV1().Pods(test.args.Pod.Namespace).Create(context.Background(), test.args.Pod, metav1.CreateOptions{})
			got, gotErr := s.Filter(test.args)
			assert.DeepEqual(t, test.wantErr, gotErr)
			assert.DeepEqual(t, test.want, got)
			getPod, _ := client.KubeClient.CoreV1().Pods(test.args.Pod.Namespace).Get(context.Background(), test.args.Pod.Name, metav1.GetOptions{})
			podDevices, _ := device.DecodePodDevices(device.SupportDevices, getPod.Annotations)
			assert.DeepEqual(t, test.wantPodAnnotationDeviceID, podDevices["NVIDIA"][0][0].UUID)
		})
	}
}

func TestSchedulerOnDelQuotaClearsLimitFromTombstone(t *testing.T) {
	const namespace = "quota-tombstone-test"

	s := NewScheduler()

	nvidiaDevice, ok := device.GetDevices()[nvidia.NvidiaGPUDevice]
	require.True(t, ok, "NVIDIA device should be registered")

	resourceName := nvidiaDevice.GetResourceNames().ResourceMemoryName
	require.NotEmpty(t, resourceName, "NVIDIA memory resource name should not be empty")

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-quota",
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits." + resourceName): *resource.NewQuantity(1024, resource.BinarySI),
			},
		},
	}

	s.onAddQuota(quota)
	t.Cleanup(func() {
		s.onDelQuota(quota)
	})

	quotas := s.quotaManager.GetResourceQuota()
	namespaceQuota, ok := quotas[namespace]
	require.True(t, ok)
	require.Equal(t, int64(1024), (*namespaceQuota)[resourceName].Limit)

	s.onDelQuota(cache.DeletedFinalStateUnknown{
		Key: namespace + "/" + quota.Name,
		Obj: quota,
	})

	quotas = s.quotaManager.GetResourceQuota()
	namespaceQuota, ok = quotas[namespace]
	require.True(t, ok)
	require.Equal(t, int64(0), (*namespaceQuota)[resourceName].Limit)
}

func TestSchedulerOnDelQuotaIgnoresInvalidObjects(t *testing.T) {
	const namespace = "invalid-quota-delete-test"

	s := NewScheduler()

	nvidiaDevice, ok := device.GetDevices()[nvidia.NvidiaGPUDevice]
	require.True(t, ok, "NVIDIA device should be registered")

	resourceName := nvidiaDevice.GetResourceNames().ResourceMemoryName
	require.NotEmpty(t, resourceName)

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-quota",
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits." + resourceName): *resource.NewQuantity(1024, resource.BinarySI),
			},
		},
	}

	s.onAddQuota(quota)
	t.Cleanup(func() {
		s.onDelQuota(quota)
	})

	quotas := s.quotaManager.GetResourceQuota()
	namespaceQuota, ok := quotas[namespace]
	require.True(t, ok)
	expectedLimit := (*namespaceQuota)[resourceName].Limit

	tests := []struct {
		name string
		obj  any
	}{
		{
			name: "tombstone containing non-quota object",
			obj: cache.DeletedFinalStateUnknown{
				Key: "test/invalid",
				Obj: struct{}{},
			},
		},
		{
			name: "unknown delete object",
			obj:  struct{}{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				s.onDelQuota(test.obj)
			})

			currentQuotas := s.quotaManager.GetResourceQuota()
			currentNamespaceQuota, ok := currentQuotas[namespace]
			require.True(t, ok)
			require.Equal(t, expectedLimit, (*currentNamespaceQuota)[resourceName].Limit)
		})
	}
}

func TestSchedulerOnDelNodeCleansLockDirectNode(t *testing.T) {
	nodelockutil.ResetNodeLocksForTest()
	t.Cleanup(nodelockutil.ResetNodeLocksForTest)
	const nodeName = "node-direct"
	nodelockutil.EnsureNodeLockForTest(nodeName)
	require.Equal(t, 1, nodelockutil.NodeLockCountForTest())

	s := NewScheduler()
	select {
	case <-s.nodeNotify:
	default:
	}

	s.onDelNode(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})

	select {
	case <-s.nodeNotify:
	case <-time.After(time.Second):
		t.Fatal("expected node notification on delete")
	}

	require.Equal(t, 0, nodelockutil.NodeLockCountForTest())
}

func TestSchedulerOnDelNodeCleansLockFromTombstone(t *testing.T) {
	nodelockutil.ResetNodeLocksForTest()
	t.Cleanup(nodelockutil.ResetNodeLocksForTest)
	const nodeName = "node-tomb"
	nodelockutil.EnsureNodeLockForTest(nodeName)
	require.Equal(t, 1, nodelockutil.NodeLockCountForTest())

	s := NewScheduler()

	s.onDelNode(cache.DeletedFinalStateUnknown{Obj: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}})

	select {
	case <-s.nodeNotify:
	case <-time.After(time.Second):
		t.Fatal("expected node notification from tombstone delete")
	}

	require.Equal(t, 0, nodelockutil.NodeLockCountForTest())
}

func TestSchedulerOnDelNodeIgnoresNonNodeObjects(t *testing.T) {
	nodelockutil.ResetNodeLocksForTest()
	t.Cleanup(nodelockutil.ResetNodeLocksForTest)
	nodelockutil.EnsureNodeLockForTest("keep-node")
	initial := nodelockutil.NodeLockCountForTest()

	s := NewScheduler()

	s.onDelNode(cache.DeletedFinalStateUnknown{Obj: struct{}{}})
	select {
	case <-s.nodeNotify:
	case <-time.After(time.Second):
		t.Fatal("expected notification for tombstone with non-node")
	}

	s.onDelNode(struct{}{})
	select {
	case <-s.nodeNotify:
	case <-time.After(time.Second):
		t.Fatal("expected notification for unknown object delete")
	}

	require.Equal(t, initial, nodelockutil.NodeLockCountForTest())
}

func Test_RegisterFromNodeAnnotations(t *testing.T) {
	tests := []struct {
		name      string
		Scheduler *Scheduler
		want      func(node *corev1.Node) bool
	}{
		{
			name: "test node handshake annotations layout",
			Scheduler: func() *Scheduler {
				s := NewScheduler()
				s.stopCh = make(chan struct{})
				s.nodeNotify = make(chan struct{})
				client.KubeClient = fake.NewClientset()
				s.kubeClient = client.KubeClient
				informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour*1)
				s.nodeLister = informerFactory.Core().V1().Nodes().Lister()

				// Create a node
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node",
						Annotations: map[string]string{
							"hami.io/node-handshake":     "Requesting_2025-06-13 09:07:40",
							"hami.io/node-handshake-dcu": "Requesting_2025-06-13 09:07:40",
						},
					},
				}
				_, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create node: %v", err)
					return nil
				}

				// Add node to informer cache
				err = informerFactory.Core().V1().Nodes().Informer().GetIndexer().Add(node)
				if err != nil {
					t.Errorf("failed to add node to indexer: %v", err)
					return nil
				}

				// Start informer factory to sync cache
				informerFactory.Start(s.stopCh)
				informerFactory.WaitForCacheSync(s.stopCh)

				return s
			}(),
			want: func(node *corev1.Node) bool {
				handshakeTimeStr, okHami := node.Annotations["hami.io/node-handshake"]
				if !okHami {
					t.Errorf("missing annotation: hami.io/node-handshake")
					return false
				}
				dcuTimeStr, okDcu := node.Annotations["hami.io/node-handshake-dcu"]
				if !okDcu {
					t.Errorf("missing annotation: hami.io/node-handshake-dcu")
					return false
				}
				_, errHami := time.Parse(time.DateTime, strings.TrimPrefix(handshakeTimeStr, "Requesting_"))
				_, errDcu := time.Parse(time.DateTime, strings.TrimPrefix(dcuTimeStr, "Requesting_"))
				if errHami != nil {
					t.Errorf("invalid time format in annotation 'hami.io/node-handshake': %v", errHami)
					return false
				}
				if errDcu != nil {
					t.Errorf("invalid time format in annotation 'hami.io/node-handshake-dcu': %v", errDcu)
					return false
				}
				return true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Graceful shutdown after 5 seconds
			time.AfterFunc(5*time.Second, func() {
				close(test.Scheduler.stopCh)
			})

			// Notify node annotations
			go func() {
				test.Scheduler.nodeNotify <- struct{}{}
			}()

			// Invoke the method to test
			test.Scheduler.RegisterFromNodeAnnotations()

			// Get the node to verify annotations
			node, err := test.Scheduler.kubeClient.CoreV1().Nodes().Get(context.TODO(), "node", metav1.GetOptions{})
			if err != nil {
				t.Errorf("failed to get node: %v", err)
				return
			}

			// Verify the annotations
			assert.Equal(t, test.want(node), true)
		})
	}
}

func Test_RegisterFromNodeAnnotations_NIL(t *testing.T) {
	// Define a helper function to create a scheduler with a node that has nil annotations.
	createSchedulerWithNilAnnotations := func() *Scheduler {
		s := NewScheduler()
		s.stopCh = make(chan struct{})
		s.nodeNotify = make(chan struct{})

		client.KubeClient = fake.NewClientset()
		s.kubeClient = client.KubeClient

		informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
		s.nodeLister = informerFactory.Core().V1().Nodes().Lister()

		// Create a node without annotations (nil annotations)
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-nil-annotations",
			},
		}

		// Create the node and add it to the indexer
		_, err := s.kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create node: %v", err)
		}
		err = informerFactory.Core().V1().Nodes().Informer().GetIndexer().Add(node)
		if err != nil {
			t.Fatalf("failed to add node to indexer: %v", err)
		}

		// Start informer factory to sync cache
		informerFactory.Start(s.stopCh)

		// Check if cache sync was successful
		_ = informerFactory.WaitForCacheSync(s.stopCh)

		return s
	}

	tests := []struct {
		name      string
		Scheduler *Scheduler
		want      func(*corev1.Node) bool
	}{
		{
			name:      "test nil annotations handling",
			Scheduler: createSchedulerWithNilAnnotations(),
			want: func(node *corev1.Node) bool {
				if node == nil {
					t.Errorf("node is nil")
					return false
				}

				// Check if RegisterFromNodeAnnotations handles nil annotations gracefully
				if node.Annotations == nil {
					t.Logf("node annotations are nil, checking if handled properly...")
					return true // Adjust based on expected behavior
				}

				// If annotations exist, check for specific annotations
				handshakeTimeStr, okHami := node.Annotations["hami.io/node-handshake"]
				dcuTimeStr, okDcu := node.Annotations["hami.io/node-handshake-dcu"]

				// Here you can define what should happen when annotations are present but not set
				if !okHami || !okDcu {
					t.Logf("expected annotations are missing, checking if handled properly...")
					return true // Adjust based on expected behavior
				}

				// Verify time format in annotations if they exist
				_, errHami := time.Parse(time.DateTime, strings.TrimPrefix(handshakeTimeStr, "Requesting_"))
				_, errDcu := time.Parse(time.DateTime, strings.TrimPrefix(dcuTimeStr, "Requesting_"))

				if errHami != nil {
					t.Errorf("invalid time format in annotation 'hami.io/node-handshake': %v", errHami)
					return false
				}
				if errDcu != nil {
					t.Errorf("invalid time format in annotation 'hami.io/node-handshake-dcu': %v", errDcu)
					return false
				}

				return true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Recovered from panic: %v", r)
				}
			}()

			// Ensure scheduler starts before running the test
			time.AfterFunc(5*time.Second, func() {
				close(test.Scheduler.stopCh)
			})

			// Notify node annotations after a short delay to ensure scheduler is ready
			go func() {
				time.Sleep(100 * time.Millisecond) // Give some time for scheduler to start
				test.Scheduler.nodeNotify <- struct{}{}
			}()

			// Invoke the method to test
			test.Scheduler.RegisterFromNodeAnnotations()

			// Get the node to verify annotations
			node, err := test.Scheduler.kubeClient.CoreV1().Nodes().Get(context.TODO(), "node-nil-annotations", metav1.GetOptions{})
			require.NoError(t, err) // Use require to fail fast on error
			require.NotNil(t, node) // Ensure node is not nil

			// Verify the annotations
			if !test.want(node) {
				t.Errorf("annotations validation failed")
			}
		})
	}
}

type registerMockDevice struct {
	nodeDevices   []*device.DeviceInfo
	getNodeErr    error
	health        bool
	needUpdate    bool
	nodeCleanedUp int
}

func (m *registerMockDevice) CommonWord() string { return "mock-vendor" }
func (m *registerMockDevice) MutateAdmission(_ *corev1.Container, _ *corev1.Pod) (bool, error) {
	return false, nil
}
func (m *registerMockDevice) CheckHealth(_ string, _ *corev1.Node) (bool, bool) {
	return m.health, m.needUpdate
}
func (m *registerMockDevice) NodeCleanUp(_ string) error {
	m.nodeCleanedUp++
	return nil
}
func (m *registerMockDevice) GetResourceNames() device.ResourceNames { return device.ResourceNames{} }
func (m *registerMockDevice) GetNodeDevices(_ corev1.Node) ([]*device.DeviceInfo, error) {
	return m.nodeDevices, m.getNodeErr
}
func (m *registerMockDevice) LockNode(_ *corev1.Node, _ *corev1.Pod) error        { return nil }
func (m *registerMockDevice) ReleaseNodeLock(_ *corev1.Node, _ *corev1.Pod) error { return nil }
func (m *registerMockDevice) GenerateResourceRequests(_ *corev1.Container) device.ContainerDeviceRequest {
	return device.ContainerDeviceRequest{}
}
func (m *registerMockDevice) PatchAnnotations(_ *corev1.Pod, _ *map[string]string, _ device.PodDevices) map[string]string {
	return nil
}
func (m *registerMockDevice) ScoreNode(_ *corev1.Node, _ device.PodSingleDevice, _ []*device.DeviceUsage, _ string) float32 {
	return 0
}
func (m *registerMockDevice) AddResourceUsage(_ *corev1.Pod, _ *device.DeviceUsage, _ *device.ContainerDevice) error {
	return nil
}
func (m *registerMockDevice) Fit(_ []*device.DeviceUsage, _ device.ContainerDeviceRequest, _ *corev1.Pod, _ *device.NodeInfo, _ *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	return false, nil, ""
}

func TestRegisterSkipsCleanupForUntrackedVendor(t *testing.T) {
	oldDevicesMap := device.DevicesMap
	defer func() { device.DevicesMap = oldDevicesMap }()

	mockDev := &registerMockDevice{
		nodeDevices: []*device.DeviceInfo{},
		health:      false,
		needUpdate:  true,
	}
	device.DevicesMap = map[string]device.Devices{
		"mock-vendor": mockDev,
	}

	s := NewScheduler()
	s.stopCh = make(chan struct{})
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient

	t.Setenv("POD_NAMESPACE", "default")
	t.Setenv("POD_NAME", "scheduler-0")

	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	s.nodeLister = informerFactory.Core().V1().Nodes().Lister()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
	}
	_, err := client.KubeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
	require.NoError(t, err)
	err = informerFactory.Core().V1().Nodes().Informer().GetIndexer().Add(node)
	require.NoError(t, err)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scheduler-0",
			Namespace: "default",
			Labels: map[string]string{
				util.HAMiComponentLabel: util.HAMiComponentScheduler,
			},
		},
	}
	_, err = client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)
	err = informerFactory.Core().V1().Pods().Informer().GetIndexer().Add(pod)
	require.NoError(t, err)

	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	s.addNode("node-1", &device.NodeInfo{
		ID:   "node-1",
		Node: node.DeepCopy(),
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:           "GPU-0",
				Index:        0,
				Count:        1,
				Devmem:       1024,
				Devcore:      100,
				Type:         nvidia.NvidiaGPUDevice,
				Health:       true,
				DeviceVendor: nvidia.NvidiaGPUDevice,
			}},
		},
	})

	atomic.StoreUint32(&s.started, 1)
	s.register(labels.Everything(), map[string]bool{})

	assert.Equal(t, mockDev.nodeCleanedUp, 0)

	nodeInfo, err := s.GetNode("node-1")
	require.NoError(t, err)
	_, ok := nodeInfo.Devices[nvidia.NvidiaGPUDevice]
	assert.Equal(t, ok, true)
	_, ok = nodeInfo.Devices["mock-vendor"]
	assert.Equal(t, ok, false)
}

func Test_ResourceQuota(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour*1)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informer := informerFactory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAddPod,
		UpdateFunc: s.onUpdatePod,
		DeleteFunc: s.onDelPod,
	})
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)
	s.addAllEventHandlers()
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

	initNode := func() {
		nodes, _ := s.ListNodes()
		for index := range nodes {
			s.rmNodeDevices(index, nvidia.NvidiaGPUDevice)
		}
		pods, _ := s.podManager.ListPodsUID()
		for index := range pods {
			s.podManager.DelPod(pods[index])
		}

		s.addNode("node1", &device.NodeInfo{
			ID:   "node1",
			Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
			Devices: map[string][]device.DeviceInfo{
				nvidia.NvidiaGPUDevice: {{
					ID:           "device1",
					Index:        0,
					Count:        10,
					Devmem:       2000,
					Devcore:      100,
					Numa:         0,
					Mode:         "hami",
					Type:         nvidia.NvidiaGPUDevice,
					Health:       true,
					DeviceVendor: nvidia.NvidiaGPUDevice,
				},
					{
						ID:           "device2",
						Index:        1,
						Count:        10,
						Devmem:       8000,
						Devcore:      100,
						Numa:         0,
						Mode:         "hami",
						Type:         nvidia.NvidiaGPUDevice,
						Health:       true,
						DeviceVendor: nvidia.NvidiaGPUDevice,
					},
					{
						ID:      "device3",
						Index:   0,
						Count:   10,
						Devmem:  4000,
						Devcore: 100,
						Numa:    0,
						Mode:    "hami",
						Type:    nvidia.NvidiaGPUDevice,
						Health:  true,
					},
					{
						ID:      "device4",
						Index:   1,
						Count:   10,
						Devmem:  6000,
						Devcore: 100,
						Numa:    0,
						Type:    nvidia.NvidiaGPUDevice,
						Health:  true,
					}},
			},
		})
	}

	tests := []struct {
		name    string
		args    extenderv1.ExtenderArgs
		quota   corev1.ResourceQuota
		want    *extenderv1.ExtenderFilterResult
		wantErr error
	}{
		{
			name: "multi device Resourcequota pass",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						UID:       "test1-uid1",
						Namespace: "default",
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
										"hami.io/gpucores": *resource.NewQuantity(100, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(2000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1"},
			},
			quota: corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-quota",
					Namespace: "default",
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						"limits.hami.io/gpucores": *resource.NewQuantity(200, resource.BinarySI),
						"limits.hami.io/gpumem":   *resource.NewQuantity(4000, resource.BinarySI),
					},
				},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				NodeNames: &[]string{"node1"},
			},
		},
		{
			name: "multi device Resourcequota deny",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						UID:       "test2-uid2",
						Namespace: "default",
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
										"hami.io/gpucores": *resource.NewQuantity(60, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(3000, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1"},
			},
			quota: corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-quota",
					Namespace: "default",
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						"limits.hami.io/gpucores": *resource.NewQuantity(200, resource.BinarySI),
						"limits.hami.io/gpumem":   *resource.NewQuantity(4000, resource.BinarySI),
					},
				},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				FailedNodes: map[string]string{
					"node1": "NodeUnfitPod",
				},
			},
		},
		{
			name: "unspecified device Resourcequota pass",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test3",
						UID:       "test3-uid3",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1"},
			},
			quota: corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-quota",
					Namespace: "default",
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						"limits.hami.io/gpucores": *resource.NewQuantity(100, resource.BinarySI),
						"limits.hami.io/gpumem":   *resource.NewQuantity(2000, resource.BinarySI),
					},
				},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				NodeNames: &[]string{"node1"},
			},
		},
		{
			name: "unspecified device Resourcequota deny",
			args: extenderv1.ExtenderArgs{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test4",
						UID:       "test4-uid4",
						Namespace: "default",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-burn",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
									},
								},
							},
						},
					},
				},
				NodeNames: &[]string{"node1"},
			},
			quota: corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-quota",
					Namespace: "default",
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						"limits.hami.io/gpucores": *resource.NewQuantity(100, resource.BinarySI),
						"limits.hami.io/gpumem":   *resource.NewQuantity(1500, resource.BinarySI),
					},
				},
			},
			wantErr: nil,
			want: &extenderv1.ExtenderFilterResult{
				FailedNodes: map[string]string{
					"node1": "NodeUnfitPod",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s.onAddQuota(&test.quota)
			initNode()
			client.KubeClient.CoreV1().Pods(test.args.Pod.Namespace).Create(context.Background(), test.args.Pod, metav1.CreateOptions{})
			got, gotErr := s.Filter(test.args)
			client.KubeClient.CoreV1().Pods(test.args.Pod.Namespace).Delete(context.Background(), test.args.Pod.Name, metav1.DeleteOptions{})
			// wait for pod deletion to be processed by the informer
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
				_, ok := s.podManager.GetPod(test.args.Pod)
				return !ok, nil
			})
			require.NoError(t, err, "timed out waiting for pod to be deleted from pod manager")
			s.onDelQuota(&test.quota)
			assert.DeepEqual(t, test.wantErr, gotErr)
			assert.DeepEqual(t, test.want, got)
		})
	}
}

func Test_ListNodes_Concurrent(t *testing.T) {
	t.Parallel()

	m := newNodeManager()
	stopCh := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		i := 0
		for {
			select {
			case <-stopCh:
				return
			default:
				nodeID := fmt.Sprintf("node-%d", i%100)
				m.addNode(nodeID,
					&device.NodeInfo{
						ID:   nodeID,
						Node: &corev1.Node{},
						Devices: map[string][]device.DeviceInfo{
							nvidia.NvidiaGPUDevice: {
								{
									ID:           "gpu-1",
									DeviceVendor: "NVIDIA",
								},
							},
						},
					},
				)
				i++
			}
		}
	}()

	for range 5000 {
		nodes, _ := m.ListNodes()
		for k, v := range nodes {
			_ = k
			_ = v
		}
	}

	close(stopCh)
	<-done
}

func Test_Filter_EvictsStaleEntry(t *testing.T) {
	require.NoError(t, config.InitDevicesWithConfig(&config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName: "hami.io/gpu", ResourceMemoryName: "hami.io/gpumem",
			ResourceCoreName: "hami.io/gpucores", DefaultGPUNum: 1,
		},
	}))
	s := NewScheduler()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "uid-s2", Name: "stale", Namespace: "ns-evict"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "c",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"hami.io/gpu":    *resource.NewQuantity(1, resource.BinarySI),
				"hami.io/gpumem": *resource.NewQuantity(500, resource.BinarySI),
			}},
		}}},
	}
	devs := device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{{device.ContainerDevice{Usedmem: 500, Usedcores: 10}}},
	}
	s.podManager.AddPod(pod, "node1", devs)
	s.quotaManager.AddUsage(pod, devs)
	s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{}})
	_, inCache := s.podManager.GetPod(pod)
	assert.Equal(t, false, inCache)
	for _, v := range *s.quotaManager.GetResourceQuota()[pod.Namespace] {
		assert.Equal(t, int64(0), v.Used)
	}
}

func TestFilterUsesTemplateNodesWithoutSideEffects(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewSimpleClientset()
	s.kubeClient = client.KubeClient
	s.podLister = informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour).Core().V1().Pods().Lister()

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}
	require.NoError(t, config.InitDevicesWithConfig(sConfig))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "template-pod",
			Namespace: "default",
			UID:       types.UID("template-pod-uid"),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "worker",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
						"hami.io/gpumem":   *resource.NewQuantity(1024, resource.BinarySI),
					},
				},
			}},
		},
	}
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	templateNode := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "template-node-a",
			Annotations: map[string]string{
				nvidia.RegisterAnnos: `[{"id":"GPU-0","count":10,"devmem":8192,"devcore":100,"type":"NVIDIA-A100","numa":0,"health":true,"index":0,"mode":"hami-core"}]`,
			},
		},
	}

	res, err := s.Filter(extenderv1.ExtenderArgs{
		Pod:   pod,
		Nodes: &corev1.NodeList{Items: []corev1.Node{templateNode}},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Nil(t, res.NodeNames)
	require.NotNil(t, res.Nodes)
	require.Len(t, res.Nodes.Items, 1)
	require.Equal(t, "template-node-a", res.Nodes.Items[0].Name)

	cachedPod, ok := s.podManager.GetPod(pod)
	require.False(t, ok)
	require.Nil(t, cachedPod)

	updatedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, updatedPod.Annotations[util.AssignedNodeAnnotations])
	require.Empty(t, updatedPod.Annotations[util.AssignedTimeAnnotations])
}

func TestFilterTemplateNodesDoesNotTouchSchedulingCaches(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewSimpleClientset()
	s.kubeClient = client.KubeClient
	s.podLister = informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour).Core().V1().Pods().Lister()
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}
	require.NoError(t, config.InitDevicesWithConfig(sConfig))

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gpu-quota",
			Namespace: "default",
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				"limits.hami.io/gpucores": *resource.NewQuantity(1000, resource.BinarySI),
				"limits.hami.io/gpumem":   *resource.NewQuantity(65536, resource.BinarySI),
			},
		},
	}
	s.onAddQuota(quota)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "template-pod-cache",
			Namespace: "default",
			UID:       types.UID("template-pod-cache-uid"),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "worker",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
						"hami.io/gpumem":   *resource.NewQuantity(2048, resource.BinarySI),
					},
				},
			}},
		},
	}
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	templateNode := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "template-node-b",
			Annotations: map[string]string{
				nvidia.RegisterAnnos: `[{"id":"GPU-1","count":10,"devmem":8192,"devcore":100,"type":"NVIDIA-A100","numa":0,"health":true,"index":1,"mode":"hami-core"}]`,
			},
		},
	}

	_, err = s.Filter(extenderv1.ExtenderArgs{
		Pod:   pod,
		Nodes: &corev1.NodeList{Items: []corev1.Node{templateNode}},
	})
	require.NoError(t, err)

	pods, err := s.podManager.ListPodsUID()
	require.NoError(t, err)
	require.Len(t, pods, 0)

	quotas := s.quotaManager.GetResourceQuota()
	require.Contains(t, quotas, "default")
	require.NotNil(t, quotas["default"])
	require.Equal(t, int64(0), (*quotas["default"])["hami.io/gpucores"].Used)
	require.Equal(t, int64(0), (*quotas["default"])["hami.io/gpumem"].Used)
}

func TestFilterTemplateNodesReturnsDetailedFailureReason(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewSimpleClientset()
	s.kubeClient = client.KubeClient
	s.podLister = informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour).Core().V1().Pods().Lister()

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}
	require.NoError(t, config.InitDevicesWithConfig(sConfig))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "template-pod-unfit",
			Namespace: "default",
			UID:       types.UID("template-pod-unfit-uid"),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "worker",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
						"hami.io/gpumem":   *resource.NewQuantity(16384, resource.BinarySI),
					},
				},
			}},
		},
	}

	templateNode := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "template-node-unfit",
			Annotations: map[string]string{
				nvidia.RegisterAnnos: `[{"id":"GPU-0","count":10,"devmem":8192,"devcore":100,"type":"NVIDIA-A100","numa":0,"health":true,"index":0,"mode":"hami-core"}]`,
			},
		},
	}

	res, err := s.Filter(extenderv1.ExtenderArgs{
		Pod:   pod,
		Nodes: &corev1.NodeList{Items: []corev1.Node{templateNode}},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Nil(t, res.Nodes)
	require.Contains(t, res.FailedNodes, "template-node-unfit")
	require.Contains(t, res.FailedNodes["template-node-unfit"], common.CardInsufficientMemory)
}

func TestFilterTemplateNodesMissingRegisterAnnotation(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewSimpleClientset()
	s.kubeClient = client.KubeClient
	s.podLister = informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour).Core().V1().Pods().Lister()

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}
	require.NoError(t, config.InitDevicesWithConfig(sConfig))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "template-pod-cold-zero",
			Namespace: "default",
			UID:       types.UID("template-pod-cold-zero-uid"),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "worker",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
					},
				},
			}},
		},
	}

	res, err := s.Filter(extenderv1.ExtenderArgs{
		Pod: pod,
		Nodes: &corev1.NodeList{Items: []corev1.Node{{
			ObjectMeta: metav1.ObjectMeta{Name: "template-node-cold-zero"},
		}}},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Nil(t, res.Nodes)
	require.Equal(t, "node unregistered", res.FailedNodes["template-node-cold-zero"])
}

func Test_Scheduler_Issue1368_TerminatingPodRetainsCache(t *testing.T) {
	s := NewScheduler()

	podDevces := device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			[]device.ContainerDevice{
				{Idx: 0, UUID: "GPU0", Usedmem: 1000, Usedcores: 10},
			},
		},
	}

	basePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "1368-test-uid",
			Name:      "test-terminating-pod",
			Namespace: "default",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: "node1",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	encodedAnnotations := device.EncodePodDevices(device.SupportDevices, podDevces)
	maps.Copy(basePod.Annotations, encodedAnnotations)
	s.onAddPod(basePod)

	_, ok := s.podManager.GetPod(basePod)
	assert.Equal(t, true, ok, "Pod should be in cache after initial addition")

	terminatingPod := basePod.DeepCopy()
	now := metav1.Now()
	terminatingPod.DeletionTimestamp = &now

	s.onAddPod(terminatingPod)

	_, ok = s.podManager.GetPod(terminatingPod)
	assert.Equal(t, true, ok, "BUGFIX #1368: Pod should STILL be in cache while terminating (DeletionTimestamp != nil)")

	terminatedPod := terminatingPod.DeepCopy()
	terminatedPod.Status.Phase = corev1.PodSucceeded

	s.onAddPod(terminatedPod)

	_, ok = s.podManager.GetPod(terminatedPod)
	assert.Equal(t, false, ok, "Pod should be removed from cache after reaching a terminal phase (Succeeded/Failed)")
}

func Test_onAddPod_BadDeviceAnnotation(t *testing.T) {
	device.SupportDevices["TEST"] = "hami.io/test-allocated"
	s := NewScheduler()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "bad-anno-uid",
			Name:      "bad-anno-pod",
			Namespace: "default",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: "node1",
				"hami.io/test-allocated":     "uuid,type,100:;",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	s.onAddPod(pod)
	_, ok := s.podManager.GetPod(pod)
	assert.Equal(t, false, ok)
}

func Test_Bind_DelPodOnGetPodFailure(t *testing.T) {
	t.Parallel()

	podUID := types.UID("test-uid-1")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       podUID,
		},
	}

	s := NewScheduler()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	s.eventRecorder = record.NewBroadcaster().NewRecorder(scheme, corev1.EventSource{})
	client.KubeClient = fake.NewSimpleClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour*1)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	s.podManager.AddPod(pod, "node1", nil)

	podsBefore, _ := s.podManager.ListPodsUID()
	require.Len(t, podsBefore, 1)
	require.Equal(t, podUID, podsBefore[0].UID)

	args := extenderv1.ExtenderBindingArgs{
		PodName:      pod.Name,
		PodNamespace: pod.Namespace,
		PodUID:       podUID,
		Node:         "node1",
	}
	_, err := s.Bind(args)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")

	podsAfter, _ := s.podManager.ListPodsUID()
	require.Len(t, podsAfter, 0)
}

func Test_Bind_DelPodOnGetNodeFailure(t *testing.T) {
	t.Parallel()

	podUID := types.UID("test-uid-2")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-nodefail",
			Namespace: "default",
			UID:       podUID,
		},
	}

	s := NewScheduler()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	s.eventRecorder = record.NewBroadcaster().NewRecorder(scheme, corev1.EventSource{})

	fakeClient := fake.NewSimpleClientset()
	s.kubeClient = fakeClient

	_, err := fakeClient.CoreV1().Pods(pod.Namespace).Create(
		context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)

	informerFactory := informers.NewSharedInformerFactoryWithOptions(fakeClient, time.Hour*1)

	err = informerFactory.Core().V1().Pods().Informer().GetIndexer().Add(pod)
	require.NoError(t, err)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	s.nodeLister = informerFactory.Core().V1().Nodes().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	s.podManager.AddPod(pod, "non-existent-node", nil)

	podsBefore, _ := s.podManager.ListPodsUID()
	require.Len(t, podsBefore, 1)
	require.Equal(t, podUID, podsBefore[0].UID)

	args := extenderv1.ExtenderBindingArgs{
		PodName:      pod.Name,
		PodNamespace: pod.Namespace,
		PodUID:       podUID,
		Node:         "non-existent-node",
	}
	res, err := s.Bind(args)

	require.NoError(t, err)
	require.Contains(t, res.Error, "not found")

	podsAfter, _ := s.podManager.ListPodsUID()
	require.Len(t, podsAfter, 0)
}

type bindLockMockDevice struct {
	registerMockDevice
	lockErr      error
	lockErrOnce  bool
	lockCalls    atomic.Int32
	releaseCalls atomic.Int32
}

func (m *bindLockMockDevice) CommonWord() string { return "bind-lock-mock" }
func (m *bindLockMockDevice) LockNode(_ *corev1.Node, _ *corev1.Pod) error {
	n := m.lockCalls.Add(1)
	if m.lockErr != nil && (!m.lockErrOnce || n == 1) {
		return m.lockErr
	}
	return nil
}
func (m *bindLockMockDevice) ReleaseNodeLock(_ *corev1.Node, _ *corev1.Pod) error {
	m.releaseCalls.Add(1)
	return nil
}

var errContention = fmt.Errorf("contended: %w", nodelockutil.ErrNodeLockContention)

func setupBindLockRetryTest(t *testing.T, retryTimeout time.Duration, pod *corev1.Pod, mock *bindLockMockDevice) (*Scheduler, extenderv1.ExtenderBindingArgs, func()) {
	t.Helper()

	oldRetry := config.NodeLockRetryTimeout
	config.NodeLockRetryTimeout = retryTimeout
	oldDevicesMap := device.DevicesMap
	device.DevicesMap = map[string]device.Devices{"bind-lock-mock": mock}

	s := NewScheduler()
	cleanup := func() {
		config.NodeLockRetryTimeout = oldRetry
		device.DevicesMap = oldDevicesMap
		close(s.stopCh)
	}
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	s.eventRecorder = record.NewBroadcaster().NewRecorder(scheme, corev1.EventSource{})

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
	fakeClient := fake.NewSimpleClientset(pod, node)
	s.kubeClient = fakeClient
	client.KubeClient = fakeClient

	informerFactory := informers.NewSharedInformerFactoryWithOptions(fakeClient, time.Hour)
	require.NoError(t, informerFactory.Core().V1().Pods().Informer().GetIndexer().Add(pod))
	require.NoError(t, informerFactory.Core().V1().Nodes().Informer().GetIndexer().Add(node))
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	s.nodeLister = informerFactory.Core().V1().Nodes().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	args := extenderv1.ExtenderBindingArgs{
		PodName: pod.Name, PodNamespace: pod.Namespace, PodUID: pod.UID, Node: "node1",
	}
	return s, args, cleanup
}

func Test_Bind_NonPodGroupPodDoesNotRetry(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-nogroup", Namespace: "default", UID: types.UID("uid-nogroup"),
		},
	}
	mock := &bindLockMockDevice{lockErr: errContention, lockErrOnce: true}
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, pod, mock)
	defer cleanup()

	res, err := s.Bind(args)
	require.NoError(t, err)
	require.Contains(t, res.Error, "node lock contention")
	require.Equal(t, int32(1), mock.lockCalls.Load(),
		"non-PodGroup pod must not retry LockNode")
}

func Test_Bind_PodGroupPodRetriesOnContention(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-gang", Namespace: "default", UID: types.UID("uid-gang"),
			Labels: map[string]string{util.PodGroupLabel: "my-training-job"},
		},
	}
	mock := &bindLockMockDevice{lockErr: errContention, lockErrOnce: true}
	s, args, cleanup := setupBindLockRetryTest(t, 2*time.Second, pod, mock)
	defer cleanup()

	s.Bind(args)
	require.GreaterOrEqual(t, mock.lockCalls.Load(), int32(2),
		"expected at least 2 LockNode calls (initial + retry)")
}

func Test_Bind_PodGroupPodContendsUntilTimeout(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-timeout", Namespace: "default", UID: types.UID("uid-timeout"),
			Labels: map[string]string{util.PodGroupLabel: "my-training-job"},
		},
	}
	mock := &bindLockMockDevice{lockErr: errContention}
	s, args, cleanup := setupBindLockRetryTest(t, 300*time.Millisecond, pod, mock)
	defer cleanup()

	res, err := s.Bind(args)
	require.NoError(t, err)
	require.Contains(t, res.Error, "node lock contention",
		"timeout error should wrap ErrNodeLockContention for observability")
	require.GreaterOrEqual(t, mock.releaseCalls.Load(), int32(1),
		"expected ReleaseNodeLock to be called at least once on timeout path")
}

func Test_Bind_PodGroupPodNonContentionErrorDoesNotRetry(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-other-err", Namespace: "default", UID: types.UID("uid-other-err"),
			Labels: map[string]string{util.PodGroupLabel: "my-training-job"},
		},
	}
	mock := &bindLockMockDevice{lockErr: fmt.Errorf("apiserver 500"), lockErrOnce: true}
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, pod, mock)
	defer cleanup()

	res, err := s.Bind(args)
	require.NoError(t, err)
	require.Contains(t, res.Error, "apiserver 500")
	require.Equal(t, int32(1), mock.lockCalls.Load(),
		"non-contention error must not trigger retry")
}

func setupNvidiaConfigForTest(t testing.TB) {
	oldDevicesMap := maps.Clone(device.DevicesMap)
	oldDevicesToHandle := append([]string{}, device.DevicesToHandle...)
	t.Cleanup(func() {
		device.DevicesMap = oldDevicesMap
		device.DevicesToHandle = oldDevicesToHandle
	})

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
		t.Fatalf("Failed to initialize devices: %v", err)
	}
}

// Test_Filter_Bug1896_QuotaRollbackOnPatchFailure verifies that when
// util.PatchPodAnnotations returns an error after AddPod/AddUsage have
// already committed the allocation, Filter rolls back BOTH podManager and
// quotaManager, leaving quota usage at its pre-call level.
// This is a regression test for upstream bug #1896.
func Test_Filter_Bug1896_QuotaRollbackOnPatchFailure(t *testing.T) {
	setupNvidiaConfigForTest(t)

	const ns = "default"

	// Add a quota limit so quotaManager has a real entry to roll back.
	nvidiaDevice, ok := device.GetDevices()[nvidia.NvidiaGPUDevice]
	require.True(t, ok)
	memResourceName := nvidiaDevice.GetResourceNames().ResourceMemoryName
	require.NotEmpty(t, memResourceName)

	s := NewScheduler()
	s.onAddQuota(&corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "test-quota", Namespace: ns},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits." + memResourceName): *resource.NewQuantity(100000, resource.BinarySI),
			},
		},
	})

	// Add a node with one GPU (8 GiB memory).
	s.addNode("node1", &device.NodeInfo{
		ID:   "node1",
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID:           "gpu0",
				Index:        0,
				Count:        10,
				Devmem:       8192,
				Devcore:      100,
				Health:       true,
				Type:         nvidia.NvidiaGPUDevice,
				DeviceVendor: nvidia.NvidiaGPUDevice,
			}},
		},
	})

	// Record quota usage before the call.
	quotaBefore := func() int64 {
		q := s.quotaManager.GetResourceQuota()
		nsq, ok := q[ns]
		if !ok {
			return 0
		}
		entry, ok := (*nsq)[memResourceName]
		if !ok {
			return 0
		}
		return entry.Used
	}
	require.Equal(t, int64(0), quotaBefore(), "pre-condition: quota must be zero before test")

	// Use a fake KubeClient whose Patch calls always fail — this makes
	// util.PatchPodAnnotations return an error, simulating a transient API failure
	// AFTER AddPod/AddUsage have already committed the allocation.
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "patch-fail-pod",
			Namespace: ns,
			UID:       "patch-fail-uid",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "gpu-container",
				Image: "ubuntu",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
						"hami.io/gpumem":   *resource.NewQuantity(2048, resource.BinarySI),
						"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
					},
				},
			}},
		},
	}
	// Do NOT add the pod to the fake client — PatchPodAnnotations will fail
	// because the pod does not exist in the API server.

	nodeNames := []string{"node1"}
	_, err := s.Filter(extenderv1.ExtenderArgs{
		Pod:       pod,
		NodeNames: &nodeNames,
	})

	// Filter must return an error because PatchPodAnnotations failed.
	require.Error(t, err, "Filter must propagate PatchPodAnnotations error")

	// Bug #1896: quota must be rolled back to zero.
	require.Equal(t, int64(0), quotaBefore(),
		"quota usage must be fully rolled back after PatchPodAnnotations failure (Bug #1896)")

	// The pod must no longer be in the podManager cache.
	_, inCache := s.podManager.GetPod(pod)
	require.False(t, inCache,
		"pod must be removed from podManager cache after PatchPodAnnotations failure")
}

// Test_Filter_Bug1330_NoConcurrentOverAllocation verifies that concurrent
// Filter() calls for multiple pods against a node/namespace with capacity for
// only ONE pod never admit more than one pod.
// This is a regression test for upstream bug #1330.
//
// The test loops 100 times to make any surviving race statistically visible.
// Run with: go test ./pkg/scheduler/... -race -run Test_Filter_Bug1330
func Test_Filter_Bug1330_NoConcurrentOverAllocation(t *testing.T) {
	setupNvidiaConfigForTest(t)

	nvidiaDevice, ok := device.GetDevices()[nvidia.NvidiaGPUDevice]
	require.True(t, ok)
	memResourceName := nvidiaDevice.GetResourceNames().ResourceMemoryName
	require.NotEmpty(t, memResourceName)

	const (
		ns          = "race-test-ns"
		concurrency = 5  // pods racing to claim the only GPU slot
		iterations  = 100 // repeat to expose statistical races
	)

	for iter := 0; iter < iterations; iter++ {
		// Fresh scheduler per iteration.
		s := NewScheduler()

		// Set a quota that fits exactly ONE pod request (pod requests 2 GiB, quota = 2 GiB).
		s.onAddQuota(&corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "gpu-quota", Namespace: ns},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceName("limits." + memResourceName): *resource.NewQuantity(2048, resource.BinarySI),
				},
			},
		})

		// Node has a single GPU with exactly 2 GiB of memory.
		s.addNode("node-race", &device.NodeInfo{
			ID:   "node-race",
			Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-race"}},
			Devices: map[string][]device.DeviceInfo{
				nvidia.NvidiaGPUDevice: {{
					ID:           "gpu-race",
					Index:        0,
					Count:        1,
					Devmem:       2048,
					Devcore:      100,
					Health:       true,
					Type:         nvidia.NvidiaGPUDevice,
					DeviceVendor: nvidia.NvidiaGPUDevice,
				}},
			},
		})

		// Fake API server — pods are pre-created so PatchPodAnnotations succeeds.
		client.KubeClient = fake.NewClientset()
		s.kubeClient = client.KubeClient
		informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
		s.podLister = informerFactory.Core().V1().Pods().Lister()
		informerFactory.Start(s.stopCh)
		informerFactory.WaitForCacheSync(s.stopCh)

		pods := make([]*corev1.Pod, concurrency)
		for i := range pods {
			pods[i] = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("race-pod-%d-%d", iter, i),
					Namespace: ns,
					UID:       types.UID(fmt.Sprintf("race-uid-%d-%d", iter, i)),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "gpu-container",
						Image: "ubuntu",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
								"hami.io/gpumem":   *resource.NewQuantity(2048, resource.BinarySI),
								"hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
							},
						},
					}},
				},
			}
			client.KubeClient.CoreV1().Pods(ns).Create(
				context.Background(), pods[i], metav1.CreateOptions{},
			)
		}

		nodeNames := []string{"node-race"}
		var admitted atomic.Int32

		// Launch concurrency goroutines all calling Filter at the same time.
		var wg sync.WaitGroup
		for i := range pods {
			wg.Add(1)
			go func(pod *corev1.Pod) {
				defer wg.Done()
				result, err := s.Filter(extenderv1.ExtenderArgs{
					Pod:       pod,
					NodeNames: &nodeNames,
				})
				if err == nil && result != nil && result.NodeNames != nil && len(*result.NodeNames) > 0 {
					admitted.Add(1)
				}
			}(pods[i])
		}
		wg.Wait()

		admittedCount := admitted.Load()
		require.LessOrEqualf(t, admittedCount, int32(1),
			"iteration %d: admitted %d pods against a node/quota with capacity for 1 (Bug #1330)",
			iter, admittedCount)

		close(s.stopCh)
	}
}

// Test_Filter_NonContendingPodsBothEventuallyAdmitted verifies that two pods requesting
// distinct resources in different namespaces/nodes are both successfully admitted
// (processed sequentially under filterMutex).
func Test_Filter_NonContendingPodsBothEventuallyAdmitted(t *testing.T) {
	setupNvidiaConfigForTest(t)

	s := NewScheduler()
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	// Add two distinct nodes with GPU resources.
	s.addNode("node-a", &device.NodeInfo{
		ID:   "node-a",
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID: "gpu-a", Index: 0, Count: 10, Devmem: 8192, Devcore: 100, Health: true, Type: nvidia.NvidiaGPUDevice, DeviceVendor: nvidia.NvidiaGPUDevice,
			}},
		},
	})
	s.addNode("node-b", &device.NodeInfo{
		ID:   "node-b",
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID: "gpu-b", Index: 0, Count: 10, Devmem: 8192, Devcore: 100, Health: true, Type: nvidia.NvidiaGPUDevice, DeviceVendor: nvidia.NvidiaGPUDevice,
			}},
		},
	})

	podA := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-a", UID: "uid-a"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "c1", Image: "ubuntu",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI), "hami.io/gpumem": *resource.NewQuantity(1024, resource.BinarySI), "hami.io/gpucores": *resource.NewQuantity(10, resource.BinarySI),
					},
				},
			}},
		},
	}
	podB := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns-b", UID: "uid-b"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "c1", Image: "ubuntu",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI), "hami.io/gpumem": *resource.NewQuantity(1024, resource.BinarySI), "hami.io/gpucores": *resource.NewQuantity(10, resource.BinarySI),
					},
				},
			}},
		},
	}
	client.KubeClient.CoreV1().Pods("ns-a").Create(context.Background(), podA, metav1.CreateOptions{})
	client.KubeClient.CoreV1().Pods("ns-b").Create(context.Background(), podB, metav1.CreateOptions{})

	nodesA := []string{"node-a"}
	resA, errA := s.Filter(extenderv1.ExtenderArgs{Pod: podA, NodeNames: &nodesA})
	require.NoError(t, errA)
	require.NotNil(t, resA)
	require.Equal(t, []string{"node-a"}, *resA.NodeNames)

	nodesB := []string{"node-b"}
	resB, errB := s.Filter(extenderv1.ExtenderArgs{Pod: podB, NodeNames: &nodesB})
	require.NoError(t, errB)
	require.NotNil(t, resB)
	require.Equal(t, []string{"node-b"}, *resB.NodeNames)
}

// Test_Filter_FitFailureReleasesLockCleanly verifies that when calcScore/Fit fails
// inside tryAllocate (e.g. no nodes have sufficient capacity), filterMutex is released
// cleanly without leaving phantom state in podManager or quotaManager.
func Test_Filter_FitFailureReleasesLockCleanly(t *testing.T) {
	setupNvidiaConfigForTest(t)

	s := NewScheduler()
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	// Node with only 1 GiB GPU memory.
	s.addNode("node-small", &device.NodeInfo{
		ID:   "node-small",
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-small"}},
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {{
				ID: "gpu0", Index: 0, Count: 1, Devmem: 1024, Devcore: 100, Health: true, Type: nvidia.NvidiaGPUDevice, DeviceVendor: nvidia.NvidiaGPUDevice,
			}},
		},
	})

	// Pod requesting 4 GiB GPU memory (exceeds node capacity).
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "huge-pod", Namespace: "default", UID: "huge-uid"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "c1", Image: "ubuntu",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI), "hami.io/gpumem": *resource.NewQuantity(4096, resource.BinarySI), "hami.io/gpucores": *resource.NewQuantity(20, resource.BinarySI),
					},
				},
			}},
		},
	}
	client.KubeClient.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})

	nodes := []string{"node-small"}
	res, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &nodes})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, res.NodeNames == nil || len(*res.NodeNames) == 0, "no node should fit")

	// Ensure no phantom allocation was created in podManager.
	_, inCache := s.podManager.GetPod(pod)
	require.False(t, inCache, "pod must not be recorded in podManager cache when Fit fails")

	// Ensure lock can be acquired immediately (proving filterMutex was unlocked).
	acquired := make(chan struct{})
	go func() {
		s.filterMutex.Lock()
		s.filterMutex.Unlock()
		close(acquired)
	}()

	select {
	case <-acquired:
		// Success: lock was cleanly released.
	case <-time.After(time.Second):
		t.Fatal("filterMutex was not released after Fit failure")
	}
}

// BenchmarkFilter measures Filter() latency and lock throughput over 200 candidate nodes.
func BenchmarkFilter(b *testing.B) {
	setupNvidiaConfigForTest(b)

	const nodeCount = 200
	s := NewScheduler()
	client.KubeClient = fake.NewClientset()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

	nodeNames := make([]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		name := fmt.Sprintf("bench-node-%d", i)
		nodeNames[i] = name
		s.addNode(name, &device.NodeInfo{
			ID:   name,
			Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}},
			Devices: map[string][]device.DeviceInfo{
				nvidia.NvidiaGPUDevice: {{
					ID:           fmt.Sprintf("gpu-%d", i),
					Index:        0,
					Count:        100,
					Devmem:       32768,
					Devcore:      100,
					Health:       true,
					Type:         nvidia.NvidiaGPUDevice,
					DeviceVendor: nvidia.NvidiaGPUDevice,
				}},
			},
		})
	}

	b.Run("Uncontended", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("bench-pod-%d", i),
					Namespace: "default",
					UID:       types.UID(fmt.Sprintf("bench-uid-%d", i)),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "c1",
						Image: "ubuntu",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
								"hami.io/gpumem":   *resource.NewQuantity(1024, resource.BinarySI),
								"hami.io/gpucores": *resource.NewQuantity(10, resource.BinarySI),
							},
						},
					}},
				},
			}
			client.KubeClient.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
			_, _ = s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &nodeNames})
		}
	})

	b.Run("ContendedParallel", func(b *testing.B) {
		var podCounter atomic.Int64
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				idx := podCounter.Add(1)
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("par-pod-%d", idx),
						Namespace: "default",
						UID:       types.UID(fmt.Sprintf("par-uid-%d", idx)),
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  "c1",
							Image: "ubuntu",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
									"hami.io/gpumem":   *resource.NewQuantity(1024, resource.BinarySI),
									"hami.io/gpucores": *resource.NewQuantity(10, resource.BinarySI),
								},
							},
						}},
					},
				}
				client.KubeClient.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
				_, _ = s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &nodeNames})
			}
		})
	})
}

// Test_onAddPod_AlreadyTrackedPodDoesNotDoubleCountQuota verifies that when onAddPod
// is called for a pod that is already tracked in podManager (e.g., via Filter or an
// earlier informer event), AddPod returns false and onAddPod does NOT call AddUsage
// a second time.
func Test_onAddPod_AlreadyTrackedPodDoesNotDoubleCountQuota(t *testing.T) {
	setupNvidiaConfigForTest(t)

	const ns = "double-count-ns"
	s := NewScheduler()

	nvidiaDevice, ok := device.GetDevices()[nvidia.NvidiaGPUDevice]
	require.True(t, ok)
	memResourceName := nvidiaDevice.GetResourceNames().ResourceMemoryName

	s.onAddQuota(&corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "quota1", Namespace: ns},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits." + memResourceName): *resource.NewQuantity(100000, resource.BinarySI),
			},
		},
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tracked-pod",
			Namespace: ns,
			UID:       "tracked-uid",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: "node1",
			},
		},
	}

	podDev := device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			{
				{
					Idx:       0,
					UUID:      "gpu0",
					Type:      nvidia.NvidiaGPUDevice,
					Usedmem:   2048,
					Usedcores: 20,
				},
			},
		},
	}

	// 1. Add pod to podManager and quotaManager directly (simulating prior Filter allocation).
	added := s.podManager.AddPod(pod, "node1", podDev)
	require.True(t, added, "initial AddPod must return true for new pod")
	s.quotaManager.AddUsage(pod, podDev)

	getUsage := func() int64 {
		q := s.quotaManager.GetResourceQuota()
		if nsq, ok := q[ns]; ok {
			if entry, ok := (*nsq)[memResourceName]; ok {
				return entry.Used
			}
		}
		return 0
	}

	initialUsage := getUsage()
	require.Greater(t, initialUsage, int64(0), "quota usage should be recorded initially")

	// 2. Trigger onAddPod (simulating informer resync or event for already-tracked pod).
	s.onAddPod(pod)

	usageAfterInformerEvent := getUsage()
	require.Equal(t, initialUsage, usageAfterInformerEvent,
		"quota usage must NOT be double-counted when onAddPod runs for an already-tracked pod")
}
