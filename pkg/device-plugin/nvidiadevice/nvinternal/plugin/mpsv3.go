// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"golang.org/x/sys/unix"
	cdspec "tags.cncf.io/container-device-interface/specs-go"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cudainfo"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const mpsV3ContainerPipe = "/run/hami-mps"

type mpsV3Allocation struct {
	PodUID     string            `json:"podUID"`
	Container  string            `json:"container"`
	GPUs       []string          `json:"gpus"`
	Memory     int64             `json:"memoryMiB"`
	Percent    int64             `json:"percent"`
	SMCount    int64             `json:"smCount,omitempty"`
	Partitions map[string]string `json:"partitions,omitempty"`
	Prepared   bool              `json:"prepared"`
	Server     string            `json:"server"`
	Namespace  string            `json:"namespace"`
}

type mpsV3Manager struct {
	mu          sync.Mutex
	root        string
	binary      string
	cdiRoot     string
	lock        *os.File
	run         func(context.Context, ...string) (string, error)
	allocations map[string]mpsV3Allocation
	geometry    map[string]nvidia.MPSV3Geometry
	healthy     atomic.Bool
}

func newMPSV3Manager() *mpsV3Manager {
	root := os.Getenv("HAMI_MPS_V3_ROOT")
	if root == "" {
		root = "/var/run/hami/mps-v3"
	}
	binary := os.Getenv("HAMI_MPS_V3_BINARY")
	if binary == "" {
		binary = "nvidia-cuda-mps-control"
	}
	m := &mpsV3Manager{root: root, binary: binary, cdiRoot: "/var/run/cdi", allocations: make(map[string]mpsV3Allocation)}
	m.run = func(ctx context.Context, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, m.binary, append([]string{"-p", "3"}, args...)...)
		env := os.Environ()
		for _, key := range []string{"CUDA_MPS_PROTOCOL_VERSION", "CUDA_MPS_PIPE_DIRECTORY", "CUDA_MPS_LOG_DIRECTORY", "CUDA_VISIBLE_DEVICES", "CUDA_MPS_ACTIVE_THREAD_PERCENTAGE", "CUDA_MPS_SM_PARTITION", "CUDA_MPS_PINNED_DEVICE_MEM_LIMIT"} {
			env = slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, key+"=") })
		}
		cmd.Env = append(env, "CUDA_MPS_PROTOCOL_VERSION=3", "CUDA_MPS_PIPE_DIRECTORY="+filepath.Join(m.root, "pipe"), "CUDA_MPS_LOG_DIRECTORY="+filepath.Join(m.root, "log"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("MPS v3 %v: %w: %s", args, err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	return m
}

func mpsV3Name(prefix, key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s_%x", prefix, sum[:12])
}
func (m *mpsV3Manager) statePath() string { return filepath.Join(m.root, "allocations.json") }
func atomicMPSFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".mps-v3-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (m *mpsV3Manager) save() error {
	data, err := json.Marshal(m.allocations)
	if err != nil {
		return err
	}
	return atomicMPSFile(m.statePath(), data, 0600)
}
func (m *mpsV3Manager) load() error {
	data, err := os.ReadFile(m.statePath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved map[string]mpsV3Allocation
	if err = json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("read MPS allocation state: %w", err)
	}
	for key, a := range saved {
		if key != a.Namespace || a.PodUID == "" || a.Container == "" || a.Namespace != mpsV3Name("ns", a.PodUID+"/"+a.Container) || len(a.GPUs) == 0 || !slices.IsSorted(a.GPUs) || a.Server != mpsV3Name("srv", strings.Join(a.GPUs, ",")) || a.Memory <= 0 || a.Percent < 1 || a.Percent > 100 || a.SMCount < 0 {
			return fmt.Errorf("invalid MPS allocation state for %q", key)
		}
		for i, id := range a.GPUs {
			if !validMPSGPU(id) || i > 0 && id == a.GPUs[i-1] {
				return fmt.Errorf("invalid GPU in MPS state")
			}
		}
	}
	if saved != nil {
		m.allocations = saved
	}
	return nil
}
func (m *mpsV3Manager) close() {
	if m.lock != nil {
		_ = m.lock.Close()
		m.lock = nil
	}
}
func (m *mpsV3Manager) start(ctx context.Context, live map[string]bool) (resultErr error) {
	if !filepath.IsAbs(m.root) || filepath.Clean(m.root) != m.root || m.root == "/" {
		return fmt.Errorf("MPS root must be an absolute clean dedicated host path")
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return fmt.Errorf("mps-v3 requires cgroup v2: %w", err)
	}
	for _, p := range []string{m.root, filepath.Join(m.root, "pipe"), filepath.Join(m.root, "log"), m.cdiRoot} {
		if err := os.MkdirAll(p, 0755); err != nil {
			return err
		}
	}
	var err error
	m.lock, err = os.OpenFile(filepath.Join(m.root, "manager.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			m.close()
		}
	}()
	if err = unix.Flock(int(m.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("another MPS manager owns %s: %w", m.root, err)
	}
	if err = m.load(); err != nil {
		return err
	}
	if _, err = m.run(ctx, "server", "list"); err != nil {
		// A missing daemon cannot retain CUDA contexts. Only recover automatically
		// when the authoritative API confirms no recorded Pod is still live.
		for _, a := range m.allocations {
			if live[a.PodUID] {
				return fmt.Errorf("MPS daemon lost with live allocations; restart affected workloads: %w", err)
			}
		}
		for _, a := range m.allocations {
			if err = m.removeCDI(a); err != nil {
				return err
			}
		}
		m.allocations = make(map[string]mpsV3Allocation)
		if err = m.save(); err != nil {
			return err
		}
		if _, err = m.run(ctx, "-d", "-m"); err != nil {
			return err
		}
	}
	if err = m.wait(ctx, func() error { _, err := m.run(ctx, "namespace", "list"); return err }); err != nil {
		return err
	}
	if _, err = m.run(ctx, "context-share", "set", "default_socket=off"); err != nil {
		return err
	}
	for key, a := range m.allocations {
		if !live[a.PodUID] || !a.Prepared {
			if err = m.release(ctx, key); err != nil {
				return fmt.Errorf("recover incomplete MPS allocation: %w", err)
			}
			continue
		}
		if err = m.check(ctx, a); err != nil {
			return fmt.Errorf("recover MPS allocation: %w", err)
		}
		if err = m.publish(a); err != nil {
			return err
		}
	}
	m.healthy.Store(true)
	return nil
}
func (m *mpsV3Manager) wait(ctx context.Context, check func() error) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		err := check()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return err
		case <-tick.C:
		}
	}
}
func (m *mpsV3Manager) serverReady(ctx context.Context, server string) error {
	return m.wait(ctx, func() error {
		status, err := m.run(ctx, "server", "get", server, "status")
		if err != nil {
			return err
		}
		if status != "Ready" {
			return fmt.Errorf("MPS server %s status %q", server, status)
		}
		return nil
	})
}
func (m *mpsV3Manager) check(ctx context.Context, a mpsV3Allocation) error {
	status, err := m.run(ctx, "server", "get", a.Server, "status")
	if err != nil {
		return err
	}
	if status != "Ready" {
		return fmt.Errorf("MPS server %s is %s", a.Server, status)
	}
	allowed, err := m.run(ctx, "server", "get", a.Server, "allowed-devices")
	if err != nil {
		return err
	}
	ids := strings.Split(allowed, ",")
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
	}
	slices.Sort(ids)
	if !slices.Equal(ids, a.GPUs) {
		return fmt.Errorf("MPS server GPU set changed")
	}
	out, err := m.run(ctx, "namespace", "get", a.Namespace, a.Server, "active-thread-percentage")
	if err != nil {
		return err
	}
	want := a.Percent
	if a.SMCount > 0 {
		want = 100
	}
	p, err := strconv.ParseFloat(out, 64)
	if err != nil || p != float64(want) {
		return fmt.Errorf("MPS namespace %s has unexpected compute limit %q", a.Namespace, out)
	}
	if a.SMCount > 0 {
		for _, gpu := range a.GPUs {
			part, err := m.partition(ctx, a, gpu)
			if err != nil {
				return err
			}
			if part.ID == "" || part.ID != a.Partitions[gpu] || part.SMs != a.SMCount {
				return fmt.Errorf("MPS static partition changed for %s", gpu)
			}
		}
	}
	info, err := os.Stat(filepath.Join(m.root, "pipe", a.Server, a.Namespace, "control"))
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("MPS namespace socket missing")
	}
	return nil
}
func sameMPSRequest(a, b mpsV3Allocation) bool {
	a.Prepared = false
	b.Prepared = false
	a.Partitions = nil
	b.Partitions = nil
	return reflect.DeepEqual(a, b)
}
func (m *mpsV3Manager) ensure(ctx context.Context, a mpsV3Allocation) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.healthy.Load() {
		return false, fmt.Errorf("MPS v3 is unhealthy")
	}
	if old, ok := m.allocations[a.Namespace]; ok {
		if !sameMPSRequest(old, a) {
			return false, fmt.Errorf("MPS allocation identity reused with different limits")
		}
		if !old.Prepared {
			return false, fmt.Errorf("previous MPS allocation is incomplete; waiting for cleanup")
		}
		if err := m.check(ctx, old); err != nil {
			return false, err
		}
		return false, m.publish(old)
	}
	for _, other := range m.allocations {
		for _, gpu := range a.GPUs {
			if slices.Contains(other.GPUs, gpu) {
				if (a.SMCount > 0) != (other.SMCount > 0) {
					return false, fmt.Errorf("cannot mix static SM and percentage sharing on GPU %s", gpu)
				}
				if a.Server != other.Server {
					return false, fmt.Errorf("GPU %s already belongs to another GPU set", gpu)
				}
			}
		}
	}
	if len(filepath.Join(m.root, "pipe", a.Server, a.Namespace, "control")) >= 104 {
		return false, fmt.Errorf("MPS namespace socket path too long; shorten HAMI_MPS_V3_ROOT")
	}
	if a.SMCount > 0 {
		for _, gpu := range a.GPUs {
			g := m.geometry[gpu]
			charge, err := g.Percent(a.SMCount)
			if err != nil {
				return false, err
			}
			if charge != a.Percent {
				return false, fmt.Errorf("SM allocation has incorrect scheduler compute charge")
			}
		}
	}
	// Persist intent before any external mutation. Rollback derives all resource
	// names from this record, including partitions created just before a crash.
	m.allocations[a.Namespace] = a
	if err := m.save(); err != nil {
		delete(m.allocations, a.Namespace)
		return false, err
	}
	exists, err := m.serverExists(ctx, a.Server)
	if err != nil {
		return true, err
	}
	if !exists {
		if _, err = m.run(ctx, "server", "create", a.Server, "--uid=0", "--locality-domains=false", "--allowed-devices="+strings.Join(a.GPUs, ",")); err != nil {
			return true, err
		}
	}
	if err = m.serverReady(ctx, a.Server); err != nil {
		return true, err
	}
	if _, err = m.run(ctx, "namespace", "create", a.Namespace, "--server="+a.Server); err != nil {
		return true, err
	}
	percent := a.Percent
	if a.SMCount > 0 {
		percent = 100
	}
	if _, err = m.run(ctx, "namespace", "set", a.Namespace, "--server="+a.Server, "--active-thread-percentage="+strconv.FormatInt(percent, 10)); err != nil {
		return true, err
	}
	if a.SMCount > 0 {
		a.Partitions = make(map[string]string)
		for _, gpu := range a.GPUs {
			chunks, remainder, _ := m.geometry[gpu].Partition(a.SMCount)
			if _, err = m.run(ctx, "sm-partition", "create", m.partitionName(a, gpu), "--server="+a.Server, "--device="+gpu, "--chunks="+strconv.FormatInt(chunks, 10), "--include-remainder="+strconv.FormatBool(remainder)); err != nil {
				return true, err
			}
			part, err := m.partition(ctx, a, gpu)
			if err != nil {
				return true, err
			}
			if part.SMs != a.SMCount || part.ID == "" {
				return true, fmt.Errorf("driver returned a different SM partition size")
			}
			a.Partitions[gpu] = part.ID
		}
	}
	if err = m.wait(ctx, func() error { return m.check(ctx, a) }); err != nil {
		return true, err
	}
	a.Prepared = true
	m.allocations[a.Namespace] = a
	if err = m.publish(a); err != nil {
		return true, err
	}
	if err = m.save(); err != nil {
		return true, err
	}
	return true, nil
}
func (m *mpsV3Manager) serverExists(ctx context.Context, name string) (bool, error) {
	out, err := m.run(ctx, "server", "list", "--format=csv,noheader")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(strings.Split(line, ",")[0]) == name {
			return true, nil
		}
	}
	return false, nil
}

