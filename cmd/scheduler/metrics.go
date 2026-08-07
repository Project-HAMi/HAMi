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
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	klog "k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	versionmetrics "github.com/Project-HAMi/HAMi/pkg/metrics"
	schedulerpkg "github.com/Project-HAMi/HAMi/pkg/scheduler"
)

type ClusterManager struct {
	Zone          string
	LegacyMetrics bool
}

type schedulerMetricsProvider interface {
	InspectAllNodesUsage() *map[string]*schedulerpkg.NodeUsage
	GetQuotaManager() *device.QuotaManager
	GetPodManager() *device.PodManager
}

// ClusterManagerCollector implements the Collector interface.
type ClusterManagerCollector struct {
	ClusterManager  *ClusterManager
	metricsProvider schedulerMetricsProvider
}

const normalizedCoreLimit = 100

// normalizeAMDCoreMetrics converts AMD physical CU counts to the percentage
// unit used by HAMi's core ratio metrics. Other devices keep their existing
// metric values.
func normalizeAMDCoreMetrics(deviceType string, total, allocated int32) (float64, float64) {
	if !strings.HasPrefix(strings.ToUpper(deviceType), "AMD") || total <= 0 {
		return float64(total), float64(allocated)
	}
	return normalizedCoreLimit, math.Ceil(float64(allocated) / float64(total) * normalizedCoreLimit)
}

// Describe is implemented with DescribeByCollect. That's possible because the
// Collect method will always return the same metrics with the same descriptors.
func (cc ClusterManagerCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cc, ch)
}

