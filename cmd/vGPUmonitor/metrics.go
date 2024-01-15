package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
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

func getsrlist() map[string]podusage {
	return srPodList
}

// Collect first triggers the ReallyExpensiveAssessmentOfTheSystemState. Then it
// creates constant metrics for each host on the fly based on the returned data.
//
// Note that Collect could be called concurrently, so we depend on
// ReallyExpensiveAssessmentOfTheSystemState to be concurrency-safe.
func (cc ClusterManagerCollector) Collect(ch chan<- prometheus.Metric) {
	fmt.Println("begin collect")
	if srPodList == nil {
		srPodList = make(map[string]podusage)
	}
	err := monitorPath(srPodList)
	if err != nil {
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
				memory, nvret := hdev.GetMemoryInfo_v2()
				if nvret != nvml.SUCCESS {
					klog.Error(nvml.ErrorString(nvret))
				}
				uuid, nvret := hdev.GetUUID()
				if nvret != nvml.SUCCESS {
					klog.Error(nvml.ErrorString(nvret))
				} else {
					ch <- prometheus.MustNewConstMetric(
						hostGPUdesc,
						prometheus.GaugeValue,
						float64(memory.Used),
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

		pods, err := clientset.CoreV1().Pods("").List(context.TODO(), v1.ListOptions{})
		if err != nil {
			fmt.Println("err=", err.Error())
		}
		for _, val := range pods.Items {
			for sridx := range srPodList {
				if srPodList[sridx].sr == nil {
					continue
				}
				pod_uid := strings.Split(srPodList[sridx].idstr, "_")[0]
				ctr_name := strings.Split(srPodList[sridx].idstr, "_")[1]
				fmt.Println("compareing", val.UID, pod_uid)
				if strings.Compare(string(val.UID), pod_uid) == 0 {
					fmt.Println("Pod matched!", val.Name, val.Namespace, val.Labels)
					for _, ctr := range val.Spec.Containers {
						if strings.Compare(ctr.Name, ctr_name) == 0 {
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
								uuid := string(srPodList[sridx].sr.uuids[i].uuid[:])[0:40]

								//fmt.Println("uuid=", uuid, "length=", len(uuid))
								ch <- prometheus.MustNewConstMetric(
									ctrvGPUdesc,
									prometheus.GaugeValue,
									float64(value.total),
									val.Namespace, val.Name, ctr_name, fmt.Sprint(i), uuid, /*,string(sr.sr.uuids[i].uuid[:])*/
								)
								ch <- prometheus.MustNewConstMetric(
									ctrvGPUlimitdesc,
									prometheus.GaugeValue,
									float64(srPodList[sridx].sr.limit[i]),
									val.Namespace, val.Name, ctr_name, fmt.Sprint(i), uuid, /*,string(sr.sr.uuids[i].uuid[:])*/
								)
								ch <- prometheus.MustNewConstMetric(
									ctrDeviceMemorydesc,
									prometheus.CounterValue,
									float64(value.total),
									val.Namespace, val.Name, ctr_name, fmt.Sprint(i), uuid, fmt.Sprint(value.contextSize), fmt.Sprint(value.moduleSize), fmt.Sprint(value.bufferSize), fmt.Sprint(value.offset),
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
	cc := ClusterManagerCollector{ClusterManager: c}
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": zone}, reg).MustRegister(cc)
	return c
}

func initmetrics() {
	// Since we are dealing with custom Collector implementations, it might
	// be a good idea to try it out with a pedantic registry.
	fmt.Println("Initializing metrics...")

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