type mpsV3Partition struct {
	ID  string
	SMs int64
}

func (m *mpsV3Manager) partitionName(a mpsV3Allocation, gpu string) string {
	return mpsV3Name("sm", a.Namespace+"/"+gpu)
}
func (m *mpsV3Manager) partition(ctx context.Context, a mpsV3Allocation, gpu string) (mpsV3Partition, error) {
	out, err := m.run(ctx, "sm-partition", "list", "--server="+a.Server, "--device="+gpu, "--format=csv,noheader")
	if err != nil {
		return mpsV3Partition{}, err
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		return mpsV3Partition{}, err
	}
	for _, row := range rows {
		if len(row) >= 8 && strings.TrimSpace(row[0]) == a.Server && strings.TrimSpace(row[1]) == gpu && strings.TrimSpace(row[2]) == m.partitionName(a, gpu) {
			sms, err := strconv.ParseInt(strings.TrimSpace(row[4]), 10, 64)
			if err != nil {
				return mpsV3Partition{}, err
			}
			if strings.TrimSpace(row[6]) != "created" {
				return mpsV3Partition{}, fmt.Errorf("SM partition is not live")
			}
			return mpsV3Partition{ID: strings.TrimSpace(row[7]), SMs: sms}, nil
		}
	}
	return mpsV3Partition{}, nil
}
func (m *mpsV3Manager) release(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.allocations[key]
	if !ok {
		return nil
	}
	exists, err := m.serverExists(ctx, a.Server)
	if err != nil {
		return err
	}
	if exists {
		out, err := m.run(ctx, "client", "list", "--server="+a.Server, "--format=csv,noheader")
		if err != nil {
			return err
		}
		if strings.Contains(out, a.Namespace) {
			return fmt.Errorf("MPS namespace %s still has active clients", a.Namespace)
		}
		// Older drivers may not include namespace names in CSV. If there is
		// output but no namespace routing column, preserve the server's state.
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, ",") && !strings.Contains(line, a.Server+"/") {
				return fmt.Errorf("cannot confirm client namespace ownership")
			}
		}
		// Deletion is never forced. A live CUDA client makes cleanup retry later.
		if a.SMCount > 0 {
			for _, gpu := range a.GPUs {
				part, err := m.partition(ctx, a, gpu)
				if err != nil {
					return err
				}
				if part.ID != "" {
					if _, err = m.run(ctx, "sm-partition", "delete", m.partitionName(a, gpu), "--server="+a.Server, "--device="+gpu); err != nil {
						return err
					}
				}
			}
		}
		out, err = m.run(ctx, "namespace", "list", "--server="+a.Server, "--format=csv,noheader")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(strings.Split(line, ",")[0]) == a.Namespace {
				if _, err = m.run(ctx, "namespace", "delete", a.Namespace, "--server="+a.Server); err != nil {
					return err
				}
				break
			}
		}
		last := true
		for key, other := range m.allocations {
			if key != a.Namespace && other.Server == a.Server {
				last = false
			}
		}
		if last {
			if _, err = m.run(ctx, "server", "delete", a.Server); err != nil {
				return err
			}
		}
	}
	if err = m.removeCDI(a); err != nil {
		return err
	}
	delete(m.allocations, key)
	if err = m.save(); err != nil {
		m.allocations[key] = a
	}
	return err
}
func (m *mpsV3Manager) cdiPath(key string) string {
	return filepath.Join(m.cdiRoot, "hami-mps-v3-"+key+".json")
}
func (m *mpsV3Manager) removeCDI(a mpsV3Allocation) error {
	data, err := os.ReadFile(m.cdiPath(a.Namespace))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var spec cdspec.Spec
	if json.Unmarshal(data, &spec) != nil || spec.Kind != "hami.io/mps-v3" || len(spec.Devices) != 1 || spec.Devices[0].Name != a.Namespace {
		return fmt.Errorf("refusing to remove unowned MPS CDI file %s", a.Namespace)
	}
	return os.Remove(m.cdiPath(a.Namespace))
}
func (m *mpsV3Manager) allocationEnvs(a mpsV3Allocation) map[string]string {
	env := map[string]string{"NVIDIA_GPU_MEMORY_REQUEST": strconv.FormatInt(a.Memory, 10), "NVIDIA_GPU_MEMORY_LIMIT": strconv.FormatInt(a.Memory, 10), "CUDA_MPS_PIPE_DIRECTORY": mpsV3ContainerPipe}
	if a.SMCount > 0 {
		var ids []string
		for _, gpu := range a.GPUs {
			ids = append(ids, gpu+"/"+a.Partitions[gpu])
		}
		env["CUDA_MPS_SM_PARTITION"] = strings.Join(ids, ",")
	}
	return env
}
func (m *mpsV3Manager) publish(a mpsV3Allocation) error {
	env := m.allocationEnvs(a)
	var edits []string
	for key, val := range env {
		edits = append(edits, key+"="+val)
	}
	slices.Sort(edits)
	spec := cdspec.Spec{Version: "0.6.0", Kind: "hami.io/mps-v3", Devices: []cdspec.Device{{Name: a.Namespace, ContainerEdits: cdspec.ContainerEdits{Env: edits}}}}
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(m.cdiPath(a.Namespace)); err == nil {
		if string(old) == string(data) {
			return nil
		}
		return fmt.Errorf("refusing to replace mismatched MPS CDI allocation %s", a.Namespace)
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicMPSFile(m.cdiPath(a.Namespace), data, 0644)
}
func (m *mpsV3Manager) deviceInfo(gpu string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := map[string]any{nvidia.MPSV3GeometryKey: m.geometry[gpu]}
	for _, a := range m.allocations {
		if slices.Contains(a.GPUs, gpu) {
			mode := "percentage"
			if a.SMCount > 0 {
				mode = "sm"
			}
			info[nvidia.MPSV3ModeKey] = mode
			info[nvidia.MPSV3GPUSetKey] = slices.Clone(a.GPUs)
			break
		}
	}
	return info
}
func validMPSGPU(id string) bool {
	return strings.HasPrefix(id, "GPU-") && !strings.ContainsAny(id, "/, \t\n\r")
}

func mpsV3AllocationFor(pod *corev1.Pod, ctr *corev1.Container, devs device.ContainerDevices) (mpsV3Allocation, error) {
	a := mpsV3Allocation{}
	if pod == nil || pod.UID == "" || ctr == nil || ctr.Name == "" || len(devs) == 0 {
		return a, fmt.Errorf("MPS allocation requires Pod UID, container and devices")
	}
	a.PodUID = string(pod.UID)
	a.Container = ctr.Name
	a.Memory = devs[0].Usedmem
	var err error
	a.SMCount, err = nvidia.MPSV3RequestedSMs(pod.Annotations)
	if err != nil {
		return a, err
	}
	a.Percent = devs[0].Usedcores
	if a.Percent == 0 {
		a.Percent = 100
	}
	if a.Memory <= 0 || a.Percent < 1 || a.Percent > 100 {
		return a, fmt.Errorf("invalid MPS memory or compute limit")
	}
	for _, d := range devs {
		p := d.Usedcores
		if p == 0 {
			p = 100
		}
		if d.Usedmem != a.Memory || p != a.Percent {
			return a, fmt.Errorf("mps-v3 requires equal memory and compute limits on every GPU")
		}
		if !validMPSGPU(d.UUID) || slices.Contains(a.GPUs, d.UUID) {
			return a, fmt.Errorf("invalid or duplicate physical GPU %q", d.UUID)
		}
		a.GPUs = append(a.GPUs, d.UUID)
	}
	slices.Sort(a.GPUs)
	a.Server = mpsV3Name("srv", strings.Join(a.GPUs, ","))
	a.Namespace = mpsV3Name("ns", a.PodUID+"/"+a.Container)
	// Reserved env vars must not shadow limits read by the privileged OCI hook.
	for _, e := range ctr.Env {
		if strings.HasPrefix(e.Name, "NVIDIA_GPU_MEMORY_") || strings.HasPrefix(e.Name, "CUDA_MPS_") {
			return a, fmt.Errorf("container sets reserved MPS environment variable %s", e.Name)
		}
	}

	return a, nil
}

func (plugin *NvidiaDevicePlugin) allocateMPSV3(ctx context.Context, pod *corev1.Pod, ctr *corev1.Container, devs device.ContainerDevices, response *kubeletdevicepluginv1beta1.ContainerAllocateResponse) (string, bool, error) {
	if plugin.mpsV3 == nil {
		return "", false, fmt.Errorf("MPS v3 manager is not initialized")
	}
	a, err := mpsV3AllocationFor(pod, ctr, devs)
	if err != nil {
		return "", false, err
	}
	for _, d := range devs {
		total, ok := plugin.registeredMemoryMB(d.UUID)
		if !ok || d.Usedmem > total {
			return "", false, fmt.Errorf("MPS allocation exceeds physical GPU memory")
		}
	}
	created, err := plugin.mpsV3.ensure(ctx, a)
	if err != nil {
		return a.Namespace, created, err
	}
	plugin.mpsV3.mu.Lock()
	a = plugin.mpsV3.allocations[a.Namespace]
	plugin.mpsV3.mu.Unlock()
	for key, value := range plugin.mpsV3.allocationEnvs(a) {
		response.Envs[key] = value
	}
	cdiID := "hami.io/mps-v3=" + a.Namespace
	if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyCDICRI) {
		response.CdiDevices = append(response.CdiDevices, &kubeletdevicepluginv1beta1.CDIDevice{Name: cdiID})
	}
	if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyCDIAnnotations) {
		annos, err := plugin.getCDIDeviceAnnotations("mps-v3", cdiID)
		if err != nil {
			return a.Namespace, created, err
		}
		if response.Annotations == nil {
			response.Annotations = make(map[string]string)
		}
		for k, v := range annos {
			response.Annotations[k] = v
		}
	}
	response.Mounts = append(response.Mounts, &kubeletdevicepluginv1beta1.Mount{HostPath: filepath.Join(plugin.mpsV3.root, "pipe", a.Server, a.Namespace), ContainerPath: mpsV3ContainerPipe, ReadOnly: true})
	return a.Namespace, created, nil
}

