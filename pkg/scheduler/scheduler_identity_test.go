/*
Copyright 2024 The HAMi Authors.

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

package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// schedulerWithPods returns a scheduler whose lister and API client both hold
// pods, which is what /filter and /bind resolve their requests against.
func schedulerWithPods(t *testing.T, pods ...*corev1.Pod) *Scheduler {
	t.Helper()

	s := NewScheduler()
	objects := make([]runtime.Object, 0, len(pods))
	for _, p := range pods {
		objects = append(objects, p)
	}
	s.kubeClient = fake.NewClientset(objects...)

	informerFactory := informers.NewSharedInformerFactoryWithOptions(s.kubeClient, time.Hour)
	indexer := informerFactory.Core().V1().Pods().Informer().GetIndexer()
	for _, p := range pods {
		require.NoError(t, indexer.Add(p))
	}
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	return s
}

func gpuPod(namespace, name string, uid types.UID) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: uid},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "main",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"hami.io/gpu":    *resource.NewQuantity(1, resource.BinarySI),
				"hami.io/gpumem": *resource.NewQuantity(500, resource.BinarySI),
			}},
		}}},
	}
}

func initNvidiaDevices(t *testing.T) {
	t.Helper()
	require.NoError(t, config.InitDevicesWithConfig(&config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:  "hami.io/gpu",
			ResourceMemoryName: "hami.io/gpumem",
			ResourceCoreName:   "hami.io/gpucores",
			DefaultGPUNum:      1,
		},
	}))
}

// The node chosen during filtering is the only node a pod may be bound to.
// Honouring a different one would leave the reservation on the scheduled node
// while the pod consumed devices somewhere else.
//
// Reproduce on a cluster: after Filter has picked node-1 for a pod (so the
// scheduler cache holds that reservation), send /bind for the same pod naming
// a different node, for example node-2. Before this fix the extender bound
// it there anyway, since args.Node was trusted outright; the reservation
// stayed on node-1 while the pod actually ran on node-2. The AddPod call
// below stands in for that completed Filter step.
func TestBindRejectsNodeDifferentFromScheduledNode(t *testing.T) {
	pod := gpuPod("team-a", "gpu-pod", "gpu-pod-uid")
	s := schedulerWithPods(t, pod)
	s.podManager.AddPod(pod, "node-1", device.PodDevices{})

	res, err := s.Bind(extenderv1.ExtenderBindingArgs{
		PodName:      "gpu-pod",
		PodNamespace: "team-a",
		PodUID:       "gpu-pod-uid",
		Node:         "node-2",
	})

	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(res.Error, "node-1"), "bind result: %q", res.Error)
	assert.Assert(t, strings.Contains(res.Error, "node-2"), "bind result: %q", res.Error)

	pi, ok := s.podManager.GetPod(pod)
	assert.Equal(t, true, ok, "the reservation was dropped by a rejected bind")
	assert.Equal(t, "node-1", pi.NodeID)
}

func TestVerifyBindTargetRequiresReservationForHAMiPod(t *testing.T) {
	initNvidiaDevices(t)

	for _, test := range []struct {
		name              string
		addEmptyNodeEntry bool
	}{
		{name: "missing reservation"},
		{name: "reservation without node", addEmptyNodeEntry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pod := gpuPod("team-a", "gpu-pod", "gpu-pod-uid")
			s := schedulerWithPods(t, pod)
			if test.addEmptyNodeEntry {
				s.podManager.AddPod(pod, "", device.PodDevices{})
			}

			err := s.verifyBindTarget(extenderv1.ExtenderBindingArgs{
				PodName:      pod.Name,
				PodNamespace: pod.Namespace,
				PodUID:       pod.UID,
				Node:         "node-1",
			}, pod)

			assert.ErrorContains(t, err, "no scheduler reservation")
		})
	}
}

func TestVerifyBindTargetAllowsPodWithoutHAMiResourcesWithoutReservation(t *testing.T) {
	initNvidiaDevices(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "cpu-pod", UID: "cpu-pod-uid"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	s := schedulerWithPods(t, pod)

	err := s.verifyBindTarget(extenderv1.ExtenderBindingArgs{
		PodName:      pod.Name,
		PodNamespace: pod.Namespace,
		PodUID:       pod.UID,
		Node:         "node-1",
	}, pod)

	assert.NilError(t, err)
}

// The reported attack: a bind request pairs a running pod's UID with a pod
// name that does not exist. The lookup fails, and the stale-allocation
// cleanup that follows must not free the devices of whoever holds that UID.
//
// Reproduce on a cluster (issue #3030): run team-a/victim with
// nvidia.com/gpumem: 20000 on a node with one 24 GiB GPU, then from any other
// pod on that node's network:
//
//	curl -sk -X POST https://hami-scheduler.kube-system.svc/bind \
//	  -H 'Content-Type: application/json' \
//	  -d '{"PodName":"does-not-exist","PodNamespace":"team-b","PodUID":"<victim-uid>","Node":"gpu-node-1"}'
//
// Before this fix, the log showed "Pod taken and deleted"
// pod="team-b/does-not-exist", the victim's GPU memory was freed, and a
// second pod could be placed on the same GPU. The setup below is that same
// state (a cached reservation under victim-uid) with the forged request
// replayed directly against Bind, so it needs no cluster or curl.
func TestBindDoesNotDropAnotherPodsReservation(t *testing.T) {
	victim := gpuPod("team-a", "victim", "victim-uid")
	s := schedulerWithPods(t, victim)
	reserved := device.PodDevices{nvidia.NvidiaGPUDevice: device.PodSingleDevice{{
		device.ContainerDevice{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000},
	}}}
	s.podManager.AddPod(victim, "gpu-node-1", reserved)

	_, err := s.Bind(extenderv1.ExtenderBindingArgs{
		PodName:      "does-not-exist",
		PodNamespace: "team-b",
		PodUID:       "victim-uid",
		Node:         "gpu-node-1",
	})
	assert.Assert(t, err != nil, "a bind for a pod that does not exist should fail")

	pi, ok := s.podManager.GetPod(victim)
	assert.Equal(t, true, ok, "the victim's reservation was freed by a bind naming another pod")
	assert.Equal(t, "gpu-node-1", pi.NodeID)
	assert.Equal(t, 1, len(pi.Devices[nvidia.NvidiaGPUDevice]))
}

// A filter request carrying an identity that no longer matches the live pod
// must be refused before it reserves anything, and before it patches
// annotations onto the pod whose name it borrowed.
//
// Reproduce on a cluster: send /filter with a Pod object naming a real,
// running pod (team-a/gpu-pod) but a UID that pod no longer has, for example
// one left over from a prior pod of the same name. Before this fix, Filter
// scored and reserved devices for that name/namespace and patched
// hami.io/vgpu-node onto whatever pod team-a/gpu-pod currently is, without
// ever checking the UID matched.
func TestFilterRejectsForgedPodIdentityBeforeReservation(t *testing.T) {
	initNvidiaDevices(t)

	live := gpuPod("team-a", "gpu-pod", "current-uid")
	s := schedulerWithPods(t, live)

	forged := gpuPod("team-a", "gpu-pod", "stale-uid")
	res, err := s.Filter(extenderv1.ExtenderArgs{Pod: forged, NodeNames: &[]string{"node-1"}})

	assert.Assert(t, err != nil, "a filter request with a mismatched UID should be refused")
	assert.Assert(t, res == nil)
	assert.Assert(t, strings.Contains(err.Error(), "stale-uid"), "error: %v", err)

	_, cached := s.podManager.GetPod(live)
	assert.Equal(t, false, cached, "the request reserved devices despite the identity mismatch")
	_, cachedForged := s.podManager.GetPod(forged)
	assert.Equal(t, false, cachedForged)

	stored, getErr := s.kubeClient.CoreV1().Pods("team-a").Get(t.Context(), "gpu-pod", metav1.GetOptions{})
	assert.NilError(t, getErr)
	_, patched := stored.Annotations[util.AssignedNodeAnnotations]
	assert.Equal(t, false, patched, "the live pod was annotated by a request it did not make")
}

// A pod that already carries a node has been through binding, so its devices
// are in use. Filtering it again would release and re-reserve them.
func TestFilterRejectsPodAlreadyAssignedToNode(t *testing.T) {
	initNvidiaDevices(t)

	running := gpuPod("team-a", "running", "running-uid")
	running.Spec.NodeName = "gpu-node-1"
	s := schedulerWithPods(t, running)
	reserved := device.PodDevices{nvidia.NvidiaGPUDevice: device.PodSingleDevice{{
		device.ContainerDevice{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000},
	}}}
	s.podManager.AddPod(running, "gpu-node-1", reserved)

	_, err := s.Filter(extenderv1.ExtenderArgs{
		Pod:       gpuPod("team-a", "running", "running-uid"),
		NodeNames: &[]string{"node-1"},
	})

	assert.Assert(t, err != nil, "filtering an already assigned pod should be refused")
	assert.Assert(t, strings.Contains(err.Error(), "gpu-node-1"), "error: %v", err)

	pi, ok := s.podManager.GetPod(running)
	assert.Equal(t, true, ok, "the running pod's reservation was released")
	assert.Equal(t, 1, len(pi.Devices[nvidia.NvidiaGPUDevice]))
}

// Leaving the UID out must not be a way around the identity check.
func TestBindRejectsRequestWithoutUID(t *testing.T) {
	pod := gpuPod("team-a", "gpu-pod", "gpu-pod-uid")
	s := schedulerWithPods(t, pod)
	s.podManager.AddPod(pod, "node-1", device.PodDevices{})

	res, err := s.Bind(extenderv1.ExtenderBindingArgs{
		PodName:      "gpu-pod",
		PodNamespace: "team-a",
		Node:         "node-1",
	})

	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(res.Error, "no UID"), "bind result: %q", res.Error)
}

func TestFilterRejectsRequestWithoutUID(t *testing.T) {
	initNvidiaDevices(t)

	live := gpuPod("team-a", "gpu-pod", "gpu-pod-uid")
	s := schedulerWithPods(t, live)

	_, err := s.Filter(extenderv1.ExtenderArgs{
		Pod:       gpuPod("team-a", "gpu-pod", ""),
		NodeNames: &[]string{"node-1"},
	})

	assert.Assert(t, err != nil, "a filter request with no UID should be refused")
	assert.Assert(t, strings.Contains(err.Error(), "no UID"), "error: %v", err)

	_, cached := s.podManager.GetPod(live)
	assert.Equal(t, false, cached, "the request reserved devices despite carrying no UID")
}
