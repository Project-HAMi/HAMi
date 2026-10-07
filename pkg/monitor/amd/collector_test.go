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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testBDF  = "0000:06:00.0"
	testUUID = "33ff7590-0000-1000-8021-340866ba8c47"
	testUID  = "76ee6770-1f4c-436c-9d68-2c6d515ad8f7"
	region   = "drm/" + testBDF + "/vram"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// fixture lays out one amdgpu card, one iGPU the plugin does not register, and
// one container holding 1 GiB on the card.
func fixture(t *testing.T) *Collector {
	t.Helper()
	drm, cg := t.TempDir(), t.TempDir()
	card := filepath.Join(drm, "card1", "device")
	writeFile(t, filepath.Join(card, "uevent"), "DRIVER=amdgpu\nPCI_SLOT_NAME="+testBDF+"\n")
	writeFile(t, filepath.Join(card, "mem_info_vram_used"), "1251446784\n")
	writeFile(t, filepath.Join(card, "gpu_busy_percent"), "37\n")
	writeFile(t, filepath.Join(card, "hwmon", "hwmon3", "temp1_input"), "42000\n")
	writeFile(t, filepath.Join(card, "hwmon", "hwmon3", "power1_average"), "11000000\n")
	igpu := filepath.Join(drm, "card2", "device")
	writeFile(t, filepath.Join(igpu, "uevent"), "DRIVER=amdgpu\nPCI_SLOT_NAME=0000:0e:00.0\n")
	writeFile(t, filepath.Join(igpu, "mem_info_vram_used"), "1\n")
	writeFile(t, filepath.Join(igpu, "gpu_busy_percent"), "1\n")

	scope := filepath.Join(cg, "kubepods.slice", "kubepods-besteffort.slice",
		"kubepods-besteffort-pod76ee6770_1f4c_436c_9d68_2c6d515ad8f7.slice", "cri-containerd-abc123.scope")
	writeFile(t, filepath.Join(scope, "dmem.current"), region+" 1073741824\n")
	writeFile(t, filepath.Join(scope, "dmem.max"), region+" max\n")

	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-1", Annotations: map[string]string{
		"hami.io/node-amd-register": `[{"id":"` + testUUID + `","index":1,"type":"AMD Radeon Graphics","custominfo":{"pciBDF":"` + testBDF + `"}}]`,
	}}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "burn", Namespace: "amd-e2e", UID: testUID},
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "burn", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"amd.com/gpu": resource.MustParse("1"), "amd.com/gpumem": resource.MustParse("4096")}}},
			{Name: "sidecar"},
		}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "burn", ContainerID: "containerd://abc123"},
			{Name: "sidecar", ContainerID: "containerd://def456"},
		}},
	}
	return &Collector{
		NodeName: "gpu-1", CgroupRoot: cg, DRMRoot: drm,
		Pods: func() ([]*corev1.Pod, error) { return []*corev1.Pod{pod}, nil },
		Node: func() (*corev1.Node, error) { return node, nil },
	}
}

func TestCollect(t *testing.T) {
	c := fixture(t)
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)

	want := `
# HELP hami_host_gpu_memory_used_bytes GPU device memory usage in bytes
# TYPE hami_host_gpu_memory_used_bytes gauge
hami_host_gpu_memory_used_bytes{device_index="1",device_type="AMD Radeon Graphics",device_uuid="` + testUUID + `",node="gpu-1"} 1.251446784e+09
# HELP hami_host_gpu_power_usage_watts GPU power draw in watts
# TYPE hami_host_gpu_power_usage_watts gauge
hami_host_gpu_power_usage_watts{device_index="1",device_type="AMD Radeon Graphics",device_uuid="` + testUUID + `",node="gpu-1"} 11
# HELP hami_host_gpu_temperature_celsius GPU temperature in degrees Celsius
# TYPE hami_host_gpu_temperature_celsius gauge
hami_host_gpu_temperature_celsius{device_index="1",device_type="AMD Radeon Graphics",device_uuid="` + testUUID + `",node="gpu-1"} 42
# HELP hami_host_gpu_utilization_ratio GPU core utilization ratio (0-100)
# TYPE hami_host_gpu_utilization_ratio gauge
hami_host_gpu_utilization_ratio{device_index="1",device_type="AMD Radeon Graphics",device_uuid="` + testUUID + `",node="gpu-1"} 37
# HELP hami_vgpu_memory_limit_bytes vGPU device memory limit in bytes
# TYPE hami_vgpu_memory_limit_bytes gauge
hami_vgpu_memory_limit_bytes{container="burn",device_uuid="` + testUUID + `",namespace="amd-e2e",pod="burn",vdevice_index="0"} 4.294967296e+09
# HELP hami_vgpu_memory_used_bytes vGPU device memory usage in bytes
# TYPE hami_vgpu_memory_used_bytes gauge
hami_vgpu_memory_used_bytes{container="burn",device_uuid="` + testUUID + `",namespace="amd-e2e",pod="burn",vdevice_index="0"} 1.073741824e+09
# HELP hami_vgpumonitor_collect_success Whether the last metrics collection fully succeeded (1) or any part of it failed (0)
# TYPE hami_vgpumonitor_collect_success gauge
hami_vgpumonitor_collect_success{node="gpu-1"} 1
`
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want)))
}

