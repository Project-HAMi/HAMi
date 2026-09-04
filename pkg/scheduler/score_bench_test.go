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
	"io"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// Benchmarks for the scoring path Filter runs on every scheduling attempt.
// They exist to give a reproducible baseline for changes to scoreNode and
// calcScoreWithOptions, whose cost grows with candidate-node count: scoreNode
// deep-copies the whole NodeUsage once for the app containers and once more
// per non-sidecar init container, and calcScoreWithOptions scores candidates
// concurrently. Allocation counts matter as much as wall time here, so every
// benchmark reports them.
//
// TestMain registers the real NVIDIA backend for this package, so these
// measure the production Fit implementation rather than a stub.

const (
	benchDeviceCount     = 10
	benchDeviceTotalMem  = 8192
	benchDeviceTotalCore = 100
	benchMemreq          = 1024
	benchCoresreq        = 10
)

// quietKlog silences klog for the duration of a benchmark. Fit logs once per
// container request at Info level, so writing those lines to stderr would
// otherwise dominate both the timing and the allocation counts. Uses the same
// SetOutput and restore pattern as routes/route_test.go.
func quietKlog(b *testing.B) {
	b.Helper()
	klog.SetOutput(io.Discard)
	b.Cleanup(func() { klog.SetOutput(os.Stderr) })
}

// newBenchmarkNodes builds nodeCount nodes carrying gpusPerNode idle NVIDIA
// devices each. NodeInfo is populated so scoreNode reads it directly instead
// of reaching for the node lister, which keeps the benchmark to the scoring
// path.
func newBenchmarkNodes(nodeCount, gpusPerNode int) *map[string]*NodeUsage {
	nodes := make(map[string]*NodeUsage, nodeCount)
	for i := range nodeCount {
		nodeName := fmt.Sprintf("node-%d", i)
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
		deviceLists := make([]*policy.DeviceListsScore, 0, gpusPerNode)
		for j := range gpusPerNode {
			dev := makeDevice(
				fmt.Sprintf("%s-gpu-%d", nodeName, j),
				j%2, // spread devices over two NUMA nodes.
				nvidia.NvidiaGPUDevice,
				0, benchDeviceCount, benchDeviceTotalMem, 0, 0, benchDeviceTotalCore,
			)
			deviceLists = append(deviceLists, &policy.DeviceListsScore{Device: dev})
		}
		nodes[nodeName] = &NodeUsage{
			Node:     node,
			NodeInfo: &device.NodeInfo{ID: nodeName, Node: node},
			Devices: policy.DeviceUsageList{
				Policy:      util.GPUSchedulerPolicyBinpack.String(),
				DeviceLists: deviceLists,
			},
		}
	}
	return &nodes
}

// newBenchmarkPod returns a pod with initContainers non-sidecar init
// containers and a single app container. Init containers are left without a
// RestartPolicy so util.IsSidecarContainer treats them as regular init
// containers, which is the shape that makes scoreNode take an extra
// NodeUsage copy per container.
func newBenchmarkPod(initContainers int) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "bench-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app"}},
		},
	}
	for i := range initContainers {
		pod.Spec.InitContainers = append(pod.Spec.InitContainers,
			corev1.Container{Name: fmt.Sprintf("init-%d", i)})
	}
	return pod
}

// newBenchmarkRequests mirrors what device.Resourcereqs produces for a pod
// built by newBenchmarkPod: one entry per init container first, then one per
// app container, each asking for a single NVIDIA device.
func newBenchmarkRequests(initContainers int) device.PodDeviceRequests {
	request := device.ContainerDeviceRequest{
		Nums:     1,
		Type:     nvidia.NvidiaGPUDevice,
		Memreq:   benchMemreq,
		Coresreq: benchCoresreq,
	}
	reqs := make(device.PodDeviceRequests, 0, initContainers+1)
	for range initContainers + 1 {
		reqs = append(reqs, device.ContainerDeviceRequests{nvidia.NvidiaGPUDevice: request})
	}
	return reqs
}

// BenchmarkCalcScore measures the whole candidate-node scoring pass across
// cluster sizes.
//
// The fixture is rebuilt on every iteration because scoring is not read-only:
// applyPeakUsage writes the resulting usage back onto the input NodeUsage, so
// reusing one fixture would accumulate usage until the devices stopped fitting
// and the benchmark quietly switched to measuring the rejection path. The
// rebuild happens with the timer stopped so it is excluded from both the time
// and the allocation figures.
func BenchmarkCalcScore(b *testing.B) {
	sizes := []struct {
		nodes       int
		gpusPerNode int
	}{
		{nodes: 10, gpusPerNode: 4},
		{nodes: 100, gpusPerNode: 8},
		{nodes: 1000, gpusPerNode: 8},
	}

	quietKlog(b)

	scheduler := &Scheduler{}
	pod := newBenchmarkPod(0)
	requests := newBenchmarkRequests(0)

	for _, size := range sizes {
		b.Run(fmt.Sprintf("nodes=%d/gpus=%d", size.nodes, size.gpusPerNode), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				nodes := newBenchmarkNodes(size.nodes, size.gpusPerNode)
				failedNodes := make(map[string]string)
				b.StartTimer()

				if _, err := scheduler.calcScoreWithOptions(nodes, requests, pod, failedNodes, false, false); err != nil {
					b.Fatalf("calcScoreWithOptions returned an error: %v", err)
				}
			}
		})
	}
}

// BenchmarkScoreNode measures a single node in isolation, without the
// concurrent fan-out, while varying the number of non-sidecar init
// containers. Each one costs an additional NodeUsage deep copy, so the
// spread across these cases is the per-init-container scoring cost.
func BenchmarkScoreNode(b *testing.B) {
	quietKlog(b)

	scheduler := &Scheduler{}
	weights := util.DefaultDeviceScoringWeights()
	nodePolicy := util.NodeSchedulerPolicyBinpack.String()

	for _, initContainers := range []int{0, 1, 4} {
		b.Run(fmt.Sprintf("initContainers=%d", initContainers), func(b *testing.B) {
			pod := newBenchmarkPod(initContainers)
			requests := newBenchmarkRequests(initContainers)

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				nodes := *newBenchmarkNodes(1, 8)
				node := nodes["node-0"]
				b.StartTimer()

				result := scheduler.scoreNode("node-0", node, requests, pod, nodePolicy, weights)
				if result.err != nil {
					b.Fatalf("scoreNode returned an error: %v", result.err)
				}
				if result.score == nil {
					b.Fatalf("scoreNode did not fit the pod: %s", result.reason)
				}
			}
		})
	}
}
