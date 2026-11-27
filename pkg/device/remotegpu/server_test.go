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

package remotegpu

import (
	"context"
	"sort"
	"testing"
	"time"

	yamlv2 "go.yaml.in/yaml/v2"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

var testServer = ServerConfig{Image: "lupine-server:test"}

// placedPod is a client pod Filter placed on gpu-a: container a shares GPU-1
// by memory, container b takes GPU-2 whole.
func placedPod(uid string) *corev1.Pod {
	alloc := device.EncodePodSingleDevice(device.PodSingleDevice{
		{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 3000}},
		{{UUID: "gpu-a/GPU-2", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
	})
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-" + uid, Namespace: "team", UID: types.UID(uid),
			Labels: map[string]string{RemoteGPUPodLabel: "true"},
			Annotations: map[string]string{
				AllocatedAnnos:    alloc,
				LupineServerAnno:  "10.0.0.5:14834",
				ServerDevicesAnno: "GPU-1,GPU-2",
			},
		},
		Spec: corev1.PodSpec{NodeName: "cpu-1", Containers: []corev1.Container{{Name: "a"}, {Name: "b"}}},
	}
}

func serverEnv(t *testing.T, pod *corev1.Pod) map[string]string {
	t.Helper()
	env := map[string]string{}
	for _, e := range pod.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	return env
}

// The server sees only the pod's cards and listens on the pod's endpoint. A
// card shared by memory is limited to the pod's share; a whole card is not.
func TestServerPod_SeesOnlyThePodsCards(t *testing.T) {
	InitRemoteGPUDevice(testConfig())
	server, err := serverPod("hami", placedPod("uid-a"), testServer)
	assert.NilError(t, err)
	assert.Equal(t, server.Namespace, "hami")
	assert.Equal(t, server.Spec.NodeName, "gpu-a")
	assert.Assert(t, server.Spec.HostNetwork)
	assert.Equal(t, server.Labels[ServerClientLabel], "uid-a")
	assert.Equal(t, server.Labels["hami.io/webhook"], "ignore", "the webhook must not treat the server as a GPU workload")
	assert.Equal(t, server.Annotations[LupineServerAnno], "10.0.0.5:14834")
	assert.Equal(t, server.Annotations[serverClientAnno], "team/worker-uid-a")
	assert.Equal(t, *server.Spec.AutomountServiceAccountToken, false)

	ctr := server.Spec.Containers[0]
	assert.Equal(t, ctr.Image, "lupine-server:test")
	assert.Equal(t, ctr.Ports[0].HostPort, int32(14834), "kubelet then refuses a second server on the port")
	env := serverEnv(t, server)
	assert.Equal(t, env["NVIDIA_VISIBLE_DEVICES"], "GPU-1,GPU-2")
	assert.Equal(t, env["LUPINE_PORT"], "14834")
	assert.Equal(t, env["CUDA_DEVICE_ORDER"], "PCI_BUS_ID")
	assert.Equal(t, env["LD_PRELOAD"], libMountPath+"/libvgpu.so")
	assert.Equal(t, env["CUDA_DEVICE_MEMORY_LIMIT_0"], "3000m")
	_, limited := env["CUDA_DEVICE_MEMORY_LIMIT_1"]
	assert.Assert(t, !limited, "a whole card is not limited")
	_, fallback := env["CUDA_DEVICE_MEMORY_LIMIT"]
	assert.Assert(t, !fallback, "a fallback limit would cap the whole card too")
	assert.Equal(t, server.Spec.InitContainers[0].Image, "projecthami/hami:test")
}

// Containers of the pod sharing a card are held to their shares together.
func TestServerPod_AddsUpThePodsSharesOfACard(t *testing.T) {
	InitRemoteGPUDevice(testConfig())
	pod := placedPod("uid-a")
	pod.Annotations[AllocatedAnnos] = device.EncodePodSingleDevice(device.PodSingleDevice{
		{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 3000}},
		{{UUID: "gpu-a/GPU-1", Type: RemoteGPUCommonWord, Usedmem: 2000}},
	})
	pod.Annotations[ServerDevicesAnno] = "GPU-1"
	server, err := serverPod("hami", pod, testServer)
	assert.NilError(t, err)
	assert.Equal(t, serverEnv(t, server)["CUDA_DEVICE_MEMORY_LIMIT_0"], "5000m")
}

