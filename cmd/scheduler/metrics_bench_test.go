/*
Copyright 2026 The HAMi Authors.

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

package main

import (
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	schedulerpkg "github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
)

func quietKlog(b *testing.B) {
	b.Helper()
	klog.LogToStderr(false)
	klog.SetOutput(io.Discard)
	b.Cleanup(func() {
		klog.SetOutput(os.Stderr)
		klog.LogToStderr(true)
	})
}

// newBenchmarkNodeUsage builds nodeCount nodes carrying gpusPerNode devices
// each, with globally unique UUIDs so every pod device resolves to exactly one
// node.
func newBenchmarkNodeUsage(nodeCount, gpusPerNode int) map[string]*schedulerpkg.NodeUsage {
	nodes := make(map[string]*schedulerpkg.NodeUsage, nodeCount)
	for n := range nodeCount {
		lists := make([]*policy.DeviceListsScore, 0, gpusPerNode)
		for g := range gpusPerNode {
			lists = append(lists, &policy.DeviceListsScore{
				Device: &device.DeviceUsage{
					ID:        benchmarkDeviceUUID(n, g),
					Index:     uint(g),
					Count:     1,
					Usedmem:   4096,
					Totalmem:  8192,
					Totalcore: 100,
					Usedcores: 50,
					Type:      "NVIDIA",
					Mode:      "hami-core",
				},
			})
		}
		nodes[fmt.Sprintf("node-%d", n)] = &schedulerpkg.NodeUsage{
			Devices: policy.DeviceUsageList{DeviceLists: lists},
		}
	}
	return nodes
}

func benchmarkDeviceUUID(node, gpu int) string {
	return fmt.Sprintf("GPU-%d-%d", node, gpu)
}

// newBenchmarkPodManager registers podCount pods, each holding one device drawn
// from across the cluster so the container-level collector performs podCount
// device lookups per scrape.
func newBenchmarkPodManager(podCount, nodeCount, gpusPerNode int) *device.PodManager {
	pm := device.NewPodManager()
	for i := range podCount {
		nodeIdx := i % nodeCount
		gpuIdx := i % gpusPerNode
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%d", i),
				Namespace: "default",
				UID:       k8stypes.UID(fmt.Sprintf("uid-%d", i)),
			},
		}
		podDevices := device.PodDevices{
			"NVIDIA": device.PodSingleDevice{
				device.ContainerDevices{
					{
						UUID:      benchmarkDeviceUUID(nodeIdx, gpuIdx),
						Type:      "NVIDIA",
						Usedmem:   2048,
						Usedcores: 25,
					},
				},
			},
		}
		pm.AddPod(pod, fmt.Sprintf("node-%d", nodeIdx), podDevices)
	}
	return pm
}

// BenchmarkCollect measures a full scrape while holding the allocated-pod count
// fixed and growing the cluster. Resolving each pod's device used to rescan
// every node's device list, so the per-scrape cost grew with nodes x devices
// per node on top of the unavoidable per-node emission work. With the UUID
// index the per-device lookup is constant time; the remaining growth comes from
// node-level emission and the linear index build.
func BenchmarkCollect(b *testing.B) {
	quietKlog(b)

	sizes := []struct {
		nodes       int
		gpusPerNode int
		pods        int
	}{
		{nodes: 10, gpusPerNode: 8, pods: 200},
		{nodes: 100, gpusPerNode: 8, pods: 200},
		{nodes: 1000, gpusPerNode: 8, pods: 200},
	}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("nodes=%d/gpus=%d/pods=%d", size.nodes, size.gpusPerNode, size.pods), func(b *testing.B) {
			nodeUsage := newBenchmarkNodeUsage(size.nodes, size.gpusPerNode)
			cc := ClusterManagerCollector{
				ClusterManager: &ClusterManager{LegacyMetrics: false},
				metricsProvider: &fakeMetricsProvider{
					nodeUsage:    nodeUsage,
					quotaManager: device.NewQuotaManager(),
					podManager:   newBenchmarkPodManager(size.pods, size.nodes, size.gpusPerNode),
				},
			}

			b.ReportAllocs()
			for b.Loop() {
				ch := make(chan prometheus.Metric, 1024)
				done := make(chan struct{})
				go func() {
					for range ch {
					}
					close(done)
				}()
				cc.Collect(ch)
				close(ch)
				<-done
			}
		})
	}
}

// BenchmarkCollectContainerMetrics isolates the container-level collector, the
// path the UUID index changes, holding the allocated-pod count fixed while
// growing the cluster. Node-level emission is excluded here because it must
// grow with the cluster regardless, which masks the lookup cost in a full
// scrape. Resolving a device used to scan every node's device list, so this
// benchmark grew linearly with node count; with the index it is flat. Building
// the index is timed along with the collection it serves, so the figure is the
// net gain rather than only the lookup saving.
func BenchmarkCollectContainerMetrics(b *testing.B) {
	quietKlog(b)

	const pods = 2000

	for _, nodes := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("nodes=%d/gpus=8/pods=%d", nodes, pods), func(b *testing.B) {
			nodeUsage := newBenchmarkNodeUsage(nodes, 8)
			cc := ClusterManagerCollector{
				ClusterManager: &ClusterManager{LegacyMetrics: false},
				metricsProvider: &fakeMetricsProvider{
					nodeUsage:    nodeUsage,
					quotaManager: device.NewQuotaManager(),
					podManager:   newBenchmarkPodManager(pods, nodes, 8),
				},
			}

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				ch := make(chan prometheus.Metric, 1024)
				done := make(chan struct{})
				go func() {
					for range ch {
					}
					close(done)
				}()
				b.StartTimer()

				cc.collectContainerMetrics(ch, newDeviceMetaIndex(&nodeUsage), false)

				b.StopTimer()
				close(ch)
				<-done
				b.StartTimer()
			}
		})
	}
}
