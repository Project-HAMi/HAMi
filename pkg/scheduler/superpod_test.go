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
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	appslisters "k8s.io/client-go/listers/apps/v1"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func resetSuperpodTestDevices(t *testing.T) {
	t.Helper()
	err := config.InitDevicesWithConfig(&config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
		},
	})
	assert.NilError(t, err)
}

func newSuperpodTestListers(t *testing.T, objects ...runtime.Object) (appslisters.ReplicaSetLister, appslisters.StatefulSetLister, batchlisters.JobLister) {
	t.Helper()
	replicaSets := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	statefulSets := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	jobs := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, obj := range objects {
		var indexer cache.Indexer
		switch obj := obj.(type) {
		case *appsv1.ReplicaSet:
			indexer = replicaSets
		case *appsv1.StatefulSet:
			indexer = statefulSets
		case *batchv1.Job:
			indexer = jobs
		default:
			t.Fatalf("unsupported test object type %T", obj)
		}
		assert.NilError(t, indexer.Add(obj))
	}
	return appslisters.NewReplicaSetLister(replicaSets), appslisters.NewStatefulSetLister(statefulSets), batchlisters.NewJobLister(jobs)
}

func TestEnsureSuperpodInformers(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	s := &Scheduler{
		informerFactory: informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0),
		stopCh:          stopCh,
	}
	if s.replicaSetLister != nil || s.statefulSetLister != nil || s.jobLister != nil {
		t.Fatal("SuperPod listers should not be initialized before the policy is used")
	}
	if err := s.ensureSuperpodInformers(); err != nil {
		t.Fatalf("ensureSuperpodInformers() error = %v", err)
	}
	if s.replicaSetLister == nil || s.statefulSetLister == nil || s.jobLister == nil {
		t.Fatal("expected all SuperPod controller listers to be initialized")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// makeGPUPod builds a pod that requests n GPU(s) via the hami.io/gpu resource
// limit, matching the device config registered in TestMain.
func makeGPUPod(name, namespace, taskID string, gpuCount int64) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(name + "-uid"),
			Labels:    map[string]string{},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "worker",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hami.io/gpu":      *resource.NewQuantity(gpuCount, resource.BinarySI),
							"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
							"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
						},
					},
				},
			},
		},
	}
}

// makeNodeWithLabels creates a corev1.Node with the given name and labels.
func makeNodeWithLabels(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

// makeNodeUsage creates a NodeUsage with one healthy GPU device that has
// count total shares and used shares already consumed.
func makeNodeUsage(name string, labels map[string]string, count, used int64) *NodeUsage {
	node := makeNodeWithLabels(name, labels)
	return &NodeUsage{
		Node:     node,
		NodeInfo: &device.NodeInfo{ID: name, Node: node},
		Devices: policy.DeviceUsageList{
			DeviceLists: []*policy.DeviceListsScore{
				{Device: &device.DeviceUsage{
					ID:           name + "-gpu0",
					Type:         nvidia.NvidiaGPUDevice,
					DeviceVendor: nvidia.NvidiaGPUDevice,
					Count:        count,
					Used:         used,
					Totalmem:     8000,
					Health:       true,
				}},
			},
		},
	}
}

func makeNodeUsageWithPhysicalCount(name string, labels map[string]string, physicalCount int, virtualCount, used int64) *NodeUsage {
	devices := make([]*device.DeviceUsage, physicalCount)
	for i := range devices {
		devices[i] = &device.DeviceUsage{
			ID:           fmt.Sprintf("%s-gpu%d", name, i),
			Type:         nvidia.NvidiaGPUDevice,
			DeviceVendor: nvidia.NvidiaGPUDevice,
			Count:        virtualCount,
			Used:         used,
			Totalmem:     8000,
			Totalcore:    100,
			Health:       true,
		}
	}
	return makeNodeUsageMultiDevices(name, labels, devices)
}

// makeNodeUsageMultiDevices creates a NodeUsage with multiple healthy GPU devices.
func makeNodeUsageMultiDevices(name string, labels map[string]string, devices []*device.DeviceUsage) *NodeUsage {
	list := make([]*policy.DeviceListsScore, 0, len(devices))
	for _, d := range devices {
		list = append(list, &policy.DeviceListsScore{Device: d})
	}
	node := makeNodeWithLabels(name, labels)
	return &NodeUsage{
		Node:     node,
		NodeInfo: &device.NodeInfo{ID: name, Node: node},
		Devices: policy.DeviceUsageList{
			DeviceLists: list,
		},
	}
}

// makeGPUDeviceInfo creates a healthy nvidia DeviceInfo with count shares.
func makeGPUDeviceInfo(id string, index uint, count int64) device.DeviceInfo {
	return device.DeviceInfo{
		ID:           id,
		Index:        index,
		Count:        count,
		Devmem:       8000,
		Devcore:      100,
		Mode:         "hami",
		Type:         nvidia.NvidiaGPUDevice,
		Health:       true,
		DeviceVendor: nvidia.NvidiaGPUDevice,
	}
}

// newSuperpodTestScheduler builds a Scheduler wired with a real nodeManager
// and podManager, mirroring the production initialization path, so
// getNodesUsage/collectSuperpodCapacities run against a genuine node
// snapshot instead of degrading on a nil manager.
func newSuperpodTestScheduler() *Scheduler {
	return &Scheduler{
		nodeManager: newNodeManager(),
		podManager:  device.NewPodManager(),
		podLister:   listerscorev1.NewPodLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}
}

// registerSuperpodNode adds a node with the requested number of physical GPUs,
// each advertising virtualCount allocatable shares.
func (s *Scheduler) registerSuperpodNode(name string, labels map[string]string, physicalCount int, virtualCount int64) {
	devices := make([]device.DeviceInfo, physicalCount)
	for i := range devices {
		devices[i] = makeGPUDeviceInfo(fmt.Sprintf("%s-gpu%d", name, i), uint(i), virtualCount)
	}
	s.addNode(name, &device.NodeInfo{
		ID:   name,
		Node: makeNodeWithLabels(name, labels),
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: devices,
		},
	})
}

func newTestPodLister(t *testing.T, pods ...*corev1.Pod) listerscorev1.PodLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, pod := range pods {
		assert.NilError(t, indexer.Add(pod))
	}
	return listerscorev1.NewPodLister(indexer)
}

// ---------------------------------------------------------------------------
// superpodContext.TaskAnchored
// ---------------------------------------------------------------------------

func TestSuperpodContext_TaskAnchored(t *testing.T) {
	t.Run("default context is not anchored", func(t *testing.T) {
		ctx := &superpodContext{}
		assert.Equal(t, false, ctx.TaskAnchored)
	})

	t.Run("empty superpods not anchored", func(t *testing.T) {
		ctx := &superpodContext{
			Superpods: map[string]struct{}{},
		}
		assert.Equal(t, false, ctx.TaskAnchored)
	})

	t.Run("explicitly anchored is true", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: true,
			Superpods:    map[string]struct{}{"sp-1": {}},
		}
		assert.Equal(t, true, ctx.TaskAnchored)
	})

	t.Run("anchored with empty preferred racks still true", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:   true,
			Superpods:      map[string]struct{}{"sp-1": {}},
			PreferredRacks: map[string]struct{}{},
		}
		assert.Equal(t, true, ctx.TaskAnchored)
	})
}

