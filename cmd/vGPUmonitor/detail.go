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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	CgroupRoot  = "/sys/fs/cgroup/memory/kubepods.slice"
	CgroupProcs = "cgroup.procs"
	RUNTIME     = "docker"
	burstable   = "kubepods-burstable"
	besteffort  = "kubepods-besteffort"
	guaranteed  = "kubepods"
)

func getContainerUtilizationByUUID(containerName string, pod *corev1.Pod, maDevUtil map[string]map[int]int) float64 {
	var containerID string
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			containerID = strings.TrimPrefix(status.ContainerID, RUNTIME+"://")
		}
	}
	klog.V(5).Infof("containerID: %s", containerID)
	res := float64(0)
	ma, err := getPidsInContainer(pod, containerID)
	if err != nil {
		klog.Errorf("Failed to get pids in container %v: %v", containerID, err)
		return res
	}
	for _, dev := range maDevUtil {
		for k, v := range dev {
			if _, ok := ma[k]; ok {
				res += float64(v)
			}
		}
	}
	return res
}

func getPidsInContainer(pod *corev1.Pod, containerID string) (map[int]struct{}, error) {
	cgroupPath := getCgroupName(pod, containerID)
	if cgroupPath == "" {
		return nil, fmt.Errorf("no cgroup found for container %v", containerID)
	}
	klog.V(5).Infof("cgroupPath: %s", cgroupPath)
	baseDir := filepath.Clean(cgroupPath)
	procFile := filepath.Join(baseDir, CgroupProcs)
	return readProcsFile(procFile)
}

func getCgroupName(pod *corev1.Pod, containerID string) string {
	podQos := pod.Status.QOSClass
	var parentContainer, prefix string
	switch podQos {
	case corev1.PodQOSGuaranteed:
		parentContainer = CgroupRoot
		prefix = guaranteed
	case corev1.PodQOSBurstable:
		parentContainer = filepath.Join(CgroupRoot, burstable+".slice")
		prefix = burstable
	case corev1.PodQOSBestEffort:
		parentContainer = filepath.Join(CgroupRoot, besteffort+".slice")
		prefix = besteffort
	default:
		return ""
	}

	podContainer := prefix + "-pod" + strings.Replace(string(pod.UID), "-", "_", -1) + ".slice"
	cgroupName := filepath.Join(parentContainer, podContainer)
	return fmt.Sprintf("%s/%s-%s.scope", cgroupName, RUNTIME, containerID)
}

func readProcsFile(file string) (map[int]struct{}, error) {
	f, err := os.Open(file)
	if err != nil {
		klog.Errorf("can't read %s, %v", file, err)
		return nil, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	pids := make(map[int]struct{})
	for scanner.Scan() {
		line := scanner.Text()
		if pid, err := strconv.Atoi(line); err == nil {
			pids[pid] = struct{}{}
		}
	}

	klog.V(5).Infof("Read from %s, pids: %v", file, pids)
	return pids, nil
}
