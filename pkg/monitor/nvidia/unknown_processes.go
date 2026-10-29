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

package nvidia

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/NVIDIA/go-nvml/pkg/nvml"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// UnknownProcessCount counts distinct live NVML compute/graphics PIDs that
// cannot be matched to a container allocated this GPU by HAMi. It is not a
// count of shared-memory slots, nor a claim that those PIDs are outside K8s.
type UnknownProcessCount struct {
	UUID  string
	Count int
}

// Private cgroup namespaces can expose sibling Pod paths without kubepods.
var podCgroupUID = regexp.MustCompile(`(?:^|/)(?:kubepods[^\n]*?)?pod([0-9a-fA-F]{8}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{12})(?:[/.]|$)`)
var runtimeContainerID = regexp.MustCompile(`^[0-9a-f]{64}$`)
var containerCgroupID = regexp.MustCompile(`(?:^|/)(?:(?:cri-containerd|crio|docker)-)?([0-9a-f]{64})(?:\.scope)?(?:/|\n|$)`)

type processIdentity struct{ cgroup, start string }

// readProcessIdentity brackets the cgroup read with start-time reads to detect
// PID reuse during the ownership lookup.
func readProcessIdentity(root string, pid uint32) (processIdentity, error) {
	dir := filepath.Join(root, strconv.FormatUint(uint64(pid), 10))
	readStart := func() (string, error) {
		b, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			return "", err
		}
		i := strings.LastIndexByte(string(b), ')')
		if i < 0 {
			return "", fmt.Errorf("invalid proc stat")
		}
		fields := strings.Fields(string(b[i+1:]))
		if len(fields) <= 19 {
			return "", fmt.Errorf("short proc stat")
		}
		return fields[19], nil // starttime is field 22; fields starts at field 3.
	}
	start, err := readStart()
	if err != nil {
		return processIdentity{}, err
	}
	cgroup, err := os.ReadFile(filepath.Join(dir, "cgroup"))
	if err != nil {
		return processIdentity{}, err
	}
	end, err := readStart()
	if err != nil {
		return processIdentity{}, err
	}
	if start != end {
		return processIdentity{}, fmt.Errorf("PID changed during lookup")
	}
	return processIdentity{string(cgroup), start}, nil
}

// allocatedProcessOwners uses real runtime container IDs, not Pod UID alone:
// a non-GPU sidecar in the same Pod must not hide unexpected GPU access.
func allocatedProcessOwners(pods []*corev1.Pod) (map[string]map[string]string, error) {
	owners := map[string]map[string]string{}
	for _, pod := range pods {
		if pod.Annotations[nv.AllocatedDevicesAnnotation] == "" {
			continue
		}
		allocations, err := device.DecodePodDevices(nvidiaDeviceChecklist, pod.Annotations)
		if err != nil {
			return nil, fmt.Errorf("decode GPU allocation: %w", err)
		}
		statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
		for index, devices := range allocations[nv.NvidiaGPUDevice] {
			if len(devices) == 0 {
				continue
			}
			name, ok := containerNameByIndex(pod, index)
			if !ok {
				return nil, fmt.Errorf("invalid allocated container index")
			}
			for _, status := range statuses {
				if status.Name != name || status.State.Running == nil {
					continue
				}
				_, id, ok := strings.Cut(status.ContainerID, "://")
				if !ok || !runtimeContainerID.MatchString(id) {
					continue
				}
				for _, allocated := range devices {
					if owners[allocated.UUID] == nil {
						owners[allocated.UUID] = map[string]string{}
					}
					owners[allocated.UUID][id] = string(pod.UID)
				}
			}
		}
	}
	return owners, nil
}

// gpuProcessIDs deduplicates compute and graphics PIDs; both inventories must
// succeed so an incomplete inventory cannot undercount unknown processes.
func gpuProcessIDs(dev nvml.Device) (map[uint32]struct{}, error) {
	compute, ret := dev.GetComputeRunningProcesses()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("compute process inventory: %v", ret)
	}
	graphics, ret := dev.GetGraphicsRunningProcesses()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("graphics process inventory: %v", ret)
	}
	pids := map[uint32]struct{}{}
	for _, p := range append(compute, graphics...) {
		pids[p.Pid] = struct{}{}
	}
	return pids, nil
}

// matchesAllocatedProcess requires Pod and runtime IDs from the same cgroup
// path; unrelated v1 controller entries must not be combined into an owner.
func matchesAllocatedProcess(cgroup string, owners map[string]string) bool {
	for line := range strings.SplitSeq(cgroup, "\n") {
		pod := podCgroupUID.FindStringSubmatch(line)
		container := containerCgroupID.FindStringSubmatch(line)
		if len(pod) == 2 && len(container) == 2 && owners[container[1]] != "" &&
			owners[container[1]] == strings.ReplaceAll(pod[1], "_", "-") {
			return true
		}
	}
	return false
}