func TestPrepareSuperpodContext(t *testing.T) {
	task := makeGPUPod("task", "default", "", 1)
	task.Labels[util.TaskIDLabel] = "task-1"
	s := &Scheduler{
		podLister: newTestPodLister(t, task),
		nodeLister: newTestNodeLister(t,
			makeNodeWithLabels("sp-node-1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}),
			makeNodeWithLabels("sp-node-2", map[string]string{util.SuperPodIDLabel: "sp-2", util.RackIDLabel: "rack-B"}),
			makeNodeWithLabels("plain-node", nil),
		),
	}

	t.Run("inactive policy skips context", func(t *testing.T) {
		assert.Assert(t, s.prepareSuperpodContext(task, util.NodeSchedulerPolicyBinpack.String()) == nil)
	})

	t.Run("missing task id skips context", func(t *testing.T) {
		pod := makeGPUPod("task", "default", "", 1)
		assert.Assert(t, s.prepareSuperpodContext(pod, util.NodeSchedulerPolicySuperpod.String()) == nil)
	})

	t.Run("collects anchored superpods and racks from active task pods", func(t *testing.T) {
		other := makeGPUPod("other", "default", "", 1)
		other.Labels[util.TaskIDLabel] = "task-1"
		other.Spec.NodeName = "sp-node-2"

		annotated := makeGPUPod("annotated", "default", "", 1)
		annotated.Labels[util.TaskIDLabel] = "task-1"
		annotated.Spec.NodeName = "plain-node"
		annotated.Annotations = map[string]string{util.AssignedNodeAnnotations: "sp-node-1"}

		self := makeGPUPod("self", "default", "", 1)
		self.Labels[util.TaskIDLabel] = "task-1"
		self.UID = task.UID
		self.Spec.NodeName = "sp-node-2"

		pending := makeGPUPod("pending", "default", "", 1)
		pending.Labels[util.TaskIDLabel] = "task-1"

		missingNode := makeGPUPod("missing-node", "default", "", 1)
		missingNode.Labels[util.TaskIDLabel] = "task-1"
		missingNode.Spec.NodeName = "deleted-node"

		terminating := makeGPUPod("terminating", "default", "", 1)
		terminating.Labels[util.TaskIDLabel] = "task-1"
		terminating.Spec.NodeName = "sp-node-2"
		now := metav1.Now()
		terminating.DeletionTimestamp = &now

		plain := makeGPUPod("plain", "default", "", 1)
		plain.Labels[util.TaskIDLabel] = "task-1"
		plain.Spec.NodeName = "plain-node"

		s.podLister = newTestPodLister(t, task, other, annotated, self, pending, missingNode, terminating, plain)
		ctx := s.prepareSuperpodContext(task, util.NodeSchedulerPolicySuperpod.String())

		assert.Assert(t, ctx != nil)
		assert.Equal(t, "task-1", ctx.TaskID)
		assert.Assert(t, ctx.TaskAnchored)
		assert.DeepEqual(t, ctx.Superpods, map[string]struct{}{"sp-1": {}, "sp-2": {}})
		assert.DeepEqual(t, ctx.PreferredRacks, map[string]struct{}{"rack-A": {}, "rack-B": {}})
	})

	t.Run("ignores succeeded and failed task pods when finding anchors", func(t *testing.T) {
		for _, phase := range []corev1.PodPhase{corev1.PodSucceeded, corev1.PodFailed} {
			t.Run(string(phase), func(t *testing.T) {
				finished := makeGPUPod("finished", "default", "", 1)
				finished.Labels[util.TaskIDLabel] = "task-1"
				finished.Spec.NodeName = "sp-node-2"
				finished.Status.Phase = phase
				s.podLister = newTestPodLister(t, task, finished)

				ctx := s.prepareSuperpodContext(task, util.NodeSchedulerPolicySuperpod.String())

				assert.Assert(t, ctx != nil)
				assert.Assert(t, !ctx.TaskAnchored)
				assert.Equal(t, 0, len(ctx.Superpods))
				assert.Equal(t, 0, len(ctx.PreferredRacks))
			})
		}
	})

	t.Run("prefers PodManager reservation before informer assignment", func(t *testing.T) {
		sibling := makeGPUPod("sibling", "default", "", 1)
		sibling.Labels[util.TaskIDLabel] = "task-1"
		s.podLister = newTestPodLister(t, task, sibling)
		s.podManager = device.NewPodManager()
		assert.Assert(t, s.podManager.AddPod(sibling, "sp-node-2", nil))

		ctx := s.prepareSuperpodContext(task, util.NodeSchedulerPolicySuperpod.String())

		assert.Assert(t, ctx != nil)
		assert.Assert(t, ctx.TaskAnchored)
		assert.DeepEqual(t, ctx.Superpods, map[string]struct{}{"sp-2": {}})
		assert.DeepEqual(t, ctx.PreferredRacks, map[string]struct{}{"rack-B": {}})
	})
}

// ---------------------------------------------------------------------------
// countFittingPods
// ---------------------------------------------------------------------------

func TestCountFittingPods(t *testing.T) {
	resetSuperpodTestDevices(t)

	count := func(t *testing.T, node *NodeUsage, pod *corev1.Pod) int64 {
		t.Helper()
		requests, err := device.Resourcereqs(pod)
		assert.NilError(t, err)
		weights, err := util.GetDeviceScoringWeightsByPod(pod)
		assert.NilError(t, err)
		got, err := (&Scheduler{}).countFittingPods(
			node.Node.Name, node, pod, requests, resolveNodeSchedulerPolicy(pod), weights)
		assert.NilError(t, err)
		return got
	}

	t.Run("counts complete pod fits constrained by core and memory", func(t *testing.T) {
		node := makeNodeUsageWithPhysicalCount("n1", nil, 1, 10, 0)
		pod := makeGPUPod("pod", "default", "task-1", 1)

		assert.Equal(t, int64(3), count(t, node, pod))
		assert.Equal(t, int64(0), node.Devices.DeviceLists[0].Device.Used)
	})

	t.Run("rejects aggregate shares that cannot satisfy one complete pod", func(t *testing.T) {
		node := makeNodeUsageWithPhysicalCount("n1", nil, 1, 10, 0)
		pod := makeGPUPod("pod", "default", "task-1", 2)

		assert.Equal(t, int64(0), count(t, node, pod))
	})

	t.Run("counts multi-device pod allocations", func(t *testing.T) {
		node := makeNodeUsageWithPhysicalCount("n1", nil, 3, 1, 0)
		pod := makeGPUPod("pod", "default", "task-1", 2)

		assert.Equal(t, int64(1), count(t, node, pod))
	})

	t.Run("does not count devices from unrequested vendors", func(t *testing.T) {
		node := makeNodeUsageWithPhysicalCount("n1", nil, 1, 10, 0)
		node.Devices.DeviceLists = append(node.Devices.DeviceLists, &policy.DeviceListsScore{
			Device: &device.DeviceUsage{
				ID:           "n1-unrequested",
				Type:         "unrequested-vendor",
				DeviceVendor: "unrequested-vendor",
				Count:        100,
				Totalmem:     8000,
				Totalcore:    100,
				Health:       true,
			},
		})
		pod := makeGPUPod("pod", "default", "task-1", 1)

		assert.Equal(t, int64(3), count(t, node, pod))
	})

	t.Run("pod without device requests has no finite capacity", func(t *testing.T) {
		node := makeNodeUsageWithPhysicalCount("n1", nil, 1, 10, 0)
		pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "cpu"}}}}

		assert.Equal(t, int64(0), count(t, node, pod))
	})
}

