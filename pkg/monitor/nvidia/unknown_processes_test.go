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
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	mock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func TestUnknownProcessInventory(t *testing.T) {
	uid := "12345678-1234-1234-1234-123456789abc"
	id := strings.Repeat("a", 64)
	known := "0::/kubepods.slice/kubepods-pod" + strings.ReplaceAll(uid, "-", "_") + ".slice/cri-containerd-" + id + ".scope\n"
	owners := map[string]string{id: uid}
	cases := []struct {
		name              string
		compute, graphics []nvml.ProcessInfo
		ret               nvml.Return
		identities        map[uint32]processIdentity
		readErr           error
		count             int
		wantErr           bool
	}{
		{name: "empty real inventory is zero", ret: nvml.SUCCESS},
		{name: "tracked Pod is not unknown", compute: []nvml.ProcessInfo{{Pid: 1}}, ret: nvml.SUCCESS, identities: map[uint32]processIdentity{1: {known, "1"}}},
		{name: "tracked Pod with relative cgroup path is not unknown", compute: []nvml.ProcessInfo{{Pid: 1}}, ret: nvml.SUCCESS, identities: map[uint32]processIdentity{1: {"0::/../../pod" + uid + "/" + id, "1"}}},
		{name: "nonallocated sidecar with relative cgroup path is unknown", compute: []nvml.ProcessInfo{{Pid: 1}}, ret: nvml.SUCCESS, identities: map[uint32]processIdentity{1: {"0::/../../pod" + uid + "/" + strings.Repeat("b", 64), "1"}}, count: 1},
		{name: "host is unknown", compute: []nvml.ProcessInfo{{Pid: 1}}, ret: nvml.SUCCESS, identities: map[uint32]processIdentity{1: {"0::/system.slice/test.service", "1"}}, count: 1},
		{name: "compute graphics duplicate counted once", compute: []nvml.ProcessInfo{{Pid: 1}}, graphics: []nvml.ProcessInfo{{Pid: 1}, {Pid: 2}}, ret: nvml.SUCCESS, count: 2},
		{name: "nonallocated sidecar is unknown", compute: []nvml.ProcessInfo{{Pid: 1}}, ret: nvml.SUCCESS, identities: map[uint32]processIdentity{1: {strings.ReplaceAll(known, id, strings.Repeat("b", 64)), "1"}}, count: 1},
		{name: "NVML failure is not zero", ret: nvml.ERROR_NO_PERMISSION, wantErr: true},
		{name: "missing proc with still-live NVML PID fails", compute: []nvml.ProcessInfo{{Pid: 1}}, ret: nvml.SUCCESS, readErr: os.ErrNotExist, wantErr: true},
		{name: "proc permission failure is not zero", compute: []nvml.ProcessInfo{{Pid: 1}}, ret: nvml.SUCCESS, readErr: os.ErrPermission, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dev := &mock.Device{GetComputeRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return tc.compute, tc.ret }, GetGraphicsRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return tc.graphics, tc.ret }}
			count, err := countUnknownProcesses(dev, owners, func(pid uint32) (processIdentity, error) { return tc.identities[pid], tc.readErr })
			if (err != nil) != tc.wantErr || count != tc.count {
				t.Fatalf("count=%d error=%v", count, err)
			}
		})
	}
}

func TestUnknownProcessPIDReuse(t *testing.T) {
	dev := &mock.Device{GetComputeRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return []nvml.ProcessInfo{{Pid: 1}}, nvml.SUCCESS }, GetGraphicsRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return nil, nvml.SUCCESS }}
	calls := 0
	_, err := countUnknownProcesses(dev, nil, func(uint32) (processIdentity, error) {
		calls++
		return processIdentity{start: strings.Repeat("x", calls)}, nil
	})
	if err == nil {
		t.Fatal("reused PID must invalidate sample")
	}
}

