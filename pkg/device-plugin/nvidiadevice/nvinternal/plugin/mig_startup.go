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
	"net"
	"sort"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// Tunables for the one-shot startup poll of kubelet's pod-resources API.
// Separate from the long-lived watcher's dialWait/listTimeout because the
// startup path runs before kubelet is known to have seeded pod state, so
// falling back to NVML-only detection is acceptable.
const (
	migStartupPodresourcesSocket = "/var/lib/kubelet/pod-resources/kubelet.sock"
	migStartupDialWait           = 5 * time.Second
	migStartupListTimeout        = 10 * time.Second
	migStartupMaxMsgSize         = 16 * 1024 * 1024
)

func normalizeUnixDialAddr(addr string) string {
	trimmed := strings.TrimPrefix(addr, "unix://")
	if trimmed != addr {
		return trimmed
	}
	return strings.TrimPrefix(addr, "unix:")
}

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

// resetIdleMigGPUs edits the per-device MIG spec in place: GPUs that show no
// sign of in-use compute are returned to "MIG-on with no partitions" so the
// on-demand migMgr path can reshape them per request without destroying live
// GIs on busy cards. Returns the set of GPU indexes that were reset.
//
// cfg is expected to already be in per-device form (one MigConfigSpec per
// Devices=[i]); this is how processMigConfigs arranges it.
func resetIdleMigGPUs(cfg nvidia.MigConfigSpecSlice, inUse map[int]struct{}) []int {
	reset := []int{}
	for i := range cfg {
		devs := cfg[i].Devices
		if len(devs) == 0 {
			continue
		}
		gpu := int(devs[0])
		if _, busy := inUse[gpu]; busy {
			continue
		}
		cfg[i].MigEnabled = true
		cfg[i].MigDevices = map[string]int32{}
		reset = append(reset, gpu)
	}
	sort.Ints(reset)
	return reset
}

// collectInUseGPUs returns the set of GPU indexes that have at least one
// in-use MIG instance, unioned from two best-effort sources:
//   - kubelet's pod-resources List (authoritative for k8s-managed usage).
//   - NVML running processes on each MIG instance or the parent card (catches
//     usage that bypasses kubelet, e.g. bare processes on the node).
//
// Failures in either source are logged and downgrade to the other source.
// When both fail, an empty set is returned and the caller treats every GPU
// as idle; the very first apply after a failed detection window will
// reshape cards conservatively because idle means "MIG on, no partitions".
func collectInUseGPUs(ctx context.Context, resourceName, nodeName string) (map[int]struct{}, error) {
	out := make(map[int]struct{})

	if uuids, err := listPodResourcesMigUUIDs(ctx, resourceName); err != nil {
		klog.InfoS("mig init: pod-resources List skipped", "err", err)
	} else {
		for uuid := range uuids {
			if gpu, ok := migUUIDToGPUIndex(uuid); ok {
				out[gpu] = struct{}{}
			}
		}
	}

	annotated, err := kubernetesAllocatedMigGPUs(ctx, nodeName)
	if err != nil {
		return out, fmt.Errorf("list Kubernetes MIG allocations: %w", err)
	}
	for g := range annotated {
		out[g] = struct{}{}
	}

	if busy, err := nvmlBusyGPUs(); err != nil {
		klog.InfoS("mig init: NVML busy-GPU detection skipped", "err", err)
	} else {
		for g := range busy {
			out[g] = struct{}{}
		}
	}

	return out, nil
}

// activeMigGPUUUIDs returns physical GPU UUIDs referenced by live HAMi MIG
// allocations. HAMi exposes virtual resource IDs to kubelet and passes the
// dynamically created MIG UUID through NVIDIA_VISIBLE_DEVICES, so kubelet's
// pod-resources API cannot by itself identify these allocations after a
// device-plugin restart.
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