// ---------------------------------------------------------------------------
// singleSuperpodPodRequest
// ---------------------------------------------------------------------------

func assertSingleSuperpodPodRequest(t *testing.T, pod *corev1.Pod, expected int64) {
	t.Helper()
	got, err := singleSuperpodPodRequest(pod)
	assert.NilError(t, err)
	assert.Equal(t, expected, got)
}

func TestSingleSuperpodPodRequest(t *testing.T) {
	resetSuperpodTestDevices(t)

	t.Run("nil pod returns 0", func(t *testing.T) {
		assertSingleSuperpodPodRequest(t, nil, 0)
	})

	t.Run("pod with no containers returns 0", func(t *testing.T) {
		pod := &corev1.Pod{}
		assertSingleSuperpodPodRequest(t, pod, 0)
	})

	t.Run("single container requesting 1 GPU", func(t *testing.T) {
		pod := makeGPUPod("test-pod", "default", "task-1", 1)
		assertSingleSuperpodPodRequest(t, pod, 1)
	})

	t.Run("single container requesting 2 GPUs", func(t *testing.T) {
		pod := makeGPUPod("test-pod", "default", "task-1", 2)
		assertSingleSuperpodPodRequest(t, pod, 2)
	})

	t.Run("two app containers sum requests", func(t *testing.T) {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					},
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu": *resource.NewQuantity(2, resource.BinarySI),
							},
						},
					},
				},
			},
		}
		assertSingleSuperpodPodRequest(t, pod, 3)
	})

	t.Run("init container max wins over app total", func(t *testing.T) {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu": *resource.NewQuantity(4, resource.BinarySI),
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					},
				},
			},
		}
		assertSingleSuperpodPodRequest(t, pod, 4)
	})

	t.Run("app total wins over init max", func(t *testing.T) {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu": *resource.NewQuantity(2, resource.BinarySI),
							},
						},
					},
					{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"hami.io/gpu": *resource.NewQuantity(2, resource.BinarySI),
							},
						},
					},
				},
			},
		}
		assertSingleSuperpodPodRequest(t, pod, 4)
	})

	t.Run("invalid fractional GPU count returns an error", func(t *testing.T) {
		pod := makeGPUPod("test-pod", "default", "task-1", 1)
		pod.Spec.Containers[0].Resources.Limits["hami.io/gpu"] = resource.MustParse("1.5")
		_, err := singleSuperpodPodRequest(pod)
		assert.Assert(t, err != nil)
	})
}

// ---------------------------------------------------------------------------
// rejectNodesBySuperpod
// ---------------------------------------------------------------------------

func TestRejectNodesBySuperpod(t *testing.T) {
	task := makeGPUPod("task-pod", "default", "task-1", 1)
	ctx := &superpodContext{TaskID: "task-1"}

	t.Run("reads current superpod label when cached node is stale", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-sp1": makeNodeUsage("node-sp1", nil, 10, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{"sp-1": {}}
		ctx.TaskAnchored = true
		s := &Scheduler{
			nodeLister: newTestNodeLister(t,
				makeNodeWithLabels("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}),
			),
		}

		rejected := s.rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 0, rejected)
		assert.Assert(t, nodes["node-sp1"] != nil)
	})

	t.Run("anchored rejects nodes without superpod label", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-plain": makeNodeUsage("node-plain", map[string]string{}, 10, 0),
			"node-sp1":   makeNodeUsage("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{"sp-1": {}}
		ctx.TaskAnchored = true

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 1, rejected)
		assert.Equal(t, 1, len(nodes))
		assert.Assert(t, nodes["node-sp1"] != nil)
		assert.Equal(t, common.SuperpodNodeOutside, failed["node-plain"])
	})

	t.Run("anchored rejects nodes in different superpod", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-sp1": makeNodeUsage("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
			"node-sp2": makeNodeUsage("node-sp2", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{"sp-1": {}}
		ctx.TaskAnchored = true

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 1, rejected)
		assert.Equal(t, 1, len(nodes))
		assert.Assert(t, nodes["node-sp1"] != nil)
		assert.Equal(t, common.SuperpodMismatch, failed["node-sp2"])
	})

	t.Run("anchored keeps nodes in task superpod", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-sp1a": makeNodeUsage("node-sp1a", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
			"node-sp1b": makeNodeUsage("node-sp1b", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{"sp-1": {}}
		ctx.TaskAnchored = true

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 0, rejected)
		assert.Equal(t, 2, len(nodes))
	})

	t.Run("unanchored rejects superpod with insufficient capacity", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-sp1": makeNodeUsage("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
			"node-sp2": makeNodeUsage("node-sp2", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{}
		ctx.TaskAnchored = false
		ctx.podRequest = 1
		ctx.totalPods = 10
		ctx.superpodCapacities = map[string]int64{"sp-1": 5, "sp-2": 20}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 1, rejected)
		assert.Equal(t, 1, len(nodes))
		assert.Assert(t, nodes["node-sp2"] != nil)
		assert.Equal(t, common.SuperpodInsufficientCapacity, failed["node-sp1"])
	})

	t.Run("unanchored keeps superpod with sufficient capacity", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-sp1": makeNodeUsage("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{}
		ctx.TaskAnchored = false
		ctx.podRequest = 1
		ctx.totalPods = 10
		ctx.superpodCapacities = map[string]int64{"sp-1": 15}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 0, rejected)
		assert.Equal(t, 1, len(nodes))
	})

	t.Run("binpack chooses the smallest superpod that fits", func(t *testing.T) {
		task := makeGPUPod("task-pod", "default", "task-1", 1)
		task.Annotations = map[string]string{
			util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicyBinpack.String(),
		}
		nodes := map[string]*NodeUsage{
			"node-too-small": makeNodeUsage("node-too-small", map[string]string{util.SuperPodIDLabel: "sp-small"}, 10, 0),
			"node-best-fit":  makeNodeUsage("node-best-fit", map[string]string{util.SuperPodIDLabel: "sp-fit"}, 10, 0),
			"node-larger":    makeNodeUsage("node-larger", map[string]string{util.SuperPodIDLabel: "sp-large"}, 10, 0),
		}
		failed := map[string]string{}
		ctx := &superpodContext{
			TaskAnchored:       false,
			totalPods:          10,
			superpodCapacities: map[string]int64{"sp-small": 5, "sp-fit": 12, "sp-large": 30},
		}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 2, rejected)
		assert.Assert(t, nodes["node-best-fit"] != nil)
		assert.Assert(t, nodes["node-too-small"] == nil)
		assert.Assert(t, nodes["node-larger"] == nil)
		assert.Equal(t, common.SuperpodInsufficientCapacity, failed["node-too-small"])
		assert.Equal(t, common.SuperpodNotBestFit, failed["node-larger"])
	})

	t.Run("spread keeps all superpods that fit", func(t *testing.T) {
		task := makeGPUPod("task-pod", "default", "task-1", 1)
		task.Annotations = map[string]string{
			util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicySpread.String(),
		}
		nodes := map[string]*NodeUsage{
			"node-sp1": makeNodeUsage("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
			"node-sp2": makeNodeUsage("node-sp2", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 0),
		}
		ctx := &superpodContext{
			TaskAnchored:       false,
			totalPods:          10,
			superpodCapacities: map[string]int64{"sp-1": 12, "sp-2": 30},
		}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, map[string]string{})

		assert.Equal(t, 0, rejected)
		assert.Equal(t, 2, len(nodes))
	})

	t.Run("unanchored rejects nodes without superpod label", func(t *testing.T) {
		// Nodes without a superpodid label are rejected in both anchored and
		// unanchored modes: superpod-aware scheduling only considers nodes
		// carrying a superpod label.
		nodes := map[string]*NodeUsage{
			"node-plain": makeNodeUsage("node-plain", map[string]string{}, 10, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{}
		ctx.TaskAnchored = false
		ctx.podRequest = 1
		ctx.totalPods = 10
		ctx.superpodCapacities = map[string]int64{}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 1, rejected)
		assert.Equal(t, 0, len(nodes))
		assert.Equal(t, common.SuperpodNodeOutside, failed["node-plain"])
	})

	t.Run("unanchored with zero totalPods keeps all superpod nodes", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-sp1": makeNodeUsage("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 1, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{}
		ctx.TaskAnchored = false
		ctx.podRequest = 1
		ctx.totalPods = 0
		ctx.superpodCapacities = map[string]int64{"sp-1": 0}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 0, rejected)
		assert.Equal(t, 1, len(nodes))
	})
}