// Collect creates constant metrics for each host on the fly based on the returned data.
func (cc ClusterManagerCollector) Collect(ch chan<- prometheus.Metric) {
	klog.V(3).Info("Starting to collect metrics for scheduler")
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
		[]string{"node", "device_uuid", "device_index", "device_cores", "device_memory_limit", "device_type"}, nil,
	)
	nodeGPUMemoryPercentage := prometheus.NewDesc(
		"hami_node_gpu_memory_allocated_ratio",
		"GPU Memory Allocated Percentage on a certain GPU",
		[]string{"node", "device_uuid", "device_index"}, nil,
	)
	nodeGPUMigInstance := prometheus.NewDesc(
		"hami_node_gpu_mig_instance_info",
		"Realized MIG instance identity and scheduler placement",
		[]string{"node", "device_uuid", "device_index", "mig_uuid", "profile", "gpu_instance_id", "compute_instance_id", "placement_start", "placement_size"}, nil,
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
			[]string{"nodeid", "deviceuuid", "deviceidx", "devicecores", "devicememorylimit", "devicetype"}, nil,
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

	nu := cc.metricsProvider.InspectAllNodesUsage()
	for nodeID, val := range *nu {
		for _, devs := range val.Devices.DeviceLists {
			coreLimit, coreAllocated := normalizeAMDCoreMetrics(devs.Device.Type, devs.Device.Totalcore, devs.Device.Usedcores)
			if devs.Device.Mode == "mig" {
				for _, allocation := range devs.Device.MigAllocationsInUse {
					if !allocation.RuntimeReady {
						continue
					}
					klog.V(3).InfoS("MIG instance allocation",
						"profile", allocation.Profile,
						"gpuInstanceID", allocation.GPUInstanceID,
						"computeInstanceID", allocation.ComputeInstanceID,
						"migUUID", allocation.MigUUID)
					if err := sendMetric(
						ch,
						nodeGPUMigInstance,
						prometheus.GaugeValue,
						1,
						nodeID,
						devs.Device.ID,
						fmt.Sprint(devs.Device.Index),
						allocation.MigUUID,
						allocation.Profile,
						fmt.Sprint(allocation.GPUInstanceID),
						fmt.Sprint(allocation.ComputeInstanceID),
						fmt.Sprint(allocation.Placement.Start),
						fmt.Sprint(allocation.Placement.Size),
					); err != nil {
						klog.V(4).Infof("Failed to send nodeGPUMigInstance metric: %v", err)
					}
					if legacy {
						sendLegacyMetric(
							ch,
							legacyMigInstance,
							prometheus.GaugeValue,
							1,
							nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), allocation.Profile+"-"+fmt.Sprint(allocation.GPUInstanceID),
						)
					}
				}
			}

			if err := sendMetric(ch, nodevGPUMemoryLimitDesc, prometheus.GaugeValue, float64(devs.Device.Totalmem)*float64(1024)*float64(1024), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type); err != nil {
				klog.V(4).Infof("Failed to send nodevGPUMemoryLimitDesc metric: %v", err)
			}
			if err := sendMetric(ch, nodevGPUCoreLimitDesc, prometheus.GaugeValue, coreLimit, nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type); err != nil {
				klog.V(4).Infof("Failed to send nodevGPUCoreLimitDesc metric: %v", err)
			}
			if err := sendMetric(ch, nodevGPUMemoryAllocatedDesc, prometheus.GaugeValue, float64(devs.Device.Usedmem)*float64(1024)*float64(1024), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Totalcore), devs.Device.Type); err != nil {
				klog.V(4).Infof("Failed to send nodevGPUMemoryAllocatedDesc metric: %v", err)
			}
			if err := sendMetric(ch, nodevGPUSharedNumDesc, prometheus.GaugeValue, float64(devs.Device.Used), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type); err != nil {
				klog.V(4).Infof("Failed to send nodevGPUSharedNumDesc metric: %v", err)
			}
			if err := sendMetric(ch, nodeGPUCoreAllocatedDesc, prometheus.GaugeValue, coreAllocated, nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type); err != nil {
				klog.V(4).Infof("Failed to send nodeGPUCoreAllocatedDesc metric: %v", err)
			}
			if err := sendMetric(ch, nodeGPUOverview, prometheus.GaugeValue, float64(devs.Device.Usedmem)*float64(1024)*float64(1024), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Totalcore), fmt.Sprint(devs.Device.Totalmem), devs.Device.Type); err != nil {
				klog.V(4).Infof("Failed to send nodeGPUOverview metric: %v", err)
			}

			if devs.Device.Totalmem > 0 {
				if err := sendMetric(ch, nodeGPUMemoryPercentage, prometheus.GaugeValue, float64(devs.Device.Usedmem)/float64(devs.Device.Totalmem), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index)); err != nil {
					klog.V(4).Infof("Failed to send nodeGPUMemoryPercentage metric: %v", err)
				}
			}

			if legacy {
				sendLegacyMetric(ch, legacyMemoryLimitDesc, prometheus.GaugeValue, float64(devs.Device.Totalmem)*float64(1024)*float64(1024), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type)
				sendLegacyMetric(ch, legacyCoreLimitDesc, prometheus.GaugeValue, float64(devs.Device.Totalcore), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type)
				sendLegacyMetric(ch, legacyMemoryAllocatedDesc, prometheus.GaugeValue, float64(devs.Device.Usedmem)*float64(1024)*float64(1024), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Totalcore), devs.Device.Type)
				sendLegacyMetric(ch, legacySharedNumDesc, prometheus.GaugeValue, float64(devs.Device.Used), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type)
				sendLegacyMetric(ch, legacyCoreAllocatedDesc, prometheus.GaugeValue, float64(devs.Device.Usedcores), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), devs.Device.Type)
				sendLegacyMetric(ch, legacyOverview, prometheus.GaugeValue, float64(devs.Device.Usedmem)*float64(1024)*float64(1024), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index), fmt.Sprint(devs.Device.Totalcore), fmt.Sprint(devs.Device.Totalmem), devs.Device.Type)
				if devs.Device.Totalmem > 0 {
					sendLegacyMetric(ch, legacyMemoryPercentage, prometheus.GaugeValue, float64(devs.Device.Usedmem)/float64(devs.Device.Totalmem), nodeID, devs.Device.ID, fmt.Sprint(devs.Device.Index))
				}
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
	for ns, val := range cc.metricsProvider.GetQuotaManager().GetResourceQuota() {
		for quotaname, q := range *val {
			if err := sendMetric(ch, quotaUsedDesc, prometheus.GaugeValue, float64(q.Used), ns, quotaname, fmt.Sprint(q.Limit)); err != nil {
				klog.V(4).Infof("Failed to send quotaUsedDesc metric: %v", err)
			}
			if legacy {
				sendLegacyMetric(ch, legacyQuotaUsed, prometheus.GaugeValue, float64(q.Used), ns, quotaname, fmt.Sprint(q.Limit))
			}
		}
	}
	schedpods, _ := cc.metricsProvider.GetPodManager().GetScheduledPods()
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
					if err := sendMetric(ch, ctrvGPUdeviceAllocatedMemoryDesc, prometheus.GaugeValue, float64(ctrdevval.Usedmem)*float64(1024)*float64(1024), val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID); err != nil {
						klog.V(4).Infof("Failed to send ctrvGPUdeviceAllocatedMemoryDesc metric: %v", err)
					}
					if err := sendMetric(ch, ctrvGPUdeviceAllocatedCoreDesc, prometheus.GaugeValue, float64(ctrdevval.Usedcores), val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID); err != nil {
						klog.V(4).Infof("Failed to send ctrvGPUdeviceAllocatedCoreDesc metric: %v", err)
					}
					if legacy {
						sendLegacyMetric(ch, legacyAllocatedMemory, prometheus.GaugeValue, float64(ctrdevval.Usedmem)*float64(1024)*float64(1024), val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
						sendLegacyMetric(ch, legacyAllocatedCore, prometheus.GaugeValue, float64(ctrdevval.Usedcores), val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
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
func NewClusterManager(zone string, reg prometheus.Registerer, metricsProvider schedulerMetricsProvider, legacyMetrics bool) *ClusterManager {
	c := &ClusterManager{
		Zone:          zone,
		LegacyMetrics: legacyMetrics,
	}
	cc := ClusterManagerCollector{
		ClusterManager:  c,
		metricsProvider: metricsProvider,
	}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}

func initMetrics(bindAddress string, metricsProvider schedulerMetricsProvider, legacyMetrics bool) {
	klog.Info("Initializing metrics for scheduler")
	reg := prometheus.NewRegistry()
	reg.MustRegister(versionmetrics.NewBuildInfoCollector())

	NewClusterManager("vGPU", reg, metricsProvider, legacyMetrics)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	server := &http.Server{
		Addr:              bindAddress,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func sendLegacyMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, valueType prometheus.ValueType, value float64, labels ...string) {
	if desc == nil {
		return
	}
	if err := sendMetric(ch, desc, valueType, value, labels...); err != nil {
		klog.V(4).Infof("Failed to send legacy metric: %v", err)
	}
}

func sendMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, valueType prometheus.ValueType, value float64, labels ...string) error {
	metric, err := prometheus.NewConstMetric(desc, valueType, value, labels...)
	if err != nil {
		return fmt.Errorf("failed to create metric: %w", err)
	}
	ch <- metric
	return nil
}