func (plugin *NvidiaDevicePlugin) startMPSV3() error {
	if os.Getenv("HAMI_MPS_V3_ENABLED") != "true" {
		return fmt.Errorf("mps-v3 requires devicePlugin.mpsV3.enabled and its host mounts")
	}
	if _, err := os.Stat("/proc/1/root/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return fmt.Errorf("mps-v3 requires hostPID and host cgroup v2: %w", err)
	}
	if !plugin.deviceListStrategies.AnyCDIEnabled() || !plugin.deviceListStrategies.AllCDIEnabled() {
		return fmt.Errorf("mps-v3 requires exclusively CDI device-list strategies")
	}
	if plugin.schedulerConfig.DeviceMemoryScaling == nil || *plugin.schedulerConfig.DeviceMemoryScaling != 1 || plugin.schedulerConfig.DeviceCoreScaling == nil || *plugin.schedulerConfig.DeviceCoreScaling != 1 {
		return fmt.Errorf("mps-v3 requires memory and core scaling of 1")
	}
	if plugin.config.Flags.Plugin.DeviceIDStrategy == nil || *plugin.config.Flags.Plugin.DeviceIDStrategy != spec.DeviceIDStrategyUUID {
		return fmt.Errorf("mps-v3 requires UUID device ID strategy")
	}
	if plugin.listLiveNodePods == nil {
		return fmt.Errorf("mps-v3 requires authoritative node Pod listing")
	}
	checker, ok := plugin.cdiHandler.(interface{ ValidateMemoryLimitsHooks([]string) error })
	if !ok {
		return fmt.Errorf("CDI handler cannot validate CUDA memory hooks")
	}
	ids := plugin.uniqueDeviceIDsFromAnnotatedDeviceIDs(plugin.rm.Devices().GetIDs())
	if err := checker.ValidateMemoryLimitsHooks(ids); err != nil {
		return err
	}
	plugin.mpsV3 = newMPSV3Manager()
	geometry, err := cudainfo.Geometry()
	if err != nil {
		return err
	}
	plugin.mpsV3.geometry = geometry
	for _, id := range ids {
		if geometry[id].SMs <= 0 {
			return fmt.Errorf("CUDA geometry missing for allocated GPU %s", id)
		}
	}
	pods, err := plugin.listLiveNodePods()
	if err != nil {
		return err
	}
	live := make(map[string]bool)
	for _, pod := range pods {
		if pod != nil && !util.IsPodInTerminatedState(pod) {
			live[string(pod.UID)] = true
		}
	}
	return plugin.mpsV3.start(plugin.operationContext(), live)
}