// ---------------------------------------------------------------------------
// collectRackFittingPods
// ---------------------------------------------------------------------------

func TestCollectRackFittingPods(t *testing.T) {
	ctx := &superpodContext{
		TaskID:          "task-1",
		RackFittingPods: make(map[string]map[string]int64),
	}

	t.Run("accumulates fitting pod counts per rack and superpod", func(t *testing.T) {
		ctx.RackFittingPods = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsageWithPhysicalCount("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 2, 10, 0),
			"n2": makeNodeUsageWithPhysicalCount("n2", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 2, 10, 0),
			"n3": makeNodeUsageWithPhysicalCount("n3", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-B"}, 1, 10, 0),
			"n4": makeNodeUsageWithPhysicalCount("n4", map[string]string{util.SuperPodIDLabel: "sp-2", util.RackIDLabel: "rack-A"}, 1, 10, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		assert.NilError(t, (&Scheduler{}).collectRackFittingPods(ctx, task, &nodes))

		// Each physical GPU can fit three copies of the complete pod request.
		assert.Equal(t, int64(12), ctx.RackFittingPods["sp-1"]["rack-A"])
		assert.Equal(t, int64(3), ctx.RackFittingPods["sp-1"]["rack-B"])
		assert.Equal(t, int64(3), ctx.RackFittingPods["sp-2"]["rack-A"])
	})

	t.Run("skips nodes without superpod or rack label", func(t *testing.T) {
		ctx.RackFittingPods = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsage("n1", map[string]string{util.RackIDLabel: "rack-A"}, 10, 0),
			"n2": makeNodeUsage("n2", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
			"n3": makeNodeUsage("n3", map[string]string{}, 10, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		assert.NilError(t, (&Scheduler{}).collectRackFittingPods(ctx, task, &nodes))

		assert.Equal(t, 0, len(ctx.RackFittingPods))
	})

	t.Run("counts only complete multi-device pod fits", func(t *testing.T) {
		ctx.RackFittingPods = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsageWithPhysicalCount("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 4, 1, 0),
			"n2": makeNodeUsageWithPhysicalCount("n2", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 10, 1, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 2)

		assert.NilError(t, (&Scheduler{}).collectRackFittingPods(ctx, task, &nodes))

		// Four GPUs fit two pods and ten GPUs fit five; each counted pod
		// consumes both requested GPUs.
		assert.Equal(t, int64(7), ctx.RackFittingPods["sp-1"]["rack-A"])
	})

	t.Run("counts only devices with enough free memory for the pod", func(t *testing.T) {
		ctx.RackFittingPods = make(map[string]map[string]int64)
		labels := map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}
		lowMemory := makeNodeUsageWithPhysicalCount("low-memory", labels, 1, 20, 0)
		lowMemory.Devices.DeviceLists[0].Device.Usedmem = 7500
		enoughMemory := makeNodeUsageWithPhysicalCount("enough-memory", labels, 1, 10, 0)
		enoughMemory.Devices.DeviceLists[0].Device.Usedmem = 6000
		nodes := map[string]*NodeUsage{
			"low-memory":    lowMemory,
			"enough-memory": enoughMemory,
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		assert.NilError(t, (&Scheduler{}).collectRackFittingPods(ctx, task, &nodes))

		assert.Equal(t, int64(2), ctx.RackFittingPods["sp-1"]["rack-A"])
	})

	t.Run("checks percentage memory requests against each device", func(t *testing.T) {
		ctx.RackFittingPods = make(map[string]map[string]int64)
		labels := map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}
		underPercent := makeNodeUsageWithPhysicalCount("under-percent", labels, 1, 20, 0)
		underPercent.Devices.DeviceLists[0].Device.Usedmem = 2500
		overPercent := makeNodeUsageWithPhysicalCount("over-percent", labels, 1, 10, 0)
		overPercent.Devices.DeviceLists[0].Device.Usedmem = 1500
		nodes := map[string]*NodeUsage{
			"under-percent": underPercent,
			"over-percent":  overPercent,
		}
		task := makeGPUPod("pod", "default", "task-1", 1)
		delete(task.Spec.Containers[0].Resources.Limits, "hami.io/gpumem")
		task.Spec.Containers[0].Resources.Limits["hami.io/gpumem-percentage"] = *resource.NewQuantity(75, resource.BinarySI)

		assert.NilError(t, (&Scheduler{}).collectRackFittingPods(ctx, task, &nodes))

		assert.Equal(t, int64(1), ctx.RackFittingPods["sp-1"]["rack-A"])
	})

	t.Run("nodes with no fitting pod capacity contribute no pods", func(t *testing.T) {
		ctx.RackFittingPods = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsage("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 0, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		assert.NilError(t, (&Scheduler{}).collectRackFittingPods(ctx, task, &nodes))

		assert.Equal(t, 0, len(ctx.RackFittingPods))
	})

	t.Run("skips nil node entries", func(t *testing.T) {
		ctx.RackFittingPods = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"nil-node": nil,
			"n1":       makeNodeUsageWithPhysicalCount("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 1, 10, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		assert.NilError(t, (&Scheduler{}).collectRackFittingPods(ctx, task, &nodes))

		assert.Equal(t, int64(3), ctx.RackFittingPods["sp-1"]["rack-A"])
	})
}

// ---------------------------------------------------------------------------
// applySuperpodRackBoost
// ---------------------------------------------------------------------------

func TestApplySuperpodRackBoost(t *testing.T) {
	t.Run("nil context returns base score", func(t *testing.T) {
		node := makeNodeWithLabels("n1", map[string]string{util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(nil, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("node without rack label returns base score", func(t *testing.T) {
		ctx := &superpodContext{TaskAnchored: true, Superpods: map[string]struct{}{"sp-1": {}}}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("anchored binpack boosts preferred rack", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:   true,
			Superpods:      map[string]struct{}{"sp-1": {}},
			PreferredRacks: map[string]struct{}{"rack-A": {}},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0+superpodRackBoost), score)
	})

	t.Run("anchored spread subtracts boost for preferred rack", func(t *testing.T) {
		oldBoost := superpodRackBoost
		superpodRackBoost = 100
		t.Cleanup(func() { superpodRackBoost = oldBoost })

		ctx := &superpodContext{
			TaskAnchored:   true,
			Superpods:      map[string]struct{}{"sp-1": {}},
			PreferredRacks: map[string]struct{}{"rack-A": {}},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicySpread.String())
		assert.Equal(t, float32(2.5), score)
	})

	t.Run("anchored binpack-first chain keeps the boost", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:   true,
			PreferredRacks: map[string]struct{}{"rack-A": {}},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, "binpack,spread")
		assert.Equal(t, float32(5.0+superpodRackBoost), score)
	})

	t.Run("anchored non-preferred rack keeps base score", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:   true,
			Superpods:      map[string]struct{}{"sp-1": {}},
			PreferredRacks: map[string]struct{}{"rack-A": {}},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-B"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("first pod proportional boost for largest rack", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   1,
			RackFittingPods: map[string]map[string]int64{
				"sp-1": {"rack-A": 30, "rack-B": 10},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// rack-A has the most fitting pods, so it gets the full boost.
		expectedBoost := superpodRackBoost
		assert.Equal(t, float32(5.0+expectedBoost), score)
	})

	t.Run("first pod proportional boost across superpods", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   1,
			RackFittingPods: map[string]map[string]int64{
				"sp-1": {"rack-A": 30, "rack-B": 10},
				"sp-2": {"rack-C": 20},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// cross-superpod denominator: rack-A 30/30 full boost
		assert.Equal(t, float32(5.0+superpodRackBoost), score)

		node2 := makeNodeWithLabels("n2", map[string]string{util.SuperPodIDLabel: "sp-2", util.RackIDLabel: "rack-C"})
		score2 := applySuperpodRackBoost(ctx, node2, "n2", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// rack-C 20/30 of maxRackFittingPods
		expectedBoost2 := superpodRackBoost * float32(20) / float32(30)
		assert.Equal(t, float32(5.0+expectedBoost2), score2)
	})

	t.Run("first pod rack with no fitting pods gets no boost", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			RackFittingPods: map[string]map[string]int64{
				"sp-1": {"rack-A": 0, "rack-B": 30},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// rack-A has no fitting pod capacity, so it gets no rack preference.
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("first pod boost is proportional to fitting pod count", func(t *testing.T) {
		ctx := &superpodContext{
			RackFittingPods: map[string]map[string]int64{
				"sp-1": {"rack-A": 2, "rack-B": 4},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0+superpodRackBoost/2), score)
	})

	t.Run("first pod no rack counts for superpod returns base", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:    false,
			Superpods:       map[string]struct{}{},
			podRequest:      1,
			RackFittingPods: map[string]map[string]int64{},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("first pod node without superpod label returns base", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:    false,
			Superpods:       map[string]struct{}{},
			podRequest:      1,
			RackFittingPods: map[string]map[string]int64{"sp-1": {"rack-A": 20}},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("first pod spread subtracts proportional boost", func(t *testing.T) {
		oldBoost := superpodRackBoost
		superpodRackBoost = 100
		t.Cleanup(func() { superpodRackBoost = oldBoost })

		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   1,
			RackFittingPods: map[string]map[string]int64{
				"sp-1": {"rack-A": 20, "rack-B": 20},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicySpread.String())
		assert.Equal(t, float32(2.5), score)
	})

	t.Run("spread caps boost without flattening scores", func(t *testing.T) {
		boost := float32(util.Weight) * 2
		higher := applyRackBoostToScore(5, boost, true)
		lower := applyRackBoostToScore(4, boost, true)
		assert.Equal(t, float32(2.5), higher)
		assert.Equal(t, float32(2), lower)
		assert.Assert(t, lower < higher)
	})

	t.Run("spread boosts zero and negative scores", func(t *testing.T) {
		boost := float32(util.Weight) * 2
		assert.Equal(t, -boost, applyRackBoostToScore(0, boost, true))
		assert.Equal(t, float32(-10)-boost, applyRackBoostToScore(-10, boost, true))
	})

	t.Run("spread keeps idle preferred rack ahead of idle rack elsewhere", func(t *testing.T) {
		boost := float32(2)
		preferredRackScore := applyRackBoostToScore(0, boost, true)
		otherRackScore := float32(0)
		assert.Equal(t, float32(-2), preferredRackScore)
		assert.Assert(t, preferredRackScore < otherRackScore)
	})

	t.Run("first pod binpack-first chain keeps proportional boost", func(t *testing.T) {
		ctx := &superpodContext{
			podRequest: 1,
			RackFittingPods: map[string]map[string]int64{
				"sp-1": {"rack-A": 20, "rack-B": 10},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, "binpack,spread")
		assert.Equal(t, float32(5.0+superpodRackBoost), score)
	})

	t.Run("first pod all fitting pods in same rack gets full boost", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   1,
			RackFittingPods: map[string]map[string]int64{
				"sp-1": {"rack-A": 40},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// rack-A has all fitting pods, so it gets the full boost.
		assert.Equal(t, float32(5.0+superpodRackBoost), score)
	})
}

func TestResolveSuperpodRackBoost(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want float32
	}{
		{name: "unset uses default", env: "", want: float32(util.Weight)},
		{name: "valid override is trimmed and parsed", env: " 2.5 ", want: 2.5},
		{name: "invalid override uses default", env: "invalid", want: float32(util.Weight)},
		{name: "zero override uses default", env: "0", want: float32(util.Weight)},
		{name: "negative override uses default", env: "-1", want: float32(util.Weight)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HAMI_SUPERPOD_RACK_BOOST", tt.env)
			assert.Equal(t, tt.want, resolveSuperpodRackBoost())
		})
	}
}

// ---------------------------------------------------------------------------
// resolveSuperpodTotalRequest
// ---------------------------------------------------------------------------

func TestResolveSuperpodTotalRequest(t *testing.T) {
	resetSuperpodTestDevices(t)

	s := &Scheduler{}

	assertTotalRequest := func(task *corev1.Pod, single, expected int64) {
		t.Helper()
		got, err := s.resolveSuperpodTotalRequest(task, single)
		assert.NilError(t, err)
		assert.Equal(t, expected, got)
	}

	t.Run("nil task returns 0", func(t *testing.T) {
		assertTotalRequest(nil, 1, 0)
	})

	t.Run("zero single returns 0", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 1)
		assertTotalRequest(pod, 0, 0)
	})

	t.Run("negative single returns 0", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 1)
		assertTotalRequest(pod, -1, 0)
	})

	t.Run("explicit label takes highest priority", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "42"
		assertTotalRequest(pod, 1, 42)
	})

	t.Run("same-task ReplicaSets are aggregated", func(t *testing.T) {
		replicas := int32(3)
		template := makeGPUPod("template", "default", "", 2)
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker",
				Namespace: "default",
				Labels:    map[string]string{util.TaskIDLabel: "task-1"},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{Spec: template.Spec},
			},
		}
		s.replicaSetLister, _, _ = newSuperpodTestListers(t, rs)
		pod := makeGPUPod("pod", "default", "", 1)
		pod.Labels[util.TaskIDLabel] = "task-1"
		assertTotalRequest(pod, 1, 3)
		s.replicaSetLister = nil
	})

	t.Run("explicit share label rounds up to whole pods", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 2)
		pod.Labels[util.SuperpodTotalRequestLabel] = "5"
		assertTotalRequest(pod, 2, 3)
	})

	t.Run("invalid label value falls through", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "not-a-number"
		// No kubeClient, no owner refs → falls back to single
		assertTotalRequest(pod, 1, 1)
	})

	t.Run("zero label value falls through", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "0"
		// No kubeClient, no owner refs → falls back to single
		assertTotalRequest(pod, 1, 1)
	})

	t.Run("negative label value falls through", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "-5"
		// No kubeClient, no owner refs → falls back to single
		assertTotalRequest(pod, 1, 1)
	})

	t.Run("fallback to single pod request when nothing else available", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "task-1", 1)
		// No label, no kubeClient, no owner refs
		assertTotalRequest(pod, 1, 1)
	})

	t.Run("fallback to single pod request without taskid label", func(t *testing.T) {
		pod := makeGPUPod("pod", "default", "", 1)
		// No label, no taskid → falls back to single
		assertTotalRequest(pod, 1, 1)
	})
}

