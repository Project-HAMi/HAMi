// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	cdspec "tags.cncf.io/container-device-interface/specs-go"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

type fakeMPSV3 struct {
	servers          map[string]string
	weights          map[string]string
	namespaces       map[string]int
	namespaceServers map[string]string
	assignments      map[string]map[string]string
	partitions       map[string]mpsV3Partition
	calls            []string
	fail             string
	busy             bool
	timeslice        string
	yield            string
	serverCSV        string
}

func testMPSV3(t *testing.T) (*mpsV3Manager, *fakeMPSV3) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "mp")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	m := newMPSV3Manager()
	m.root = root
	m.cdiRoot = filepath.Join(root, "cdi")
	require.NoError(t, os.Mkdir(m.cdiRoot, 0755))
	m.geometry = map[string]nvidia.MPSV3Geometry{"GPU-a": {SMs: 72, Chunk: 2}, "GPU-b": {SMs: 72, Chunk: 2}}
	m.healthy.Store(true)
	f := &fakeMPSV3{
		servers: make(map[string]string), weights: make(map[string]string), namespaces: make(map[string]int),
		namespaceServers: make(map[string]string), assignments: make(map[string]map[string]string),
		partitions: make(map[string]mpsV3Partition), timeslice: "default", yield: "unset",
	}
	m.run = func(ctx context.Context, args ...string) (string, error) {
		call := strings.Join(args, " ")
		f.calls = append(f.calls, call)
		if f.fail != "" && strings.Contains(call, f.fail) {
			return "", fmt.Errorf("injected failure: %s", call)
		}
		flag := func(name string) string {
			for _, arg := range args {
				if strings.HasPrefix(arg, "--"+name+"=") {
					return strings.TrimPrefix(arg, "--"+name+"=")
				}
			}
			return ""
		}
		switch args[0] + " " + args[1] {
		case "compute-policy get":
			if args[2] == "timeslice-yield-level" {
				return f.yield, nil
			}
			return f.timeslice, nil
		case "compute-policy set":
			if len(f.servers) != 0 {
				return "", fmt.Errorf("servers exist")
			}
			f.timeslice = flag("timeslice")
			if y := flag("timeslice-yield-level"); y != "" {
				f.yield = y
			}
			return "", nil
		case "context-share set":
			return "", nil
		case "device list":
			server := flag("server")
			var rows []string
			for i, gpu := range strings.Split(f.servers[server], ",") {
				rows = append(rows, fmt.Sprintf("%d,%s,0000:03.00.00,%s,NVIDIA A10,72,36,", i, server, gpu))
			}
			return strings.Join(rows, "\n"), nil
		case "server list":
			if f.serverCSV != "" {
				return f.serverCSV, nil
			}
			var rows []string
			for name, gpus := range f.servers {
				rows = append(rows, fmt.Sprintf("%s,0,123,Ready,/run/mps/%s,%s,1,%s", name, name, gpus, f.weights[name]))
			}
			return strings.Join(rows, "\n"), nil
		case "server create":
			f.servers[args[2]] = flag("allowed-devices")
			f.weights[args[2]] = "0"
			return "", nil
		case "server set":
			f.weights[args[2]] = flag("timeslice-pct")
			return "", nil
		case "server get":
			switch args[3] {
			case "name":
				return args[2], nil
			case "status":
				return "Ready", nil
			case "allowed-devices":
				return f.servers[args[2]], nil
			case "timeslice-pct":
				return f.weights[args[2]], nil
			}
		case "server delete":
			delete(f.servers, args[2])
			delete(f.weights, args[2])
			return "", nil
		case "namespace create":
			f.namespaces[args[2]] = 100
			f.namespaceServers[args[2]] = flag("server")
			f.assignments[args[2]] = make(map[string]string)
			dir := filepath.Join(root, "pipe", flag("server"), args[2])
			require.NoError(t, os.MkdirAll(dir, 0755))
			listener, err := net.Listen("unix", filepath.Join(dir, "control"))
			require.NoError(t, err)
			t.Cleanup(func() { listener.Close() })
			return "", nil
		case "namespace set":
			if partition := flag("sm-partition"); partition != "" {
				f.assignments[args[2]][flag("device")] = partition
				return "", nil
			}
			n, _ := strconv.Atoi(flag("active-thread-percentage"))
			f.namespaces[args[2]] = n
			return "", nil
		case "namespace get":
			n, ok := f.namespaces[args[2]]
			if !ok {
				return "", fmt.Errorf("missing namespace")
			}
			if args[4] == "sm-partitions" {
				var bindings []string
				for gpu, partition := range f.assignments[args[2]] {
					bindings = append(bindings, gpu+":"+partition)
				}
				return strings.Join(bindings, "\n"), nil
			}
			return strconv.Itoa(n), nil
		case "namespace list":
			var rows []string
			for name, cap := range f.namespaces {
				if server := f.namespaceServers[name]; server == flag("server") {
					rows = append(rows, fmt.Sprintf("%s,%s,Ready,unlimited,%d.00,0,-,-,/run/mps/%s/%s,-,-", name, server, cap, server, name))
				}
			}
			return strings.Join(rows, "\n"), nil
		case "namespace delete":
			if f.busy {
				return "", fmt.Errorf("active client")
			}
			delete(f.namespaces, args[2])
			delete(f.namespaceServers, args[2])
			delete(f.assignments, args[2])
			return "", nil
		case "sm-partition create":
			chunks, _ := strconv.ParseInt(flag("chunks"), 10, 64)
			maxChunks := flag("max-chunks")
			if maxChunks == "" {
				maxChunks = "-"
			}
			f.partitions[flag("device")+"/"+args[2]] = mpsV3Partition{
				SMs: chunks * 2, Chunks: chunks, ID: "opaque_" + args[2], Status: "created", MaxChunks: maxChunks,
				Shareable: flag("shareable") == "true", Remainder: flag("include-remainder") == "true",
			}
			return "", nil
		case "sm-partition list":
			var rows []string
			for key, p := range f.partitions {
				gpu, name, _ := strings.Cut(key, "/")
				if gpu == flag("device") {
					rows = append(rows, fmt.Sprintf("%s,%s,%s,%d,%d,%t,%t,%s,-,%s,%s", flag("server"), gpu, name, p.Chunks, p.SMs, p.Remainder, p.Shareable, p.MaxChunks, p.Status, p.ID))
				}
			}
			return strings.Join(rows, "\n"), nil
		case "sm-partition delete":
			for _, bindings := range f.assignments {
				if bindings[flag("device")] == args[2] {
					return "", fmt.Errorf("partition in use")
				}
			}
			delete(f.partitions, flag("device")+"/"+args[2])
			return "", nil
		}
		return "", fmt.Errorf("unhandled command %s", call)
	}
	return m, f
}
func testMPSAllocation(t *testing.T, sms int64) mpsV3Allocation {
	t.Helper()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "test-pod", Annotations: map[string]string{}}}
	if sms > 0 {
		pod.Annotations[nvidia.MPSV3SMCountAnnotation] = strconv.FormatInt(sms, 10)
	}
	cores := int64(25)
	if sms > 0 {
		var err error
		cores, err = (nvidia.MPSV3Geometry{SMs: 72, Chunk: 2}).Percent(sms)
		require.NoError(t, err)
	}
	a, err := mpsV3AllocationFor(pod, &corev1.Container{Name: "main"}, device.ContainerDevices{cd("GPU-a", "NVIDIA", 2048, cores)})
	require.NoError(t, err)
	return a
}
func TestMPSV3Lifecycle(t *testing.T) {
	for _, sms := range []int64{0, 2} {
		t.Run(strconv.FormatInt(sms, 10), func(t *testing.T) {
			m, f := testMPSV3(t)
			a := testMPSAllocation(t, sms)
			created, err := m.ensure(context.Background(), a)
			require.NoError(t, err)
			require.True(t, created)
			saved := m.allocations[a.Namespace]
			require.True(t, saved.Prepared)
			if sms > 0 {
				require.Equal(t, m.partitionName(a, "GPU-a"), f.assignments[a.Namespace]["GPU-a"])
				require.Equal(t, 100, f.namespaces[a.Namespace])
			} else {
				require.Equal(t, 25, f.namespaces[a.Namespace])
			}
			data, err := os.ReadFile(m.cdiPath(a.Namespace))
			require.NoError(t, err)
			var spec cdspec.Spec
			require.NoError(t, json.Unmarshal(data, &spec))
			require.Contains(t, spec.Devices[0].ContainerEdits.Env, "NVIDIA_GPU_MEMORY_LIMIT=2048")
			require.NotContains(t, strings.Join(spec.Devices[0].ContainerEdits.Env, "\n"), "CUDA_MPS_SM_PARTITION=")
			mounts := spec.Devices[0].ContainerEdits.Mounts
			require.Len(t, mounts, 1)
			require.Equal(t, filepath.Join(m.root, "pipe", a.Server, a.Namespace), mounts[0].HostPath)
			require.Equal(t, mpsV3ContainerPipe, mounts[0].ContainerPath)
			require.Contains(t, mounts[0].Options, "ro")
			// A retry verifies the existing namespace and its bindings.
			calls := len(f.calls)
			created, err = m.ensure(context.Background(), a)
			require.NoError(t, err)
			require.False(t, created)
			for _, call := range f.calls[calls:] {
				require.NotContains(t, call, " create ")
			}
			recovered := newMPSV3Manager()
			recovered.root = m.root
			require.NoError(t, recovered.load())
			require.Equal(t, m.allocations, recovered.allocations)
			require.NoError(t, m.check(context.Background(), recovered.allocations[a.Namespace]))
			f.busy = true
			require.Error(t, m.release(context.Background(), a.Namespace))
			require.Contains(t, m.allocations, a.Namespace)
			f.busy = false
			require.NoError(t, m.release(context.Background(), a.Namespace))
			require.Empty(t, m.allocations)
			require.Empty(t, f.partitions)
			_, err = os.Stat(m.cdiPath(a.Namespace))
			require.True(t, os.IsNotExist(err))
		})
	}
}
func TestMPSV3Rollback(t *testing.T) {
	for _, fail := range []string{"server create", "namespace create", "namespace set", "sm-partition create"} {
		t.Run(fail, func(t *testing.T) {
			m, f := testMPSV3(t)
			a := testMPSAllocation(t, 2)
			f.fail = fail
			created, err := m.ensure(context.Background(), a)
			require.Error(t, err)
			require.True(t, created)
			f.fail = ""
			require.NoError(t, m.release(context.Background(), a.Namespace))
			require.Empty(t, m.allocations)
			require.Empty(t, f.servers)
			require.Empty(t, f.namespaces)
			require.Empty(t, f.partitions)
		})
	}
}
func TestMPSV3RejectsConflicts(t *testing.T) {
	m, f := testMPSV3(t)
	a := testMPSAllocation(t, 2)
	_, err := m.ensure(context.Background(), a)
	require.NoError(t, err)
	b := testMPSAllocation(t, 0)
	b.PodUID = "other"
	b.Namespace = mpsV3Name("ns", b.PodUID+"/"+b.Container)
	_, err = m.ensure(context.Background(), b)
	require.ErrorContains(t, err, "cannot mix")
	b = a
	b.Memory++
	_, err = m.ensure(context.Background(), b)
	require.ErrorContains(t, err, "different limits")
	saved := m.allocations[a.Namespace]
	f.namespaces[a.Namespace] = 50
	require.Error(t, m.check(context.Background(), saved))
	m.healthy.Store(false)
	_, err = m.ensure(context.Background(), a)
	require.ErrorContains(t, err, "unhealthy")
}
func TestMPSV3AllocationValidation(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod"}}
	ctr := &corev1.Container{Name: "main"}
	for _, devs := range []device.ContainerDevices{
		nil, {cd("GPU-a", "NVIDIA", 0, 25)}, {cd("MIG-a", "NVIDIA", 1024, 25)},
		{cd("GPU-a", "NVIDIA", 1024, 25), cd("GPU-b", "NVIDIA", 2048, 25)},
		{cd("GPU-a", "NVIDIA", 1024, 25), cd("GPU-b", "NVIDIA", 1024, 50)},
		{cd("GPU-a", "NVIDIA", 1024, 25), cd("GPU-a", "NVIDIA", 1024, 25)},
	} {
		_, err := mpsV3AllocationFor(pod, ctr, devs)
		require.Error(t, err)
	}
	a, err := mpsV3AllocationFor(pod, ctr, device.ContainerDevices{cd("GPU-b", "NVIDIA", 1024, 0), cd("GPU-a", "NVIDIA", 1024, 0)})
	require.NoError(t, err)
	require.Equal(t, int64(100), a.Percent)
	require.Equal(t, []string{"GPU-a", "GPU-b"}, a.GPUs)
	ctr.Env = []corev1.EnvVar{{Name: "NVIDIA_GPU_MEMORY_LIMIT", Value: "999999"}}
	_, err = mpsV3AllocationFor(pod, ctr, device.ContainerDevices{cd("GPU-a", "NVIDIA", 1024, 25)})
	require.ErrorContains(t, err, "reserved")
}
func TestMPSV3Response(t *testing.T) {
	m, _ := testMPSV3(t)
	plugin := newTestPlugin(t)
	plugin.operatingMode = nvidia.MpsV3Mode
	plugin.mpsV3 = m
	plugin.deviceListStrategies = mustStrategies(t, "cdi-cri")
	plugin.rm = &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices {
		return rm.Devices{"GPU-a": {Device: kubeletdevicepluginv1beta1.Device{ID: "GPU-a"}, TotalMemory: 8 << 30}}
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod"}}
	ctr := &corev1.Container{Name: "main", EnvFrom: []corev1.EnvFromSource{{Prefix: "app_"}}}
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{Envs: map[string]string{}}
	_, _, err := plugin.allocateMPSV3(context.Background(), pod, ctr, device.ContainerDevices{cd("GPU-a", "NVIDIA", 2048, 25)}, response)
	require.NoError(t, err)
	require.Equal(t, "2048", response.Envs["NVIDIA_GPU_MEMORY_LIMIT"])
	require.Empty(t, response.Mounts)
	require.Len(t, response.CdiDevices, 1)
	require.True(t, strings.HasPrefix(response.CdiDevices[0].Name, "hami.io/mps-v3="))
	for key := range response.Envs {
		require.False(t, strings.HasPrefix(key, "CUDA_DEVICE_MEMORY_"))
		require.NotEqual(t, "CUDA_DEVICE_SM_LIMIT", key)
	}
	require.False(t, hasLdSoPreloadMount(response.Mounts))
}

func TestAllocateMPSV3ExcludesHAMiCore(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	plugin.operatingMode = nvidia.MpsV3Mode
	plugin.mpsV3, _ = testMPSV3(t)
	plugin.deviceListStrategies = mustStrategies(t, "cdi-cri")
	*plugin.config.Flags.Plugin.DeviceIDStrategy = "uuid"
	plugin.cdiHandler = &cdi.InterfaceMock{QualifiedNameFunc: func(class, id string) string { return "nvidia.com/" + class + "=" + id }}
	plugin.rm = &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices {
		return rm.Devices{"GPU-a": {Device: kubeletdevicepluginv1beta1.Device{ID: "GPU-a"}, TotalMemory: 8 << 30}}
	}}
	plugin.prepareCache = func(string, string) (string, error) { t.Fatal("mps-v3 must not prepare libvgpu cache"); return "", nil }
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "mps", Namespace: "default", UID: "pod", Annotations: map[string]string{device.InRequestDevices[nvidia.NvidiaGPUDevice]: "GPU-a,NVIDIA,2048,25:;"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)
	response, err := plugin.Allocate(context.Background(), &kubeletdevicepluginv1beta1.AllocateRequest{ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{{DevicesIds: []string{"GPU-a-0"}}}})
	require.NoError(t, err)
	require.Len(t, response.ContainerResponses, 1)
	ctr := response.ContainerResponses[0]
	require.Equal(t, "2048", ctr.Envs["NVIDIA_GPU_MEMORY_LIMIT"])
	require.Len(t, ctr.CdiDevices, 2)
	require.False(t, hasLdSoPreloadMount(ctr.Mounts))
	require.Empty(t, ctr.Mounts)
	require.NotContains(t, ctr.Envs, "CUDA_DEVICE_MEMORY_SHARED_CACHE")
	require.NotContains(t, ctr.Envs, "CUDA_DEVICE_MEMORY_LIMIT_0")
	require.NotContains(t, ctr.Envs, "CUDA_DEVICE_SM_LIMIT")
}

