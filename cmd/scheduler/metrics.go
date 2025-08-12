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

package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	klog "k8s.io/klog/v2"
)

// ClusterManager is an example for a system that might have been built without
// Prometheus in mind. It models a central manager of jobs running in a
// cluster. Thus, we implement a custom Collector called
// ClusterManagerCollector, which collects information from a ClusterManager
// using its provided methods and turns them into Prometheus Metrics for
// collection.
//
// An additional challenge is that multiple instances of the ClusterManager are
// run within the same binary, each in charge of a different zone. We need to
// make use of wrapping Registerers to be able to register each
// ClusterManagerCollector instance with Prometheus.
type ClusterManager struct {
	Zone string
	// Contains many more fields not listed in this example.
}

// ClusterManagerCollector implements the Collector interface.
type ClusterManagerCollector struct {
	ClusterManager *ClusterManager
}

// Describe is implemented with DescribeByCollect. That's possible because the
// Collect method will always return the same two metrics with the same two
// descriptors.
func (cc ClusterManagerCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cc, ch)
}

// Collect first triggers the ReallyExpensiveAssessmentOfTheSystemState. Then it
// creates constant metrics for each host on the fly based on the returned data.
//
// Note that Collect could be called concurrently, so we depend on
// ReallyExpensiveAssessmentOfTheSystemState to be concurrency-safe.
func (cc ClusterManagerCollector) Collect(ch chan<- prometheus.Metric) {
	klog.Info("Starting to collect metrics for scheduler")
	nodevGPUMemoryLimitDesc := prometheus.NewDesc(
		"GPUDeviceMemoryLimit",
		"Device memory limit for a certain GPU",
		[]string{"nodeid", "deviceuuid", "deviceidx"}, nil,
	)
	nodevGPUCoreLimitDesc := prometheus.NewDesc(
		"GPUDeviceCoreLimit",
		"Device memory core limit for a certain GPU",
		[]string{"nodeid", "deviceuuid", "deviceidx"}, nil,
	)
	nodevGPUMemoryAllocatedDesc := prometheus.NewDesc(
		"GPUDeviceMemoryAllocated",
		"Device memory allocated for a certain GPU",
		[]string{"nodeid", "deviceuuid", "deviceidx", "devicecores"}, nil,
	)
	nodevGPUSharedNumDesc := prometheus.NewDesc(
		"GPUDeviceSharedNum",
		"Number of containers sharing this GPU",
		[]string{"nodeid", "deviceuuid", "deviceidx"}, nil,
	)

	nodeGPUCoreAllocatedDesc := prometheus.NewDesc(
		"GPUDeviceCoreAllocated",
		"Device core allocated for a certain GPU",
		[]string{"nodeid", "deviceuuid", "deviceidx"}, nil,
	)
	nodeGPUOverview := prometheus.NewDesc(
		"nodeGPUOverview",
		"GPU overview on a certain node",
		[]string{"nodeid", "deviceuuid", "deviceidx", "devicecores", "sharedcontainers", "devicememorylimit", "devicetype"}, nil,
	)
	nodeGPUMemoryPercentage := prometheus.NewDesc(
		"nodeGPUMemoryPercentage",
		"GPU Memory Allocated Percentage on a certain GPU",
		[]string{"nodeid", "deviceuuid", "deviceidx"}, nil,
	)
	nodeGPUMigInstance := prometheus.NewDesc(
		"nodeGPUMigInstance",
		"GPU Sharing mode. 0 for hami-core, 1 for mig, 2 for mps",
		[]string{"nodeid", "deviceuuid", "deviceidx", "migname"}, nil,
	)
	nu := sher.InspectAllNodesUsage()
	for nodeID, val := range *nu {
		for _, devs := range val.Devices.DeviceLists {
			if devs.Device.Mode == "mig" {
				for idx, migs := range devs.Device.MigUsage.UsageList {
					klog.Infoln("mig instances=", devs.Device.MigUsage)
					inuse := 0
					if migs.InUse {
						inuse = 1
					}
					ch <- prometheus.MustNewConstMetric(
						nodeGPUMigInstance,
						prometheus.GaugeValue,
						float64(inuse),
						nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), migs.Name+"-"+fmt.Sprint(idx),
					)
				}
			}

			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryLimitDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Totalmem)*float64(1024)*float64(1024),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index),
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUCoreLimitDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Totalcore),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index),
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Usedmem)*float64(1024)*float64(1024),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Usedcores),
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUSharedNumDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Used),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index),
			)

			ch <- prometheus.MustNewConstMetric(
				nodeGPUCoreAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Usedcores),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index),
			)
			ch <- prometheus.MustNewConstMetric(
				nodeGPUOverview,
				prometheus.GaugeValue,
				float64(devs.Device.Usedmem)*float64(1024)*float64(1024),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Usedcores), fmt.Sprint(devs.Device.Used), fmt.Sprint(devs.Device.Totalmem), devs.Device.Type,
			)
			ch <- prometheus.MustNewConstMetric(
				nodeGPUMemoryPercentage,
				prometheus.GaugeValue,
				float64(devs.Device.Usedmem)/float64(devs.Device.Totalmem),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index),
			)
		}
	}

	ctrvGPUDeviceAllocatedDesc := prometheus.NewDesc(
		"vGPUPodsDeviceAllocated",
		"vGPU Allocated from pods (This metric will be deprecated in v2.8.0, use vGPUMemoryAllocated and vGPUCoreAllocated instead.)",
		[]string{"deprecated_version", "podnamespace", "nodename", "podname", "containeridx", "deviceuuid", "deviceusedcore"}, nil,
	)
	ctrvGPUdeviceAllocatedMemoryPercentageDesc := prometheus.NewDesc(
		"vGPUMemoryPercentage",
		"vGPU memory percentage allocated from a container (This metric will be deprecated in v2.8.0, use vGPUMemoryAllocated instead.)",
		[]string{"deprecated_version", "podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	ctrvGPUdeviceAllocateCorePercentageDesc := prometheus.NewDesc(
		"vGPUCorePercentage",
		"vGPU core allocated from a container (This metric will be deprecated in v2.8.0, use vGPUCoreAllocated instead.)",
		[]string{"deprecated_version", "podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	ctrvGPUdeviceAllocatedMemoryDesc := prometheus.NewDesc(
		"vGPUMemoryAllocated",
		"vGPU memory allocated from a container",
		[]string{"podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	ctrvGPUdeviceAllocatedCoreDesc := prometheus.NewDesc(
		"vGPUCoreAllocated",
		"vGPU core allocated from a container",
		[]string{"podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	schedpods, _ := sher.GetScheduledPods()
	for _, val := range schedpods {
		for _, podSingleDevice := range val.Devices {
			for ctridx, ctrdevs := range podSingleDevice {
				for _, ctrdevval := range ctrdevs {
					klog.V(4).InfoS("Collecting metrics",
						"namespace", val.Namespace,
						"podName", val.Name,
						"deviceUUID", ctrdevval.UUID,
						"usedCores", ctrdevval.Usedcores,
						"usedMem", ctrdevval.Usedmem,
						"nodeID", val.NodeID,
					)
					if len(ctrdevval.UUID) == 0 {
						klog.Warningf("Device UUID is empty, omitting metric collection for namespace=%s, podName=%s, ctridx=%d, nodeID=%s",
							val.Namespace, val.Name, ctridx, val.NodeID)
						continue
					}
					ch <- prometheus.MustNewConstMetric(
						ctrvGPUDeviceAllocatedDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedmem)*float64(1024)*float64(1024),
						"v2.8.0", val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID, fmt.Sprint(ctrdevval.Usedcores))
					ch <- prometheus.MustNewConstMetric(
						ctrvGPUdeviceAllocatedMemoryDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedmem)*float64(1024)*float64(1024),
						val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
					ch <- prometheus.MustNewConstMetric(
						ctrvGPUdeviceAllocatedCoreDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedcores),
						val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
					var totaldev int32
					found := false
					for _, ni := range *nu {
						for _, nodedev := range ni.Devices.DeviceLists {
							//fmt.Println("uuid=", nodedev.ID, ctrdevval.UUID)
							if strings.Compare(nodedev.Device.ID, ctrdevval.UUID) == 0 {
								totaldev = nodedev.Device.Totalmem
								found = true
								break
							}
						}
						if found {
							break
						}
					}
					klog.V(4).InfoS("Total memory for device",
						"deviceUUID", ctrdevval.UUID,
						"totalMemory", totaldev,
						"nodeID", val.NodeID,
					)
					if totaldev > 0 {
						ch <- prometheus.MustNewConstMetric(
							ctrvGPUdeviceAllocatedMemoryPercentageDesc,
							prometheus.GaugeValue,
							float64(ctrdevval.Usedmem)/float64(totaldev),
							"v2.8.0", val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
					}
					ch <- prometheus.MustNewConstMetric(
						ctrvGPUdeviceAllocateCorePercentageDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedcores),
						"v2.8.0", val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
				}
			}
		}
	}
}

// NewClusterManager first creates a Prometheus-ignorant ClusterManager
// instance. Then, it creates a ClusterManagerCollector for the just created
// ClusterManager. Finally, it registers the ClusterManagerCollector with a
// wrapping Registerer that adds the zone as a label. In this way, the metrics
// collected by different ClusterManagerCollectors do not collide.
func NewClusterManager(zone string, reg prometheus.Registerer) *ClusterManager {
	c := &ClusterManager{
		Zone: zone,
	}
	cc := ClusterManagerCollector{ClusterManager: c}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}

func initMetrics(bindAddress string) {
	// Since we are dealing with custom Collector implementations, it might
	// be a good idea to try it out with a pedantic registry.
	klog.Info("Initializing metrics for scheduler")
	reg := prometheus.NewRegistry()

	// Construct cluster managers. In real code, we would assign them to
	// variables to then do something with them.
	NewClusterManager("vGPU", reg)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	log.Fatal(http.ListenAndServe(bindAddress, nil))
}