// processExited includes procfs reads that race with task exit after open.
func processExited(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// countUnknownProcesses checks ownership against stable process identities.
// A missing process is ignored only after NVML confirms that it has exited.
func countUnknownProcesses(dev nvml.Device, owners map[string]string, read func(uint32) (processIdentity, error)) (int, error) {
	pids, err := gpuProcessIDs(dev)
	if err != nil {
		return 0, err
	}
	exited := func(pid uint32) error {
		current, err := gpuProcessIDs(dev)
		if err != nil {
			return err
		}
		if _, present := current[pid]; present {
			return fmt.Errorf("NVML PID is still live but not visible in procfs; host PID namespace required")
		}
		return nil
	}
	count := 0
	for pid := range pids {
		before, err := read(pid)
		if processExited(err) {
			if err := exited(pid); err != nil {
				return 0, err
			}
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("process ownership lookup: %w", err)
		}
		known := matchesAllocatedProcess(before.cgroup, owners)
		after, err := read(pid)
		if processExited(err) {
			if err := exited(pid); err != nil {
				return 0, err
			}
			continue
		}
		if err != nil {
			return 0, err
		}
		if before != after {
			return 0, fmt.Errorf("process identity changed during collection")
		}
		if !known {
			count++
		}
	}
	return count, nil
}

// UnknownProcessCounts requires host PID namespace/procfs access. Failed device
// inventories are omitted and returned as errors, never emitted as a false zero.
func (l *ContainerLister) UnknownProcessCounts() ([]UnknownProcessCount, error) {
	devices, err := l.fetchNodeDevices()
	if err != nil {
		return nil, err
	}
	devices, scopeErr := unknownProcessDevices(devices)
	if len(devices) == 0 {
		return nil, scopeErr
	}
	if l.podListerSynced == nil || !l.podListerSynced() {
		return nil, errors.Join(scopeErr, fmt.Errorf("pod inventory not synchronized"))
	}
	pods, err := l.podLister.List(labels.Everything())
	if err != nil {
		return nil, errors.Join(scopeErr, err)
	}
	owners, err := allocatedProcessOwners(pods)
	if err != nil {
		return nil, errors.Join(scopeErr, err)
	}
	lib := nvml.New()
	if ret := lib.Init(); ret != nvml.SUCCESS {
		return nil, errors.Join(scopeErr, fmt.Errorf("NVML init: %v", ret))
	}
	defer lib.Shutdown()
	counts, err := collectUnknownProcessCounts(devices, owners, lib.DeviceGetHandleByUUID, func(pid uint32) (processIdentity, error) { return readProcessIdentity("/proc", pid) })
	return counts, errors.Join(scopeErr, err)
}

// unknownProcessDevices skips known unsupported modes without reporting a
// collection failure. Missing registrations and unrecognized modes remain errors.
func unknownProcessDevices(devices map[string]*device.DeviceInfo) (map[string]*device.DeviceInfo, error) {
	supported := make(map[string]*device.DeviceInfo)
	var errs []error
	for uuid, registered := range devices {
		if registered == nil {
			errs = append(errs, fmt.Errorf("GPU %s: missing device registration", uuid))
			continue
		}
		switch registered.Mode {
		case nv.MigMode, nv.MpsMode, nv.RemoteMode:
			continue
		case "", nv.HamiCoreMode:
			// Empty mode preserves compatibility with older registrations.
		default:
			errs = append(errs, fmt.Errorf("GPU %s: unrecognized device mode %q", uuid, registered.Mode))
			continue
		}
		if strings.HasPrefix(uuid, "MIG-") {
			continue
		}
		supported[uuid] = registered
	}
	return supported, errors.Join(errs...)
}

// collectUnknownProcessCounts receives supported devices only and preserves
// successful samples when another device's NVML or procfs inventory fails.
func collectUnknownProcessCounts(devices map[string]*device.DeviceInfo, owners map[string]map[string]string, lookup func(string) (nvml.Device, nvml.Return), read func(uint32) (processIdentity, error)) ([]UnknownProcessCount, error) {
	var result []UnknownProcessCount
	var errs []error
	for uuid := range devices {
		dev, ret := lookup(uuid)
		if ret != nvml.SUCCESS {
			errs = append(errs, fmt.Errorf("GPU lookup %s: %v", uuid, ret))
			continue
		}
		count, err := countUnknownProcesses(dev, owners[uuid], read)
		if err != nil {
			errs = append(errs, fmt.Errorf("GPU %s: %w", uuid, err))
			continue
		}
		result = append(result, UnknownProcessCount{UUID: uuid, Count: count})
	}
	return result, errors.Join(errs...)
}