// Whole cards, or no library to enforce a share with, need no HAMi-core.
func TestServerPod_NoLibraryWithoutAShare(t *testing.T) {
	InitRemoteGPUDevice(testConfig())
	pod := placedPod("uid-a")
	pod.Annotations[AllocatedAnnos] = device.EncodePodSingleDevice(device.PodSingleDevice{
		{{UUID: "gpu-a/GPU-2", Type: RemoteGPUCommonWord, Usedmem: 40000, Usedcores: 100}},
	})
	pod.Annotations[ServerDevicesAnno] = "GPU-2"
	server, err := serverPod("hami", pod, testServer)
	assert.NilError(t, err)
	assert.Equal(t, len(server.Spec.InitContainers), 0)
	_, preload := serverEnv(t, server)["LD_PRELOAD"]
	assert.Assert(t, !preload)

	cfg := testConfig()
	cfg.LibImage = ""
	InitRemoteGPUDevice(cfg)
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })
	server, err = serverPod("hami", placedPod("uid-a"), testServer)
	assert.NilError(t, err)
	assert.Equal(t, len(server.Spec.InitContainers), 0)
}

// A pod placed again gets a new server, so the old one is told apart and
// removed rather than left serving the last placement.
func TestServerPod_NameFollowsThePlacement(t *testing.T) {
	InitRemoteGPUDevice(testConfig())
	pod := placedPod("uid-a")
	first, err := serverPod("hami", pod, testServer)
	assert.NilError(t, err)
	again, err := serverPod("hami", pod, testServer)
	assert.NilError(t, err)
	assert.Equal(t, first.Name, again.Name)
	pod.Annotations[LupineServerAnno] = "10.0.0.5:14835"
	moved, err := serverPod("hami", pod, testServer)
	assert.NilError(t, err)
	assert.Assert(t, moved.Name != first.Name)
	assert.Assert(t, len(moved.Name) <= 63, moved.Name)
}

func TestServerPod_RefusesAnIncompletePlacement(t *testing.T) {
	InitRemoteGPUDevice(testConfig())
	for name, mutate := range map[string]func(*corev1.Pod){
		"no endpoint": func(p *corev1.Pod) { delete(p.Annotations, LupineServerAnno) },
		"bad port":    func(p *corev1.Pod) { p.Annotations[LupineServerAnno] = "10.0.0.5:0" },
		"no cards":    func(p *corev1.Pod) { delete(p.Annotations, ServerDevicesAnno) },
		"no alloc":    func(p *corev1.Pod) { delete(p.Annotations, AllocatedAnnos) },
	} {
		t.Run(name, func(t *testing.T) {
			pod := placedPod("uid-a")
			mutate(pod)
			_, err := serverPod("hami", pod, testServer)
			assert.Assert(t, err != nil)
		})
	}
	_, err := serverPod("hami", placedPod("uid-a"), ServerConfig{})
	assert.ErrorContains(t, err, "image")
}

// The chart renders Kubernetes types by their field names.
func TestServerConfig_ReadsKubernetesFieldNames(t *testing.T) {
	var cfg RemoteGPUConfig
	err := yamlv2.UnmarshalStrict([]byte(`
server:
  image: lupine-server:test
  imagePullPolicy: Always
  imagePullSecrets: [regcred]
  runtimeClassName: nvidia
  tolerations:
  - key: gpu
    operator: Exists
    effect: NoSchedule
  securityContext:
    capabilities:
      add: [IPC_LOCK]
  resources:
    limits:
      memory: 4Gi
  env:
  - name: LD_LIBRARY_PATH
    value: /usr/local/nvidia/lib64
  livenessProbe:
    tcpSocket:
      port: lupine
    periodSeconds: 10
  readinessProbe:
    httpGet:
      path: /metrics
      port: lupine
`), &cfg)
	assert.NilError(t, err)
	assert.Equal(t, cfg.Server.Image, "lupine-server:test")
	assert.Equal(t, cfg.Server.ImagePullPolicy, corev1.PullAlways)
	assert.Equal(t, cfg.Server.Tolerations[0].Operator, corev1.TolerationOpExists)
	assert.Equal(t, cfg.Server.SecurityContext.Capabilities.Add[0], corev1.Capability("IPC_LOCK"))
	assert.Assert(t, cfg.Server.Resources.Limits.Memory().Equal(resource.MustParse("4Gi")))
	assert.DeepEqual(t, cfg.Server.Env, []corev1.EnvVar{{Name: "LD_LIBRARY_PATH", Value: "/usr/local/nvidia/lib64"}})
	assert.Equal(t, cfg.Server.LivenessProbe.TCPSocket.Port.String(), "lupine")
	assert.Equal(t, cfg.Server.LivenessProbe.PeriodSeconds, int32(10))
	assert.Equal(t, cfg.Server.ReadinessProbe.HTTPGet.Path, "/metrics")
	assert.Equal(t, cfg.Server.ReadinessProbe.HTTPGet.Port.String(), "lupine")

	err = yamlv2.UnmarshalStrict([]byte("server:\n  imag: typo\n"), &cfg)
	assert.ErrorContains(t, err, "imag")
}