// ---------------------------------------------------------------------------
// resolveTaskIDSuperpodTotal
// ---------------------------------------------------------------------------

func TestResolveTaskIDSuperpodTotal(t *testing.T) {
	resetSuperpodTestDevices(t)

	t.Run("nil clientset returns 0", func(t *testing.T) {
		total, err := resolveTaskIDSuperpodTotal(nil, "default", "task-1")
		assert.NilError(t, err)
		assert.Equal(t, int64(0), total)
	})

	t.Run("empty taskID returns 0", func(t *testing.T) {
		total, err := resolveTaskIDSuperpodTotal(nil, "default", "")
		assert.NilError(t, err)
		assert.Equal(t, int64(0), total)
	})

	t.Run("whitespace taskID returns 0", func(t *testing.T) {
		total, err := resolveTaskIDSuperpodTotal(nil, "default", "   ")
		assert.NilError(t, err)
		assert.Equal(t, int64(0), total)
	})

	t.Run("aggregates positive replica counts and skips empty templates", func(t *testing.T) {
		two := int32(2)
		zero := int32(0)
		withRequest := makeGPUPod("template", "default", "", 3)
		withoutRequest := &corev1.Pod{}
		replicaSetLister, _, _ := newSuperpodTestListers(t,
			&appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "workers-a", Namespace: "default", Labels: map[string]string{util.TaskIDLabel: "task-1"}},
				Spec:       appsv1.ReplicaSetSpec{Replicas: &two, Template: corev1.PodTemplateSpec{Spec: withRequest.Spec}},
			},
			&appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "workers-zero", Namespace: "default", Labels: map[string]string{util.TaskIDLabel: "task-1"}},
				Spec:       appsv1.ReplicaSetSpec{Replicas: &zero, Template: corev1.PodTemplateSpec{Spec: withRequest.Spec}},
			},
			&appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "workers-empty", Namespace: "default", Labels: map[string]string{util.TaskIDLabel: "task-1"}},
				Spec:       appsv1.ReplicaSetSpec{Replicas: &two, Template: corev1.PodTemplateSpec{Spec: withoutRequest.Spec}},
			},
			&appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "workers-unspecified", Namespace: "default", Labels: map[string]string{util.TaskIDLabel: "task-1"}},
				Spec:       appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{Spec: withRequest.Spec}},
			},
		)

		total, err := resolveTaskIDSuperpodTotal(replicaSetLister, "default", "task-1")
		assert.NilError(t, err)
		assert.Equal(t, int64(2), total)
	})

	t.Run("invalid template request is returned", func(t *testing.T) {
		pod := makeGPUPod("template", "default", "", 1)
		pod.Spec.Containers[0].Resources.Limits["hami.io/gpu"] = resource.MustParse("1.5")
		replicas := int32(1)
		replicaSetLister, _, _ := newSuperpodTestListers(t, &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default", Labels: map[string]string{util.TaskIDLabel: "task-1"}},
			Spec:       appsv1.ReplicaSetSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: pod.Spec}},
		})
		_, err := resolveTaskIDSuperpodTotal(replicaSetLister, "default", "task-1")
		assert.Assert(t, err != nil)
	})
}

