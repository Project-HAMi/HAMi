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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	cdspec "tags.cncf.io/container-device-interface/specs-go"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

type fakeMPSV3 struct {
	servers    map[string]string
	namespaces map[string]int
	partitions map[string]mpsV3Partition
	calls      []string
	fail       string
	busy       bool
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
	f := &fakeMPSV3{servers: make(map[string]string), namespaces: make(map[string]int), partitions: make(map[string]mpsV3Partition)}
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
		case "client list":
			if f.busy {
				for name := range f.namespaces {
					return "123,srv,GPU-a,0,0," + flag("server") + "/" + name, nil
				}
			}
			return "", nil
		case "server list":
			var rows []string
			for name := range f.servers {
				rows = append(rows, name+",0,123,Ready")
			}
			return strings.Join(rows, "\n"), nil
		case "server create":
			f.servers[args[2]] = flag("allowed-devices")
			return "", nil
		case "server get":
			switch args[3] {
			case "name":
				return args[2], nil
			case "status":
				return "Ready", nil
			case "allowed-devices":
				return f.servers[args[2]], nil
			}
		case "server delete":
			delete(f.servers, args[2])
			return "", nil
		case "namespace create":
			f.namespaces[args[2]] = 100
			dir := filepath.Join(root, "pipe", flag("server"), args[2])
			require.NoError(t, os.MkdirAll(dir, 0755))
			listener, err := net.Listen("unix", filepath.Join(dir, "control"))
			require.NoError(t, err)
			t.Cleanup(func() { listener.Close() })
			return "", nil
		case "namespace set":
			n, _ := strconv.Atoi(flag("active-thread-percentage"))
			f.namespaces[args[2]] = n
			return "", nil
		case "namespace get":
			n, ok := f.namespaces[args[2]]
			if !ok {
				return "", fmt.Errorf("missing namespace")
			}
			return strconv.Itoa(n), nil
		case "namespace list":
			var rows []string
			for name := range f.namespaces {
				rows = append(rows, name+","+flag("server")+",Ready")
			}
			return strings.Join(rows, "\n"), nil
		case "namespace delete":
			if f.busy {
				return "", fmt.Errorf("active client")
			}
			delete(f.namespaces, args[2])
			return "", nil
		case "sm-partition create":
			chunks, _ := strconv.ParseInt(flag("chunks"), 10, 64)
			f.partitions[flag("device")+"/"+args[2]] = mpsV3Partition{SMs: chunks * 2, ID: "opaque_" + args[2]}
			return "", nil
		case "sm-partition list":
			var rows []string
			for key, p := range f.partitions {
				gpu, name, _ := strings.Cut(key, "/")
				if gpu == flag("device") {
					rows = append(rows, fmt.Sprintf("%s,%s,%s,%d,%d,false,created,%s", flag("server"), gpu, name, p.SMs/2, p.SMs, p.ID))
				}
			}
			return strings.Join(rows, "\n"), nil
		case "sm-partition delete":
			if f.busy {
				return "", fmt.Errorf("partition in use")
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
				require.NotEmpty(t, saved.Partitions["GPU-a"])
				require.Equal(t, 100, f.namespaces[a.Namespace])
			} else {
				require.Equal(t, 25, f.namespaces[a.Namespace])
			}
			data, err := os.ReadFile(m.cdiPath(a.Namespace))
			require.NoError(t, err)
			var spec cdspec.Spec
			require.NoError(t, json.Unmarshal(data, &spec))
			require.Contains(t, spec.Devices[0].ContainerEdits.Env, "NVIDIA_GPU_MEMORY_LIMIT=2048")
			if sms > 0 {
				require.Contains(t, strings.Join(spec.Devices[0].ContainerEdits.Env, "\n"), "CUDA_MPS_SM_PARTITION=GPU-a/opaque_")
			}
			// Retry uses the same namespace and opaque driver partition ID.
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
	require.Len(t, response.Mounts, 1)
	require.Equal(t, mpsV3ContainerPipe, response.Mounts[0].ContainerPath)
	require.True(t, response.Mounts[0].ReadOnly)
	require.NotContains(t, response.Mounts[0].HostPath, "default")
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
	require.Len(t, ctr.Mounts, 1)
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

func TestMPSV3InitContainerCompletion(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "init"}}}, Status: corev1.PodStatus{InitContainerStatuses: []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}}}}
	require.True(t, finishedMPSInitContainer(pod, "init"))
	require.False(t, finishedMPSInitContainer(pod, "main"))
	policy := corev1.ContainerRestartPolicyAlways
	pod.Spec.InitContainers[0].RestartPolicy = &policy
	require.False(t, finishedMPSInitContainer(pod, "init"))
	pod.Spec.InitContainers[0].RestartPolicy = nil
	pod.Status.InitContainerStatuses[0].State.Terminated.ExitCode = 1
	require.False(t, finishedMPSInitContainer(pod, "init"))
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
