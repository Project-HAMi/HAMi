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
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestFilterWorkerCount(t *testing.T) {
	original := config.FilterParallelism
	t.Cleanup(func() { config.FilterParallelism = original })

	gomaxprocs := runtime.GOMAXPROCS(0)

	tests := []struct {
		name        string
		parallelism int
		nodeCount   int
		want        int
	}{
		{
			name:        "explicit parallelism below node count is honoured",
			parallelism: 2,
			nodeCount:   16,
			want:        2,
		},
		{
			name:        "worker count never exceeds the node count",
			parallelism: 64,
			nodeCount:   3,
			want:        3,
		},
		{
			name:        "zero falls back to GOMAXPROCS",
			parallelism: 0,
			nodeCount:   1024,
			want:        gomaxprocs,
		},
		{
			name:        "negative falls back to GOMAXPROCS",
			parallelism: -1,
			nodeCount:   1024,
			want:        gomaxprocs,
		},
		{
			name:        "no candidate nodes needs no workers",
			parallelism: 8,
			nodeCount:   0,
			want:        0,
		},
		{
			name:        "single node stays single worker",
			parallelism: 8,
			nodeCount:   1,
			want:        1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config.FilterParallelism = test.parallelism
			assert.Equal(t, test.want, filterWorkerCount(test.nodeCount))
		})
	}
}

// TestRunNodeWorkersBoundsConcurrency is the regression guard for the bound
// itself. The equivalence test below only proves the results are unchanged,
// which stays true if the worker pool is reverted to one goroutine per node,
// so this asserts the property the pool exists to provide: no more than
// `workers` calls to fn are ever in flight at once.
//
// The upper-bound assertion is timing-independent — a correct pool can never
// exceed the limit. The sleep only widens the window in which an unbounded
// implementation would be caught overlapping.
func TestRunNodeWorkersBoundsConcurrency(t *testing.T) {
	const nodeCount = 32

	nodes := make(map[string]*NodeUsage, nodeCount)
	for i := range nodeCount {
		name := "node" + strconv.Itoa(i)
		nodes[name] = &NodeUsage{
			Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}},
		}
	}

	for _, workers := range []int{1, 2, 4} {
		t.Run("workers="+strconv.Itoa(workers), func(t *testing.T) {
			var inFlight, peak atomic.Int64

			visitMutex := sync.Mutex{}
			visits := map[string]int{}

			runNodeWorkers(nodes, workers, func(nodeID string, node *NodeUsage) {
				current := inFlight.Add(1)
				for {
					observed := peak.Load()
					if current <= observed || peak.CompareAndSwap(observed, current) {
						break
					}
				}

				time.Sleep(2 * time.Millisecond)

				visitMutex.Lock()
				visits[nodeID]++
				visitMutex.Unlock()

				inFlight.Add(-1)
			})

			assert.Assert(t, peak.Load() <= int64(workers),
				"observed %d concurrent calls with a limit of %d", peak.Load(), workers)

			if workers > 1 {
				assert.Assert(t, peak.Load() > 1,
					"expected concurrent execution with a limit of %d, observed %d", workers, peak.Load())
			}

			assert.Equal(t, nodeCount, len(visits))
			for nodeID, count := range visits {
				assert.Equal(t, 1, count, "node %s visited %d times", nodeID, count)
			}
		})
	}
}

// TestRunNodeWorkersNoNodes covers the zero-worker path: filterWorkerCount
// returns 0 for an empty cluster, so no goroutine is started and the send loop
// never runs. It must return rather than deadlock.
func TestRunNodeWorkersNoNodes(t *testing.T) {
	called := atomic.Bool{}
	runNodeWorkers(map[string]*NodeUsage{}, 0, func(string, *NodeUsage) {
		called.Store(true)
	})
	assert.Equal(t, false, called.Load())
}

// parallelismTestContainer builds a container requesting one whole-card slice.
func parallelismTestContainer(name string, memory int64) corev1.Container {
	return corev1.Container{
		Name: name,
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
				"hami.io/gpucores": *resource.NewQuantity(10, resource.BinarySI),
				"hami.io/gpumem":   *resource.NewQuantity(memory, resource.BinarySI),
			},
		},
	}
}

// parallelismTestNodes builds nodeCount nodes carrying gpusPerNode devices each.
// Node "node0" is deliberately given devices too small for the request so the
// fixture exercises the fit path and the rejection path in the same run.
func parallelismTestNodes(nodeCount, gpusPerNode int) *map[string]*NodeUsage {
	nodes := make(map[string]*NodeUsage, nodeCount)
	for i := range nodeCount {
		name := "node" + strconv.Itoa(i)
		k8sNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}

		totalmem := int32(8000)
		if i == 0 {
			totalmem = 10
		}

		lists := make([]*policy.DeviceListsScore, 0, gpusPerNode)
		infos := make([]device.DeviceInfo, 0, gpusPerNode)
		for g := range gpusPerNode {
			id := name + "-gpu" + strconv.Itoa(g)
			lists = append(lists, &policy.DeviceListsScore{
				Device: &device.DeviceUsage{
					ID:        id,
					Index:     uint(g),
					Count:     10,
					Totalmem:  totalmem,
					Totalcore: 100,
					Numa:      g % 2,
					Type:      nvidia.NvidiaGPUDevice,
					Health:    true,
				},
			})
			infos = append(infos, device.DeviceInfo{
				ID:      id,
				Index:   uint(g),
				Count:   10,
				Devmem:  totalmem,
				Devcore: 100,
				Type:    nvidia.NvidiaGPUDevice,
				Health:  true,
			})
		}

		nodes[name] = &NodeUsage{
			Node: k8sNode,
			NodeInfo: &device.NodeInfo{
				ID:      name,
				Node:    k8sNode,
				Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: infos},
			},
			Devices: policy.DeviceUsageList{
				Policy:      util.GPUSchedulerPolicySpread.String(),
				DeviceLists: lists,
			},
		}
	}
	return &nodes
}

