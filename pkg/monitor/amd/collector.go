/*
Copyright 2026 The HAMi Authors.

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

package amd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	amddevice "github.com/Project-HAMi/HAMi/pkg/device/amd"
)

const (
	// DefaultCgroupRoot and DefaultDRMRoot are where a node mounts them.
	DefaultCgroupRoot = "/sys/fs/cgroup"
	DefaultDRMRoot    = "/sys/class/drm"

	resourcePrefix = "amd.com/"
	memoryResource = "amd.com/gpumem"
	mib            = 1 << 20
)

// The metric names and labels match the NVIDIA vGPUmonitor, so dashboards that
// chart one vendor chart the other.
var (
	collectSuccessDesc = prometheus.NewDesc("hami_vgpumonitor_collect_success",
		"Whether the last metrics collection fully succeeded (1) or any part of it failed (0)",
		[]string{"node"}, nil)

	hostLabels         = []string{"node", "device_index", "device_uuid", "device_type"}
	hostMemoryUsedDesc = prometheus.NewDesc("hami_host_gpu_memory_used_bytes", "GPU device memory usage in bytes", hostLabels, nil)
	hostUtilDesc       = prometheus.NewDesc("hami_host_gpu_utilization_ratio", "GPU core utilization ratio (0-100)", hostLabels, nil)
	hostTempDesc       = prometheus.NewDesc("hami_host_gpu_temperature_celsius", "GPU temperature in degrees Celsius", hostLabels, nil)
	hostPowerDesc      = prometheus.NewDesc("hami_host_gpu_power_usage_watts", "GPU power draw in watts", hostLabels, nil)

	ctrLabels         = []string{"namespace", "pod", "container", "vdevice_index", "device_uuid"}
	ctrMemoryUsedDesc = prometheus.NewDesc("hami_vgpu_memory_used_bytes", "vGPU device memory usage in bytes", ctrLabels, nil)
	ctrMemoryLimitDsc = prometheus.NewDesc("hami_vgpu_memory_limit_bytes", "vGPU device memory limit in bytes", ctrLabels, nil)
)

// Collector reports AMD GPU metrics of one node. Device memory, load,
// temperature and power come from sysfs, and the per-container memory comes
// from the dmem cgroup controller, so it needs no ROCm tooling in its image.
type Collector struct {
	NodeName   string
	CgroupRoot string
	DRMRoot    string
	// Pods lists the pods on this node and Node returns this node, whose
	// registration annotation names the devices by UUID.
	Pods func() ([]*corev1.Pod, error)
	Node func() (*corev1.Node, error)
}

var _ prometheus.Collector = (*Collector)(nil)

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{collectSuccessDesc, hostMemoryUsedDesc, hostUtilDesc, hostTempDesc, hostPowerDesc, ctrMemoryUsedDesc, ctrMemoryLimitDsc} {
		ch <- d
	}
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	success := 1.0
	devices, err := c.registeredDevices()
	if err != nil {
		// Without the registration neither host nor container metrics can be named.
		klog.ErrorS(err, "Failed to read the registered AMD devices")
		success = 0
	} else {
		if err := c.collectHost(ch, devices); err != nil {
			klog.ErrorS(err, "Failed to collect AMD host metrics")
			success = 0
		}
		if err := c.collectContainers(ch, devices); err != nil {
			klog.ErrorS(err, "Failed to collect AMD container metrics")
			success = 0
		}
	}
	ch <- prometheus.MustNewConstMetric(collectSuccessDesc, prometheus.GaugeValue, success, c.NodeName)
}

// registeredDevices maps a PCI address to the device the plugin registered.
func (c *Collector) registeredDevices() (map[string]*device.DeviceInfo, error) {
	node, err := c.Node()
	if err != nil {
		return nil, err
	}
	raw, ok := node.Annotations[amddevice.RegisterAnnos]
	if !ok {
		return nil, fmt.Errorf("node %s has no %s annotation", node.Name, amddevice.RegisterAnnos)
	}
	var infos []*device.DeviceInfo
	if err := json.Unmarshal([]byte(raw), &infos); err != nil {
		return nil, fmt.Errorf("decode %s: %w", amddevice.RegisterAnnos, err)
	}
	byBDF := make(map[string]*device.DeviceInfo, len(infos))
	for _, info := range infos {
		if bdf, ok := info.CustomInfo["pciBDF"].(string); ok {
			byBDF[bdf] = info
		}
	}
	return byBDF, nil
}

func (c *Collector) collectHost(ch chan<- prometheus.Metric, devices map[string]*device.DeviceInfo) error {
	cards, err := filepath.Glob(filepath.Join(c.DRMRoot, "card*", "device"))
	if err != nil {
		return err
	}
	var failed []string
	for _, dir := range cards {
		uevent, err := os.ReadFile(filepath.Join(dir, "uevent"))
		if err != nil || !strings.Contains(string(uevent), "DRIVER=amdgpu") {
			continue
		}
		bdf := ueventValue(string(uevent), "PCI_SLOT_NAME")
		info, ok := devices[bdf]
		if !ok {
			// An unregistered card, such as an iGPU the plugin does not share.
			continue
		}
		labels := []string{c.NodeName, strconv.Itoa(int(info.Index)), info.ID, info.Type}
		emit := func(desc *prometheus.Desc, file string, scale float64, optional bool) {
			v, err := readNumber(filepath.Join(dir, file))
			if err != nil {
				if optional && os.IsNotExist(err) {
					return
				}
				failed = append(failed, fmt.Sprintf("%s %s: %v", bdf, file, err))
				return
			}
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v*scale, append([]string{}, labels...)...)
		}
		emit(hostMemoryUsedDesc, "mem_info_vram_used", 1, false)
		emit(hostUtilDesc, "gpu_busy_percent", 1, false)
		// hwmon reports millidegrees and microwatts, and a card may lack either sensor.
		if hw, _ := filepath.Glob(filepath.Join(dir, "hwmon", "hwmon*")); len(hw) > 0 {
			rel := func(f string) string { r, _ := filepath.Rel(dir, filepath.Join(hw[0], f)); return r }
			emit(hostTempDesc, rel("temp1_input"), 1e-3, true)
			emit(hostPowerDesc, rel("power1_average"), 1e-6, true)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("unreadable sysfs files: %s", strings.Join(failed, "; "))
	}
	return nil
}

func (c *Collector) collectContainers(ch chan<- prometheus.Metric, devices map[string]*device.DeviceInfo) error {
	pods, err := c.Pods()
	if err != nil {
		return err
	}
	usages, err := ReadContainerVRAM(c.CgroupRoot)
	if err != nil {
		return err
	}
	type owner struct {
		pod       *corev1.Pod
		container corev1.Container
	}
	owners := map[string]owner{}
	for _, pod := range pods {
		containers := append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...)
		statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
		for _, st := range statuses {
			_, id, ok := strings.Cut(st.ContainerID, "://")
			if !ok {
				continue
			}
			for _, ctr := range containers {
				if ctr.Name == st.Name && requestsAMD(ctr) {
					owners[id] = owner{pod, ctr}
				}
			}
		}
	}

	// A container holding several cards numbers them in region order.
	sort.Slice(usages, func(i, j int) bool { return usages[i].Region < usages[j].Region })
	next := map[string]int{}
	var unknown []string
	for _, u := range usages {
		o, ok := owners[u.ContainerID]
		if !ok {
			continue
		}
		bdf := strings.Split(u.Region, "/")[1]
		info, ok := devices[bdf]
		if !ok {
			unknown = append(unknown, bdf)
			continue
		}
		idx := next[u.ContainerID]
		next[u.ContainerID]++
		labels := []string{o.pod.Namespace, o.pod.Name, o.container.Name, strconv.Itoa(idx), info.ID}
		ch <- prometheus.MustNewConstMetric(ctrMemoryUsedDesc, prometheus.GaugeValue, float64(u.Used), labels...)
		// The dmem cap is the exact limit, where the node enforces one. Without it
		// the limit is what the pod asked for.
		limit := float64(u.Limit)
		if limit == 0 {
			if q, ok := o.container.Resources.Limits[memoryResource]; ok {
				limit = float64(q.Value() * mib)
			}
		}
		if limit > 0 {
			ch <- prometheus.MustNewConstMetric(ctrMemoryLimitDsc, prometheus.GaugeValue, limit, labels...)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("containers hold memory on unregistered devices: %s", strings.Join(unknown, ", "))
	}
	return nil
}

// requestsAMD reports whether the container asks for any amd.com resource, which
// is how a container using a card is told from the rest of the node's pods.
func requestsAMD(ctr corev1.Container) bool {
	for name := range ctr.Resources.Limits {
		if strings.HasPrefix(string(name), resourcePrefix) {
			return true
		}
	}
	return false
}

func ueventValue(uevent, key string) string {
	for line := range strings.SplitSeq(uevent, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readNumber(path string) (float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
}
