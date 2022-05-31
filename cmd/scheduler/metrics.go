package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	clientset *kubernetes.Clientset
)

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
	fmt.Println("begin collect")
	nodevGPUMemoryLimitDesc := prometheus.NewDesc(
		"GPUDeviceMemoryLimit",
		"Device memory limit for a certain GPU",
		[]string{"nodeid", "deviceuuid"}, nil,
	)
	nodevGPUMemoryAllocatedDesc := prometheus.NewDesc(
		"GPUDeviceMemoryAllocated",
		"Device memory allocated for a certain GPU",
		[]string{"nodeid", "deviceuuid", "devicecores"}, nil,
	)
	nodevGPUSharedNumDesc := prometheus.NewDesc(
		"GPUDeviceSharedNum",
		"Number of containers sharing this GPU",
		[]string{"nodeid", "deviceuuid"}, nil,
	)

	nodeGPUCoreAllocatedDesc := prometheus.NewDesc(
		"GPUDeviceCoreAllocated",
		"Device core allocated for a certain GPU",
		[]string{"nodeid", "deviceuuid"}, nil,
	)
	nodeGPUOverview := prometheus.NewDesc(
		"nodeGPUOverview",
		"GPU overview on a certain node",
		[]string{"nodeid", "deviceuuid", "devicecores", "sharedcontainers", "devicememorylimit", "devicetype"}, nil,
	)
	nodeGPUMemoryPercentage := prometheus.NewDesc(
		"nodeGPUMemoryPercentage",
		"GPU Memory Allocated Percentage on a certain GPU",
		[]string{"nodeid", "deviceuuid"}, nil,
	)
	nu := sher.InspectAllNodesUsage()
	for nodeID, val := range *nu {
		for _, devs := range val.Devices {
			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryLimitDesc,
				prometheus.GaugeValue,
				float64(devs.Totalmem*1024*1024),
				nodeID, devs.Id,
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Usedmem*1024*1024),
				nodeID, devs.Id, fmt.Sprint(devs.Usedcores),
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUSharedNumDesc,
				prometheus.GaugeValue,
				float64(devs.Used),
				nodeID, devs.Id,
			)

			ch <- prometheus.MustNewConstMetric(
				nodeGPUCoreAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Usedcores),
				nodeID, devs.Id,
			)
			ch <- prometheus.MustNewConstMetric(
				nodeGPUOverview,
				prometheus.GaugeValue,
				float64(devs.Usedmem*1024*1024),
				nodeID, devs.Id, fmt.Sprint(devs.Usedcores), fmt.Sprint(devs.Used), fmt.Sprint(devs.Totalmem), devs.Type,
			)
			ch <- prometheus.MustNewConstMetric(
				nodeGPUMemoryPercentage,
				prometheus.GaugeValue,
				float64(devs.Usedmem)/float64(devs.Totalmem),
				nodeID, devs.Id,
			)
		}
	}

	ctrvGPUDeviceAllocatedDesc := prometheus.NewDesc(
		"vGPUPodsDeviceAllocated",
		"vGPU Allocated from pods",
		[]string{"namespace", "nodename", "podname", "containeridx", "deviceuuid", "deviceusedcore"}, nil,
	)
	ctrvGPUdeviceAllocatedMemoryPercentageDesc := prometheus.NewDesc(
		"vGPUMemoryPercentage",
		"vGPU memory percentage allocated from a container",
		[]string{"namespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	ctrvGPUdeviceAllocateCorePercentageDesc := prometheus.NewDesc(
		"vGPUCorePercentage",
		"vGPU core allocated from a container",
		[]string{"namespace", "nmodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	schedpods, _ := sher.GetScheduledPods()
	for _, val := range schedpods {
		for ctridx, ctrval := range val.Devices {
			for _, ctrdevval := range ctrval {
				fmt.Println("Collecting", val.Namespace, val.NodeID, val.Name, ctrdevval.UUID, ctrdevval.Usedcores, ctrdevval.Usedmem)
				ch <- prometheus.MustNewConstMetric(
					ctrvGPUDeviceAllocatedDesc,
					prometheus.GaugeValue,
					float64(ctrdevval.Usedmem*1024*1024),
					val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID, fmt.Sprint(ctrdevval.Usedcores))
				var totaldev int32
				found := false
				for _, ni := range *nu {
					for _, nodedev := range ni.Devices {
						fmt.Println("uuid=", nodedev.Id, ctrdevval.UUID)
						if strings.Compare(nodedev.Id, ctrdevval.UUID) == 0 {
							totaldev = nodedev.Totalmem
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if totaldev > 0 {
					ch <- prometheus.MustNewConstMetric(
						ctrvGPUdeviceAllocatedMemoryPercentageDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedmem)/float64(totaldev),
						val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
				}
				ch <- prometheus.MustNewConstMetric(
					ctrvGPUdeviceAllocateCorePercentageDesc,
					prometheus.GaugeValue,
					float64(ctrdevval.Usedcores),
					val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID)
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
	log.Fatal(http.ListenAndServe(":9395", nil))
}