func TestUnknownOwnersAreDeviceAndContainerSpecific(t *testing.T) {
	id := strings.Repeat("a", 64)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID("pod-uid"), Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-a", Type: "NVIDIA"}})}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}, {Name: "sidecar"}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://" + id, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}, {Name: "sidecar", ContainerID: "containerd://" + strings.Repeat("b", 64), State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	owners, err := allocatedProcessOwners([]*corev1.Pod{pod})
	if err != nil || owners["GPU-a"][id] != "pod-uid" || len(owners["GPU-a"]) != 1 || len(owners["GPU-b"]) != 0 {
		t.Fatalf("owners=%v err=%v", owners, err)
	}
	pod.Spec.Containers = pod.Spec.Containers[:1]
	if _, err := allocatedProcessOwners([]*corev1.Pod{pod}); err != nil {
		t.Fatalf("trailing empty allocation slot: %v", err)
	}
	pod.Annotations = map[string]string{}
	owners, err = allocatedProcessOwners([]*corev1.Pod{pod})
	if err != nil || len(owners) != 0 {
		t.Fatal("Pod without HAMi allocation is not a known owner")
	}
}

func TestUnknownGraphicsFailure(t *testing.T) {
	dev := &mock.Device{GetComputeRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return nil, nvml.SUCCESS }, GetGraphicsRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return nil, nvml.ERROR_NOT_SUPPORTED }}
	_, err := countUnknownProcesses(dev, nil, func(uint32) (processIdentity, error) { return processIdentity{}, errors.New("not reached") })
	if err == nil {
		t.Fatal("incomplete process population must not become zero")
	}
}

func TestExitedNVMLProcessIsExcludedAfterRecheck(t *testing.T) {
	calls := 0
	dev := &mock.Device{
		GetComputeRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) {
			calls++
			if calls == 1 {
				return []nvml.ProcessInfo{{Pid: 42}}, nvml.SUCCESS
			}
			return nil, nvml.SUCCESS
		},
		GetGraphicsRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return nil, nvml.SUCCESS },
	}
	count, err := countUnknownProcesses(dev, nil, func(uint32) (processIdentity, error) { return processIdentity{}, os.ErrNotExist })
	if err != nil || count != 0 || calls != 2 {
		t.Fatalf("count=%d calls=%d err=%v", count, calls, err)
	}
}

func TestReadUnknownProcessIdentity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "42")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// comm may contain spaces and parentheses; starttime is still field 22.
	stat := "42 (worker (test)) S " + strings.Repeat("0 ", 18) + "12345 0\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte("0::/system.slice/test.service\n"), 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := readProcessIdentity(root, 42)
	if err != nil || identity.start != "12345" {
		t.Fatalf("identity=%v err=%v", identity, err)
	}
}

// TestUnknownProcessCgroupMatching covers host and namespace-relative paths
// while requiring both ownership IDs in the same cgroup entry.
func TestUnknownProcessCgroupMatching(t *testing.T) {
	uid := "12345678-1234-1234-1234-123456789abc"
	id := strings.Repeat("a", 64)
	owners := map[string]string{id: uid}
	for _, tc := range []struct {
		name, path string
		known      bool
	}{
		{"systemd containerd", "0::/kubepods.slice/kubepods-burstable-pod" + strings.ReplaceAll(uid, "-", "_") + ".slice/cri-containerd-" + id + ".scope", true},
		{"cgroupfs v1", "9:memory:/kubepods/burstable/pod" + uid + "/" + id, true},
		{"systemd CRI-O", "0::/kubepods.slice/kubepods-pod" + strings.ReplaceAll(uid, "-", "_") + ".slice/crio-" + id + ".scope", true},
		{"relative cgroupfs v2", "0::/../../pod" + uid + "/" + id, true},
		{"relative cgroupfs v1", "9:memory:/../pod" + uid + "/" + id, true},
		{"relative QoS path", "0::/../../burstable/pod" + uid + "/" + id, true},
		{"Pod at namespace root", "0::/pod" + uid + "/" + id, true},
		{"relative systemd path", "0::/../../kubepods-burstable-pod" + strings.ReplaceAll(uid, "-", "_") + ".slice/cri-containerd-" + id + ".scope", true},
		{"relative Docker child cgroup", "0::/../pod" + uid + "/docker-" + id + ".scope/worker", true},
		{"different Pod", "0::/../../pod00000000-0000-0000-0000-000000000000/" + id, false},
		{"different container", "0::/../../pod" + uid + "/" + strings.Repeat("b", 64), false},
		{"container ID alone", "0::/system.slice/docker-" + id + ".scope", false},
		{"Pod UID alone", "0::/../../pod" + uid, false},
		{"Pod name embedded in component", "0::/../../notpod" + uid + "/" + id, false},
		{"Pod UID with extra suffix", "0::/../../pod" + uid + "a/" + id, false},
		{"different v1 controllers", "9:memory:/kubepods/pod" + uid + "/" + strings.Repeat("b", 64) + "\n1:cpu:/docker/" + id, false},
		{"different relative v1 controllers", "9:memory:/../../pod" + uid + "/" + strings.Repeat("b", 64) + "\n1:cpu:/../../pod00000000-0000-0000-0000-000000000000/" + id, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesAllocatedProcess(tc.path, owners); got != tc.known {
				t.Fatalf("known=%v want=%v for %s", got, tc.known, tc.path)
			}
		})
	}
}

