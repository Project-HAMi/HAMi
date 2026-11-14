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
	"errors"
	"testing"

	"gotest.tools/v3/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	k8stesting "k8s.io/client-go/testing"
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
// count total slots and used slots already consumed.
func makeNodeUsage(name string, labels map[string]string, count, used int64) *NodeUsage {
	return &NodeUsage{
		Node: makeNodeWithLabels(name, labels),
		Devices: policy.DeviceUsageList{
			DeviceLists: []*policy.DeviceListsScore{
				{Device: &device.DeviceUsage{
					ID:     name + "-gpu0",
					Type:   nvidia.NvidiaGPUDevice,
					Count:  count,
					Used:   used,
					Health: true,
				}},
			},
		},
	}
}

// makeNodeUsageMultiDevices creates a NodeUsage with multiple healthy GPU devices.
func makeNodeUsageMultiDevices(name string, labels map[string]string, devices []*device.DeviceUsage) *NodeUsage {
	list := make([]*policy.DeviceListsScore, 0, len(devices))
	for _, d := range devices {
		list = append(list, &policy.DeviceListsScore{Device: d})
	}
	return &NodeUsage{
		Node: makeNodeWithLabels(name, labels),
		Devices: policy.DeviceUsageList{
			DeviceLists: list,
		},
	}
}

// makeGPUDeviceInfo creates a healthy nvidia DeviceInfo with count slots.
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
	}
}

// registerSuperpodNode adds a node carrying the given labels and one healthy
// GPU device with count slots to the scheduler's node snapshot.
func (s *Scheduler) registerSuperpodNode(name string, labels map[string]string, count int64) {
	s.addNode(name, &device.NodeInfo{
		ID:   name,
		Node: makeNodeWithLabels(name, labels),
		Devices: map[string][]device.DeviceInfo{
			nvidia.NvidiaGPUDevice: {makeGPUDeviceInfo(name+"-gpu0", 0, count)},
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
}

// ---------------------------------------------------------------------------
// nodeUsableSlots
// ---------------------------------------------------------------------------

func TestNodeUsableSlots(t *testing.T) {
	t.Run("nil node returns 0", func(t *testing.T) {
		assert.Equal(t, int64(0), nodeUsableSlots(nil))
	})

	t.Run("no devices returns 0", func(t *testing.T) {
		node := &NodeUsage{
			Node: &corev1.Node{},
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{},
			},
		}
		assert.Equal(t, int64(0), nodeUsableSlots(node))
	})

	t.Run("single healthy device", func(t *testing.T) {
		node := &NodeUsage{
			Node: &corev1.Node{},
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{Count: 10, Used: 3, Health: true}},
				},
			},
		}
		assert.Equal(t, int64(7), nodeUsableSlots(node))
	})

	t.Run("unhealthy device excluded", func(t *testing.T) {
		node := &NodeUsage{
			Node: &corev1.Node{},
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{Count: 10, Used: 0, Health: false}},
					{Device: &device.DeviceUsage{Count: 8, Used: 2, Health: true}},
				},
			},
		}
		assert.Equal(t, int64(6), nodeUsableSlots(node))
	})

	t.Run("negative available clamped to 0", func(t *testing.T) {
		node := &NodeUsage{
			Node: &corev1.Node{},
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{Device: &device.DeviceUsage{Count: 5, Used: 10, Health: true}},
					{Device: &device.DeviceUsage{Count: 10, Used: 3, Health: true}},
				},
			},
		}
		assert.Equal(t, int64(7), nodeUsableSlots(node))
	})

	t.Run("nil device entries skipped", func(t *testing.T) {
		node := &NodeUsage{
			Node: &corev1.Node{},
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					{Device: nil},
					{Device: &device.DeviceUsage{Count: 10, Used: 4, Health: true}},
				},
			},
		}
		assert.Equal(t, int64(6), nodeUsableSlots(node))
	})

	t.Run("nil device list entries skipped", func(t *testing.T) {
		node := &NodeUsage{
			Node: &corev1.Node{},
			Devices: policy.DeviceUsageList{
				DeviceLists: []*policy.DeviceListsScore{
					nil,
					{Device: &device.DeviceUsage{Count: 10, Used: 4, Health: true}},
				},
			},
		}
		assert.Equal(t, int64(6), nodeUsableSlots(node))
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
		ctx.totalRequest = 10
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
		ctx.totalRequest = 10
		ctx.superpodCapacities = map[string]int64{"sp-1": 15}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 0, rejected)
		assert.Equal(t, 1, len(nodes))
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
		ctx.totalRequest = 10
		ctx.superpodCapacities = map[string]int64{}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 1, rejected)
		assert.Equal(t, 0, len(nodes))
		assert.Equal(t, common.SuperpodNodeOutside, failed["node-plain"])
	})

	t.Run("unanchored with zero totalRequest keeps all superpod nodes", func(t *testing.T) {
		nodes := map[string]*NodeUsage{
			"node-sp1": makeNodeUsage("node-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 1, 0),
		}
		failed := map[string]string{}
		ctx.Superpods = map[string]struct{}{}
		ctx.TaskAnchored = false
		ctx.podRequest = 1
		ctx.totalRequest = 0
		ctx.superpodCapacities = map[string]int64{"sp-1": 0}

		rejected := (&Scheduler{}).rejectNodesBySuperpod(ctx, task, &nodes, failed)

		assert.Equal(t, 0, rejected)
		assert.Equal(t, 1, len(nodes))
	})
}

