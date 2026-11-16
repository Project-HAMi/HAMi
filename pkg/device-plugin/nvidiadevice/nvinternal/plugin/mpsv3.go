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
	"maps"
	"net"
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

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"golang.org/x/sys/unix"
	cdspec "tags.cncf.io/container-device-interface/specs-go"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cudainfo"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	mpsV3ContainerPipe = "/run/hami-mps"
	mpsV3StateVersion  = 620
)

type mpsV3Allocation struct {
	Version     int                  `json:"version,omitempty"`
	ComputeMode string               `json:"computeMode,omitempty"`
	SMPolicy    nvidia.MPSV3SMPolicy `json:"smPolicy,omitzero"`
	PodUID      string               `json:"podUID"`
	Container   string               `json:"container"`
	GPUs        []string             `json:"gpus"`
	Memory      int64                `json:"memoryMiB"`
	Percent     int64                `json:"percent"`
	SMCount     int64                `json:"smCount,omitempty"`
	Prepared    bool                 `json:"prepared"`
	Server      string               `json:"server"`
	Namespace   string               `json:"namespace"`
}

type mpsV3Manager struct {
	policy           nvidia.MPSV3NodeConfig
	mu               sync.Mutex
	root             string
	binary           string
	cdiRoot          string
	lock             *os.File
	run              func(context.Context, ...string) (string, error)
	allocations      map[string]mpsV3Allocation
	geometry         map[string]nvidia.MPSV3Geometry
	healthy          atomic.Bool
	incompatibleGPUs map[string]bool
	unhealthyGPUs    atomic.Pointer[map[string]bool]
	generation       uint64 // Protected by mu. Changes before allocation mutations.
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
	m := &mpsV3Manager{
		root: root, binary: binary, cdiRoot: "/var/run/cdi", allocations: make(map[string]mpsV3Allocation),
		policy: nvidia.MPSV3NodeConfig{ComputeMode: nvidia.MPSV3NamespaceCap},
	}
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
		if a.Version != 0 && a.Version != mpsV3StateVersion {
			return fmt.Errorf("unrecognized MPS allocation state version %d", a.Version)
		}
		if a.Version == mpsV3StateVersion && ((a.ComputeMode != nvidia.MPSV3NamespaceCap && a.ComputeMode != nvidia.MPSV3ServerTimeslice) ||
			(a.SMCount > 0 && a.ComputeMode != nvidia.MPSV3NamespaceCap) || (a.SMCount == 0 && a.SMPolicy.Elastic())) {
			return fmt.Errorf("invalid MPS compute policy in allocation state for %q", key)
		}
		if key != a.Namespace || a.PodUID == "" || a.Container == "" || a.Namespace != mpsV3Name("ns", a.PodUID+"/"+a.Container) || len(a.GPUs) == 0 || !slices.IsSorted(a.GPUs) || a.Server != a.serverName() || a.Memory <= 0 || a.Percent < 1 || a.Percent > 100 || a.SMCount < 0 {
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
func (a mpsV3Allocation) serverName() string {
	key := strings.Join(a.GPUs, ",")
	if a.ComputeMode == nvidia.MPSV3ServerTimeslice {
		key = a.PodUID + "/" + a.Container + "/" + key
	}
	return mpsV3Name("srv", key)
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
	if err = m.recover(ctx, live); err != nil {
		return err
	}
	m.healthy.Store(true)
	return nil
}

func (m *mpsV3Manager) recover(ctx context.Context, live map[string]bool) error {
	for _, a := range m.allocations {
		if live[a.PodUID] && (a.Version != mpsV3StateVersion || a.ComputeMode != m.policy.ComputeMode) {
			return fmt.Errorf("MPS allocation %s uses an older state format or different compute mode; drain the node first", a.Namespace)
		}
	}
	if _, err := m.run(ctx, "compute-policy", "get", "timeslice"); err != nil {
		// A CLI error alone cannot distinguish a missing daemon from a
		// protocol mismatch or a temporarily unresponsive daemon.
		listening, socketErr := m.daemonListening(ctx)
		if listening || socketErr != nil {
			return fmt.Errorf("cannot query r620 MPS daemon: %w", errors.Join(err, socketErr))
		}
		for _, a := range m.allocations {
			if live[a.PodUID] {
				return fmt.Errorf("MPS daemon lost with live allocations; restart affected workloads: %w", err)
			}
		}
		if _, err := m.run(ctx, "-d", "-m"); err != nil {
			return err
		}
	}
	if err := m.waitForDaemon(ctx); err != nil {
		return err
	}
	names, err := m.serverNames(ctx)
	if err != nil {
		return err
	}
	owned := make(map[string]bool, len(m.allocations))
	for _, a := range m.allocations {
		owned[a.Server] = true
	}
	for _, name := range names {
		if !owned[name] {
			return fmt.Errorf("unowned MPS server %s in the dedicated daemon; drain the node first", name)
		}
	}
	for _, a := range m.allocations {
		if live[a.PodUID] && a.Prepared {
			if err = m.checkComputePolicy(ctx); err != nil {
				return err
			}
			break
		}
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
	return m.configureComputePolicy(ctx)
}

func (m *mpsV3Manager) waitForDaemon(ctx context.Context) error {
	return m.wait(ctx, func() error {
		_, err := m.run(ctx, "compute-policy", "get", "timeslice")
		return err
	})
}

func (m *mpsV3Manager) daemonListening(ctx context.Context) (bool, error) {
	socket := filepath.Join(m.root, "pipe", "control")
	info, err := os.Lstat(socket)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return false, fmt.Errorf("unexpected file at MPS daemon socket %s", socket)
	}
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if errors.Is(err, unix.ECONNREFUSED) || os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = conn.Close()
	return true, nil
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

func (m *mpsV3Manager) createServer(ctx context.Context, a mpsV3Allocation) error {
	if _, err := m.run(ctx, "server", "create", a.Server, "--uid=0", "--locality-domains=false", "--allowed-devices="+strings.Join(a.GPUs, ",")); err != nil {
		return err
	}
	return m.serverReady(ctx, a.Server)
}
func (m *mpsV3Manager) checkServer(ctx context.Context, a mpsV3Allocation) error {
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
	return nil
}

func (m *mpsV3Manager) check(ctx context.Context, a mpsV3Allocation) error {
	if err := m.checkServer(ctx, a); err != nil {
		return err
	}
	out, err := m.run(ctx, "namespace", "get", a.Namespace, a.Server, "active-thread-percentage")
	if err != nil {
		return err
	}
	want := a.Percent
	if a.SMCount > 0 || a.ComputeMode == nvidia.MPSV3ServerTimeslice {
		want = 100
	}
	p, err := strconv.ParseFloat(out, 64)
	if err != nil || p != float64(want) {
		return fmt.Errorf("MPS namespace %s has unexpected compute limit %q", a.Namespace, out)
	}
	if a.ComputeMode == nvidia.MPSV3ServerTimeslice {
		value, err := m.run(ctx, "server", "get", a.Server, "timeslice-pct")
		if err != nil {
			return err
		}
		if value != strconv.FormatInt(a.Percent, 10) {
			return fmt.Errorf("MPS server %s has unexpected timeslice percentage %q", a.Server, value)
		}
	}
	if err := m.checkPartitions(ctx, a); err != nil {
		return err
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
	return reflect.DeepEqual(a, b)
}
func (m *mpsV3Manager) ensure(ctx context.Context, a mpsV3Allocation) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, gpu := range a.GPUs {
		if !m.gpuHealthy(gpu) {
			return false, fmt.Errorf("MPS v3 is unhealthy on %s", gpu)
		}
	}
	if a.ComputeMode != m.policy.ComputeMode {
		return false, fmt.Errorf("Pod MPS mode %q does not match node mode %q", a.ComputeMode, m.policy.ComputeMode)
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
	shared := false
	for _, other := range m.allocations {
		if other.Server == a.Server && other.Prepared && a.ComputeMode == nvidia.MPSV3NamespaceCap {
			shared = true
		}
		for _, gpu := range a.GPUs {
			if slices.Contains(other.GPUs, gpu) {
				if a.ComputeMode != other.ComputeMode || (a.SMCount > 0) != (other.SMCount > 0) {
					return false, fmt.Errorf("cannot mix static SM and percentage sharing on GPU %s", gpu)
				}
				if !slices.Equal(a.GPUs, other.GPUs) {
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
			if err := a.SMPolicy.Validate(g, a.SMCount); err != nil {
				return false, err
			}
			charge, err := g.Percent(a.SMCount)
			if err != nil {
				return false, err
			}
			if charge != a.Percent {
				return false, fmt.Errorf("SM allocation has incorrect scheduler compute charge")
			}
		}
	}
	exists, err := m.serverExists(ctx, a.Server)
	if err != nil {
		return false, err
	}
	if exists != shared {
		return false, fmt.Errorf("MPS server %s presence does not match recorded ownership", a.Server)
	}
	if shared {
		if err := m.checkServer(ctx, a); err != nil {
			return false, err
		}
		names, err := m.namespaceNames(ctx, a.Server)
		if err != nil {
			return false, err
		}
		if slices.Contains(names, a.Namespace) {
			return false, fmt.Errorf("unowned MPS namespace %s already exists", a.Namespace)
		}
		if a.SMCount > 0 {
			for _, gpu := range a.GPUs {
				part, err := m.partition(ctx, a, gpu)
				if err != nil {
					return false, err
				}
				if part.Status != "" {
					return false, fmt.Errorf("unowned MPS partition %s already exists on %s", m.partitionName(a, gpu), gpu)
				}
			}
		}
	}
	// Persist intent before any external mutation. Rollback derives all resource
	// names from this record, including partitions created just before a crash.
	m.generation++
	m.allocations[a.Namespace] = a
	if err := m.save(); err != nil {
		delete(m.allocations, a.Namespace)
		return false, err
	}
	if !exists {
		if err := m.createServer(ctx, a); err != nil {
			return true, err
		}
	}
	if a.SMCount > 0 {
		if err := m.checkGeometry(ctx, a); err != nil {
			return true, err
		}
	}
	if a.ComputeMode == nvidia.MPSV3ServerTimeslice {
		if _, err = m.run(ctx, "server", "set", a.Server, "--timeslice-pct="+strconv.FormatInt(a.Percent, 10)); err != nil {
			return true, err
		}
	}
	if _, err = m.run(ctx, "namespace", "create", a.Namespace, "--server="+a.Server); err != nil {
		return true, err
	}
	percent := a.Percent
	if a.SMCount > 0 || a.ComputeMode == nvidia.MPSV3ServerTimeslice {
		percent = 100
	}
	if _, err = m.run(ctx, "namespace", "set", a.Namespace, "--server="+a.Server, "--active-thread-percentage="+strconv.FormatInt(percent, 10)); err != nil {
		return true, err
	}
	if a.SMCount > 0 {
		for _, gpu := range a.GPUs {
			chunks, remainder, _ := m.geometry[gpu].Partition(a.SMCount)
			args := []string{"sm-partition", "create", m.partitionName(a, gpu), "--server=" + a.Server,
				"--device=" + gpu, "--chunks=" + strconv.FormatInt(chunks, 10),
				"--include-remainder=" + strconv.FormatBool(remainder), "--shareable=" + strconv.FormatBool(a.SMPolicy.Shareable)}
			if a.SMPolicy.MaxChunks != "" {
				args = append(args, "--max-chunks="+a.SMPolicy.MaxChunks)
			}
			if _, err = m.run(ctx, args...); err != nil {
				return true, err
			}
			if _, err = m.run(ctx, "namespace", "set", a.Namespace, "--server="+a.Server, "--device="+gpu, "--sm-partition="+m.partitionName(a, gpu)); err != nil {
				return true, err
			}
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
func (m *mpsV3Manager) configureComputePolicy(ctx context.Context) error {
	names, err := m.serverNames(ctx)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		yield, err := m.run(ctx, "compute-policy", "get", "timeslice-yield-level")
		if err != nil {
			return err
		}
		switch yield {
		case "unset":
		case "1", "2":
			if m.policy.TimesliceYieldLevel == nil {
				// r620 only accepts 1 or 2 in set. Restart the drained
				// daemon to restore the default yield behavior.
				if err := m.restartDrainedDaemon(ctx); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected r620 compute policy yield level %q", yield)
		}
		args := []string{"compute-policy", "set", "--timeslice=" + m.desiredTimeslice()}
		if m.policy.TimesliceYieldLevel != nil {
			args = append(args, "--timeslice-yield-level="+strconv.Itoa(*m.policy.TimesliceYieldLevel))
		}
		if _, err := m.run(ctx, args...); err != nil {
			return err
		}
	}
	return m.checkComputePolicy(ctx)
}

func (m *mpsV3Manager) restartDrainedDaemon(ctx context.Context) error {
	if _, err := m.run(ctx, "-q"); err != nil {
		return err
	}
	if err := m.wait(ctx, func() error {
		listening, err := m.daemonListening(ctx)
		if listening {
			return fmt.Errorf("waiting for drained MPS daemon to exit")
		}
		return err
	}); err != nil {
		return err
	}
	if _, err := m.run(ctx, "-d", "-m"); err != nil {
		return err
	}
	if err := m.waitForDaemon(ctx); err != nil {
		return err
	}
	_, err := m.run(ctx, "context-share", "set", "default_socket=off")
	return err
}

func (m *mpsV3Manager) desiredTimeslice() string {
	if m.policy.ComputeMode == nvidia.MPSV3ServerTimeslice {
		return m.policy.Timeslice
	}
	return "default"
}

func (m *mpsV3Manager) checkComputePolicy(ctx context.Context) error {
	timeslice, err := m.run(ctx, "compute-policy", "get", "timeslice")
	if err != nil {
		return err
	}
	wantTimeslice := m.desiredTimeslice()
	if timeslice != wantTimeslice {
		return fmt.Errorf("MPS timeslice is %q, expected %q; drain and restart the daemon", timeslice, wantTimeslice)
	}
	yield, err := m.run(ctx, "compute-policy", "get", "timeslice-yield-level")
	if err != nil {
		return err
	}
	wantYield := "unset"
	if m.policy.TimesliceYieldLevel != nil {
		wantYield = strconv.Itoa(*m.policy.TimesliceYieldLevel)
	}
	if yield != wantYield {
		return fmt.Errorf("MPS timeslice yield level is %q, expected %q; drain and restart the daemon", yield, wantYield)
	}
	return nil
}

func mpsV3CSV(out string) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(out))
	reader.FieldsPerRecord = -1
	return reader.ReadAll()
}

func validMPSV3Name(name string) bool {
	return name != "" && len(name) <= 64 && strings.IndexFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	}) == -1
}

func (m *mpsV3Manager) serverNames(ctx context.Context) ([]string, error) {
	out, err := m.run(ctx, "server", "list", "--format=csv,noheader")
	if err != nil {
		return nil, err
	}
	rows, err := mpsV3CSV(out)
	if err != nil {
		return nil, fmt.Errorf("read r620 server inventory: %w", err)
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		// r620 emits the GPU and context ID lists as comma-separated
		// fields, so a multi-GPU row can have extra CSV fields.
		if len(row) < 8 || !validMPSV3Name(row[0]) || slices.Contains(names, row[0]) || !filepath.IsAbs(row[4]) {
			return nil, fmt.Errorf("unexpected r620 server inventory row %q", row)
		}
		if _, err := strconv.ParseUint(row[1], 10, 32); err != nil {
			return nil, fmt.Errorf("unexpected r620 server UID %q", row[1])
		}
		fields := strings.Split(strings.Join(row[5:len(row)-1], ","), ",")
		gpuCount := 0
		for gpuCount < len(fields) && validMPSGPU(fields[gpuCount]) {
			gpuCount++
		}
		if gpuCount == 0 || gpuCount == len(fields) {
			return nil, fmt.Errorf("unexpected r620 server device/context fields %q", fields)
		}
		for _, id := range fields[gpuCount:] {
			if id == "-" || id == "x" {
				continue
			}
			if _, err := strconv.ParseUint(id, 10, 64); err != nil {
				return nil, fmt.Errorf("unexpected r620 server context ID %q", id)
			}
		}
		weight := row[len(row)-1]
		if weight != "unset" {
			value, err := strconv.ParseUint(weight, 10, 8)
			if err != nil || value > 100 {
				return nil, fmt.Errorf("unexpected r620 server timeslice %q", weight)
			}
		}
		names = append(names, row[0])
	}
	return names, nil
}

func (m *mpsV3Manager) serverExists(ctx context.Context, name string) (bool, error) {
	names, err := m.serverNames(ctx)
	return slices.Contains(names, name), err
}

func (m *mpsV3Manager) namespaceNames(ctx context.Context, server string) ([]string, error) {
	out, err := m.run(ctx, "namespace", "list", "--server="+server, "--format=csv,noheader")
	if err != nil {
		return nil, err
	}
	rows, err := mpsV3CSV(out)
	if err != nil {
		return nil, fmt.Errorf("read r620 namespace inventory: %w", err)
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		// r620 emits a row per GPU assignment, so names may repeat.
		if len(row) != 11 || !validMPSV3Name(row[0]) || row[1] != server || !filepath.IsAbs(row[8]) {
			return nil, fmt.Errorf("unexpected r620 namespace inventory row %q", row)
		}
		if !(row[9] == "-" && row[10] == "-") && !(validMPSGPU(row[9]) && validMPSV3Name(row[10])) {
			return nil, fmt.Errorf("unexpected r620 namespace assignment in inventory row %q", row)
		}
		names = append(names, row[0])
	}
	return names, nil
}

type mpsV3Partition struct {
	Status    string
	ID        string
	SMs       int64
	Chunks    int64
	Remainder bool
	Shareable bool
	MaxChunks string
}

func (m *mpsV3Manager) partitionName(a mpsV3Allocation, gpu string) string {
	return mpsV3Name("sm", a.Namespace+"/"+gpu)
}

func (m *mpsV3Manager) partition(ctx context.Context, a mpsV3Allocation, gpu string) (mpsV3Partition, error) {
	out, err := m.run(ctx, "sm-partition", "list", "--server="+a.Server, "--device="+gpu, "--format=csv,noheader", "--all")
	if err != nil {
		return mpsV3Partition{}, err
	}
	rows, err := mpsV3CSV(out)
	if err != nil {
		return mpsV3Partition{}, fmt.Errorf("read r620 SM partition inventory: %w", err)
	}
	var found mpsV3Partition
	for _, row := range rows {
		// 620.70.06 includes CONTRACT TIMEOUT immediately before STATUS.
		if len(row) != 11 || row[0] != a.Server || row[1] != gpu {
			return mpsV3Partition{}, fmt.Errorf("unexpected r620 SM partition inventory row %q", row)
		}
		if row[2] == "-" && row[9] == "unallocated" {
			continue
		}
		if !validMPSV3Name(row[2]) || (row[9] != "configured" && row[9] != "created") {
			return mpsV3Partition{}, fmt.Errorf("unexpected r620 SM partition name or status: %q", row)
		}
		if row[2] != m.partitionName(a, gpu) {
			continue
		}
		if found.Status != "" {
			return mpsV3Partition{}, fmt.Errorf("duplicate r620 SM partition %s", row[2])
		}
		chunks, chunkErr := strconv.ParseInt(row[3], 10, 64)
		remainder, remainderErr := strconv.ParseBool(row[5])
		shareable, shareableErr := strconv.ParseBool(row[6])
		if chunkErr != nil || chunks <= 0 || remainderErr != nil || shareableErr != nil {
			return mpsV3Partition{}, fmt.Errorf("invalid r620 SM partition configuration: %q", row)
		}
		maxChunks := row[7]
		if maxChunks == "-" || maxChunks == "0" {
			maxChunks = ""
		} else if maxChunks != "unlimited" {
			if value, err := strconv.ParseUint(maxChunks, 10, 32); err != nil || value == 0 {
				return mpsV3Partition{}, fmt.Errorf("invalid r620 SM partition max chunks %q", maxChunks)
			}
		}
		found = mpsV3Partition{Status: row[9], ID: row[10], Chunks: chunks, Remainder: remainder, Shareable: shareable, MaxChunks: maxChunks}
		if found.Status == "created" {
			sms, err := strconv.ParseInt(row[4], 10, 64)
			if err != nil || sms <= 0 || found.ID == "" || found.ID == "-" {
				return mpsV3Partition{}, fmt.Errorf("invalid r620 live SM partition: %q", row)
			}
			found.SMs = sms
		}
	}
	return found, nil
}

// The device inventory reports physical SMs and static partition chunks.
func (m *mpsV3Manager) checkGeometry(ctx context.Context, a mpsV3Allocation) error {
	out, err := m.run(ctx, "device", "list", "--server="+a.Server, "--format=csv,noheader")
	if err != nil {
		return err
	}
	rows, err := mpsV3CSV(out)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(a.GPUs))
	for _, row := range rows {
		if len(row) != 8 || row[1] != a.Server || !slices.Contains(a.GPUs, row[3]) || seen[row[3]] {
			return fmt.Errorf("unexpected r620 device inventory row %q", row)
		}
		geometry := m.geometry[row[3]]
		if geometry.Chunk <= 0 || row[5] != strconv.FormatInt(geometry.SMs, 10) || row[6] != strconv.FormatInt(geometry.SMs/geometry.Chunk, 10) {
			return fmt.Errorf("MPS driver SM geometry differs from scheduler geometry on %s: %s SMs, %s chunks", row[3], row[5], row[6])
		}
		seen[row[3]] = true
	}
	if len(seen) != len(a.GPUs) {
		return fmt.Errorf("incomplete MPS device inventory for %s", a.Server)
	}
	return nil
}

func (m *mpsV3Manager) checkPartitions(ctx context.Context, a mpsV3Allocation) error {
	out, err := m.run(ctx, "namespace", "get", a.Namespace, a.Server, "sm-partitions")
	if err != nil {
		return err
	}
	expected := make(map[string]string, len(a.GPUs))
	if a.SMCount > 0 {
		for _, gpu := range a.GPUs {
			expected[gpu] = m.partitionName(a, gpu)
		}
	}
	if out != "" {
		for line := range strings.SplitSeq(out, "\n") {
			gpu, partition, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok || expected[gpu] == "" || expected[gpu] != partition {
				return fmt.Errorf("unexpected MPS namespace partition assignment %q", line)
			}
			delete(expected, gpu)
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("MPS namespace %s is missing SM partition assignments", a.Namespace)
	}
	if a.SMCount == 0 {
		return nil
	}
	for _, gpu := range a.GPUs {
		chunks, remainder, err := m.geometry[gpu].Partition(a.SMCount)
		if err != nil {
			return err
		}
		part, err := m.partition(ctx, a, gpu)
		if err != nil {
			return err
		}
		if part.Status != "created" || part.Chunks != chunks || part.Remainder != remainder ||
			part.Shareable != a.SMPolicy.Shareable || part.MaxChunks != a.SMPolicy.MaxChunks ||
			(!a.SMPolicy.Elastic() && part.SMs != a.SMCount) {
			return fmt.Errorf("MPS SM partition configuration changed on %s", gpu)
		}
	}
	return nil
}

func (m *mpsV3Manager) release(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.allocations[key]
	if !ok {
		return nil
	}
	m.generation++
	if a.Prepared {
		a.Prepared = false
		m.allocations[key] = a
		if err := m.save(); err != nil {
			a.Prepared = true
			m.allocations[key] = a
			return err
		}
	}
	exists, err := m.serverExists(ctx, a.Server)
	if err != nil {
		return err
	}
	if exists {
		// Namespace deletion is non-forced. The daemon protects live clients,
		// and removing the namespace releases its partition references.
		names, err := m.namespaceNames(ctx, a.Server)
		if err != nil {
			return err
		}
		if slices.Contains(names, a.Namespace) {
			if _, err = m.run(ctx, "namespace", "delete", a.Namespace, "--server="+a.Server); err != nil {
				return err
			}
		}
		if a.SMCount > 0 {
			for _, gpu := range a.GPUs {
				part, err := m.partition(ctx, a, gpu)
				if err != nil {
					return err
				}
				if part.Status != "" {
					if _, err = m.run(ctx, "sm-partition", "delete", m.partitionName(a, gpu), "--server="+a.Server, "--device="+gpu); err != nil {
						return err
					}
				}
			}
		}
		last := true
		for key, other := range m.allocations {
			if key != a.Namespace && other.Server == a.Server {
				last = false
			}
		}
		if last {
			for _, name := range names {
				if name != "default" && name != a.Namespace {
					return fmt.Errorf("unowned MPS namespace %s on server %s", name, a.Server)
				}
			}
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
	return env
}
func (m *mpsV3Manager) publish(a mpsV3Allocation) error {
	env := m.allocationEnvs(a)
	var edits []string
	for key, val := range env {
		edits = append(edits, key+"="+val)
	}
	slices.Sort(edits)
	spec := cdspec.Spec{
		Version: "0.6.0", Kind: "hami.io/mps-v3",
		Devices: []cdspec.Device{{Name: a.Namespace, ContainerEdits: cdspec.ContainerEdits{
			Env: edits,
			Mounts: []*cdspec.Mount{{
				HostPath: filepath.Join(m.root, "pipe", a.Server, a.Namespace), ContainerPath: mpsV3ContainerPipe,
				Options: []string{"bind", "ro"},
			}},
		}}},
	}
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
	info := map[string]any{nvidia.MPSV3GeometryKey: m.geometry[gpu], nvidia.MPSV3ComputeModeKey: m.policy.ComputeMode}
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
	a.ComputeMode, a.SMCount, a.SMPolicy, err = nvidia.MPSV3Request(pod.Annotations)
	a.Version = mpsV3StateVersion
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
	a.Server = a.serverName()
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
	driver, ret := plugin.migMgr.nvmllib.SystemGetDriverVersion()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("query NVIDIA driver version: %s", nvml.ErrorString(ret))
	}
	major, _, _ := strings.Cut(driver, ".")
	version, err := strconv.Atoi(major)
	if err != nil || version != 620 {
		return fmt.Errorf("mps-v3 requires NVIDIA driver branch r620, got %q", driver)
	}
	var config nvidia.MPSV3NodeConfig
	if plugin.schedulerConfig.MPSV3 != nil {
		config = *plugin.schedulerConfig.MPSV3
	}
	policy, err := config.Resolve()
	if err != nil {
		return err
	}
	plugin.mpsV3 = newMPSV3Manager()
	plugin.mpsV3.policy = policy
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
	if policy.ComputeMode == nvidia.MPSV3ServerTimeslice {
		plugin.mpsV3.incompatibleGPUs = incompatibleMPSV3TimesliceGPUs(plugin.migMgr.nvmllib, ids)
	}
	return plugin.mpsV3.start(plugin.operationContext(), live)
}

func incompatibleMPSV3TimesliceGPUs(library nvml.Interface, ids []string) map[string]bool {
	incompatible := make(map[string]bool)
	for _, id := range ids {
		gpu, ret := library.DeviceGetHandleByUUID(id)
		if ret != nvml.SUCCESS {
			klog.ErrorS(fmt.Errorf("%s", nvml.ErrorString(ret)), "Cannot query MPS timeslice GPU", "gpu", id)
			incompatible[id] = true
			continue
		}
		mode, ret := gpu.GetComputeMode()
		if ret != nvml.SUCCESS || mode != nvml.COMPUTEMODE_DEFAULT {
			klog.InfoS("MPS server-timeslice requires DEFAULT compute mode", "gpu", id, "mode", mode, "nvmlStatus", nvml.ErrorString(ret))
			incompatible[id] = true
		}
	}
	return incompatible
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
		plugin.reconcileMPSV3()
	}
}

func (m *mpsV3Manager) gpuHealthy(gpu string) bool {
	unhealthy := m.unhealthyGPUs.Load()
	return m.healthy.Load() && !m.incompatibleGPUs[gpu] && (unhealthy == nil || !(*unhealthy)[gpu])
}

func (plugin *NvidiaDevicePlugin) reconcileMPSV3() {
	m := plugin.mpsV3
	if !m.mu.TryLock() {
		return
	}
	snapshot := maps.Clone(m.allocations)
	generation := m.generation
	m.mu.Unlock()

	// Read-only checks must not hold either allocation lock.
	err := m.checkComputePolicy(plugin.runCtx)
	unhealthy := make(map[string]bool)
	if err != nil {
		klog.ErrorS(err, "MPS v3 daemon health check failed")
	} else {
		for _, a := range snapshot {
			if !a.Prepared {
				continue
			}
			if checkErr := m.check(plugin.runCtx, a); checkErr != nil {
				klog.ErrorS(checkErr, "MPS v3 allocation health check failed", "namespace", a.Namespace)
				for _, gpu := range a.GPUs {
					unhealthy[gpu] = true
				}
			}
		}
	}
	m.mu.Lock()
	if generation == m.generation {
		m.unhealthyGPUs.Store(&unhealthy)
	}
	m.healthy.Store(err == nil)
	m.mu.Unlock()
	if err != nil {
		return
	}

	pods, listErr := plugin.listLiveNodePods()
	if listErr != nil {
		return
	}
	live := make(map[string]*corev1.Pod, len(pods))
	for _, pod := range pods {
		if pod != nil && !util.IsPodInTerminatedState(pod) {
			live[string(pod.UID)] = pod
		}
	}
	for key, a := range snapshot {
		pod := live[a.PodUID]
		if a.Prepared && pod != nil {
			continue
		}
		if !plugin.applyMutex.TryLock() {
			return
		}
		m.mu.Lock()
		current, exists := m.allocations[key]
		same := generation == m.generation && exists && reflect.DeepEqual(current, a)
		m.mu.Unlock()
		if same {
			ctx, cancel := context.WithTimeout(plugin.runCtx, 5*time.Second)
			if err := m.release(ctx, key); err != nil {
				klog.ErrorS(err, "release stale MPS namespace", "namespace", key)
			}
			cancel()
			m.mu.Lock()
			generation = m.generation
			m.mu.Unlock()
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