// TestProcessExitRaces covers errors returned after opening a disappearing proc
// file, on either identity read, and never discards a PID still listed by NVML.
func TestProcessExitRaces(t *testing.T) {
	for _, exitErr := range []error{os.ErrNotExist, syscall.ESRCH} {
		for _, failAt := range []int{1, 2} {
			for _, stillLive := range []bool{false, true} {
				inventoryCalls, reads := 0, 0
				dev := &mock.Device{
					GetComputeRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) {
						inventoryCalls++
						if inventoryCalls == 1 || stillLive {
							return []nvml.ProcessInfo{{Pid: 42}}, nvml.SUCCESS
						}
						return nil, nvml.SUCCESS
					},
					GetGraphicsRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return nil, nvml.SUCCESS },
				}
				count, err := countUnknownProcesses(dev, nil, func(uint32) (processIdentity, error) {
					reads++
					if reads == failAt {
						return processIdentity{}, &os.PathError{Op: "read", Path: "/proc/42/stat", Err: exitErr}
					}
					return processIdentity{cgroup: "0::/system.slice/test.service", start: "1"}, nil
				})
				if (err != nil) != stillLive || count != 0 || inventoryCalls != 2 {
					t.Fatalf("exit=%v failAt=%d stillLive=%v: count=%d calls=%d err=%v", exitErr, failAt, stillLive, count, inventoryCalls, err)
				}
			}
		}
	}
}

// TestUnknownOwnershipDuringStatusTransitions documents snapshot skew rather
// than treating a Pod UID alone as permission to use any GPU from any sidecar.
func TestUnknownOwnershipDuringStatusTransitions(t *testing.T) {
	uid := "12345678-1234-1234-1234-123456789abc"
	oldID, newID := strings.Repeat("a", 64), strings.Repeat("b", 64)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID(uid), Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-a", Type: "NVIDIA"}})}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}}
	for _, tc := range []struct {
		name, statusID, processID string
		running, known            bool
	}{
		{"before status arrives", "", newID, false, false},
		{"running", oldID, oldID, true, true},
		{"new process before restart status arrives", oldID, newID, true, false},
		{"restart status caught up", newID, newID, true, true},
		{"old process is not new owner", newID, oldID, true, false},
		{"terminated status", newID, newID, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := corev1.ContainerState{}
			if tc.running {
				state.Running = &corev1.ContainerStateRunning{}
			}
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://" + tc.statusID, State: state}}
			owners, err := allocatedProcessOwners([]*corev1.Pod{pod})
			if err != nil {
				t.Fatal(err)
			}
			cg := "0::/kubepods/pod" + uid + "/" + tc.processID
			if got := matchesAllocatedProcess(cg, owners["GPU-a"]); got != tc.known {
				t.Fatalf("known=%v want=%v", got, tc.known)
			}
		})
	}
}

