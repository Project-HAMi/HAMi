/*
Copyright 2025 The HAMi Authors.

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
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// sessionScene points the package at a fake cluster and a namespace to run in.
func sessionScene(t *testing.T, nodes []corev1.Node, pods ...*corev1.Pod) *fake.Clientset {
	t.Helper()
	objs := make([]runtime.Object, 0, len(pods))
	for _, p := range pods {
		objs = append(objs, p)
	}
	fakeClient := fake.NewSimpleClientset(objs...)
	prev := client.GetClient()
	client.KubeClient = fakeClient
	t.Setenv(podNamespaceEnv, "kube-system")
	stubFleet(t, nodes, nil, nil)
	t.Cleanup(func() { client.KubeClient = prev })
	return fakeClient
}

func stubsIn(t *testing.T, c *fake.Clientset) []corev1.Pod {
	t.Helper()
	list, err := c.CoreV1().Pods("kube-system").List(context.Background(),
		metav1.ListOptions{LabelSelector: sessionStubLabel})
	assert.NilError(t, err)
	return list.Items
}

// The design has the user port-forward to a stub rather than to the server, so
// one has to be waiting on every server before anybody asks.
func TestReconcileSessionStubs_OnePerServer(t *testing.T) {
	nodes := []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
		lupineNode("gpu-b", "10.0.0.6", "24000", []*device.DeviceInfo{gpu("GPU-2", 40000)}),
	}
	c := sessionScene(t, nodes)
	cfg := testConfig()
	cfg.SessionImage = "alpine/socat:test"
	dev := InitRemoteGPUDevice(cfg)
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })

	dev.ReconcileSessionStubs(context.Background())
	stubs := stubsIn(t, c)
	assert.Equal(t, len(stubs), 2)

	byNode := map[string]corev1.Pod{}
	for _, s := range stubs {
		byNode[s.Labels[sessionStubLabel]] = s
	}
	a, b := byNode["gpu-a"], byNode["gpu-b"]
	assert.Equal(t, a.Spec.NodeName, "gpu-a", "the stub sits on its own server's node")
	assert.Equal(t, stubEndpoint(&a), "10.0.0.5:14833")
	// The label's port carries through, so a user forwards the port they would
	// have used against the server.
	assert.Equal(t, stubEndpoint(&b), "10.0.0.6:24000")
	assert.Assert(t, strings.Contains(b.Spec.Containers[0].Args[0], "TCP-LISTEN:24000"))
	assert.Equal(t, b.Spec.Containers[0].Ports[0].ContainerPort, int32(24000))

	// A second pass changes nothing.
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, len(stubsIn(t, c)), 2)
}

// A stub outlives its usefulness the moment its server stops being one, and a
// stub pointed at an address the server no longer answers on is worse than
// none, so both are replaced rather than left.
func TestReconcileSessionStubs_DropsStubsThatNoLongerFit(t *testing.T) {
	nodes := []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	}
	stale := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: sessionStubPrefix + "gpu-gone", Namespace: "kube-system",
		Labels: map[string]string{sessionStubLabel: "gpu-gone"},
	}}
	moved := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: sessionStubPrefix + "gpu-a", Namespace: "kube-system",
			Labels: map[string]string{sessionStubLabel: "gpu-a"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "relay",
			Env:  []corev1.EnvVar{{Name: sessionEndpointEnv, Value: "10.0.0.5:9999"}},
		}}},
	}
	c := sessionScene(t, nodes, stale, moved)
	cfg := testConfig()
	cfg.SessionImage = "alpine/socat:test"
	dev := InitRemoteGPUDevice(cfg)
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })

	dev.ReconcileSessionStubs(context.Background())
	stubs := stubsIn(t, c)
	assert.Equal(t, len(stubs), 1, "the server that went takes its stub with it")
	assert.Equal(t, stubs[0].Labels[sessionStubLabel], "gpu-a")
	assert.Equal(t, stubEndpoint(&stubs[0]), "10.0.0.5:14833", "the stale address is replaced")
}

// Sessions are opt in. With no image configured there is nothing to run a stub
// from, and the scheduler should not be creating pods nobody asked for.
func TestReconcileSessionStubs_OffWithoutAnImage(t *testing.T) {
	c := sessionScene(t, []corev1.Node{
		lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)}),
	})
	dev := InitRemoteGPUDevice(testConfig())

	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, len(stubsIn(t, c)), 0)
}
