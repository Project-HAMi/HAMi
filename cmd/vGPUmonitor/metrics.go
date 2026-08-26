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
	"os"
	"time"
	"unicode/utf8"

	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

// ClusterManager holds the state the vGPUMonitor's Prometheus collector reads
// from: the pod and container listers used to gather per-container GPU usage,
// and the zone label each ClusterManagerCollector is registered under via a
// wrapping Registerer.
type ClusterManager struct {
	Zone            string
	PodLister       listerscorev1.PodLister
	containerLister *nvidia.ContainerLister
	LegacyMetrics   bool
}

// ClusterManagerCollector implements the Collector interface.
type ClusterManagerCollector struct {
	ClusterManager *ClusterManager
}

// Descriptors used by the ClusterManagerCollector below.
// Metric and label names follow Prometheus naming best practices:
// https://prometheus.io/docs/practices/naming/
var (
	hostGPUdesc = prometheus.NewDesc(
		"hami_host_gpu_memory_used_bytes",
		"GPU device memory usage in bytes",
		[]string{"node", "device_index", "device_uuid", "device_type"}, nil,
	)

	hostGPUUtilizationdesc = prometheus.NewDesc(
		"hami_host_gpu_utilization_ratio",
		"GPU core utilization ratio (0-100)",
		[]string{"node", "device_index", "device_uuid", "device_type"}, nil,
	)

	hostGPUMemoryUtilizationdesc = prometheus.NewDesc(
		"hami_host_gpu_memory_controller_utilization_ratio",
		"GPU memory controller utilization ratio (0-100)",
		[]string{"device_index", "device_uuid", "device_type"}, nil,
	)

	ctrvGPUdesc = prometheus.NewDesc(
		"hami_vgpu_memory_used_bytes",
		"vGPU device memory usage in bytes",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)

	ctrvGPUlimitdesc = prometheus.NewDesc(
		"hami_vgpu_memory_limit_bytes",
		"vGPU device memory limit in bytes",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)
	ctrDeviceMemorydesc = prometheus.NewDesc(
		"hami_container_device_memory_bytes",
		`Container device memory usage in bytes`,
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)
	ctrDeviceUtilizationdesc = prometheus.NewDesc(
		"hami_container_device_utilization_ratio",
		"Container device SM utilization ratio",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)
	ctrDeviceLastKernelDesc = prometheus.NewDesc(
		"hami_container_last_kernel_elapsed_seconds",
		"Seconds since last kernel execution in container",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)
	ctrDeviceMigInfo = prometheus.NewDesc(
		"hami_mig_device_info",
		"MIG runtime identity for a container allocation",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid", "mig_uuid", "profile", "gpu_instance_id", "compute_instance_id"}, nil,
	)
	ctrDeviceMemoryContextDesc = prometheus.NewDesc(
		"hami_vgpu_memory_context_bytes",
		"Container device memory context size in bytes",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)

	ctrDeviceMemoryModuleDesc = prometheus.NewDesc(
		"hami_vgpu_memory_module_bytes",
		"Container device memory module size in bytes",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)

	ctrDeviceMemoryBufferDesc = prometheus.NewDesc(
		"hami_vgpu_memory_buffer_bytes",
		"Container device memory buffer size in bytes",
		[]string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}, nil,
	)
)

// Legacy metric descriptors (populated only when --legacy-metrics is enabled).
var (
	legacyHostGPUdesc              *prometheus.Desc
	legacyHostGPUUtilizationdesc   *prometheus.Desc
	legacyCtrvGPUdesc              *prometheus.Desc
	legacyCtrvGPUlimitdesc         *prometheus.Desc
	legacyCtrDeviceMemorydesc      *prometheus.Desc
	legacyCtrDeviceUtilizationdesc *prometheus.Desc
	legacyCtrDeviceLastKernelDesc  *prometheus.Desc
	legacyCtrDeviceMigInfo         *prometheus.Desc
)