// parallelismTestPod builds a pod with initCount init containers and one app
// container, plus the matching per-container device requests.
func parallelismTestPod(initCount int) (*corev1.Pod, device.PodDeviceRequests) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "parallelism", Namespace: "default"},
	}
	for i := range initCount {
		pod.Spec.InitContainers = append(pod.Spec.InitContainers,
			parallelismTestContainer("init"+strconv.Itoa(i), 1000))
	}
	pod.Spec.Containers = append(pod.Spec.Containers,
		parallelismTestContainer("app", 1000))

	reqs := make(device.PodDeviceRequests, 0, initCount+1)
	for range initCount + 1 {
		reqs = append(reqs, device.ContainerDeviceRequests{
			"hami.io/vgpu-devices-to-allocate": device.ContainerDeviceRequest{
				Nums:     1,
				Type:     nvidia.NvidiaGPUDevice,
				Memreq:   1000,
				Coresreq: 10,
			},
		})
	}
	return pod, reqs
}

// scoreOutcome is the order-independent view of one calcScoreWithOptions run.
// NodeList order reflects goroutine completion order, so it is keyed by node
// rather than compared as a slice.
type scoreOutcome struct {
	fit         map[string]device.PodDevices
	scores      map[string]float32
	failedNodes map[string]string
}

func runCalcScore(t *testing.T, parallelism, nodeCount, initCount int) scoreOutcome {
	t.Helper()

	config.FilterParallelism = parallelism
	pod, reqs := parallelismTestPod(initCount)
	nodes := parallelismTestNodes(nodeCount, 4)
	failedNodes := map[string]string{}

	res, err := (&Scheduler{}).calcScoreWithOptions(nodes, reqs, pod, failedNodes, false, false)
	assert.NilError(t, err)

	outcome := scoreOutcome{
		fit:         map[string]device.PodDevices{},
		scores:      map[string]float32{},
		failedNodes: failedNodes,
	}
	for _, node := range res.NodeList {
		outcome.fit[node.NodeID] = node.Devices
		outcome.scores[node.NodeID] = node.Score
	}
	return outcome
}

// TestCalcScoreParallelismEquivalence is the equivalence check promised in
// issue #2858: bounding the worker count changes only how the work is
// scheduled, so every parallelism setting must produce the same fit set,
// the same per-node allocations, the same scores and the same rejections.
// Serial execution (parallelism 1) is the reference.
func TestCalcScoreParallelismEquivalence(t *testing.T) {
	original := config.FilterParallelism
	t.Cleanup(func() { config.FilterParallelism = original })

	const nodeCount = 12

	for _, initCount := range []int{0, 3} {
		t.Run("initContainers="+strconv.Itoa(initCount), func(t *testing.T) {
			reference := runCalcScore(t, 1, nodeCount, initCount)

			// The fixture must exercise both paths, otherwise the comparison
			// below would pass on an empty result.
			assert.Assert(t, len(reference.fit) > 0, "expected at least one fitting node")
			assert.Assert(t, len(reference.failedNodes) > 0, "expected at least one rejected node")

			for _, parallelism := range []int{2, 4, 64, 0, -1} {
				t.Run("parallelism="+strconv.Itoa(parallelism), func(t *testing.T) {
					got := runCalcScore(t, parallelism, nodeCount, initCount)
					assert.DeepEqual(t, reference.fit, got.fit)
					assert.DeepEqual(t, reference.scores, got.scores)
					assert.DeepEqual(t, reference.failedNodes, got.failedNodes)
				})
			}
		})
	}
}

// TestCalcScoreParallelismNoCandidateNodes covers the zero-worker path: with
// no candidate nodes no goroutine is started and the send loop never runs, so
// the function must still return cleanly rather than deadlock.
func TestCalcScoreParallelismNoCandidateNodes(t *testing.T) {
	original := config.FilterParallelism
	t.Cleanup(func() { config.FilterParallelism = original })
	config.FilterParallelism = 8

	pod, reqs := parallelismTestPod(0)
	nodes := map[string]*NodeUsage{}
	failedNodes := map[string]string{}

	res, err := (&Scheduler{}).calcScoreWithOptions(&nodes, reqs, pod, failedNodes, false, false)
	assert.NilError(t, err)
	assert.Equal(t, 0, len(res.NodeList))
	assert.Equal(t, 0, len(failedNodes))
}
