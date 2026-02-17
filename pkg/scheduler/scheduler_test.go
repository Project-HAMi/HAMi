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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
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
| pod name     | node name|  pod status | annotations                 |             result                  |
|--------------|----------|-------------|---------------------------- |-------------------------------------|
| test-pod-1   | node11   |  Succeeded  |  hami.io/bind-phase:success |  node11:{TotalPod:1,UseDevicePod:1} |
| test-pod-2   | node12   |  Running    |  none                       |  node12:{TotalPod:0;UseDevicePod:0} |
| test-pod-3   | node13   |  Succeeded  |  none                       |  node13:{TotalPod:1;UseDevicePod:0} |
test case matrix.
*/

func Test_getPodUsage(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewSimpleClientset()
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
	client.KubeClient = fake.NewSimpleClientset()
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
				client.KubeClient = fake.NewSimpleClientset()
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

		client.KubeClient = fake.NewSimpleClientset()
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

func Test_ResourceQuota(t *testing.T) {
	s := NewScheduler()
	client.KubeClient = fake.NewSimpleClientset()
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