func initLegacyDescriptors() {
	legacyHostGPUdesc = prometheus.NewDesc(
		"HostGPUMemoryUsage",
		"GPU device memory usage",
		[]string{"nodeid", "deviceidx", "deviceuuid", "devicetype"}, nil,
	)
	legacyHostGPUUtilizationdesc = prometheus.NewDesc(
		"HostCoreUtilization",
		"GPU core utilization",
		[]string{"nodeid", "deviceidx", "deviceuuid", "devicetype"}, nil,
	)
	legacyCtrvGPUdesc = prometheus.NewDesc(
		"vGPU_device_memory_usage_in_bytes",
		"vGPU device usage",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
	legacyCtrvGPUlimitdesc = prometheus.NewDesc(
		"vGPU_device_memory_limit_in_bytes",
		"vGPU device limit",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
	legacyCtrDeviceMemorydesc = prometheus.NewDesc(
		"Device_memory_desc_of_container",
		"Container device memory description",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid", "context", "module", "data", "offset"}, nil,
	)
	legacyCtrDeviceUtilizationdesc = prometheus.NewDesc(
		"Device_utilization_desc_of_container",
		"Container device utilization description",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
	legacyCtrDeviceLastKernelDesc = prometheus.NewDesc(
		"Device_last_kernel_of_container",
		"Container device last kernel description",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
	legacyCtrDeviceMigInfo = prometheus.NewDesc(
		"MigInfo",
		"Mig device information for container",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid", "instanceid"}, nil,
	)
}

func sendLegacyMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, valueType prometheus.ValueType, value float64, labels ...string) {
	if desc == nil {
		return
	}
	if err := sendMetric(ch, desc, valueType, value, labels...); err != nil {
		klog.V(4).Infof("Failed to send legacy metric: %v", err)
	}
}

// Describe sends all the metrics descriptors that the collector might use.
// These descriptors are used by the Prometheus registry to register the metrics.
func (cc ClusterManagerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- hostGPUdesc
	ch <- ctrvGPUdesc
	ch <- ctrvGPUlimitdesc
	ch <- hostGPUUtilizationdesc
	ch <- hostGPUMemoryUtilizationdesc
	ch <- ctrDeviceMemorydesc
	ch <- ctrDeviceUtilizationdesc
	ch <- ctrDeviceLastKernelDesc
	ch <- ctrDeviceMigInfo
	ch <- ctrDeviceMemoryContextDesc
	ch <- ctrDeviceMemoryModuleDesc
	ch <- ctrDeviceMemoryBufferDesc

	if cc.ClusterManager.LegacyMetrics {
		ch <- legacyHostGPUdesc
		ch <- legacyHostGPUUtilizationdesc
		ch <- legacyCtrvGPUdesc
		ch <- legacyCtrvGPUlimitdesc
		ch <- legacyCtrDeviceMemorydesc
		ch <- legacyCtrDeviceUtilizationdesc
		ch <- legacyCtrDeviceLastKernelDesc
		ch <- legacyCtrDeviceMigInfo
	}
}

// Collect gathers GPU, pod, and container metrics on each scrape and sends them
// to the provided channel. It may be called concurrently, so the collection
// helpers it calls must be concurrency-safe.
func (cc ClusterManagerCollector) Collect(ch chan<- prometheus.Metric) {
	klog.Info("Starting to collect metrics for vGPUMonitor")

	// Collect GPU information
	if err := cc.collectGPUInfo(ch); err != nil {
		klog.Errorf("Failed to collect GPU info: %v", err)
		// Decide whether to continue or return based on business requirements
	}

	// Collect Pod and Container information
	if err := cc.collectPodAndContainerInfo(ch); err != nil {
		klog.Errorf("Failed to collect Pod and Container info: %v", err)
		// Decide whether to continue or return based on business requirements
	}

	// Collect Pod and Container Mig information
	if err := cc.collectPodAndContainerMigInfo(ch); err != nil {
		klog.Errorf("Failed to collect Pod and Container Mig info: %v", err)
		// Decide whether to continue or return based on business requirements
	}

	klog.Info("Finished collecting metrics for vGPUMonitor")
}

func (cc ClusterManagerCollector) collectGPUInfo(ch chan<- prometheus.Metric) error {
	if err := cc.initNVML(); err != nil {
		return err
	}
	defer nvml.Shutdown()

	devnum, err := cc.getDeviceCount()
	if err != nil {
		return err
	}

	for ii := range devnum {
		if err := cc.collectGPUDeviceMetrics(ch, ii); err != nil {
			klog.Error("Failed to collect metrics for GPU device ", ii, ": ", err)
		}
	}

	return nil
}

func (cc ClusterManagerCollector) initNVML() error {
	nvret := nvml.Init()
	if nvret != nvml.SUCCESS {
		return fmt.Errorf("nvml Init err: %s", nvml.ErrorString(nvret))
	}
	return nil
}

func (cc ClusterManagerCollector) getDeviceCount() (int, error) {
	devnum, nvret := nvml.DeviceGetCount()
	if nvret != nvml.SUCCESS {
		return 0, fmt.Errorf("nvml GetDeviceCount err: %s", nvml.ErrorString(nvret))
	}
	return devnum, nil
}

func (cc ClusterManagerCollector) collectGPUDeviceMetrics(ch chan<- prometheus.Metric, index int) error {
	hdev, nvret := nvml.DeviceGetHandleByIndex(index)
	if nvret != nvml.SUCCESS {
		return fmt.Errorf("nvml DeviceGetHandleByIndex err: %s", nvml.ErrorString(nvret))
	}

	if err := cc.collectGPUMemoryMetrics(ch, hdev, index); err != nil {
		return err
	}

	if err := cc.collectGPUUtilizationMetrics(ch, hdev, index); err != nil {
		return err
	}

	return nil
}

func (cc ClusterManagerCollector) collectGPUMemoryMetrics(ch chan<- prometheus.Metric, hdev nvml.Device, index int) error {
	memory, ret := hdev.GetMemoryInfo()
	if ret == nvml.ERROR_NOT_SUPPORTED {
		klog.V(3).Infof("Memory metrics not supported for device %d (unified memory architecture), skipping", index)
		return nil
	}
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvml get memory error ret=%d", ret)
	}

	uuid, nvret := hdev.GetUUID()
	if nvret != nvml.SUCCESS {
		return fmt.Errorf("nvml GetUUID err: %s", nvml.ErrorString(nvret))
	}

	deviceName, nvret := hdev.GetName()
	if nvret != nvml.SUCCESS {
		return fmt.Errorf("nvml GetName err: %s", nvml.ErrorString(nvret))
	}

	deviceName = "NVIDIA-" + deviceName

	nodeName := os.Getenv(util.NodeNameEnvName)
	if nodeName == "" {
		return fmt.Errorf("node name environment variable %s is not set", util.NodeNameEnvName)
	}

	if err := sendMetric(ch, hostGPUdesc, prometheus.GaugeValue, float64(memory.Used), nodeName, fmt.Sprint(index), uuid, deviceName); err != nil {
		klog.Errorf("Failed to send hostGPUdesc metric: %v", err)
	}

	sendLegacyMetric(ch, legacyHostGPUdesc, prometheus.GaugeValue, float64(memory.Used), nodeName, fmt.Sprint(index), uuid, deviceName)

	return nil
}

func (cc ClusterManagerCollector) collectGPUUtilizationMetrics(ch chan<- prometheus.Metric, hdev nvml.Device, index int) error {
	nodeName := os.Getenv(util.NodeNameEnvName)
	if nodeName == "" {
		return fmt.Errorf("node name environment variable %s is not set", util.NodeNameEnvName)
	}

	utilRates, nvret := hdev.GetUtilizationRates()
	if nvret != nvml.SUCCESS {
		return fmt.Errorf("nvml GetUtilizationRates err: %s", nvml.ErrorString(nvret))
	}

	uuid, nvret := hdev.GetUUID()
	if nvret != nvml.SUCCESS {
		return fmt.Errorf("nvml GetUUID err: %s", nvml.ErrorString(nvret))
	}

	deviceName, nvret := hdev.GetName()
	if nvret != nvml.SUCCESS {
		return fmt.Errorf("nvml GetName err: %s", nvml.ErrorString(nvret))
	}

	deviceName = "NVIDIA-" + deviceName

	if err := sendMetric(ch, hostGPUUtilizationdesc, prometheus.GaugeValue, float64(utilRates.Gpu), nodeName, fmt.Sprint(index), uuid, deviceName); err != nil {
		klog.Errorf("Failed to send hostGPUUtilizationdesc metric: %v", err)
	}

	sendLegacyMetric(ch, legacyHostGPUUtilizationdesc, prometheus.GaugeValue, float64(utilRates.Gpu), nodeName, fmt.Sprint(index), uuid, deviceName)

	if err := sendMetric(ch, hostGPUMemoryUtilizationdesc, prometheus.GaugeValue,
		float64(utilRates.Memory),
		fmt.Sprint(index), uuid, deviceName,
	); err != nil {
		return fmt.Errorf("nvml send memory controller utilization: %w", err)
	}

	return nil
}

func (cc ClusterManagerCollector) collectPodAndContainerInfo(ch chan<- prometheus.Metric) error {
	nodeName := os.Getenv(util.NodeNameEnvName)
	if nodeName == "" {
		return fmt.Errorf("node name environment variable %s is not set", util.NodeNameEnvName)
	}

	pods, err := cc.ClusterManager.PodLister.List(labels.SelectorFromSet(labels.Set{util.AssignedNodeAnnotations: nodeName}))
	if err != nil {
		klog.Errorf("Failed to list pods for node %s: %v", nodeName, err)
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// Update() can run concurrently on its own resync timer and munmap a
	// container's backing memory while removing it from the map. c.Info is
	// backed by that same mmap'd memory, so holding the lister's lock for
	// the whole read-and-scrape below keeps it alive for as long as we
	// dereference it - the same use-after-unmap hazard loadCache's error
	// path guards against, just on the read side instead of the write side.
	cc.ClusterManager.containerLister.Lock()
	defer cc.ClusterManager.containerLister.UnLock()

	containers := cc.ClusterManager.containerLister.ListContainers()
	containerMap := make(map[string][]*nvidia.ContainerUsage) // podUID -> containers
	for _, c := range containers {
		if c.Info != nil && c.PodUID != "" {
			containerMap[c.PodUID] = append(containerMap[c.PodUID], c)
		}
	}

	nowSec := time.Now().Unix()

	// Iterate through each Pod
	for _, pod := range pods {
		podContainers, found := containerMap[string(pod.UID)]
		if !found {
			klog.V(5).Infof("No containers found for pod %s/%s", pod.Namespace, pod.Name)
			continue
		}

		klog.V(5).Infof("Processing Pod %s/%s", pod.Namespace, pod.Name)

		// Iterate through each container in the Pod (both regular and init containers)
		allContainers := make([]corev1.Container, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
		allContainers = append(allContainers, pod.Spec.InitContainers...)
		allContainers = append(allContainers, pod.Spec.Containers...)
		for _, ctr := range allContainers {
			// Find the matching container
			for _, c := range podContainers {
				if c.ContainerName == ctr.Name {
					klog.V(5).Infof("Processing Container %s in Pod %s/%s", ctr.Name, pod.Namespace, pod.Name)
					if err := cc.collectContainerMetrics(ch, pod, ctr, c, nowSec); err != nil {
						klog.Errorf("Failed to collect metrics for container %s in Pod %s/%s: %v", ctr.Name, pod.Namespace, pod.Name, err)
					}
					break // Exit the inner loop after finding the matching container
				}
			}
		}
	}

	klog.V(4).Infof("Finished collecting metrics for %d pods", len(pods))
	return nil
}

func (cc ClusterManagerCollector) collectContainerMetrics(ch chan<- prometheus.Metric, pod *corev1.Pod, ctr corev1.Container, c *nvidia.ContainerUsage, nowSec int64) error {
	// Validate inputs
	if c == nil || c.Info == nil {
		klog.Errorf("Container or ContainerInfo is nil for Pod %s/%s, Container %s", pod.Namespace, pod.Name, ctr.Name)
		return fmt.Errorf("container or container info is nil")
	}

	// Iterate through each device
	for i := range c.Info.DeviceNum() {
		if !c.Info.IsValidUUID(i) {
			klog.Warningf("Device %d in Pod %s/%s, Container %s UUID not yet initialised; skipping until next scrape", i, pod.Namespace, pod.Name, ctr.Name)
			continue
		}
		uuid := c.Info.DeviceUUID(i)[0:40]
		if !utf8.ValidString(uuid) {
			klog.Warningf("Device %d in Pod %s/%s, Container %s has invalid UTF-8 UUID; skipping until next scrape", i, pod.Namespace, pod.Name, ctr.Name)
			continue
		}

		// Collect device metrics
		memoryTotal := c.Info.DeviceMemoryTotal(i)
		memoryLimit := c.Info.DeviceMemoryLimit(i)
		memoryContextSize := c.Info.DeviceMemoryContextSize(i)
		memoryModuleSize := c.Info.DeviceMemoryModuleSize(i)
		memoryBufferSize := c.Info.DeviceMemoryBufferSize(i)
		smUtil := c.Info.DeviceSmUtil(i)
		lastKernelTime := c.Info.LastKernelTime()

		labels := []string{pod.Namespace, pod.Name, ctr.Name, fmt.Sprint(i), uuid}

		if err := sendMetric(ch, ctrvGPUdesc, prometheus.GaugeValue, float64(memoryTotal), labels...); err != nil {
			klog.Errorf("Failed to send memoryTotal metric: %v", err)
			return err
		}
		sendLegacyMetric(ch, legacyCtrvGPUdesc, prometheus.GaugeValue, float64(memoryTotal), labels...)

		if err := sendMetric(ch, ctrvGPUlimitdesc, prometheus.GaugeValue, float64(memoryLimit), labels...); err != nil {
			klog.Errorf("Failed to send memoryLimit metric: %v", err)
			return err
		}
		sendLegacyMetric(ch, legacyCtrvGPUlimitdesc, prometheus.GaugeValue, float64(memoryLimit), labels...)

		if err := sendMetric(ch, ctrDeviceMemorydesc, prometheus.GaugeValue, float64(memoryTotal), labels...); err != nil {
			klog.Errorf("Failed to send device memory desc: %v", err)
			return err
		}
		memoryOffset := memoryTotal - memoryContextSize - memoryModuleSize - memoryBufferSize
		memoryLabels := append(labels, fmt.Sprint(memoryContextSize), fmt.Sprint(memoryModuleSize), fmt.Sprint(memoryBufferSize), fmt.Sprint(memoryOffset))
		sendLegacyMetric(ch, legacyCtrDeviceMemorydesc, prometheus.GaugeValue, float64(memoryTotal), memoryLabels...)

		if err := sendMetric(ch, ctrDeviceUtilizationdesc, prometheus.GaugeValue, float64(smUtil), labels...); err != nil {
			klog.Errorf("Failed to send device utilization desc: %v", err)
			return err
		}
		sendLegacyMetric(ch, legacyCtrDeviceUtilizationdesc, prometheus.GaugeValue, float64(smUtil), labels...)

		if err := sendMetric(ch, ctrDeviceMemoryContextDesc, prometheus.GaugeValue, float64(memoryContextSize), labels...); err != nil {
			klog.Errorf("Failed to send Device Memory context size metric: %v", err)
			return err
		}
		if err := sendMetric(ch, ctrDeviceMemoryModuleDesc, prometheus.GaugeValue, float64(memoryModuleSize), labels...); err != nil {
			klog.Errorf("Failed to send Device Memory module size metric: %v", err)
			return err
		}
		if err := sendMetric(ch, ctrDeviceMemoryBufferDesc, prometheus.GaugeValue, float64(memoryBufferSize), labels...); err != nil {
			klog.Errorf("Failed to send Device Memory buffer size metric: %v", err)
			return err
		}

		if lastKernelTime > 0 {
			lastSec := max(nowSec-lastKernelTime, 0)
			if err := sendMetric(ch, ctrDeviceLastKernelDesc, prometheus.GaugeValue, float64(lastSec), labels...); err != nil {
				klog.Errorf("Failed to send last kernel time metric: %v", err)
				return err
			}
			sendLegacyMetric(ch, legacyCtrDeviceLastKernelDesc, prometheus.GaugeValue, float64(lastSec), labels...)
		}
	}

	klog.V(5).Infof("Successfully collected metrics for Pod %s/%s, Container %s", pod.Namespace, pod.Name, ctr.Name)
	return nil
}

func (cc ClusterManagerCollector) collectPodAndContainerMigInfo(ch chan<- prometheus.Metric) error {
	nodeName := os.Getenv(util.NodeNameEnvName)
	if nodeName == "" {
		return fmt.Errorf("node name environment variable %s is not set", util.NodeNameEnvName)
	}

	pods, err := cc.ClusterManager.PodLister.List(labels.SelectorFromSet(labels.Set{util.AssignedNodeAnnotations: nodeName}))
	if err != nil {
		klog.Errorf("Failed to list pods for node %s: %v", nodeName, err)
		return fmt.Errorf("failed to list pods: %w", err)
	}
	for _, pod := range pods {
		allocations, err := nv.DecodeMigAllocations(pod.Annotations[nv.MigAllocationsAnnotation])
		if err != nil {
			klog.Errorf("failed to decode MIG allocations for pod %s/%s: %v", pod.Namespace, pod.Name, err)
			continue
		}
		for _, allocation := range allocations {
			if allocation.MigUUID == "" || allocation.GPUInstanceID == nil || allocation.ComputeInstanceID == nil {
				continue
			}
			containerName, ok := migAllocationContainerName(pod, allocation.ContainerIndex)
			if !ok {
				klog.Errorf("MIG allocation container index %d out of range for Pod %s/%s", allocation.ContainerIndex, pod.Namespace, pod.Name)
				continue
			}
			metricLabels := []string{
				pod.Namespace,
				pod.Name,
				containerName,
				fmt.Sprint(allocation.DeviceIndex),
				allocation.GPUUUID,
				allocation.MigUUID,
				allocation.Profile,
				fmt.Sprint(*allocation.GPUInstanceID),
				fmt.Sprint(*allocation.ComputeInstanceID),
			}
			if err := sendMetric(ch, ctrDeviceMigInfo, prometheus.GaugeValue, 1, metricLabels...); err != nil {
				return fmt.Errorf("send MIG info metric for pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
			sendLegacyMetric(ch, legacyCtrDeviceMigInfo, prometheus.GaugeValue, 1,
				pod.Namespace, pod.Name, containerName, fmt.Sprint(allocation.DeviceIndex), allocation.GPUUUID, fmt.Sprint(*allocation.GPUInstanceID))
		}
	}
	return nil
}

func migAllocationContainerName(pod *corev1.Pod, containerIndex int) (string, bool) {
	if containerIndex < 0 {
		return "", false
	}
	if containerIndex < len(pod.Spec.InitContainers) {
		return pod.Spec.InitContainers[containerIndex].Name, true
	}
	containerIndex -= len(pod.Spec.InitContainers)
	if containerIndex >= len(pod.Spec.Containers) {
		return "", false
	}
	return pod.Spec.Containers[containerIndex].Name, true
}

func sendMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, valueType prometheus.ValueType, value float64, labels ...string) error {
	metric, err := prometheus.NewConstMetric(desc, valueType, value, labels...)
	if err != nil {
		return fmt.Errorf("failed to create metric: %w", err)
	}
	ch <- metric
	return nil
}

// NewClusterManager creates a ClusterManager for the given zone, backs its pod
// lookups with a shared informer, and registers its collector with reg through
// a wrapping Registerer that adds the zone as a label.
func NewClusterManager(zone string, reg prometheus.Registerer, containerLister *nvidia.ContainerLister, legacyMetrics bool) *ClusterManager {
	if legacyMetrics {
		initLegacyDescriptors()
	}
	c := &ClusterManager{
		Zone:            zone,
		containerLister: containerLister,
		LegacyMetrics:   legacyMetrics,
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(containerLister.Clientset(), time.Hour*1)
	c.PodLister = informerFactory.Core().V1().Pods().Lister()
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)

	cc := ClusterManagerCollector{ClusterManager: c}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}
