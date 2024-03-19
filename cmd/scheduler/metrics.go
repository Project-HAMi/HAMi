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
	nu := sher.InspectAllNodesUsage()
	for nodeID, val := range *nu {
		for _, devs := range val.Devices {
			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryLimitDesc,
				prometheus.GaugeValue,
				float64(devs.Totalmem)*float64(1024)*float64(1024),
				nodeID, devs.Id, fmt.Sprint(devs.Index),
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUCoreLimitDesc,
				prometheus.GaugeValue,
				float64(devs.Totalcore),
				nodeID, devs.Id, fmt.Sprint(devs.Index),
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUMemoryAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Usedmem)*float64(1024)*float64(1024),
				nodeID, devs.Id, fmt.Sprint(devs.Index), fmt.Sprint(devs.Usedcores),
			)
			ch <- prometheus.MustNewConstMetric(
				nodevGPUSharedNumDesc,
				prometheus.GaugeValue,
				float64(devs.Used),
				nodeID, devs.Id, fmt.Sprint(devs.Index),
			)

			ch <- prometheus.MustNewConstMetric(
				nodeGPUCoreAllocatedDesc,
				prometheus.GaugeValue,
				float64(devs.Usedcores),
				nodeID, devs.Id, fmt.Sprint(devs.Index),
			)
			ch <- prometheus.MustNewConstMetric(
				nodeGPUOverview,
				prometheus.GaugeValue,
				float64(devs.Usedmem)*float64(1024)*float64(1024),
				nodeID, devs.Id, fmt.Sprint(devs.Index), fmt.Sprint(devs.Usedcores), fmt.Sprint(devs.Used), fmt.Sprint(devs.Totalmem), devs.Type,
			)
			ch <- prometheus.MustNewConstMetric(
				nodeGPUMemoryPercentage,
				prometheus.GaugeValue,
				float64(devs.Usedmem)/float64(devs.Totalmem),
				nodeID, devs.Id, fmt.Sprint(devs.Index),
			)
		}
	}

	ctrvGPUDeviceAllocatedDesc := prometheus.NewDesc(
		"vGPUPodsDeviceAllocated",
		"vGPU Allocated from pods",
		[]string{"podnamespace", "nodename", "podname", "containeridx", "deviceuuid", "deviceusedcore"}, nil,
	)
	ctrvGPUdeviceAllocatedMemoryPercentageDesc := prometheus.NewDesc(
		"vGPUMemoryPercentage",
		"vGPU memory percentage allocated from a container",
		[]string{"podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	ctrvGPUdeviceAllocateCorePercentageDesc := prometheus.NewDesc(
		"vGPUCorePercentage",
		"vGPU core allocated from a container",
		[]string{"podnamespace", "nodename", "podname", "containeridx", "deviceuuid"}, nil,
	)
	schedpods, _ := sher.GetScheduledPods()
	for _, val := range schedpods {
		for ctridx, podSingleDevice := range val.Devices {
			for _, ctrdevs := range podSingleDevice {
				for _, ctrdevval := range ctrdevs {
					fmt.Println("Collecting", val.Namespace, val.NodeID, val.Name, ctrdevval.UUID, ctrdevval.Usedcores, ctrdevval.Usedmem)
					ch <- prometheus.MustNewConstMetric(
						ctrvGPUDeviceAllocatedDesc,
						prometheus.GaugeValue,
						float64(ctrdevval.Usedmem)*float64(1024)*float64(1024),
						val.Namespace, val.NodeID, val.Name, fmt.Sprint(ctridx), ctrdevval.UUID, fmt.Sprint(ctrdevval.Usedcores))
					var totaldev int32
					found := false
					for _, ni := range *nu {
						for _, nodedev := range ni.Devices {
							//fmt.Println("uuid=", nodedev.Id, ctrdevval.UUID)
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

func initmetrics(bindAddress string) {
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