func (plugin *NvidiaDevicePlugin) runMPSV3Reconciler() {
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-plugin.runCtx.Done():
			return
		case <-timer.C:
		}
		m := plugin.mpsV3
		plugin.applyMutex.Lock()
		pods, listErr := plugin.listLiveNodePods()
		if listErr == nil {
			live := make(map[string]*corev1.Pod)
			for _, pod := range pods {
				if pod != nil && !util.IsPodInTerminatedState(pod) {
					live[string(pod.UID)] = pod
				}
			}
			m.mu.Lock()
			var stale []string
			for key, a := range m.allocations {
				pod := live[a.PodUID]
				if pod == nil || finishedMPSInitContainer(pod, a.Container) {
					stale = append(stale, key)
				}
			}
			m.mu.Unlock()
			for _, key := range stale {
				if err := m.release(plugin.runCtx, key); err != nil {
					klog.ErrorS(err, "release stale MPS namespace", "namespace", key)
				}
			}
		}
		_, err := m.run(plugin.runCtx, "server", "list")
		m.mu.Lock()
		if err == nil {
			for _, a := range m.allocations {
				if !a.Prepared {
					continue
				}
				if err = m.check(plugin.runCtx, a); err != nil {
					break
				}
			}
		}
		m.mu.Unlock()
		m.healthy.Store(err == nil)
		if err != nil {
			klog.ErrorS(err, "MPS v3 health check failed")
		}

		plugin.applyMutex.Unlock()
	}
}

// Keep rollback failures visible; a failed cleanup must retain its saved intent.
func (plugin *NvidiaDevicePlugin) rollbackMPSV3(keys []string) {
	for _, key := range keys {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := plugin.mpsV3.release(ctx, key)
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			klog.ErrorS(err, "rollback MPS allocation", "namespace", key)
		}
	}
}

// Keep normal containers' allocation across restarts: kubelet can reuse the
// saved Allocate response without asking the plugin for a new namespace.
func finishedMPSInitContainer(pod *corev1.Pod, name string) bool {
	for _, ctr := range pod.Spec.InitContainers {
		if ctr.Name != name || ctr.RestartPolicy != nil && *ctr.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			continue
		}
		for _, status := range pod.Status.InitContainerStatuses {
			if status.Name == name && status.State.Terminated != nil && status.State.Terminated.ExitCode == 0 {
				return true
			}
		}
	}
	return false
}