// Configured variables reach the server, but none can undo the cards, port, or
// memory limits HAMi set for it.
func TestServerPod_AddsConfiguredEnv(t *testing.T) {
	InitRemoteGPUDevice(testConfig())
	cfg := testServer
	cfg.Env = []corev1.EnvVar{
		{Name: "LD_LIBRARY_PATH", Value: "/usr/local/nvidia/lib64"},
		{Name: "NVIDIA_VISIBLE_DEVICES", Value: "all"},
		{Name: "LUPINE_PORT", Value: "1"},
		{Name: "CUDA_DEVICE_MEMORY_LIMIT_0", Value: "99999m"},
		{Name: "CUDA_DEVICE_MEMORY_LIMIT_1", Value: "1m"},
		{Name: "CUDA_DEVICE_MEMORY_LIMIT", Value: "1m"},
	}
	server, err := serverPod("hami", placedPod("uid-a"), cfg)
	assert.NilError(t, err)
	seen := map[string]int{}
	for _, e := range server.Spec.Containers[0].Env {
		seen[e.Name]++
	}
	for name, n := range seen {
		assert.Equal(t, n, 1, "%s set twice", name)
	}
	env := serverEnv(t, server)
	assert.Equal(t, env["LD_LIBRARY_PATH"], "/usr/local/nvidia/lib64")
	assert.Equal(t, env["NVIDIA_VISIBLE_DEVICES"], "GPU-1,GPU-2")
	assert.Equal(t, env["LUPINE_PORT"], "14834")
	assert.Equal(t, env["CUDA_DEVICE_MEMORY_LIMIT_0"], "3000m")
	_, limited := env["CUDA_DEVICE_MEMORY_LIMIT_1"]
	assert.Assert(t, !limited, "a whole card is not limited")
	_, fallback := env["CUDA_DEVICE_MEMORY_LIMIT"]
	assert.Assert(t, !fallback)
}

// The server container carries the configured probes, and they reach its
// endpoint through the port name.
func TestServerPod_CarriesTheProbes(t *testing.T) {
	InitRemoteGPUDevice(testConfig())
	cfg := testServer
	cfg.LivenessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("lupine")}}}
	cfg.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/metrics", Port: intstr.FromString("lupine")}}}
	server, err := serverPod("hami", placedPod("uid-a"), cfg)
	assert.NilError(t, err)
	ctr := server.Spec.Containers[0]
	assert.Equal(t, ctr.Ports[0].Name, "lupine")
	assert.DeepEqual(t, ctr.LivenessProbe, cfg.LivenessProbe)
	assert.DeepEqual(t, ctr.ReadinessProbe, cfg.ReadinessProbe)

	server, err = serverPod("hami", placedPod("uid-a"), testServer)
	assert.NilError(t, err)
	assert.Assert(t, server.Spec.Containers[0].LivenessProbe == nil)
	assert.Assert(t, server.Spec.Containers[0].ReadinessProbe == nil)
}

func serverScene(t *testing.T, objs ...runtime.Object) (*RemoteGPUDevices, *fake.Clientset) {
	t.Helper()
	cfg := testConfig()
	cfg.Server = testServer
	dev := InitRemoteGPUDevice(cfg)
	c := fake.NewSimpleClientset(objs...)
	prev := client.GetClient()
	client.KubeClient = c
	t.Setenv(podNamespaceEnv, "hami")
	t.Cleanup(func() { client.KubeClient = prev })
	return dev, c
}