func TestSuperpodOwnerReplicas(t *testing.T) {
	resetSuperpodTestDevices(t)
	replicas := int32(3)
	zero := int32(0)

	tests := []struct {
		name      string
		kind      string
		object    runtime.Object
		wantTotal int64
		wantFound bool
	}{
		{
			name: "ReplicaSet",
			kind: "ReplicaSet",
			object: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       appsv1.ReplicaSetSpec{Replicas: &replicas},
			},
			wantTotal: 3,
			wantFound: true,
		},
		{
			name: "StatefulSet",
			kind: "StatefulSet",
			object: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
			},
			wantTotal: 3,
			wantFound: true,
		},
		{
			name: "Job",
			kind: "Job",
			object: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       batchv1.JobSpec{Parallelism: &replicas},
			},
			wantTotal: 3,
			wantFound: true,
		},
		{
			name: "zero replicas are not considered resolved",
			kind: "ReplicaSet",
			object: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       appsv1.ReplicaSetSpec{Replicas: &zero},
			},
		},
		{
			name: "ReplicaSet with unspecified replicas",
			kind: "ReplicaSet",
			object: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
			},
		},
		{
			name: "StatefulSet with zero replicas",
			kind: "StatefulSet",
			object: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       appsv1.StatefulSetSpec{Replicas: &zero},
			},
		},
		{
			name: "StatefulSet with unspecified replicas",
			kind: "StatefulSet",
			object: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
			},
		},
		{
			name: "Job with zero parallelism",
			kind: "Job",
			object: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       batchv1.JobSpec{Parallelism: &zero},
			},
		},
		{
			name: "Job with unspecified parallelism",
			kind: "Job",
			object: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
			},
		},
		{
			name: "unsupported owner kind",
			kind: "Deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if tt.object != nil {
				objects = append(objects, tt.object)
			}
			task := makeGPUPod("pod", "default", "", 2)
			task.OwnerReferences = []metav1.OwnerReference{{Kind: tt.kind, Name: "owner"}}
			rsLister, stsLister, jobLister := newSuperpodTestListers(t, objects...)
			s := &Scheduler{replicaSetLister: rsLister, statefulSetLister: stsLister, jobLister: jobLister}

			total, found, err := s.superpodOwnerReplicas(task)
			assert.NilError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			assert.Equal(t, tt.wantFound, found)
		})
	}

	t.Run("missing owner or client is unresolved", func(t *testing.T) {
		task := makeGPUPod("pod", "default", "", 1)
		total, found, err := (&Scheduler{}).superpodOwnerReplicas(task)
		assert.NilError(t, err)
		assert.Equal(t, int64(0), total)
		assert.Assert(t, !found)

		task.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: "owner"}}
		total, found, err = (&Scheduler{}).superpodOwnerReplicas(task)
		assert.NilError(t, err)
		assert.Equal(t, int64(0), total)
		assert.Assert(t, !found)
	})

	t.Run("invalid pod request is returned", func(t *testing.T) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
			Spec:       batchv1.JobSpec{Parallelism: &replicas},
		}
		task := makeGPUPod("pod", "default", "", 1)
		task.Spec.Containers[0].Resources.Limits["hami.io/gpu"] = resource.MustParse("1.5")
		task.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: "owner"}}
		rsLister, stsLister, jobLister := newSuperpodTestListers(t, job)
		s := &Scheduler{replicaSetLister: rsLister, statefulSetLister: stsLister, jobLister: jobLister}
		_, _, err := s.superpodOwnerReplicas(task)
		assert.Assert(t, err != nil)
	})

	t.Run("NotFound owner lookup remains eligible for fallback", func(t *testing.T) {
		task := makeGPUPod("pod", "default", "", 1)
		task.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: "missing"}}
		rsLister, stsLister, jobLister := newSuperpodTestListers(t)
		s := &Scheduler{replicaSetLister: rsLister, statefulSetLister: stsLister, jobLister: jobLister}

		total, found, err := s.superpodOwnerReplicas(task)
		assert.NilError(t, err)
		assert.Equal(t, int64(0), total)
		assert.Assert(t, !found)

		total, err = s.resolveSuperpodTotalRequest(task, 1)
		assert.NilError(t, err)
		assert.Equal(t, int64(1), total)
	})
}