func TestMPSV3MultiGPURollback(t *testing.T) {
	m, f := testMPSV3(t)
	a := testMPSAllocation(t, 2)
	a.GPUs = append(a.GPUs, "GPU-b")
	a.Server = mpsV3Name("srv", strings.Join(a.GPUs, ","))
	f.fail = "--device=GPU-b --chunks="
	created, err := m.ensure(context.Background(), a)
	require.True(t, created)
	require.Error(t, err)
	require.Len(t, f.partitions, 1)
	f.fail = ""
	require.NoError(t, m.release(context.Background(), a.Namespace))
	require.Empty(t, f.partitions)
	require.Empty(t, m.allocations)
}

func TestMPSV3MultiGPUStaticBindings(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	a := testMPSAllocation(t, 2)
	a.GPUs = append(a.GPUs, "GPU-b")
	a.Server = a.serverName()
	b := a
	b.Container = "other"
	b.Namespace = mpsV3Name("ns", b.PodUID+"/"+b.Container)
	for _, allocation := range []mpsV3Allocation{a, b} {
		created, err := m.ensure(ctx, allocation)
		require.NoError(t, err)
		require.True(t, created)
		require.Equal(t, map[string]string{
			"GPU-a": m.partitionName(allocation, "GPU-a"),
			"GPU-b": m.partitionName(allocation, "GPU-b"),
		}, f.assignments[allocation.Namespace])
	}
	require.Len(t, f.servers, 1)
	require.NoError(t, m.release(ctx, a.Namespace))
	require.NoError(t, m.check(ctx, b))
	require.NoError(t, m.release(ctx, b.Namespace))
	require.Empty(t, f.servers)
}