func kubernetesAllocatedMigGPUs(ctx context.Context, nodeName string) (map[int]struct{}, error) {
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
		idx, ok := gpuUUIDToIndex(gpuUUID)
		if !ok {
			return nil, fmt.Errorf("resolve GPU UUID %s", gpuUUID)
		}
		out[idx] = struct{}{}
	}
	return out, nil
}

func gpuUUIDToIndex(gpuUUID string) (int, bool) {
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		return 0, false
	}
	dev, ret := nvml.DeviceGetHandleByUUID(gpuUUID)
	if ret != nvml.SUCCESS {
		return 0, false
	}
	idx, ret := dev.GetIndex()
	return idx, ret == nvml.SUCCESS
}

// listPodResourcesMigUUIDs issues a single List on the kubelet pod-resources
// API and returns the set of MIG device IDs currently attached to containers
// under the given resource name. The call uses bounded dial and RPC timeouts
// so a kubelet that isn't accepting yet doesn't block plugin startup.
func listPodResourcesMigUUIDs(ctx context.Context, resourceName string) (map[string]struct{}, error) {
	conn, err := grpc.NewClient(
		"unix://"+migStartupPodresourcesSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(migStartupMaxMsgSize)),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", normalizeUnixDialAddr(addr))
		}),
	)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.Connect()
	dialCtx, cancelDial := context.WithTimeout(ctx, migStartupDialWait)
	defer cancelDial()
	for {
		s := conn.GetState()
		if s == connectivity.Ready {
			break
		}
		if !conn.WaitForStateChange(dialCtx, s) {
			return nil, fmt.Errorf("pod-resources dial: %w", dialCtx.Err())
		}
	}

	listCtx, cancelList := context.WithTimeout(ctx, migStartupListTimeout)
	defer cancelList()
	cl := podresourcesv1.NewPodResourcesListerClient(conn)
	resp, err := cl.List(listCtx, &podresourcesv1.ListPodResourcesRequest{})
	if err != nil {
		return nil, err
	}

	out := make(map[string]struct{})
	for _, pod := range resp.GetPodResources() {
		for _, c := range pod.GetContainers() {
			for _, d := range c.GetDevices() {
				if !strings.EqualFold(d.GetResourceName(), resourceName) {
					continue
				}
				for _, id := range d.GetDeviceIds() {
					if strings.HasPrefix(id, "MIG-") {
						out[id] = struct{}{}
					}
				}
			}
		}
	}
	return out, nil
}

// migUUIDToGPUIndex resolves a MIG device UUID to its parent GPU's NVML
// index. Missing MIG UUIDs (e.g. stale kubelet state) return false so the
// caller skips them rather than mis-attributing to GPU 0.
func migUUIDToGPUIndex(migUUID string) (int, bool) {
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.InfoS("mig init: nvml.Init failed", "err", nvml.ErrorString(nvret))
		return 0, false
	}
	migDev, ret := nvml.DeviceGetHandleByUUID(migUUID)
	if ret != nvml.SUCCESS {
		return 0, false
	}
	parent, ret := nvml.DeviceGetDeviceHandleFromMigDeviceHandle(migDev)
	if ret != nvml.SUCCESS {
		return 0, false
	}
	idx, ret := parent.GetIndex()
	if ret != nvml.SUCCESS {
		return 0, false
	}
	return idx, true
}

// nvmlBusyGPUs returns the set of GPU indexes with at least one running
// compute or graphics process. For MIG-enabled cards every live MIG instance
// is inspected; for non-MIG cards the parent device is inspected directly.
func nvmlBusyGPUs() (map[int]struct{}, error) {
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		return nil, fmt.Errorf("nvml Init: %s", nvml.ErrorString(nvret))
	}
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("DeviceGetCount: %s", nvml.ErrorString(ret))
	}

	out := make(map[int]struct{})
	for i := 0; i < count; i++ {
		dev, ret := nvml.DeviceGetHandleByIndex(i)
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
