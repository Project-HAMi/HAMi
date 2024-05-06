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
	"testing"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

func Test_getNodesUsage(t *testing.T) {
	nodeMage := nodeManager{}
	nodeMage.init()
	nodeMage.addNode("node1", &util.NodeInfo{
		ID: "node1",
		Devices: []util.DeviceInfo{
			{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Health:  true,
			},
			{
				ID:      "GPU1",
				Index:   1,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Health:  true,
			},
		},
	})
	podDevces := util.PodDevices{
		"NVIDIA": util.PodSingleDevice{
			[]util.ContainerDevice{
				{
					Idx:       0,
					UUID:      "GPU0",
					Usedmem:   100,
					Usedcores: 10,
				},
			},
		},
	}
	podMap := podManager{}
	podMap.init()
	podMap.addPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "1111",
			Name:      "test1",
			Namespace: "default",
		},
	}, "node1", podDevces)
	podMap.addPod(&corev1.Pod{
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
	cachenodeMap, _, err := s.getNodesUsage(&nodes, nil)
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
	client.KubeClient = fake.NewSimpleClientset()
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
			node := nodes[index]
			s.rmNodeDevice(node.ID, node)
		}
		pods, _ := s.ListPods()
		for index := range pods {
			s.delPod(pods[index])
		}

		s.addNode("node1", &util.NodeInfo{
			ID: "node1",
			Devices: []util.DeviceInfo{
				{
					ID:      "device1",
					Index:   0,
					Count:   10,
					Devmem:  8000,
					Devcore: 100,
					Numa:    0,
					Type:    nvidia.NvidiaGPUDevice,
					Health:  true,
				},
				{
					ID:      "device2",
					Index:   1,
					Count:   10,
					Devmem:  8000,
					Devcore: 100,
					Numa:    0,
					Type:    nvidia.NvidiaGPUDevice,
					Health:  true,
				},
			},
		})
		s.addNode("node2", &util.NodeInfo{
			ID: "node2",
			Devices: []util.DeviceInfo{
				{
					ID:      "device3",
					Index:   0,
					Count:   10,
					Devmem:  8000,
					Devcore: 100,
					Numa:    0,
					Type:    nvidia.NvidiaGPUDevice,
					Health:  true,
				},
				{
					ID:      "device4",
					Index:   1,
					Count:   10,
					Devmem:  8000,
					Devcore: 100,
					Numa:    0,
					Type:    nvidia.NvidiaGPUDevice,
					Health:  true,
				},
			},
		})
		s.addPod(pod1, "node1", util.PodDevices{
			nvidia.NvidiaGPUDevice: util.PodSingleDevice{
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
		s.addPod(pod2, "node2", util.PodDevices{
			nvidia.NvidiaGPUDevice: util.PodSingleDevice{
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
							policy.GPUSchedulerPolicyAnnotationKey:  policy.GPUSchedulerPolicyBinpack.String(),
							policy.NodeSchedulerPolicyAnnotationKey: policy.NodeSchedulerPolicyBinpack.String(),
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
							policy.GPUSchedulerPolicyAnnotationKey:  policy.GPUSchedulerPolicySpread.String(),
							policy.NodeSchedulerPolicyAnnotationKey: policy.NodeSchedulerPolicyBinpack.String(),
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
							policy.GPUSchedulerPolicyAnnotationKey:  policy.GPUSchedulerPolicyBinpack.String(),
							policy.NodeSchedulerPolicyAnnotationKey: policy.NodeSchedulerPolicySpread.String(),
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
							policy.GPUSchedulerPolicyAnnotationKey:  policy.GPUSchedulerPolicySpread.String(),
							policy.NodeSchedulerPolicyAnnotationKey: policy.NodeSchedulerPolicySpread.String(),
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

	nvidia.ResourceName = "hami.io/gpu"
	nvidia.ResourceMem = "hami.io/gpumem"
	nvidia.ResourceCores = "hami.io/gpucores"
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initNode()
			client.KubeClient.CoreV1().Pods(test.args.Pod.Namespace).Create(context.Background(), test.args.Pod, metav1.CreateOptions{})
			got, gotErr := s.Filter(test.args)
			assert.DeepEqual(t, test.wantErr, gotErr)
			assert.DeepEqual(t, test.want, got)
			getPod, _ := client.KubeClient.CoreV1().Pods(test.args.Pod.Namespace).Get(context.Background(), test.args.Pod.Name, metav1.GetOptions{})
			podDevices, _ := util.DecodePodDevices(util.SupportDevices, getPod.Annotations)
			assert.DeepEqual(t, test.wantPodAnnotationDeviceID, podDevices["NVIDIA"][0][0].UUID)
		})
	}
}