func TestMPSV3PreservesForeignCDI(t *testing.T) {
	m, _ := testMPSV3(t)
	a := testMPSAllocation(t, 0)
	foreign := []byte(`{"cdiVersion":"0.6.0","kind":"other.io/test","devices":[]}`)
	require.NoError(t, os.WriteFile(m.cdiPath(a.Namespace), foreign, 0644))
	_, err := m.ensure(context.Background(), a)
	require.ErrorContains(t, err, "refusing to replace")
	require.ErrorContains(t, m.release(context.Background(), a.Namespace), "unowned")
	data, err := os.ReadFile(m.cdiPath(a.Namespace))
	require.NoError(t, err)
	require.Equal(t, foreign, data)
}

func TestMPSV3RejectsCorruptState(t *testing.T) {
	m, _ := testMPSV3(t)
	a := testMPSAllocation(t, 0)
	a.Namespace = "foreign_namespace"
	data, err := json.Marshal(map[string]mpsV3Allocation{a.Namespace: a})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(m.statePath(), data, 0600))
	require.Error(t, m.load())
	require.Empty(t, m.allocations)
}

func TestMPSV3PartitionCSV620(t *testing.T) {
	a := mpsV3Allocation{Server: "probe", Namespace: "test"}
	const gpu = "GPU-7f40b924-1290-f254-98d4-1aa4acafc19d"
	const id = "00820000007lB2gl36uRJhnTKcWcSxfc"
	m := &mpsV3Manager{}
	for _, tc := range []struct {
		fields string
		want   mpsV3Partition
	}{
		{"2,4,false,true,unlimited,-,created," + id, mpsV3Partition{Status: "created", ID: id, SMs: 4, Chunks: 2, Shareable: true, MaxChunks: "unlimited"}},
		{"2,-,false,true,4,-,configured,-", mpsV3Partition{Status: "configured", ID: "-", Chunks: 2, Shareable: true, MaxChunks: "4"}},
		{"2,4,false,created," + id, mpsV3Partition{}},
	} {
		m.run = func(context.Context, ...string) (string, error) {
			return fmt.Sprintf("probe,%s,%s,%s\nprobe,%s,-,34,68,-,-,-,-,unallocated,-", gpu, m.partitionName(a, gpu), tc.fields, gpu), nil
		}
		part, err := m.partition(context.Background(), a, gpu)
		if tc.want.Status == "" {
			require.ErrorContains(t, err, "unexpected r620")
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tc.want, part)
	}
}