// TestUnknownDeviceScopeAndPartialFailure separates unsupported devices from
// real failures; neither may fabricate a zero or discard a successful sibling.
func TestUnknownDeviceScopeAndPartialFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		extra   map[string]*device.DeviceInfo
		wantErr string
		lookups int
	}{
		{name: "unsupported siblings are skipped", lookups: 2},
		{name: "lookup failure", extra: map[string]*device.DeviceInfo{"GPU-failed": {}}, wantErr: "GPU lookup GPU-failed", lookups: 3},
		{name: "inventory failure", extra: map[string]*device.DeviceInfo{"GPU-no-permission": {}}, wantErr: "compute process inventory", lookups: 3},
		{name: "missing registration", extra: map[string]*device.DeviceInfo{"GPU-nil": nil}, wantErr: "missing device registration", lookups: 2},
		{name: "unrecognized mode", extra: map[string]*device.DeviceInfo{"GPU-typo": {Mode: "hami-cor"}}, wantErr: "unrecognized device mode", lookups: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			devices := map[string]*device.DeviceInfo{
				"GPU-ok": {Mode: nv.HamiCoreMode}, "GPU-legacy": {},
				"GPU-mig-parent": {Mode: nv.MigMode}, "MIG-instance": {},
				"GPU-remote": {Mode: nv.RemoteMode}, "GPU-mps": {Mode: nv.MpsMode},
			}
			maps.Copy(devices, tc.extra)
			devices, scopeErr := unknownProcessDevices(devices)
			calls := 0
			counts, err := collectUnknownProcessCounts(devices, nil, func(uuid string) (nvml.Device, nvml.Return) {
				calls++
				if uuid == "GPU-failed" {
					return nil, nvml.ERROR_NOT_FOUND
				}
				if uuid != "GPU-ok" && uuid != "GPU-legacy" && uuid != "GPU-no-permission" {
					t.Fatalf("unsupported lookup: %s", uuid)
				}
				return &mock.Device{
					GetComputeRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) {
						if uuid == "GPU-no-permission" {
							return nil, nvml.ERROR_NO_PERMISSION
						}
						return nil, nvml.SUCCESS
					},
					GetGraphicsRunningProcessesFunc: func() ([]nvml.ProcessInfo, nvml.Return) { return nil, nvml.SUCCESS },
				}, nvml.SUCCESS
			}, func(uint32) (processIdentity, error) {
				t.Fatal("empty inventory read procfs")
				return processIdentity{}, nil
			})
			err = errors.Join(scopeErr, err)
			if (err != nil) != (tc.wantErr != "") || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) || len(counts) != 2 || calls != tc.lookups {
				t.Fatalf("counts=%v calls=%d err=%v", counts, calls, err)
			}
			for _, c := range counts {
				if c.Count != 0 || (c.UUID != "GPU-ok" && c.UUID != "GPU-legacy") {
					t.Fatalf("unexpected sample: %v", c)
				}
			}
		})
	}
}

// TestUnknownProcessCountsWithoutSupportedDevices exercises the public entry
// point without a Pod inventory or NVML: there is no process work to perform.
func TestUnknownProcessCountsWithoutSupportedDevices(t *testing.T) {
	for _, tc := range []struct {
		name    string
		devices []*device.DeviceInfo
		wantErr bool
	}{
		{name: "no devices"},
		{name: "MIG parent", devices: []*device.DeviceInfo{{ID: "GPU-mig", Mode: nv.MigMode}}},
		{name: "MIG instance", devices: []*device.DeviceInfo{{ID: "MIG-instance"}}},
		{name: "MPS", devices: []*device.DeviceInfo{{ID: "GPU-mps", Mode: nv.MpsMode}}},
		{name: "remote", devices: []*device.DeviceInfo{{ID: "GPU-remote", Mode: nv.RemoteMode}}},
		{name: "unrecognized mode", devices: []*device.DeviceInfo{{ID: "GPU-typo", Mode: "hami-cor"}}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := serveNodeForTest(t, http.StatusOK, map[string]string{nv.RegisterAnnos: device.MarshalNodeDevices(tc.devices)})
			l.podListerSynced = func() bool {
				t.Fatal("no supported devices should not require a Pod inventory")
				return false
			}
			counts, err := l.UnknownProcessCounts()
			if (err != nil) != tc.wantErr || len(counts) != 0 {
				t.Fatalf("counts=%v err=%v", counts, err)
			}
		})
	}
}