// ---------------------------------------------------------------------------
// collectRackSlotCounts
// ---------------------------------------------------------------------------

func TestCollectRackSlotCounts(t *testing.T) {
	ctx := &superpodContext{
		TaskID:         "task-1",
		RackSlotCounts: make(map[string]map[string]int64),
	}

	t.Run("accumulates device slots per rack per superpod", func(t *testing.T) {
		ctx.RackSlotCounts = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsage("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 10, 0),
			"n2": makeNodeUsage("n2", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 10, 0),
			"n3": makeNodeUsage("n3", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-B"}, 10, 0),
			"n4": makeNodeUsage("n4", map[string]string{util.SuperPodIDLabel: "sp-2", util.RackIDLabel: "rack-A"}, 10, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		(&Scheduler{}).collectRackSlotCounts(ctx, task, &nodes, 1)

		// rack-A in sp-1: n1(10) + n2(10) = 20
		assert.Equal(t, int64(20), ctx.RackSlotCounts["sp-1"]["rack-A"])
		// rack-B in sp-1: n3(10) = 10
		assert.Equal(t, int64(10), ctx.RackSlotCounts["sp-1"]["rack-B"])
		// rack-A in sp-2: n4(10) = 10
		assert.Equal(t, int64(10), ctx.RackSlotCounts["sp-2"]["rack-A"])
	})

	t.Run("skips nodes without superpod or rack label", func(t *testing.T) {
		ctx.RackSlotCounts = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsage("n1", map[string]string{util.RackIDLabel: "rack-A"}, 10, 0),
			"n2": makeNodeUsage("n2", map[string]string{util.SuperPodIDLabel: "sp-1"}, 10, 0),
			"n3": makeNodeUsage("n3", map[string]string{}, 10, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		(&Scheduler{}).collectRackSlotCounts(ctx, task, &nodes, 1)

		assert.Equal(t, 0, len(ctx.RackSlotCounts))
	})

	t.Run("skips nodes with insufficient single-pod capacity", func(t *testing.T) {
		ctx.RackSlotCounts = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsage("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 4, 0),
			"n2": makeNodeUsage("n2", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 10, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		// podRequest=8: n1 has only 4 usable (skipped), n2 has 10 usable
		(&Scheduler{}).collectRackSlotCounts(ctx, task, &nodes, 8)

		// Only n2 contributes: 10 slots
		assert.Equal(t, int64(10), ctx.RackSlotCounts["sp-1"]["rack-A"])
	})

	t.Run("zero podRequest counts all nodes", func(t *testing.T) {
		ctx.RackSlotCounts = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"n1": makeNodeUsage("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 0, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		(&Scheduler{}).collectRackSlotCounts(ctx, task, &nodes, 0)

		// count=0, used=0 → 0 usable slots
		assert.Equal(t, int64(0), ctx.RackSlotCounts["sp-1"]["rack-A"])
	})

	t.Run("skips nil node entries", func(t *testing.T) {
		ctx.RackSlotCounts = make(map[string]map[string]int64)
		nodes := map[string]*NodeUsage{
			"nil-node": nil,
			"n1":       makeNodeUsage("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"}, 10, 0),
		}
		task := makeGPUPod("pod", "default", "task-1", 1)

		(&Scheduler{}).collectRackSlotCounts(ctx, task, &nodes, 1)

		// Only n1 contributes: 10 slots
		assert.Equal(t, int64(10), ctx.RackSlotCounts["sp-1"]["rack-A"])
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
		ctx := &superpodContext{
			TaskAnchored:   true,
			Superpods:      map[string]struct{}{"sp-1": {}},
			PreferredRacks: map[string]struct{}{"rack-A": {}},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicySpread.String())
		assert.Equal(t, float32(5.0-superpodRackBoost), score)
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
			RackSlotCounts: map[string]map[string]int64{
				"sp-1": {"rack-A": 30, "rack-B": 10},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// rack-A has 30 slots, the largest rack, boost = superpodRackBoost * 30/30 (full)
		expectedBoost := superpodRackBoost
		assert.Equal(t, float32(5.0+expectedBoost), score)
	})

	t.Run("first pod proportional boost across superpods", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   1,
			RackSlotCounts: map[string]map[string]int64{
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
		// rack-C 20/30 of maxRackSlots
		expectedBoost2 := superpodRackBoost * float32(20) / float32(30)
		assert.Equal(t, float32(5.0+expectedBoost2), score2)
	})

	t.Run("first pod rack with insufficient slots gets no boost", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   2,
			RackSlotCounts: map[string]map[string]int64{
				"sp-1": {"rack-A": 1, "rack-B": 30},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// rack-A has only 1 slot, but podRequest=2, so no boost
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("first pod no rack counts for superpod returns base", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:   false,
			Superpods:      map[string]struct{}{},
			podRequest:     1,
			RackSlotCounts: map[string]map[string]int64{},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("first pod node without superpod label returns base", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored:   false,
			Superpods:      map[string]struct{}{},
			podRequest:     1,
			RackSlotCounts: map[string]map[string]int64{"sp-1": {"rack-A": 20}},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		assert.Equal(t, float32(5.0), score)
	})

	t.Run("first pod spread subtracts proportional boost", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   1,
			RackSlotCounts: map[string]map[string]int64{
				"sp-1": {"rack-A": 20, "rack-B": 20},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicySpread.String())
		// rack-A has 20 slots, the largest rack, boost = superpodRackBoost * 20/20 (full)
		expectedBoost := superpodRackBoost
		assert.Equal(t, float32(5.0-expectedBoost), score)
	})

	t.Run("first pod all slots in same rack gets full boost", func(t *testing.T) {
		ctx := &superpodContext{
			TaskAnchored: false,
			Superpods:    map[string]struct{}{},
			podRequest:   1,
			RackSlotCounts: map[string]map[string]int64{
				"sp-1": {"rack-A": 40},
			},
		}
		node := makeNodeWithLabels("n1", map[string]string{util.SuperPodIDLabel: "sp-1", util.RackIDLabel: "rack-A"})
		score := applySuperpodRackBoost(ctx, node, "n1", 5.0, util.NodeSchedulerPolicyBinpack.String())
		// rack-A has all 40 slots, boost = superpodRackBoost * 40/40 = full
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
		s.kubeClient = fake.NewClientset(rs)
		pod := makeGPUPod("pod", "default", "", 1)
		pod.Labels[util.TaskIDLabel] = "task-1"
		assertTotalRequest(pod, 1, 6)
		s.kubeClient = nil
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
		client := fake.NewClientset(
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

		total, err := resolveTaskIDSuperpodTotal(client, "default", "task-1")
		assert.NilError(t, err)
		assert.Equal(t, int64(6), total)
	})

	t.Run("list error is returned", func(t *testing.T) {
		client := fake.NewClientset()
		client.PrependReactor("list", "replicasets", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("list failed")
		})
		_, err := resolveTaskIDSuperpodTotal(client, "default", "task-1")
		assert.Assert(t, err != nil)
	})

	t.Run("invalid template request is returned", func(t *testing.T) {
		pod := makeGPUPod("template", "default", "", 1)
		pod.Spec.Containers[0].Resources.Limits["hami.io/gpu"] = resource.MustParse("1.5")
		replicas := int32(1)
		client := fake.NewClientset(&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "default", Labels: map[string]string{util.TaskIDLabel: "task-1"}},
			Spec:       appsv1.ReplicaSetSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: pod.Spec}},
		})
		_, err := resolveTaskIDSuperpodTotal(client, "default", "task-1")
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
			wantTotal: 6,
			wantFound: true,
		},
		{
			name: "StatefulSet",
			kind: "StatefulSet",
			object: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
			},
			wantTotal: 6,
			wantFound: true,
		},
		{
			name: "Job",
			kind: "Job",
			object: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "default"},
				Spec:       batchv1.JobSpec{Parallelism: &replicas},
			},
			wantTotal: 6,
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
			s := &Scheduler{kubeClient: fake.NewClientset(objects...)}

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
		s := &Scheduler{kubeClient: fake.NewClientset(job)}
		_, _, err := s.superpodOwnerReplicas(task)
		assert.Assert(t, err != nil)
	})

	t.Run("non-NotFound owner lookup errors propagate", func(t *testing.T) {
		tests := []struct {
			kind     string
			resource string
		}{
			{kind: "ReplicaSet", resource: "replicasets"},
			{kind: "StatefulSet", resource: "statefulsets"},
			{kind: "Job", resource: "jobs"},
		}
		for _, tt := range tests {
			t.Run(tt.kind, func(t *testing.T) {
				getErr := errors.New("API unavailable")
				client := fake.NewClientset()
				client.PrependReactor("get", tt.resource, func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, getErr
				})
				task := makeGPUPod("pod", "default", "", 1)
				task.Labels[util.TaskIDLabel] = "task-1"
				task.OwnerReferences = []metav1.OwnerReference{{Kind: tt.kind, Name: "owner"}}
				s := &Scheduler{kubeClient: client}

				total, found, err := s.superpodOwnerReplicas(task)
				assert.Assert(t, errors.Is(err, getErr))
				assert.Equal(t, int64(0), total)
				assert.Assert(t, !found)

				total, err = s.resolveSuperpodTotalRequest(task, 1)
				assert.Assert(t, errors.Is(err, getErr))
				assert.Equal(t, int64(0), total, "must not fall back to the single-pod request")
			})
		}
	})

	t.Run("NotFound owner lookup remains eligible for fallback", func(t *testing.T) {
		task := makeGPUPod("pod", "default", "", 1)
		task.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: "missing"}}
		s := &Scheduler{kubeClient: fake.NewClientset()}

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
			TaskID:         "task-1",
			TaskAnchored:   false,
			Superpods:      map[string]struct{}{},
			PreferredRacks: map[string]struct{}{},
			RackSlotCounts: make(map[string]map[string]int64),
		}
		// The task declares 10 GPUs in total via the explicit label, and a
		// single pod requests 1 GPU. No kubeClient and no owner refs are
		// needed because the label takes the highest priority.
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "10"

		// sp-1: one node with 5 usable slots → capacity 5 < 10, rejected.
		// sp-2: two nodes with 10 usable slots each → capacity 20 ≥ 10, kept.
		// plain node: no superpod label, rejected (superpod-aware scheduling
		// only considers superpod nodes).
		s.registerSuperpodNode("n-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 5)
		s.registerSuperpodNode("n-sp2a", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10)
		s.registerSuperpodNode("n-sp2b", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10)

		// Candidate nodes mirror the registered snapshot.
		nodes := map[string]*NodeUsage{
			"n-sp1":   makeNodeUsage("n-sp1", map[string]string{util.SuperPodIDLabel: "sp-1"}, 5, 0),
			"n-sp2a":  makeNodeUsage("n-sp2a", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 0),
			"n-sp2b":  makeNodeUsage("n-sp2b", map[string]string{util.SuperPodIDLabel: "sp-2"}, 10, 0),
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
			TaskID:         "task-1",
			TaskAnchored:   false,
			Superpods:      map[string]struct{}{},
			PreferredRacks: map[string]struct{}{},
			RackSlotCounts: make(map[string]map[string]int64),
		}
		pod := makeGPUPod("pod", "default", "task-1", 1)
		pod.Labels[util.SuperpodTotalRequestLabel] = "12"

		// sp-1's capacity is spread across two nodes: only one of them is in
		// the candidate set. The full snapshot must still see both, so sp-1
		// has capacity 6+6=12 ≥ 12 and its candidate node survives.
		s.registerSuperpodNode("n-sp1a", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6)
		s.registerSuperpodNode("n-sp1b", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6)

		nodes := map[string]*NodeUsage{
			"n-sp1a": makeNodeUsage("n-sp1a", map[string]string{util.SuperPodIDLabel: "sp-1"}, 6, 0),
		}
		failed := map[string]string{}

		s.applySuperpodFilter(ctx, pod, &nodes, failed)

		assert.Assert(t, nodes["n-sp1a"] != nil,
			"candidate node should survive when the superpod's full capacity (including non-candidate nodes) fits the task")
		assert.Equal(t, 1, len(nodes))
		assert.Equal(t, 0, len(failed))
	})

	t.Run("empty superpod set returns an empty capacity map", func(t *testing.T) {
		capacities := newSuperpodTestScheduler().collectSuperpodCapacities(nil)
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
		TaskID:         "task-1",
		TaskAnchored:   true,
		Superpods:      map[string]struct{}{"sp-1": {}},
		PreferredRacks: map[string]struct{}{"rack-A": {}},
		RackSlotCounts: make(map[string]map[string]int64),
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