func TestMPSV3ServerTimesliceRecovery(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	level := 1
	m.policy = nvidia.MPSV3NodeConfig{ComputeMode: nvidia.MPSV3ServerTimeslice, Timeslice: "long", TimesliceYieldLevel: &level}
	require.NoError(t, m.configureComputePolicy(ctx))

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "test-pod", Annotations: map[string]string{nvidia.MPSV3ComputeModeAnnotation: nvidia.MPSV3ServerTimeslice}}}
	allocate := func(name string, percent int64, devs device.ContainerDevices) mpsV3Allocation {
		t.Helper()
		a, err := mpsV3AllocationFor(pod, &corev1.Container{Name: name}, devs)
		require.NoError(t, err)
		created, err := m.ensure(ctx, a)
		require.NoError(t, err)
		require.True(t, created)
		require.Equal(t, strconv.FormatInt(percent, 10), f.weights[a.Server])
		require.Equal(t, 100, f.namespaces[a.Namespace])
		return a
	}
	a := allocate("first", 25, device.ContainerDevices{cd("GPU-a", "NVIDIA", 2048, 25)})
	b := allocate("second", 75, device.ContainerDevices{cd("GPU-a", "NVIDIA", 2048, 75)})
	require.NotEqual(t, a.Server, b.Server)
	overlap, err := mpsV3AllocationFor(pod, &corev1.Container{Name: "third"}, device.ContainerDevices{cd("GPU-a", "NVIDIA", 2048, 25), cd("GPU-b", "NVIDIA", 2048, 25)})
	require.NoError(t, err)
	created, err := m.ensure(ctx, overlap)
	require.False(t, created)
	require.ErrorContains(t, err, "another GPU set")

	recovered := newMPSV3Manager()
	recovered.root, recovered.cdiRoot, recovered.run, recovered.policy = m.root, m.cdiRoot, m.run, m.policy
	require.NoError(t, recovered.load())
	require.Equal(t, m.allocations, recovered.allocations)
	require.NoError(t, recovered.recover(ctx, map[string]bool{"test-pod": true}))
	require.NoError(t, recovered.check(ctx, a))
	require.NoError(t, recovered.check(ctx, b))
	f.weights[a.Server] = "75"
	require.ErrorContains(t, recovered.check(ctx, a), "unexpected timeslice percentage")
	f.timeslice = "medium"
	require.ErrorContains(t, recovered.configureComputePolicy(ctx), "drain")
	require.NoError(t, recovered.release(ctx, a.Namespace))
	require.Contains(t, f.servers, b.Server)
	require.NoError(t, recovered.release(ctx, b.Namespace))
	recovered.policy = nvidia.MPSV3NodeConfig{ComputeMode: nvidia.MPSV3NamespaceCap}
	require.ErrorContains(t, recovered.configureComputePolicy(ctx), "restart the external daemon")
	f.yield = "unset" // The node operator restarted the daemon.
	require.NoError(t, recovered.configureComputePolicy(ctx))
	require.Equal(t, "default", f.timeslice)
	require.Equal(t, "unset", f.yield)
}

