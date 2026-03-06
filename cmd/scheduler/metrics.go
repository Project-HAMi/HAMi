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

	versionmetrics "github.com/Project-HAMi/HAMi/pkg/metrics"
)

type ClusterManager struct {
	Zone          string
	LegacyMetrics bool
}

// ClusterManagerCollector implements the Collector interface.
type ClusterManagerCollector struct {
	ClusterManager *ClusterManager
}

// Describe is implemented with DescribeByCollect. That's possible because the
// Collect method will always return the same metrics with the same descriptors.
func (cc ClusterManagerCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cc, ch)
}

// Collect creates constant metrics for each host on the fly based on the returned data.
func (cc ClusterManagerCollector) Collect(ch chan<- prometheus.Metric) {
	klog.Info("Starting to collect metrics for scheduler")
	legacy := cc.ClusterManager.LegacyMetrics

	// New metric descriptors
	nodevGPUMemoryLimitDesc := prometheus.NewDesc(
		"hami_gpu_memory_limit_bytes",
		"Device memory limit for a certain GPU",
		[]string{"node", "device_uuid", "device_index", "device_type"}, nil,
	)
	nodevGPUCoreLimitDesc := prometheus.NewDesc(
		"hami_gpu_core_limit_ratio",
		"Device core limit for a certain GPU",
		[]string{"node", "device_uuid", "device_index", "device_type"}, nil,
	)
	nodevGPUMemoryAllocatedDesc := prometheus.NewDesc(
		"hami_gpu_memory_allocated_bytes",
		"Device memory allocated for a certain GPU",
		[]string{"node", "device_uuid", "device_index", "device_cores", "device_type"}, nil,
	)
	nodevGPUSharedNumDesc := prometheus.NewDesc(
		"hami_gpu_shared_count",
		"Number of containers sharing this GPU",
		[]string{"node", "device_uuid", "device_index", "device_type"}, nil,
	)
	nodeGPUCoreAllocatedDesc := prometheus.NewDesc(
		"hami_gpu_core_allocated_ratio",
		"Device core allocated for a certain GPU",
		[]string{"node", "device_uuid", "device_index", "device_type"}, nil,
	)
	nodeGPUOverview := prometheus.NewDesc(
		"hami_node_gpu_overview",
		"GPU overview on a certain node",
		[]string{"node", "device_uuid", "device_index", "device_cores", "shared_containers", "device_memory_limit", "device_type"}, nil,
	)
	nodeGPUMemoryPercentage := prometheus.NewDesc(
		"hami_node_gpu_memory_allocated_ratio",
		"GPU Memory Allocated Percentage on a certain GPU",
		[]string{"node", "device_uuid", "device_index"}, nil,
	)
	nodeGPUMigInstance := prometheus.NewDesc(
		"hami_node_gpu_mig_instance_info",
		"GPU Sharing mode. 0 for hami-core, 1 for mig, 2 for mps",
		[]string{"node", "device_uuid", "device_index", "mig_name"}, nil,
	)

	// Legacy metric descriptors (only created when legacy mode is enabled)
	var (
		legacyMemoryLimitDesc     *prometheus.Desc
		legacyCoreLimitDesc       *prometheus.Desc
		legacyMemoryAllocatedDesc *prometheus.Desc
		legacySharedNumDesc       *prometheus.Desc
		legacyCoreAllocatedDesc   *prometheus.Desc
		legacyOverview            *prometheus.Desc
		legacyMemoryPercentage    *prometheus.Desc
		legacyMigInstance         *prometheus.Desc
		legacyAllocatedMemory     *prometheus.Desc
		legacyAllocatedCore       *prometheus.Desc
		legacyQuotaUsed           *prometheus.Desc
	)
	if legacy {
		legacyMemoryLimitDesc = prometheus.NewDesc(
			"GPUDeviceMemoryLimit",
			"Device memory limit for a certain GPU",
			[]string{"nodeid", "deviceuuid", "deviceidx", "devicetype"}, nil,
		)
		legacyCoreLimitDesc = prometheus.NewDesc(
			"GPUDeviceCoreLimit",
			"Device memory core limit for a certain GPU",
			[]string{"nodeid", "deviceuuid", "deviceidx", "devicetype"}, nil,
		)
		legacyMemoryAllocatedDesc = prometheus.NewDesc(
			"GPUDeviceMemoryAllocated",
			"Device memory allocated for a certain GPU",
			[]string{"nodeid", "deviceuuid", "deviceidx", "devicecores", "devicetype"}, nil,
		)
		legacySharedNumDesc = prometheus.NewDesc(
			"GPUDeviceSharedNum",
			"Number of containers sharing this GPU",
			[]string{"nodeid", "deviceuuid", "deviceidx", "devicetype"}, nil,
		)
		legacyCoreAllocatedDesc = prometheus.NewDesc(
			"GPUDeviceCoreAllocated",
			"Device core allocated for a certain GPU",
			[]string{"nodeid", "deviceuuid", "deviceidx", "devicetype"}, nil,
		)
		legacyOverview = prometheus.NewDesc(
			"nodeGPUOverview",
			"GPU overview on a certain node",
			[]string{"nodeid", "deviceuuid", "deviceidx", "devicecores", "sharedcontainers", "devicememorylimit", "devicetype"}, nil,
		)
		legacyMemoryPercentage = prometheus.NewDesc(
			"nodeGPUMemoryPercentage",
			"GPU Memory Allocated Percentage on a certain GPU",
			[]string{"nodeid", "deviceuuid", "deviceidx"}, nil,
		)
		legacyMigInstance = prometheus.NewDesc(
			"nodeGPUMigInstance",
			"GPU Sharing mode. 0 for hami-core, 1 for mig, 2 for mps",
			[]string{"nodeid", "deviceuuid", "deviceidx", "migname"}, nil,
		)
		legacyAllocatedMemory = prometheus.NewDesc(
			"vGPUMemoryAllocated",
			"vGPU memory allocated from a container",
			[]string{"podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
		)
		legacyAllocatedCore = prometheus.NewDesc(
			"vGPUCoreAllocated",
			"vGPU core allocated from a container",
			[]string{"podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
		)
		legacyQuotaUsed = prometheus.NewDesc(
			"QuotaUsed",
			"resourcequota usage for a certain device",
			[]string{"quotanamespace", "quotaName", "limit"}, nil,
		)
	}

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
					if legacy {
						ch <- prometheus.MustNewConstMetric(
							legacyMigInstance,
							prometheus.GaugeValue,
							float64(inuse),
							nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), migs.Name+"-"+fmt.Sprint(idx),
						)
					}
				}
			}

			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryLimitDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Totalmem)*float64(1024)*float64(1024),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUCoreLimitDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Totalcore),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Usedmem)*float64(1024)*float64(1024),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Usedcores), devs.Device.Type,
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUSharedNumDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Used),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
			)
			ch <- prometheus.MustNewConstMetric(
				nodeGPUCoreAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Device.Usedcores),
				nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
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

			if legacy {
				ch <- prometheus.MustNewConstMetric(
					legacyMemoryLimitDesc,
					prometheus.GaugeValue,
					float64(devs.Device.Totalmem)*float64(1024)*float64(1024),
					nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
				)
				ch <- prometheus.MustNewConstMetric(
					legacyCoreLimitDesc,
					prometheus.GaugeValue,
					float64(devs.Device.Totalcore),
					nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
				)
				ch <- prometheus.MustNewConstMetric(
					legacyMemoryAllocatedDesc,
					prometheus.GaugeValue,
					float64(devs.Device.Usedmem)*float64(1024)*float64(1024),
					nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Usedcores), devs.Device.Type,
				)
				ch <- prometheus.MustNewConstMetric(
					legacySharedNumDesc,
					prometheus.GaugeValue,
					float64(devs.Device.Used),
					nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
				)
				ch <- prometheus.MustNewConstMetric(
					legacyCoreAllocatedDesc,
					prometheus.GaugeValue,
					float64(devs.Device.Usedcores),
					nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type,
				)
				ch <- prometheus.MustNewConstMetric(
					legacyOverview,
					prometheus.GaugeValue,
					float64(devs.Device.Usedmem)*float64(1024)*float64(1024),
					nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Usedcores), fmt.Sprint(devs.Device.Used), fmt.Sprint(devs.Device.Totalmem), devs.Device.Type,
				)
				ch <- prometheus.MustNewConstMetric(
					legacyMemoryPercentage,
					prometheus.GaugeValue,
					float64(devs.Device.Usedmem)/float64(devs.Device.Totalmem),
					nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index),
				)
			}
		}
	}

	ctrvGPUdeviceAllocatedMemoryDesc := prometheus.NewDesc(
		"hami_vgpu_memory_allocated_bytes",
		"vGPU memory allocated from a container",
		[]string{"namespace", "node", "pod", "container_index", "device_uuid"}, nil,
	)
	ctrvGPUdeviceAllocatedCoreDesc := prometheus.NewDesc(
		"hami_vgpu_core_allocated_ratio",
		"vGPU core allocated from a container",
		[]string{"namespace", "node", "pod", "container_index", "device_uuid"}, nil,
	)
	quotaUsedDesc := prometheus.NewDesc(
		"hami_resource_quota_used",
		"resourcequota usage for a certain device",
		[]string{"namespace", "quota_name", "limit"}, nil,
	)
	for ns, val := range sher.GetQuotaManager().GetResourceQuota() {
		for quotaname, q := range *val {
			ch <- prometheus.MustNewConstMetric(
				quotaUsedDesc,
				prometheus.GaugeValue,
				float64(q.Used),
				ns, quotaname, fmt.Sprint(q.Limit),
			)
			if legacy {
				ch <- prometheus.MustNewConstMetric(
					legacyQuotaUsed,
					prometheus.GaugeValue,
					float64(q.Used),
					ns, quotaname, fmt.Sprint(q.Limit),
				)
			}
		}
	}
	schedpods, _ := sher.GetPodManager().GetScheduledPods()
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
						ctrvGPUdeviceAllocatedMemoryDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedmem)*float64(1024)*float64(1024),
						val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
					ch <- prometheus.MustNewConstMetric(
						ctrvGPUdeviceAllocatedCoreDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedcores),
						val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
					if legacy {
						ch <- prometheus.MustNewConstMetric(
							legacyAllocatedMemory,
							prometheus.GaugeValue,
							float64(ctrdevval.Usedmem)*float64(1024)*float64(1024),
							val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
						ch <- prometheus.MustNewConstMetric(
							legacyAllocatedCore,
							prometheus.GaugeValue,
							float64(ctrdevval.Usedcores),
							val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
					}
					var totaldev int32
					found := false
					for _, ni := range *nu {
						for _, nodedev := range ni.Devices.DeviceLists {
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
				}
			}
		}
	}
}

// NewClusterManager creates a ClusterManager and registers its collector.
func NewClusterManager(zone string, reg prometheus.Registerer, legacyMetrics bool) *ClusterManager {
	c := &ClusterManager{
		Zone:          zone,
		LegacyMetrics: legacyMetrics,
	}
	cc := ClusterManagerCollector{ClusterManager: c}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}

func initMetrics(bindAddress string, legacyMetrics bool) {
	klog.Info("Initializing metrics for scheduler")
	reg := prometheus.NewRegistry()
	reg.MustRegister(versionmetrics.NewBuildInfoCollector())

	NewClusterManager("vGPU", reg, legacyMetrics)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	log.Fatal(http.ListenAndServe(bindAddress, nil))
}