// ---------------------------------------------------------------------------
// applySuperpodFilter (integration-level: nil context short-circuit)
// ---------------------------------------------------------------------------

func TestApplySuperpodFilter_NilContext(t *testing.T) {
	nodes := map[string]*NodeUsage{
		"n1": makeNodeUsage("n1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
	}
	failed := map[string]string{}
	// nil context should be a no-op
	(&Scheduler{}).applySuperpodFilter(nil, makeGPUPod("pod", "default", "task-1", 1), &nodes, failed)
	assert.Equal(t, 1, len(nodes))
	assert.Equal(t, 0, len(failed))
}

func TestApplySuperpodFilter_InvalidPodRequest(t *testing.T) {
	resetSuperpodTestDevices(t)
	pod := makeGPUPod("pod", "default", "task-1", 1)
	pod.Spec.Containers[0].Resources.Limits["hami.io/gpu"] = resource.MustParse("1.5")
	nodes := map[string]*NodeUsage{
		"n1": makeNodeUsage("n1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
	}

	err := (&Scheduler{}).applySuperpodFilter(
		&superpodContext{TaskID: "task-1"},
		pod,
		&nodes,
		map[string]string{},
	)
	assert.Assert(t, err != nil)
	assert.Equal(t, 1, len(nodes))
}

func TestCalcScoreWithOptions_SuperpodFilterError(t *testing.T) {
	resetSuperpodTestDevices(t)
	task := makeGPUPod("pod", "default", "", 1)
	task.Labels[util.TaskIDLabel] = "task-1"
	task.Annotations = map[string]string{util.NodeSchedulerPolicyAnnotationKey: "superpod"}
	task.Spec.Containers[0].Resources.Limits["hami.io/gpu"] = resource.MustParse("1.5")
	s := &Scheduler{podLister: newTestPodLister(t, task)}
	nodes := map[string]*NodeUsage{}

	result, err := s.calcScoreWithOptions(&nodes, nil, task, map[string]string{}, false, false)
	assert.Assert(t, err != nil)
	assert.Assert(t, result == nil)
}

func TestCalcScoreWithOptions_NodeScoringError(t *testing.T) {
	s := newSuperpodTestScheduler()
	task := makeGPUPod("pod", "default", "", 1)
	nodes := map[string]*NodeUsage{
		"missing": makeNodeUsage("missing", nil, 1, 0),
	}
	nodes["missing"].NodeInfo = nil
	failed := map[string]string{}

	result, err := s.calcScoreWithOptions(&nodes, nil, task, failed, false, false)
	assert.Assert(t, err != nil)
	assert.Assert(t, result != nil)
	assert.Equal(t, 0, len(result.NodeList))
	assert.Assert(t, failed["missing"] != "")
}

// ---------------------------------------------------------------------------
// applySuperpodFilter (unanchored: capacity filter + rack counting order)
// ---------------------------------------------------------------------------

func TestApplySuperpodFilter_UnanchoredCapacityFilter(t *testing.T) {
	resetSuperpodTestDevices(t)

	// Real scenario: a fully wired scheduler whose nodeManager holds the
	// same nodes as the candidate set, so collectSuperpodCapacities runs
	// the genuine getNodesUsage full-snapshot path and computes real
	// per-superpod capacities.
	t.Run("real node snapshot filters superpods by capacity", func(t *testing.T) {
		s := newSuperpodTestScheduler()
		ctx := &superpodContext{
			TaskID:          "task-1",
			TaskAnchored:    false,
			Superpods:       map[string]struct{}{},
			PreferredRacks:  map[string]struct{}{},
			RackFittingPods: make(map[string]map[string]int64),
		}
		// The task declares 60 GPU shares in total via the explicit label,
		// and a single pod requests 1 GPU share. No kubeClient and no owner refs are
		// needed because the label takes the highest priority.
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "60"

		// A 30-core request allows three shares per device: sp-1 has
		// capacity 15 < 60; sp-2 has capacity 60 and is kept.
		// plain node: no superpod label, rejected (superpod-aware scheduling
		// only considers superpod nodes).
		s.registerSuperpodNode("n-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 5, 10)
		s.registerSuperpodNode("n-sp2a", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 10)
		s.registerSuperpodNode("n-sp2b", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 10)

		// Candidate nodes mirror the registered snapshot.
		nodes := map[string]*NodeUsage{
			"n-sp1":   makeNodeUsageWithPhysicalCount("n-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 5, 10, 0),
			"n-sp2a":  makeNodeUsageWithPhysicalCount("n-sp2a", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 10, 0),
			"n-sp2b":  makeNodeUsageWithPhysicalCount("n-sp2b", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 10, 0),
			"n-plain": makeNodeUsage("n-plain", map[string]string{}, 10, 0),
		}
		failed := map[string]string{}

		s.applySuperpodFilter(ctx, pod, &nodes, failed)

		// sp-1 is too small for the whole task: its node is rejected.
		assert.Assert(t, nodes["n-sp1"] == nil, "node of a superpod with insufficient capacity should be rejected")
		assert.Equal(t, common.SuperpodInsufficientCapacity, failed["n-sp1"])
		// sp-2 can hold the whole task: both of its nodes survive.
		assert.Assert(t, nodes["n-sp2a"] != nil, "node of a sufficient superpod should survive")
		assert.Assert(t, nodes["n-sp2b"] != nil, "node of a sufficient superpod should survive")
		// The plain node (no superpod label) is rejected: superpod-aware
		// scheduling only considers nodes carrying a superpod label.
		assert.Assert(t, nodes["n-plain"] == nil, "plain node should be rejected under the superpod policy")
		assert.Equal(t, common.SuperpodNodeOutside, failed["n-plain"])
		assert.Equal(t, 2, len(nodes))
	})

	t.Run("full snapshot counts nodes outside the candidate set", func(t *testing.T) {
		s := newSuperpodTestScheduler()
		ctx := &superpodContext{
			TaskID:          "task-1",
			TaskAnchored:    false,
			Superpods:       map[string]struct{}{},
			PreferredRacks:  map[string]struct{}{},
			RackFittingPods: make(map[string]map[string]int64),
		}
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "120"
		pod.Spec.Containers[0].Resources.Limits["hami.io/gpucores"] = *resource.NewQuantity(1, resource.BinarySI)
		pod.Spec.Containers[0].Resources.Limits["hami.io/gpumem"] = *resource.NewQuantity(500, resource.BinarySI)

		// sp-1's capacity is spread across two nodes: only one of them is in
		// the candidate set. The full snapshot must still see both, so sp-1
		// has capacity for 120 complete pod copies and its candidate survives.
		s.registerSuperpodNode("n-sp1a", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6, 10)
		s.registerSuperpodNode("n-sp1b", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6, 10)

		nodes := map[string]*NodeUsage{
			"n-sp1a": makeNodeUsageWithPhysicalCount("n-sp1a", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6, 10, 0),
		}
		failed := map[string]string{}

		s.applySuperpodFilter(ctx, pod, &nodes, failed)

		assert.Assert(t, nodes["n-sp1a"] != nil,
			"candidate node should survive when the superpod's full capacity (including non-candidate nodes) fits the task")
		assert.Equal(t, 1, len(nodes))
		assert.Equal(t, 0, len(failed))
	})

	t.Run("cordoned nodes do not contribute capacity", func(t *testing.T) {
		s := newSuperpodTestScheduler()
		s.registerSuperpodNode("n-sp1-available", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6, 10)
		s.registerSuperpodNode("n-sp1-cordoned", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6, 10)
		cordoned, err := s.GetNode("n-sp1-cordoned")
		assert.NilError(t, err)
		cordoned.Node.Spec.Unschedulable = true
		s.addNode("n-sp1-cordoned", cordoned)

		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Spec.Containers[0].Resources.Limits["hami.io/gpucores"] = *resource.NewQuantity(1, resource.BinarySI)
		pod.Spec.Containers[0].Resources.Limits["hami.io/gpumem"] = *resource.NewQuantity(500, resource.BinarySI)
		capacities, err := s.collectSuperpodCapacities(map[string]struct{}{"sp-1": {}}, pod)

		assert.NilError(t, err)
		assert.Equal(t, int64(60), capacities["sp-1"])
	})

	t.Run("remaining demand of an anchored task is reserved", func(t *testing.T) {
		s := newSuperpodTestScheduler()
		s.registerSuperpodNode("n-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 8, 1)

		anchored := makeGPUPod("anchored", "default", "task-anchored", 1)
		anchored.Labels[util.TaskIDLabel] = "task-anchored"
		anchored.Labels[util.SuperpodTotalRequestLabel] = "4"
		anchored.Spec.NodeName = "n-sp1"
		scheduling := makeGPUPod("scheduling", "default", "task-new", 1)
		scheduling.Labels[util.TaskIDLabel] = "task-new"
		scheduling.Labels[util.SuperpodTotalRequestLabel] = "6"
		scheduling.Spec.NodeName = "n-sp1"
		s.podLister = newTestPodLister(t, anchored, scheduling)
		assert.Assert(t, s.podManager.AddPod(anchored, "n-sp1", device.PodDevices{
			nvidia.NvidiaGPUDevice: device.PodSingleDevice{
				device.ContainerDevices{{UUID: "n-sp1-gpu0", Type: nvidia.NvidiaGPUDevice}},
			},
		}))

		ctx := &superpodContext{
			TaskID:          "task-new",
			Superpods:       map[string]struct{}{},
			PreferredRacks:  map[string]struct{}{},
			RackFittingPods: make(map[string]map[string]int64),
		}
		nodes := map[string]*NodeUsage{
			"n-sp1": makeNodeUsageWithPhysicalCount("n-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 8, 1, 0),
		}
		failed := map[string]string{}

		err := s.applySuperpodFilter(ctx, scheduling, &nodes, failed)

		assert.NilError(t, err)
		// Seven complete pod copies fit in the snapshot. One anchored replica
		// is allocated, so its remaining three replicas are reserved, leaving
		// capacity for four new-task replicas.
		assert.Equal(t, int64(4), ctx.superpodCapacities["sp-1"])
		assert.Assert(t, nodes["n-sp1"] == nil, "new task must not consume capacity reserved for the anchored task")
		assert.Equal(t, common.SuperpodInsufficientCapacity, failed["n-sp1"])
	})

	t.Run("empty superpod set returns an empty capacity map", func(t *testing.T) {
		capacities, err := newSuperpodTestScheduler().collectSuperpodCapacities(nil, nil)
		assert.NilError(t, err)
		assert.Assert(t, capacities != nil)
		assert.Equal(t, 0, len(capacities))
	})

	// Note: there is deliberately no "nil node manager" subtest here.
	// collectSuperpodCapacities requires a fully initialized Scheduler with
	// a real nodeManager (getNodesUsage → ListNodes dereferences it), so
	// every unanchored-mode test must use newSuperpodTestScheduler().
}

// ---------------------------------------------------------------------------
// applySuperpodFilter (anchored: convergence enforcement)
// ---------------------------------------------------------------------------

func TestApplySuperpodFilter_AnchoredRejectsOtherSuperpods(t *testing.T) {
	resetSuperpodTestDevices(t)

	ctx := &superpodContext{
		TaskID:          "task-1",
		TaskAnchored:    true,
		Superpods:       map[string]struct{}{"sp-1": {}},
		PreferredRacks:  map[string]struct{}{"rack-A": {}},
		RackFittingPods: make(map[string]map[string]int64),
	}
	pod := makeGPUPod("pod", "default", "task-1", 1)

	nodes := map[string]*NodeUsage{
		"n-sp1":   makeNodeUsage("n-sp1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 10, 0),
		"n-sp2":   makeNodeUsage("n-sp2", map[string]string{util.SuperPodIDLabel: "sp-2", util.RackIDLabel: "rack-A"}, 10, 0),
		"n-plain": makeNodeUsage("n-plain", map[string]string{}, 10, 0),
	}
	failed := map[string]string{}

	(&Scheduler{}).applySuperpodFilter(ctx, pod, &nodes, failed)

	// Only sp-1 node should survive.
	assert.Equal(t, 1, len(nodes))
	assert.Assert(t, nodes["n-sp1"] != nil)
	assert.Equal(t, common.SuperpodMismatch, failed["n-sp2"])
	assert.Equal(t, common.SuperpodNodeOutside, failed["n-plain"])
}
