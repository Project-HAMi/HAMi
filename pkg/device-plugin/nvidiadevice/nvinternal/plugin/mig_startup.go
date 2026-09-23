/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// sortedIntSetKeys returns the keys of a set-style map sorted ascending.
// Small helper kept here so logging at startup emits stable key order.
func sortedIntSetKeys(s map[int]struct{}) []int {
	out := make([]int, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// collectInUseGPUs returns the set of GPU indexes that have at least one
// in-use MIG instance, unioned from two sources:
//   - live Pod allocation annotations (authoritative for HAMi allocations).
//   - NVML running processes on each MIG instance or the parent card (catches
//     usage that bypasses kubelet, e.g. bare processes on the node).
//
// Failure to read Pod annotations is returned to the caller because startup
// reset must not proceed without the authoritative allocation state. NVML
// process detection is an additional safeguard and remains best effort.
func (m *MigInstanceManager) collectInUseGPUs(ctx context.Context, nodeName string) (map[int]struct{}, error) {
	out := make(map[int]struct{})

	annotated, err := m.kubernetesAllocatedMigGPUs(ctx, nodeName)
	if err != nil {
		return out, fmt.Errorf("list Kubernetes MIG allocations: %w", err)
	}
	for g := range annotated {
		out[g] = struct{}{}
	}

	if busy, err := m.nvmlBusyGPUs(); err != nil {
		klog.InfoS("mig init: NVML busy-GPU detection skipped", "err", err)
	} else {
		for g := range busy {
			out[g] = struct{}{}
		}
	}

	return out, nil
}

// activeMigGPUUUIDs returns physical GPU UUIDs referenced by live HAMi MIG
// allocations. The annotation preserves the physical GPU identity across a
// device-plugin restart even though the MIG UUID is created at Allocate time.
func activeMigGPUUUIDs(pods []corev1.Pod) map[string]struct{} {
	out := make(map[string]struct{})
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		allocations, err := nvidia.DecodeMigAllocations(pod.Annotations[nvidia.MigAllocationsAnnotation])
		if err != nil {
			continue
		}
		for _, allocation := range allocations {
			if strings.HasPrefix(allocation.GPUUUID, "GPU-") {
				out[allocation.GPUUUID] = struct{}{}
			}
		}
	}
	return out
}

func (m *MigInstanceManager) kubernetesAllocatedMigGPUs(ctx context.Context, nodeName string) (map[int]struct{}, error) {
	kubeClient := client.GetClient()
	if kubeClient == nil {
		return nil, fmt.Errorf("Kubernetes client is not initialized")
	}
	pods, err := kubeClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return nil, err
	}

	out := make(map[int]struct{})
	for gpuUUID := range activeMigGPUUUIDs(pods.Items) {
		idx, ok := m.gpuUUIDToIndex(gpuUUID)
		if !ok {
			return nil, fmt.Errorf("resolve GPU UUID %s", gpuUUID)
		}
		out[idx] = struct{}{}
	}
	return out, nil
}

func (m *MigInstanceManager) gpuUUIDToIndex(gpuUUID string) (int, bool) {
	done, err := m.beginOperation()
	if err != nil {
		return 0, false
	}
	defer done()
	dev, ret := m.nvmllib.DeviceGetHandleByUUID(gpuUUID)
	if ret != nvml.SUCCESS {
		return 0, false
	}
	idx, ret := dev.GetIndex()
	return idx, ret == nvml.SUCCESS
}

// nvmlBusyGPUs returns the set of GPU indexes with at least one running
// compute or graphics process. For MIG-enabled cards every live MIG instance
// is inspected; for non-MIG cards the parent device is inspected directly.
func (m *MigInstanceManager) nvmlBusyGPUs() (map[int]struct{}, error) {
	done, err := m.beginOperation()
	if err != nil {
		return nil, err
	}
	defer done()
	count, ret := m.nvmllib.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("DeviceGetCount: %s", nvml.ErrorString(ret))
	}

	out := make(map[int]struct{})
	for i := 0; i < count; i++ {
		dev, ret := m.nvmllib.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			continue
		}

		curMode, _, ret := dev.GetMigMode()
		if ret == nvml.ERROR_NOT_SUPPORTED || ret != nvml.SUCCESS || curMode != nvml.DEVICE_MIG_ENABLE {
			if deviceHasProcesses(dev) {
				out[i] = struct{}{}
			}
			continue
		}

		maxCount, ret := dev.GetMaxMigDeviceCount()
		if ret != nvml.SUCCESS {
			continue
		}
		for j := 0; j < maxCount; j++ {
			migDev, ret := dev.GetMigDeviceHandleByIndex(j)
			if ret != nvml.SUCCESS {
				continue
			}
			if deviceHasProcesses(migDev) {
				out[i] = struct{}{}
				break
			}
		}
	}
	return out, nil
}

func deviceHasProcesses(dev nvml.Device) bool {
	if procs, ret := dev.GetComputeRunningProcesses(); ret == nvml.SUCCESS {
		if len(procs) > 0 {
			return true
		}
	} else {
		// NVML query failed; assume busy so startup does not reset in-use GPUs.
		return true
	}
	if gprocs, ret := dev.GetGraphicsRunningProcesses(); ret == nvml.SUCCESS {
		return len(gprocs) > 0
	}
	return true
}

// deviceInventory borrows the manager session for the startup scan.
func (m *MigInstanceManager) deviceInventory() (int, []string, error) {
	done, err := m.beginOperation()
	if err != nil {
		return 0, nil, err
	}
	defer done()
	count, ret := m.nvmllib.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return 0, nil, fmt.Errorf("get device count: %s", nvml.ErrorString(ret))
	}
	names := make([]string, 0, count)
	for i := 0; i < count; i++ {
		dev, ret := m.nvmllib.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			return 0, nil, fmt.Errorf("get device %d: %s", i, nvml.ErrorString(ret))
		}
		name, ret := dev.GetName()
		if ret != nvml.SUCCESS {
			return 0, nil, fmt.Errorf("get device %d name: %s", i, nvml.ErrorString(ret))
		}
		names = append(names, name)
	}
	return count, names, nil
}
