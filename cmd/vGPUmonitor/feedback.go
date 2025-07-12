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
	"os"
	"sort"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

var cgroupDriver int

//type hostGPUPid struct {
//	hostGPUPid int
//	mtime      uint64
//}

type UtilizationPerDevice []int

func setcGgroupDriver() int {
	// 1 for cgroupfs 2 for systemd
	kubeletconfig, err := os.ReadFile("/hostvar/lib/kubelet/config.yaml")
	if err != nil {
		return 0
	}
	content := string(kubeletconfig)
	pos := strings.LastIndex(content, "cgroupDriver:")
	if pos < 0 {
		return 0
	}
	if strings.Contains(content, "systemd") {
		return 2
	}
	if strings.Contains(content, "cgroupfs") {
		return 1
	}
	return 0
}

func getUsedGPUPid() ([]uint, nvml.Return) {
	tmp := []nvml.ProcessInfo{}
	count, err := nvml.DeviceGetCount()
	if err != nvml.SUCCESS {
		return []uint{}, err
	}
	for i := range count {
		device, err := nvml.DeviceGetHandleByIndex(i)
		if err != nvml.SUCCESS {
			return []uint{}, err
		}
		ids, err := device.GetComputeRunningProcesses()
		if err != nvml.SUCCESS {
			return []uint{}, err
		}
		tmp = append(tmp, ids...)
	}
	result := make([]uint, 0)
	m := make(map[uint]bool)
	for _, v := range tmp {
		if _, ok := m[uint(v.Pid)]; !ok {
			result = append(result, uint(v.Pid))
			m[uint(v.Pid)] = true
		}
	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].Pid > tmp[j].Pid })
	return result, nvml.SUCCESS
}

func CheckBlocking(utSwitchOn map[string]UtilizationPerDevice, p int, c *nvidia.ContainerUsage) bool {
	for i := range c.Info.DeviceMax() {
		uuid := c.Info.DeviceUUID(i)
		_, ok := utSwitchOn[uuid]
		if ok {
			for i := range p {
				if utSwitchOn[uuid][i] > 0 {
					return true
				}
			}
			return false
		}
	}
	return false
}

// Check whether task with higher priority use GPU or there are other tasks with the same priority.
func CheckPriority(utSwitchOn map[string]UtilizationPerDevice, p int, c *nvidia.ContainerUsage) bool {
	for i := range c.Info.DeviceMax() {
		uuid := c.Info.DeviceUUID(i)
		_, ok := utSwitchOn[uuid]
		if ok {
			for i := range p {
				if utSwitchOn[uuid][i] > 0 {
					return true
				}
			}
			if utSwitchOn[uuid][p] > 1 {
				return true
			}
		}
	}
	return false
}

func Observe(lister *nvidia.ContainerLister) {
	utSwitchOn := map[string]UtilizationPerDevice{}
	containers := lister.ListContainers()

	for _, c := range containers {
		recentKernel := c.Info.GetRecentKernel()
		if recentKernel > 0 {
			recentKernel--
			if recentKernel > 0 {
				for i := range c.Info.DeviceMax() {
					//for _, devuuid := range val.sr.uuids {
					// Null device condition
					if !c.Info.IsValidUUID(i) {
						continue
					}
					uuid := c.Info.DeviceUUID(i)
					if len(utSwitchOn[uuid]) == 0 {
						utSwitchOn[uuid] = []int{0, 0}
					}
					utSwitchOn[uuid][c.Info.GetPriority()]++
				}
			}
			c.Info.SetRecentKernel(recentKernel)
		}
	}
	for idx, c := range containers {
		priority := c.Info.GetPriority()
		recentKernel := c.Info.GetRecentKernel()
		utilizationSwitch := c.Info.GetUtilizationSwitch()
		if CheckBlocking(utSwitchOn, priority, c) {
			if recentKernel >= 0 {
				klog.V(5).Infof("utSwitchon=%v", utSwitchOn)
				klog.V(5).Infof("Setting Blocking to on %v", idx)
				c.Info.SetRecentKernel(-1)
			}
		} else {
			if recentKernel < 0 {
				klog.V(5).Infof("utSwitchon=%v", utSwitchOn)
				klog.V(5).Infof("Setting Blocking to off %v", idx)
				c.Info.SetRecentKernel(0)
			}
		}
		if CheckPriority(utSwitchOn, priority, c) {
			if utilizationSwitch != 1 {
				klog.V(5).Infof("utSwitchon=%v", utSwitchOn)
				klog.V(5).Infof("Setting UtilizationSwitch to on %v", idx)
				c.Info.SetUtilizationSwitch(1)
			}
		} else {
			if utilizationSwitch != 0 {
				klog.V(5).Infof("utSwitchon=%v", utSwitchOn)
				klog.V(5).Infof("Setting UtilizationSwitch to off %v", idx)
				c.Info.SetUtilizationSwitch(0)
			}
		}
	}
}