func serverNames(t *testing.T, c *fake.Clientset) []string {
	t.Helper()
	list, err := c.CoreV1().Pods("hami").List(context.Background(), metav1.ListOptions{LabelSelector: ServerClientLabel})
	assert.NilError(t, err)
	var names []string
	for _, p := range list.Items {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

func mustServer(t *testing.T, pod *corev1.Pod) *corev1.Pod {
	t.Helper()
	s, err := serverPod("hami", pod, testServer)
	assert.NilError(t, err)
	return s
}

// Bind starts the server from the annotations Filter wrote, which the
// scheduler's cached copy of the pod may not have yet, or may have from an
// earlier attempt.
func TestStartServer_ReadsTheAnnotationsFilterWrote(t *testing.T) {
	live := placedPod("uid-a")
	live.Spec.NodeName = ""
	dev, c := serverScene(t, live)
	cached := live.DeepCopy()
	cached.Annotations[LupineServerAnno] = "10.0.0.5:14899"
	assert.NilError(t, dev.StartServer(context.Background(), cached))
	assert.DeepEqual(t, serverNames(t, c), []string{mustServer(t, live).Name})
	assert.NilError(t, dev.StartServer(context.Background(), live), "a server already started is no error")
	replaced := live.DeepCopy()
	replaced.UID = "uid-old"
	assert.ErrorContains(t, dev.StartServer(context.Background(), replaced), "replaced")

	dev.StopServer(context.Background(), live)
	assert.Equal(t, len(serverNames(t, c)), 0)
}

func TestStartServer_NeedsANamespace(t *testing.T) {
	dev, _ := serverScene(t, placedPod("uid-a"))
	t.Setenv(podNamespaceEnv, "")
	assert.ErrorIs(t, dev.StartServer(context.Background(), placedPod("uid-a")), errNoServerNamespace)
}

func TestReconcileServers(t *testing.T) {
	running := placedPod("running")
	finished := placedPod("finished")
	finished.Status.Phase = corev1.PodSucceeded
	pending := placedPod("pending")
	pending.Spec.NodeName = ""
	stale := placedPod("stale-pending")
	stale.Spec.NodeName = ""
	moved := placedPod("moved")
	crashed := placedPod("crashed")

	old := mustServer(t, moved)
	moved.Annotations[LupineServerAnno] = "10.0.0.5:14840"
	failed := mustServer(t, crashed)
	failed.Status.Phase = corev1.PodFailed
	starting := mustServer(t, pending)
	starting.CreationTimestamp = metav1.Now()
	abandoned := mustServer(t, stale)
	abandoned.CreationTimestamp = metav1.NewTime(time.Now().Add(-2 * serverBindGrace))
	orphan := mustServer(t, placedPod("deleted"))
	done := mustServer(t, finished)

	dev, c := serverScene(t, running, finished, pending, stale, moved, crashed,
		old, failed, starting, abandoned, orphan, done)
	dev.ReconcileServers(context.Background())

	// The failed server is replaced under the same name.
	want := []string{mustServer(t, running).Name, mustServer(t, moved).Name, starting.Name, failed.Name}
	sort.Strings(want)
	assert.DeepEqual(t, serverNames(t, c), want)
	replaced, err := c.CoreV1().Pods("hami").Get(context.Background(), failed.Name, metav1.GetOptions{})
	assert.NilError(t, err)
	assert.Equal(t, replaced.Status.Phase, corev1.PodPhase(""))

	dev.ReconcileServers(context.Background())
	assert.DeepEqual(t, serverNames(t, c), want)
}

// A failed list must not read as "no clients", which would remove every
// server.
func TestReconcileServers_KeepsServersWhenClientsCannotBeListed(t *testing.T) {
	server := mustServer(t, placedPod("uid-a"))
	dev, c := serverScene(t, server)
	c.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() == metav1.NamespaceAll {
			return true, nil, errNoServerMetrics
		}
		return false, nil, nil
	})
	dev.ReconcileServers(context.Background())
	assert.DeepEqual(t, serverNames(t, c), []string{server.Name})
}
