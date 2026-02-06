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
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	dp "github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
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
	PodLister       listerscorev1.PodLister
	containerLister *nvidia.ContainerLister
}

// ReallyExpensiveAssessmentOfTheSystemState is a mock for the data gathering a
// real cluster manager would have to do. Since it may actually be really
// expensive, it must only be called once per collection. This implementation,
// obviously, only returns some made-up data.
func (c *ClusterManager) ReallyExpensiveAssessmentOfTheSystemState() (
	oomCountByHost map[string]int, ramUsageByHost map[string]float64,
) {
	// Just example fake data.
	oomCountByHost = map[string]int{
		"foo.example.org": 42,
		"bar.example.org": 2001,
	}
	ramUsageByHost = map[string]float64{
		"foo.example.org": 6.023e23,
		"bar.example.org": 3.14,
	}
	return
}

// ClusterManagerCollector implements the Collector interface.
type ClusterManagerCollector struct {
	ClusterManager *ClusterManager
}

// Descriptors used by the ClusterManagerCollector below.
var (
	hostGPUdesc = prometheus.NewDesc(
		"HostGPUMemoryUsage",
		"GPU device memory usage",
		[]string{"deviceidx", "deviceuuid", "devicetype"}, nil,
	)

	hostGPUUtilizationdesc = prometheus.NewDesc(
		"HostCoreUtilization",
		"GPU core utilization",
		[]string{"deviceidx", "deviceuuid", "devicetype"}, nil,
	)

	ctrvGPUdesc = prometheus.NewDesc(
		"vGPU_device_memory_usage_in_bytes",
		"vGPU device usage",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)

	ctrvGPUlimitdesc = prometheus.NewDesc(
		"vGPU_device_memory_limit_in_bytes",
		"vGPU device limit",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
	ctrDeviceMemorydesc = prometheus.NewDesc(
		"Device_memory_desc_of_container",
		`Container device memory description (The label "context", "module", "data" and "offset" will be deprecated in v2.10.0, use vGPU_device_memory_context_size_bytes, vGPU_device_memory_module_size_bytes and vGPU_device_memory_buffer_size_bytes instead)`,
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid", "context", "module", "data", "offset"}, nil,
	)
	ctrDeviceUtilizationdesc = prometheus.NewDesc(
		"Device_utilization_desc_of_container",
		"Container device utilization description",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
	ctrDeviceLastKernelDesc = prometheus.NewDesc(
		"Device_last_kernel_of_container",
		"Container device last kernel description",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
	ctrDeviceMigInfo = prometheus.NewDesc(
		"MigInfo",
		"Mig device information for container",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid", "instanceid"}, nil,
	)
	ctrDeviceMemoryContextDesc = prometheus.NewDesc(
		"vGPU_device_memory_context_size_bytes",
		"Container device memory context size",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)

	ctrDeviceMemoryModuleDesc = prometheus.NewDesc(
		"vGPU_device_memory_module_size_bytes",
		"Container device memory module size",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)

	ctrDeviceMemoryBufferDesc = prometheus.NewDesc(
		"vGPU_device_memory_buffer_size_bytes",
		"Container device memory buffer size",
		[]string{"podnamespace", "podname", "ctrname", "vdeviceid", "deviceuuid"}, nil,
	)
)

// Describe is implemented with DescribeByCollect. That's possible because the
// Collect method will always return the same two metrics with the same two
// descriptors.
func (cc ClusterManagerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- hostGPUdesc
	ch <- ctrvGPUdesc
	ch <- ctrvGPUlimitdesc
	ch <- hostGPUUtilizationdesc
	ch <- ctrDeviceMemorydesc
	ch <- ctrDeviceUtilizationdesc
	ch <- ctrDeviceMemoryContextDesc
	ch <- ctrDeviceMemoryModuleDesc
	ch <- ctrDeviceMemoryBufferDesc
	//prometheus.DescribeByCollect(cc, ch)
}

//func parseidstr(podusage string) (string, string, error) {
//	tmp := strings.Split(podusage, "_")
//	if len(tmp) > 1 {
//		return tmp[0], tmp[1], nil
//	} else {
//		return "", "", errors.New("parse error")
//	}
//}
//
//func gettotalusage(usage podusage, vidx int) (deviceMemory, error) {
//	added := deviceMemory{
//		bufferSize:  0,
//		contextSize: 0,
//		moduleSize:  0,
//		offset:      0,
//		total:       0,
//	}
//	for _, val := range usage.sr.procs {
//		added.bufferSize += val.used[vidx].bufferSize
//		added.contextSize += val.used[vidx].contextSize
//		added.moduleSize += val.used[vidx].moduleSize
//		added.offset += val.used[vidx].offset
//		added.total += val.used[vidx].total
//	}
//	return added, nil
//}
//
//func getTotalUtilization(usage podusage, vidx int) deviceUtilization {
//	added := deviceUtilization{
//		decUtil: 0,
//		encUtil: 0,
//		smUtil:  0,
//	}
//	for _, val := range usage.sr.procs {
//		added.decUtil += val.deviceUtil[vidx].decUtil
//		added.encUtil += val.deviceUtil[vidx].encUtil
//		added.smUtil += val.deviceUtil[vidx].smUtil
//	}
//	return added
//}

// Collect first triggers the ReallyExpensiveAssessmentOfTheSystemState. Then it
// creates constant metrics for each host on the fly based on the returned data.
//
// Note that Collect could be called concurrently, so we depend on
// ReallyExpensiveAssessmentOfTheSystemState to be concurrency-safe.
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

	ch <- prometheus.MustNewConstMetric(
		hostGPUdesc,
		prometheus.GaugeValue,
		float64(memory.Used),
		fmt.Sprint(index), uuid, deviceName,
	)

	return nil
}

func (cc ClusterManagerCollector) collectGPUUtilizationMetrics(ch chan<- prometheus.Metric, hdev nvml.Device, index int) error {
	util, nvret := hdev.GetUtilizationRates()
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

	ch <- prometheus.MustNewConstMetric(
		hostGPUUtilizationdesc,
		prometheus.GaugeValue,
		float64(util.Gpu),
		fmt.Sprint(index), uuid, deviceName,
	)

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

		// Iterate through each container in the Pod
		for _, ctr := range pod.Spec.Containers {
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

func (cc ClusterManagerCollector) isPodUIDMatched(pod *corev1.Pod, podUID string) bool {
	if pod == nil {
		return false
	}
	return string(pod.UID) == podUID
}

func (cc ClusterManagerCollector) collectContainerMetrics(ch chan<- prometheus.Metric, pod *corev1.Pod, ctr corev1.Container, c *nvidia.ContainerUsage, nowSec int64) error {
	// Validate inputs
	if c == nil || c.Info == nil {
		klog.Errorf("Container or ContainerInfo is nil for Pod %s/%s, Container %s", pod.Namespace, pod.Name, ctr.Name)
		return fmt.Errorf("container or container info is nil")
	}

	// Iterate through each device
	for i := range c.Info.DeviceNum() {
		uuid := c.Info.DeviceUUID(i)
		if len(uuid) < 40 {
			klog.Errorf("Invalid UUID length for device %d in Pod %s/%s, Container %s", i, pod.Namespace, pod.Name, ctr.Name)
			return fmt.Errorf("invalid UUID length for device %d", i)
		}
		uuid = uuid[0:40] // Ensure UUID is truncated to 40 characters

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

		if err := sendMetric(ch, ctrvGPUlimitdesc, prometheus.GaugeValue, float64(memoryLimit), labels...); err != nil {
			klog.Errorf("Failed to send memoryLimit metric: %v", err)
			return err
		}

		if err := sendMetric(ch, ctrDeviceMemorydesc, prometheus.GaugeValue, float64(memoryTotal), labels...); err != nil {
			klog.Errorf("Failed to send device memory desc: %v", err)
			return err
		}

		if err := sendMetric(ch, ctrDeviceUtilizationdesc, prometheus.GaugeValue, float64(smUtil), labels...); err != nil {
			klog.Errorf("Failed to send device utilization desc: %v", err)
			return err
		}

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
		pdevices, err := device.DecodePodDevices(device.SupportDevices, pod.Annotations)
		if err != nil {
			return fmt.Errorf("failed to decode pod devices: %w", err)
		}
		for ctrIdx, container := range pod.Spec.Containers {
			for ctrDevIdx, ctrDevices := range pdevices[nv.NvidiaGPUDevice] {
				if len(ctrDevices) == 0 || ctrIdx != ctrDevIdx {
					continue
				}
				for _, ctrDev := range ctrDevices {
					if strings.Contains(ctrDev.UUID, "[") {
						uuid := strings.Split(ctrDev.UUID, "[")[0]
						_, idx, err := device.ExtractMigTemplatesFromUUID(ctrDev.UUID)
						if err != nil {
							klog.Errorf("Failed to get mig template for device %s in Pod %s/%s, container %s: %v", ctrDev.UUID, pod.Namespace, pod.Name, container.Name, err)
							continue
						}
						gpuInstanceId, err := dp.GetMigGpuInstanceIdFromIndex(ctrDev.UUID, idx)
						if err != nil {
							klog.Errorf("Failed to get mig InstanceId for device %s in Pod %s/%s, container %s: %v", ctrDev.UUID, pod.Namespace, pod.Name, container.Name, err)
							continue
						}
						labels := []string{pod.Namespace, pod.Name, container.Name, fmt.Sprint(idx), uuid, fmt.Sprint(gpuInstanceId)}
						if err := sendMetric(ch, ctrDeviceMigInfo, prometheus.GaugeValue, 1, labels...); err != nil {
							klog.Errorf("Failed to send mig info metric for device %s in Pod %s/%s, container %s: %v", ctrDev.UUID, pod.Namespace, pod.Name, container.Name, err)
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func sendMetric(ch chan<- prometheus.Metric, desc *prometheus.Desc, valueType prometheus.ValueType, value float64, labels ...string) error {
	metric, err := prometheus.NewConstMetric(desc, valueType, value, labels...)
	if err != nil {
		return fmt.Errorf("failed to create metric: %w", err)
	}
	ch <- metric
	return nil
}

// NewClusterManager first creates a Prometheus-ignorant ClusterManager
// instance. Then, it creates a ClusterManagerCollector for the just created
// ClusterManager. Finally, it registers the ClusterManagerCollector with a
// wrapping Registerer that adds the zone as a label. In this way, the metrics
// collected by different ClusterManagerCollectors do not collide.
func NewClusterManager(zone string, reg prometheus.Registerer, containerLister *nvidia.ContainerLister) *ClusterManager {
	c := &ClusterManager{
		Zone:            zone,
		containerLister: containerLister,
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(containerLister.Clientset(), time.Hour*1)
	c.PodLister = informerFactory.Core().V1().Pods().Lister()
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)

	cc := ClusterManagerCollector{ClusterManager: c}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}