func TestMPSV3RejectsUnversionedState(t *testing.T) {
	m, f := testMPSV3(t)
	a := testMPSAllocation(t, 0)
	a.Version = 0
	m.allocations[a.Namespace] = a
	require.NoError(t, m.save())
	clear(m.allocations)
	require.ErrorContains(t, m.load(), "unrecognized MPS allocation state version 0")
	require.Empty(t, m.allocations)
	require.Empty(t, f.calls)
}

func TestMPSV3NodeConfigOverride(t *testing.T) {
	t.Setenv(util.NodeNameEnvName, "node1")
	path := filepath.Join(t.TempDir(), "node.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"nodeconfig":[
		{"name":"node1","operatingmode":"mps-v3","mpsv3":{"computeMode":"server-timeslice","timeslice":"long","timesliceYieldLevel":1}},
		{"name":"node1","mpsv3":{"computeMode":"namespace-cap"}}
	]}`), 0600))
	var config nvidia.NvidiaConfig
	mode, err := readFromConfigFile(&config, path, nil)
	require.NoError(t, err)
	require.Equal(t, nvidia.MpsV3Mode, mode)
	require.NotNil(t, config.MPSV3)
	resolved, err := config.MPSV3.Resolve()
	require.NoError(t, err)
	require.Equal(t, nvidia.MPSV3NodeConfig{ComputeMode: nvidia.MPSV3NamespaceCap}, resolved)
}

func TestMPSV3BorrowingAndBindingDrift(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	a := testMPSAllocation(t, 4)
	a.SMPolicy = nvidia.MPSV3SMPolicy{Shareable: true, MaxChunks: "5"}
	_, err := m.ensure(ctx, a)
	require.NoError(t, err)
	name := m.partitionName(a, "GPU-a")
	key := "GPU-a/" + name
	p := f.partitions[key]
	p.SMs = 10
	f.partitions[key] = p
	require.NoError(t, m.check(ctx, a))
	f.assignments[a.Namespace]["GPU-a"] = "another_partition"
	require.ErrorContains(t, m.check(ctx, a), "unexpected MPS namespace partition assignment")
	f.assignments[a.Namespace]["GPU-a"] = name
	p.MaxChunks = "6"
	f.partitions[key] = p
	require.ErrorContains(t, m.check(ctx, a), "configuration changed")
}

func TestMPSV3UnknownInventoryPreservesAllocation(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	a := testMPSAllocation(t, 0)
	_, err := m.ensure(ctx, a)
	require.NoError(t, err)
	f.serverCSV = a.Server + ",0,123,Ready"
	require.ErrorContains(t, m.release(ctx, a.Namespace), "unexpected r620")
	require.True(t, m.allocations[a.Namespace].Prepared)
	require.True(t, m.allocations[a.Namespace].CleanupPending)
	require.Contains(t, f.namespaces, a.Namespace)
	require.FileExists(t, m.cdiPath(a.Namespace))
	_, err = m.ensure(ctx, a)
	require.ErrorContains(t, err, "waiting for cleanup")
}

func TestMPSV3RecoverRequiresExternalDaemon(t *testing.T) {
	m, f := testMPSV3(t)
	f.fail = "compute-policy get"
	require.ErrorContains(t, m.recover(context.Background(), nil), "externally managed r620 MPS daemon")
	require.Equal(t, []string{"compute-policy get timeslice"}, f.calls)
}

func TestMPSV3GeometryDriftRollsBack(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	a := testMPSAllocation(t, 8)
	run := m.run
	m.run = func(ctx context.Context, args ...string) (string, error) {
		if args[0] == "device" {
			return fmt.Sprintf("0,%s,0000:03.00.00,GPU-a,NVIDIA A10,72,9,", a.Server), nil
		}
		return run(ctx, args...)
	}
	created, err := m.ensure(ctx, a)
	require.True(t, created)
	require.ErrorContains(t, err, "SM geometry differs")
	require.Empty(t, f.partitions)
	require.NoFileExists(t, m.cdiPath(a.Namespace))
	require.NoError(t, m.release(ctx, a.Namespace))
	require.Empty(t, f.servers)
}

func TestMPSV3HealthCheckDoesNotHoldAllocationLocks(t *testing.T) {
	m, _ := testMPSV3(t)
	plugin := newTestPlugin(t)
	plugin.mpsV3 = m
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	plugin.runCtx = ctx
	plugin.listLiveNodePods = func() ([]*corev1.Pod, error) { return nil, nil }
	entered, done := make(chan struct{}), make(chan struct{})
	m.run = func(ctx context.Context, args ...string) (string, error) {
		close(entered)
		<-ctx.Done()
		return "", ctx.Err()
	}
	go func() {
		defer close(done)
		plugin.reconcileMPSV3()
	}()
	<-entered
	require.True(t, plugin.applyMutex.TryLock())
	plugin.applyMutex.Unlock()
	require.True(t, m.mu.TryLock())
	m.mu.Unlock()
	cancel()
	<-done
	require.True(t, m.gpuHealthy("GPU-a"))
}

func TestMPSV3AllocationHealthIsScopedToGPU(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	a := testMPSAllocation(t, 0)
	_, err := m.ensure(ctx, a)
	require.NoError(t, err)
	plugin := newTestPlugin(t)
	plugin.mpsV3, plugin.runCtx = m, ctx
	plugin.listLiveNodePods = func() ([]*corev1.Pod, error) {
		return []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "test-pod"}}}, nil
	}

	f.fail = "namespace get " + a.Namespace
	plugin.reconcileMPSV3()
	require.False(t, m.gpuHealthy("GPU-a"))
	require.True(t, m.gpuHealthy("GPU-b"))
	f.fail = ""
	plugin.reconcileMPSV3()
	require.True(t, m.gpuHealthy("GPU-a"))
}

func TestMPSV3TimesliceComputeMode(t *testing.T) {
	library := &nvmlmock.Interface{
		DeviceGetHandleByUUIDFunc: func(id string) (nvml.Device, nvml.Return) {
			if id == "GPU-missing" {
				return nil, nvml.ERROR_NOT_FOUND
			}
			return &nvmlmock.Device{
				GetComputeModeFunc: func() (nvml.ComputeMode, nvml.Return) {
					switch id {
					case "GPU-exclusive":
						return nvml.COMPUTEMODE_EXCLUSIVE_PROCESS, nvml.SUCCESS
					case "GPU-prohibited":
						return nvml.COMPUTEMODE_PROHIBITED, nvml.SUCCESS
					case "GPU-query-error":
						return nvml.COMPUTEMODE_DEFAULT, nvml.ERROR_UNKNOWN
					default:
						return nvml.COMPUTEMODE_DEFAULT, nvml.SUCCESS
					}
				},
			}, nvml.SUCCESS
		},
	}
	m, _ := testMPSV3(t)
	m.incompatibleGPUs = incompatibleMPSV3TimesliceGPUs(library, []string{
		"GPU-a", "GPU-exclusive", "GPU-prohibited", "GPU-missing", "GPU-query-error",
	})
	require.True(t, m.gpuHealthy("GPU-a"))
	for _, id := range []string{"GPU-exclusive", "GPU-prohibited", "GPU-missing", "GPU-query-error"} {
		require.False(t, m.gpuHealthy(id), id)
	}
}

func TestMPSV3ReplicaHealth(t *testing.T) {
	const id = "GPU-7f40b924-1290-f254-98d4-1aa4acafc19d"
	plugin := newTestPlugin(t)
	m, _ := testMPSV3(t)
	plugin.mpsV3 = m
	m.incompatibleGPUs = map[string]bool{id: true}
	plugin.rm = &rm.ResourceManagerMock{DevicesFunc: func() rm.Devices {
		return rm.Devices{id: {Device: kubeletdevicepluginv1beta1.Device{ID: id, Health: kubeletdevicepluginv1beta1.Healthy}}}
	}}
	devices := plugin.apiDevices()
	require.NotEmpty(t, devices)
	for _, d := range devices {
		require.NotEqual(t, id, d.ID)
		require.Equal(t, kubeletdevicepluginv1beta1.Unhealthy, d.Health)
	}
}

func TestMPSV3ReconcileRetainsCompletedInitUntilPodTerminates(t *testing.T) {
	m, f := testMPSV3(t)
	plugin := newTestPlugin(t)
	plugin.mpsV3, plugin.runCtx = m, context.Background()
	a := testMPSAllocation(t, 2)
	_, err := m.ensure(plugin.runCtx, a)
	require.NoError(t, err)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "test-pod"},
		Spec:       corev1.PodSpec{InitContainers: []corev1.Container{{Name: a.Container}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, InitContainerStatuses: []corev1.ContainerStatus{{
			Name: a.Container, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}},
		}}},
	}
	plugin.listLiveNodePods = func() ([]*corev1.Pod, error) { return []*corev1.Pod{pod}, nil }

	plugin.reconcileMPSV3()
	require.Contains(t, f.namespaces, a.Namespace)
	require.FileExists(t, m.cdiPath(a.Namespace))

	pod.Status.Phase = corev1.PodSucceeded
	plugin.reconcileMPSV3()
	require.Empty(t, f.namespaces)
	require.Empty(t, f.partitions)
	require.NoFileExists(t, m.cdiPath(a.Namespace))
}

func TestMPSV3RollbackRetriesForLivePodAfterInventoryFailure(t *testing.T) {
	m, f := testMPSV3(t)
	plugin := newTestPlugin(t)
	plugin.mpsV3, plugin.runCtx = m, context.Background()
	plugin.listLiveNodePods = func() ([]*corev1.Pod, error) {
		return []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "test-pod"}}}, nil
	}
	a := testMPSAllocation(t, 2)
	_, err := m.ensure(plugin.runCtx, a)
	require.NoError(t, err)

	f.fail = "server list"
	require.Error(t, m.release(plugin.runCtx, a.Namespace))
	require.NoError(t, m.load())
	require.Contains(t, m.allocations, a.Namespace)
	require.True(t, m.allocations[a.Namespace].Prepared)
	require.True(t, m.allocations[a.Namespace].CleanupPending)
	require.Contains(t, f.namespaces, a.Namespace)

	f.fail = ""
	plugin.reconcileMPSV3()
	require.Empty(t, m.allocations)
	require.Empty(t, f.namespaces)
	require.Empty(t, f.partitions)
	require.NoFileExists(t, m.cdiPath(a.Namespace))
}

func TestMPSV3RecoveryIsolatesAllocationFailure(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	a := testMPSAllocation(t, 0)
	_, err := m.ensure(ctx, a)
	require.NoError(t, err)

	f.fail = "namespace get " + a.Namespace
	require.NoError(t, m.recover(ctx, map[string]bool{a.PodUID: true}))
	require.False(t, m.gpuHealthy("GPU-a"))
	require.True(t, m.gpuHealthy("GPU-b"))
	require.True(t, m.allocations[a.Namespace].Prepared)
}

func TestMPSV3HealthQueryThresholdAndCachedPods(t *testing.T) {
	m, f := testMPSV3(t)
	ctx := context.Background()
	a := testMPSAllocation(t, 0)
	_, err := m.ensure(ctx, a)
	require.NoError(t, err)

	plugin := newTestPlugin(t)
	plugin.mpsV3, plugin.runCtx = m, ctx
	pods := []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "test-pod"}}}
	plugin.listNodePods = func() ([]*corev1.Pod, error) { return pods, nil }
	queries := 0
	plugin.listLiveNodePods = func() ([]*corev1.Pod, error) { queries++; return pods, nil }

	f.fail = "compute-policy get"
	for range 2 {
		plugin.reconcileMPSV3()
		require.True(t, m.gpuHealthy("GPU-a"))
	}
	plugin.reconcileMPSV3()
	require.False(t, m.gpuHealthy("GPU-a"))

	f.fail = ""
	plugin.reconcileMPSV3()
	require.True(t, m.gpuHealthy("GPU-a"))
	require.Zero(t, queries)
	plugin.listNodePods = func() ([]*corev1.Pod, error) { return nil, nil }
	plugin.reconcileMPSV3()
	require.Equal(t, 1, queries)
	require.Contains(t, m.allocations, a.Namespace)
}
