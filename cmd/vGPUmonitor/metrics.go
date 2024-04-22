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
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
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
	PodLister listerscorev1.PodLister
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
	clientset *kubernetes.Clientset
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

func parseidstr(podusage string) (string, string, error) {
	tmp := strings.Split(podusage, "_")
	if len(tmp) > 1 {
		return tmp[0], tmp[1], nil
	} else {
		return "", "", errors.New("parse error")
	}
}

func gettotalusage(usage podusage, vidx int) (deviceMemory, error) {
	added := deviceMemory{
		bufferSize:  0,
		contextSize: 0,
		moduleSize:  0,
		offset:      0,
		total:       0,
	}
	for _, val := range usage.sr.procs {
		added.bufferSize += val.used[vidx].bufferSize
		added.contextSize += val.used[vidx].contextSize
		added.moduleSize += val.used[vidx].moduleSize
		added.offset += val.used[vidx].offset
		added.total += val.used[vidx].total
	}
	return added, nil
}

func getTotalUtilization(usage podusage, vidx int) deviceUtilization {
	added := deviceUtilization{
		decUtil: 0,
		encUtil: 0,
		smUtil:  0,
	}
	for _, val := range usage.sr.procs {
		added.decUtil += val.deviceUtil[vidx].decUtil
		added.encUtil += val.deviceUtil[vidx].encUtil
		added.smUtil += val.deviceUtil[vidx].smUtil
	}
	return added
}

func getsrlist() map[string]podusage {
	return srPodList
}

// Collect first triggers the ReallyExpensiveAssessmentOfTheSystemState. Then it
// creates constant metrics for each host on the fly based on the returned data.
//
// Note that Collect could be called concurrently, so we depend on
// ReallyExpensiveAssessmentOfTheSystemState to be concurrency-safe.
func (cc ClusterManagerCollector) Collect(ch chan<- prometheus.Metric) {
	klog.Info("Starting to collect metrics for vGPUMonitor")
	if srPodList == nil {
		srPodList = make(map[string]podusage)
	}
	if err := monitorpath(srPodList); err != nil {
		klog.Error("err=", err.Error())
	}
	if clientset != nil {
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

		pods, err := cc.ClusterManager.PodLister.List(labels.Everything())
		if err != nil {
			klog.Error("failed to list pods with err=", err.Error())
		}
		for _, val := range pods {
			for sridx := range srPodList {
				if srPodList[sridx].sr == nil {
					continue
				}
				podUID := strings.Split(srPodList[sridx].idstr, "_")[0]
				ctrName := strings.Split(srPodList[sridx].idstr, "_")[1]
				if strings.Compare(string(val.UID), podUID) == 0 {
					fmt.Println("Pod matched!", val.Name, val.Namespace, val.Labels)
					for _, ctr := range val.Spec.Containers {
						if strings.Compare(ctr.Name, ctrName) == 0 {
							fmt.Println("container matched", ctr.Name)
							//err := setHostPid(val, val.Status.ContainerStatuses[ctridx], &srPodList[sridx])
							//if err != nil {
							//	fmt.Println("setHostPid filed", err.Error())
							//}
							//fmt.Println("sr.list=", srPodList[sridx].sr)
							podlabels := make(map[string]string)
							for idx, val := range val.Labels {
								idxfix := strings.ReplaceAll(idx, "-", "_")
								valfix := strings.ReplaceAll(val, "-", "_")
								podlabels[idxfix] = valfix
							}
							for i := 0; i < int(srPodList[sridx].sr.num); i++ {
								value, _ := gettotalusage(srPodList[sridx], i)
								utilization := getTotalUtilization(srPodList[sridx], i)
								uuid := string(srPodList[sridx].sr.uuids[i].uuid[:])[0:40]

								//fmt.Println("uuid=", uuid, "length=", len(uuid))
								ch <- prometheus.MustNewConstMetric(
									ctrvGPUdesc,
									prometheus.GaugeValue,
									float64(value.total),
									val.Namespace, val.Name, ctrName, fmt.Sprint(i), uuid, /*,string(sr.sr.uuids[i].uuid[:])*/
								)
								ch <- prometheus.MustNewConstMetric(
									ctrvGPUlimitdesc,
									prometheus.GaugeValue,
									float64(srPodList[sridx].sr.limit[i]),
									val.Namespace, val.Name, ctrName, fmt.Sprint(i), uuid, /*,string(sr.sr.uuids[i].uuid[:])*/
								)
								ch <- prometheus.MustNewConstMetric(
									ctrDeviceMemorydesc,
									prometheus.CounterValue,
									float64(value.total),
									val.Namespace, val.Name, ctrName, fmt.Sprint(i), uuid, fmt.Sprint(value.contextSize), fmt.Sprint(value.moduleSize), fmt.Sprint(value.bufferSize), fmt.Sprint(value.offset),
								)
								ch <- prometheus.MustNewConstMetric(
									ctrDeviceUtilizationdesc,
									prometheus.GaugeValue,
									float64(utilization.smUtil),
									val.Namespace, val.Name, ctrName, fmt.Sprint(i), uuid,
								)
							}
						}
					}
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

	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Hour*1)
	c.PodLister = informerFactory.Core().V1().Pods().Lister()
	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)

	cc := ClusterManagerCollector{ClusterManager: c}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}

func initMetrics() {
	// Since we are dealing with custom Collector implementations, it might
	// be a good idea to try it out with a pedantic registry.
	klog.Info("Initializing metrics for vGPUmonitor")
	reg := prometheus.NewRegistry()
	//reg := prometheus.NewPedanticRegistry()
	config, err := rest.InClusterConfig()
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	// Construct cluster managers. In real code, we would assign them to
	// variables to then do something with them.
	NewClusterManager("vGPU", reg)
	//NewClusterManager("ca", reg)

	// Add the standard process and Go metrics to the custom registry.
	//reg.MustRegister(
	//	prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	//	prometheus.NewGoCollector(),
	//)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	log.Fatal(http.ListenAndServe(":9394", nil))
}