func TestCollectWithoutSensorsStillSucceeds(t *testing.T) {
	c := fixture(t)
	require.NoError(t, os.RemoveAll(filepath.Join(c.DRMRoot, "card1", "device", "hwmon")))
	require.InDelta(t, 1, successOf(t, c), 0.001)
}

func TestCollectReportsFailures(t *testing.T) {
	t.Run("node without the registration annotation", func(t *testing.T) {
		c := fixture(t)
		c.Node = func() (*corev1.Node, error) { return &corev1.Node{}, nil }
		require.InDelta(t, 0, successOf(t, c), 0.001)
	})
	t.Run("node lookup fails", func(t *testing.T) {
		c := fixture(t)
		c.Node = func() (*corev1.Node, error) { return nil, errors.New("boom") }
		require.InDelta(t, 0, successOf(t, c), 0.001)
	})
	t.Run("pod lookup fails", func(t *testing.T) {
		c := fixture(t)
		c.Pods = func() ([]*corev1.Pod, error) { return nil, errors.New("boom") }
		require.InDelta(t, 0, successOf(t, c), 0.001)
	})
	t.Run("unreadable device memory", func(t *testing.T) {
		c := fixture(t)
		require.NoError(t, os.Remove(filepath.Join(c.DRMRoot, "card1", "device", "mem_info_vram_used")))
		require.InDelta(t, 0, successOf(t, c), 0.001)
	})
	t.Run("container on an unregistered device", func(t *testing.T) {
		c := fixture(t)
		scope := filepath.Join(c.CgroupRoot, "kubepods.slice", "kubepods-besteffort.slice",
			"kubepods-besteffort-pod76ee6770_1f4c_436c_9d68_2c6d515ad8f7.slice", "cri-containerd-abc123.scope")
		writeFile(t, filepath.Join(scope, "dmem.current"), "drm/0000:0e:00.0/vram 5\n")
		require.InDelta(t, 0, successOf(t, c), 0.001)
	})
}

func TestContainerWithTwoCardsNumbersThemInOrderAndDmemCapWins(t *testing.T) {
	c := fixture(t)
	scope := filepath.Join(c.CgroupRoot, "kubepods.slice", "kubepods-besteffort.slice",
		"kubepods-besteffort-pod76ee6770_1f4c_436c_9d68_2c6d515ad8f7.slice", "cri-containerd-abc123.scope")
	writeFile(t, filepath.Join(scope, "dmem.current"), "drm/0000:0e:00.0/vram 7\n"+region+" 9\n")
	writeFile(t, filepath.Join(scope, "dmem.max"), "drm/0000:0e:00.0/vram 100\n"+region+" 200\n")
	node, _ := c.Node()
	node.Annotations["hami.io/node-amd-register"] = `[` +
		`{"id":"u-06","index":1,"type":"AMD","custominfo":{"pciBDF":"` + testBDF + `"}},` +
		`{"id":"u-0e","index":0,"type":"AMD","custominfo":{"pciBDF":"0000:0e:00.0"}}]`

	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	require.NoError(t, err)
	got := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() != "hami_vgpu_memory_limit_bytes" {
			continue
		}
		for _, m := range mf.GetMetric() {
			got[m.GetLabel()[4].GetValue()+"/"+m.GetLabel()[1].GetValue()] = m.GetGauge().GetValue()
		}
	}
	// Regions sort by PCI address, so 0000:06 is device 0 and 0000:0e is device 1,
	// and the dmem cap replaces the 4096 MiB the pod asked for.
	require.Equal(t, map[string]float64{"0/u-06": 200, "1/u-0e": 100}, got)
}

func successOf(t *testing.T, c *Collector) float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == "hami_vgpumonitor_collect_success" {
			return mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatal("collect_success not emitted")
	return 0
}
