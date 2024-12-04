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
	"net/http"
	"os"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/lister"
	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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
	nodeName        string
	podLister       lister.PodLister
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
		[]string{"deviceidx", "deviceuuid"}, nil,
	)

	hostGPUUtilizationdesc = prometheus.NewDesc(
		"HostCoreUtilization",
		"GPU core utilization",
		[]string{"deviceidx", "deviceuuid"}, nil,
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
		"Container device meory description",
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
)

// Describe is implemented with DescribeByCollect. That's possible because the
// Collect method will always return the same two metrics with the same two
// descriptors.
func (cc ClusterManagerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- hostGPUdesc
	ch <- ctrvGPUdesc
	ch <- ctrvGPUlimitdesc
	ch <- hostGPUUtilizationdesc
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
	containerLister := cc.ClusterManager.containerLister

	// Why do we still need to manually update after starting another goroutine ?
	//if err := containerLister.Update(); err != nil {
	//	klog.Error("Update container error: %s", err.Error())
	//}

	nvret := nvml.Init()
	if nvret != nvml.SUCCESS {
		klog.Error("nvml Init err=", nvml.ErrorString(nvret))
	}
	devnum, nvret := nvml.DeviceGetCount()
	if nvret != nvml.SUCCESS {
		klog.Error("nvml GetDeviceCount err=", nvml.ErrorString(nvret))
	} else {
		for ii := 0; ii < devnum; ii++ {
			hdev, nvret := nvml.DeviceGetHandleByIndex(ii)
			if nvret != nvml.SUCCESS {
				klog.Error(nvml.ErrorString(nvret))
			}
			memoryUsed := 0
			memory, ret := hdev.GetMemoryInfo()
			if ret == nvml.SUCCESS {
				memoryUsed = int(memory.Used)
			} else {
				klog.Error("nvml get memory error ret=", ret)
			}

			uuid, nvret := hdev.GetUUID()
			if nvret != nvml.SUCCESS {
				klog.Error(nvml.ErrorString(nvret))
			} else {
				ch <- prometheus.MustNewConstMetric(
					hostGPUdesc,
					prometheus.GaugeValue,
					float64(memoryUsed),
					fmt.Sprint(ii), uuid,
				)
			}
			util, nvret := hdev.GetUtilizationRates()
			if nvret != nvml.SUCCESS {
				klog.Error(nvml.ErrorString(nvret))
			} else {
				ch <- prometheus.MustNewConstMetric(
					hostGPUUtilizationdesc,
					prometheus.GaugeValue,
					float64(util.Gpu),
					fmt.Sprint(ii), uuid,
				)
			}

		}
	}
	pods, err := cc.ClusterManager.podLister.GetByIndex(lister.PodIndexerKey, cc.ClusterManager.nodeName)
	if err != nil {
		klog.Error("failed to list pods with err=", err.Error())
		return
	}
	nowSec := time.Now().Unix()

	for _, pod := range pods {

		for _, ctr := range pod.Spec.Containers {
			key := fmt.Sprintf("%s_%s", pod.UID, ctr.Name)
			c, ok := containerLister.GetUsage(key)
			if !ok {
				continue
			}
			fmt.Println("container matched", ctr.Name)
			//err := setHostPid(pod, pod.Status.ContainerStatuses[ctridx], &srPodList[sridx])
			//if err != nil {
			//	fmt.Println("setHostPid filed", err.Error())
			//}
			//fmt.Println("sr.list=", srPodList[sridx].sr)
			//podlabels := make(map[string]string)
			//for idx, val := range pod.Labels {
			//	idxfix := strings.ReplaceAll(idx, "-", "_")
			//	valfix := strings.ReplaceAll(val, "-", "_")
			//	podlabels[idxfix] = valfix
			//}
			for i := 0; i < c.Info.DeviceNum(); i++ {
				uuid := c.Info.DeviceUUID(i)[0:40]
				memoryTotal := c.Info.DeviceMemoryTotal(i)
				memoryLimit := c.Info.DeviceMemoryLimit(i)
				memoryContextSize := c.Info.DeviceMemoryContextSize(i)
				memoryModuleSize := c.Info.DeviceMemoryModuleSize(i)
				memoryBufferSize := c.Info.DeviceMemoryBufferSize(i)
				memoryOffset := c.Info.DeviceMemoryOffset(i)
				smUtil := c.Info.DeviceSmUtil(i)
				lastKernelTime := c.Info.LastKernelTime()

				//fmt.Println("uuid=", uuid, "length=", len(uuid))
				ch <- prometheus.MustNewConstMetric(
					ctrvGPUdesc,
					prometheus.GaugeValue,
					float64(memoryTotal),
					pod.Namespace, pod.Name, ctr.Name, fmt.Sprint(i), uuid, /*,string(sr.sr.uuids[i].uuid[:])*/
				)
				ch <- prometheus.MustNewConstMetric(
					ctrvGPUlimitdesc,
					prometheus.GaugeValue,
					float64(memoryLimit),
					pod.Namespace, pod.Name, ctr.Name, fmt.Sprint(i), uuid, /*,string(sr.sr.uuids[i].uuid[:])*/
				)
				ch <- prometheus.MustNewConstMetric(
					ctrDeviceMemorydesc,
					prometheus.CounterValue,
					float64(memoryTotal),
					pod.Namespace, pod.Name, ctr.Name, fmt.Sprint(i), uuid,
					fmt.Sprint(memoryContextSize), fmt.Sprint(memoryModuleSize), fmt.Sprint(memoryBufferSize), fmt.Sprint(memoryOffset),
				)
				ch <- prometheus.MustNewConstMetric(
					ctrDeviceUtilizationdesc,
					prometheus.GaugeValue,
					float64(smUtil),
					pod.Namespace, pod.Name, ctr.Name, fmt.Sprint(i), uuid,
				)
				if lastKernelTime > 0 {
					lastSec := nowSec - lastKernelTime
					if lastSec < 0 {
						lastSec = 0
					}
					ch <- prometheus.MustNewConstMetric(
						ctrDeviceLastKernelDesc,
						prometheus.GaugeValue,
						float64(lastSec),
						pod.Namespace, pod.Name, ctr.Name, fmt.Sprint(i), uuid,
					)
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
func NewClusterManager(zone string, reg prometheus.Registerer, podLister lister.PodLister, containerLister *nvidia.ContainerLister) *ClusterManager {
	c := &ClusterManager{
		Zone:            zone,
		containerLister: containerLister,
		nodeName:        os.Getenv("NODE_NAME"),
		podLister:       podLister,
	}

	cc := ClusterManagerCollector{ClusterManager: c}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}

func initMetrics(podLister lister.PodLister, containerLister *nvidia.ContainerLister, stopChan chan<- struct{}) {
	// Since we are dealing with custom Collector implementations, it might
	// be a good idea to try it out with a pedantic registry.
	klog.Info("Initializing metrics for vGPUmonitor")
	reg := prometheus.NewRegistry()
	//reg := prometheus.NewPedanticRegistry()

	// Construct cluster managers. In real code, we would assign them to
	// variables to then do something with them.
	NewClusterManager("vGPU", reg, podLister, containerLister)
	//NewClusterManager("ca", reg)

	// Add the standard process and Go metrics to the custom registry.
	//reg.MustRegister(
	//	prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	//	prometheus.NewGoCollector(),
	//)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	if err := http.ListenAndServe(":9394", nil); err != nil {
		klog.Errorf("failed to serve: %v", err)
		close(stopChan)
	}
}
