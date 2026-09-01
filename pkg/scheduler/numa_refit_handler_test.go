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

package scheduler

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

const (
	refitPodUID  = "refit-pod-uid"
	refitPodName = "refit-pod"
	refitNode    = "node-1"

	// The device-plugin identity RefitNumaAllocation is configured to accept
	// in these tests, standing in for the chart-populated config.DevicePluginNamespace
	// / config.DevicePluginServiceAccount. See issue #2878.
	devicePluginNamespace       = "hami-system"
	devicePluginServiceAccount  = "hami-device-plugin"
	expectedRefitCallerUsername = "system:serviceaccount:" + devicePluginNamespace + ":" + devicePluginServiceAccount

	// callerPod is the bound pod a valid token's TokenReview points at; it
	// runs on refitNode, matching the workload pod's node.
	callerPodName = "hami-device-plugin-xyz"
	callerPodUID  = "caller-pod-uid"
	// callerPodWrongNode is a same-SA caller bound pod that runs on a
	// different node, for the "right SA, wrong node" rejection case.
	callerPodWrongNodeName = "hami-device-plugin-other"
	callerPodWrongNodeUID  = "caller-pod-other-uid"

	validRefitToken     = "valid-device-plugin-token"
	wrongNodeRefitToken = "wrong-node-device-plugin-token"
	wrongSARefitToken   = "wrong-sa-token"
)

// setupRefitAuth points config.DevicePluginNamespace/DevicePluginServiceAccount
// at the fixture identity, installs a fake TokenReview responder recognizing
// validRefitToken/wrongNodeRefitToken/wrongSARefitToken, and gives s a
// podLister that can resolve the bound caller pods those tokens reference.
func setupRefitAuth(t *testing.T, s *Scheduler) {
	t.Helper()

	origNamespace, origSA := config.DevicePluginNamespace, config.DevicePluginServiceAccount
	config.DevicePluginNamespace = devicePluginNamespace
	config.DevicePluginServiceAccount = devicePluginServiceAccount
	t.Cleanup(func() {
		config.DevicePluginNamespace, config.DevicePluginServiceAccount = origNamespace, origSA
	})

	fakeClient := fake.NewClientset()
	fakeClient.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		review, ok := action.(k8stesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		if !ok {
			return false, nil, nil
		}
		review.Status = tokenReviewStatusFor(review.Spec.Token)
		return true, review, nil
	})
	client.KubeClient = fakeClient

	informerFactory := informers.NewSharedInformerFactoryWithOptions(fakeClient, time.Hour)
	indexer := informerFactory.Core().V1().Pods().Informer().GetIndexer()
	_ = indexer.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: callerPodName, Namespace: devicePluginNamespace, UID: callerPodUID},
		Spec:       corev1.PodSpec{NodeName: refitNode},
	})
	_ = indexer.Add(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: callerPodWrongNodeName, Namespace: devicePluginNamespace, UID: callerPodWrongNodeUID},
		Spec:       corev1.PodSpec{NodeName: "node-2"},
	})
	s.podLister = informerFactory.Core().V1().Pods().Lister()
}

// tokenReviewStatusFor stands in for the kube-apiserver's TokenReview
// authenticator against the fixture's canned bearer tokens.
func tokenReviewStatusFor(token string) authenticationv1.TokenReviewStatus {
	switch token {
	case validRefitToken:
		return authenticationv1.TokenReviewStatus{
			Authenticated: true,
			User: authenticationv1.UserInfo{
				Username: expectedRefitCallerUsername,
				Extra: map[string]authenticationv1.ExtraValue{
					boundPodNameExtraKey: {callerPodName},
					boundPodUIDExtraKey:  {callerPodUID},
				},
			},
		}
	case wrongNodeRefitToken:
		return authenticationv1.TokenReviewStatus{
			Authenticated: true,
			User: authenticationv1.UserInfo{
				Username: expectedRefitCallerUsername,
				Extra: map[string]authenticationv1.ExtraValue{
					boundPodNameExtraKey: {callerPodWrongNodeName},
					boundPodUIDExtraKey:  {callerPodWrongNodeUID},
				},
			},
		}
	case wrongSARefitToken:
		return authenticationv1.TokenReviewStatus{
			Authenticated: true,
			User:          authenticationv1.UserInfo{Username: "system:serviceaccount:other-namespace:other-sa"},
		}
	default:
		return authenticationv1.TokenReviewStatus{Authenticated: false, Error: "invalid bearer token"}
	}
}

// refitFixture builds a cache-only scheduler tracking one pod that reserves
// GPU-a, on a node carrying GPU-a (NUMA 1) and GPU-b (NUMA 0).
func refitFixture(t *testing.T, gpuBDevmem int32) (*Scheduler, *corev1.Pod) {
	t.Helper()
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: gpuBDevmem, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})

	reserved := device.PodSingleDevice{{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})

	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	setupRefitAuth(t, s)
	return s, pod
}

// stubRefitPatch replaces the annotation patch with a capture; returns the
// captured map and a call counter.
func stubRefitPatch(t *testing.T, fail error) (map[string]string, *int) {
	t.Helper()
	captured := map[string]string{}
	calls := 0
	previous := patchPodAnnotations
	patchPodAnnotations = func(_ *corev1.Pod, annotations map[string]string) error {
		calls++
		if fail != nil {
			return fail
		}
		maps.Copy(captured, annotations)
		return nil
	}
	t.Cleanup(func() { patchPodAnnotations = previous })
	return captured, &calls
}

func refitTestRequestFor(allowed ...string) device.NumaRefitRequest {
	return device.NumaRefitRequest{
		PodUID:             refitPodUID,
		PodNamespace:       "default",
		PodName:            refitPodName,
		NodeName:           refitNode,
		ContainerIndex:     0,
		DeviceType:         nvidia.NvidiaGPUDevice,
		AllowedDeviceUUIDs: allowed,
	}
}

func trackedUUID(t *testing.T, s *Scheduler) string {
	t.Helper()
	pi, ok := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	assert.Equal(t, ok, true)
	return pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID
}

func TestRefitNumaAllocationMovesReservation(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	captured, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-b"), validRefitToken)

	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)
	devices, err := device.DecodeContainerDevices(response.ContainerDevices)
	assert.NilError(t, err)
	assert.Equal(t, len(devices), 1)
	assert.Equal(t, devices[0].UUID, "GPU-b")
	assert.Equal(t, devices[0].Usedmem, int32(20000))
	assert.Equal(t, devices[0].Usedcores, int32(30))

	// Both annotations were patched together onto the new device, with the
	// value shape preserved byte for byte (no phantom container entries).
	assert.Equal(t, *calls, 1)
	expected := device.EncodePodSingleDevice(device.PodSingleDevice{{{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}})
	for _, key := range []string{device.InRequestDevices[nvidia.NvidiaGPUDevice], device.SupportDevices[nvidia.NvidiaGPUDevice]} {
		value, ok := captured[key]
		assert.Equal(t, ok, true, "annotation %s not patched", key)
		assert.Equal(t, value, expected, "annotation %s", key)
	}

	// In-memory accounting moved with it, without arming the init shrink.
	assert.Equal(t, trackedUUID(t, s), "GPU-b")
	pi, _ := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	assert.Equal(t, pi.InitContainerResourceReleased, false)
}

func TestRefitNumaAllocationNoOpWhenAlreadyAllowed(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	_, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-a", "GPU-b"), validRefitToken)

	assert.Equal(t, response.Succeeded, true)
	devices, err := device.DecodeContainerDevices(response.ContainerDevices)
	assert.NilError(t, err)
	assert.Equal(t, devices[0].UUID, "GPU-a")
	assert.Equal(t, *calls, 0)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationInsufficientCapacity(t *testing.T) {
	s, _ := refitFixture(t, 16000)
	_, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-b"), validRefitToken)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "no allowed device fits"), "reason: %s", response.FailureReason)
	assert.Equal(t, *calls, 0)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationUnmatchedAllowedSet(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-a-0", "GPU-a-1"), validRefitToken)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, AllowedSetUnmatched), "reason: %s", response.FailureReason)
}

func TestRefitNumaAllocationPatchFailureLeavesStateUntouched(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	_, calls := stubRefitPatch(t, errors.New("apiserver unavailable"))

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-b"), validRefitToken)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "apiserver unavailable"))
	assert.Equal(t, *calls, 1)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationConsumedAllocation(t *testing.T) {
	s, pod := refitFixture(t, 40000)
	stubRefitPatch(t, nil)
	// Simulate Allocate having consumed the container's pending entry.
	pod.Annotations[device.InRequestDevices[nvidia.NvidiaGPUDevice]] = device.EncodePodSingleDevice(device.PodSingleDevice{{}})
	s.podManager.UpdatePod(pod)

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-b"), validRefitToken)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "no pending"), "reason: %s", response.FailureReason)
	assert.Equal(t, trackedUUID(t, s), "GPU-a")
}

func TestRefitNumaAllocationValidation(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*device.NumaRefitRequest)
		wantReason string
		// token defaults to validRefitToken; the "wrong node" case needs a
		// token whose caller is actually bound to the mutated node so the
		// request clears caller authentication and reaches the deeper,
		// pod-tracking node check this case targets.
		token string
	}{
		{
			name:       "unknown pod",
			mutate:     func(r *device.NumaRefitRequest) { r.PodUID = "other-uid" },
			wantReason: "not tracked",
		},
		{
			name:       "pod identity mismatch",
			mutate:     func(r *device.NumaRefitRequest) { r.PodName = "other-name" },
			wantReason: "does not match",
		},
		{
			name:       "wrong node",
			mutate:     func(r *device.NumaRefitRequest) { r.NodeName = "node-2" },
			wantReason: "tracked on node",
			token:      wrongNodeRefitToken,
		},
		{
			name:       "unknown device type",
			mutate:     func(r *device.NumaRefitRequest) { r.DeviceType = "NoSuchVendor" },
			wantReason: "unknown device type",
		},
		{
			name:       "empty allowed set",
			mutate:     func(r *device.NumaRefitRequest) { r.AllowedDeviceUUIDs = nil },
			wantReason: "empty allowed device set",
		},
		{
			name:       "container index out of range",
			mutate:     func(r *device.NumaRefitRequest) { r.ContainerIndex = 3 },
			wantReason: "outside the pod's containers",
		},
		{
			name:       "negative container index",
			mutate:     func(r *device.NumaRefitRequest) { r.ContainerIndex = -1 },
			wantReason: "outside the pod's containers",
		},
		{
			name:       "incomplete request",
			mutate:     func(r *device.NumaRefitRequest) { r.PodNamespace = "" },
			wantReason: "incomplete refit request",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := refitFixture(t, 40000)
			_, calls := stubRefitPatch(t, nil)

			request := refitTestRequestFor("GPU-b")
			test.mutate(&request)
			token := test.token
			if token == "" {
				token = validRefitToken
			}
			response := s.RefitNumaAllocation(context.Background(), request, token)

			assert.Equal(t, response.Succeeded, false)
			assert.Assert(t, strings.Contains(response.FailureReason, test.wantReason),
				"reason %q does not contain %q", response.FailureReason, test.wantReason)
			assert.Equal(t, *calls, 0)
			assert.Equal(t, trackedUUID(t, s), "GPU-a")
		})
	}
}

func TestRefitNumaAllocationSeedsExistingAllocations(t *testing.T) {
	// Two GPU containers: container 0 already consumed its to-allocate
	// entry, container 1 is pending. The refit of container 1 must keep
	// container 0's entries intact, including the blank to-allocate slot.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})

	first := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 5000, Usedcores: 10}}
	second := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(device.PodSingleDevice{{}, second}),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(device.PodSingleDevice{first, second}),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "first"}, {Name: "second"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: {first, second}})
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	setupRefitAuth(t, s)
	captured, _ := stubRefitPatch(t, nil)

	request := refitTestRequestFor("GPU-b")
	request.ContainerIndex = 1
	response := s.RefitNumaAllocation(context.Background(), request, validRefitToken)

	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)
	toAllocate := captured[device.InRequestDevices[nvidia.NvidiaGPUDevice]]
	allocatedAnno := captured[device.SupportDevices[nvidia.NvidiaGPUDevice]]
	// Container 0's consumed to-allocate slot stays blank; its allocated
	// record stays on GPU-a; only container 1 moves to GPU-b.
	assert.Assert(t, strings.HasPrefix(toAllocate, ";"), "to-allocate: %q", toAllocate)
	assert.Assert(t, strings.Contains(toAllocate, "GPU-b"), "to-allocate: %q", toAllocate)
	assert.Assert(t, strings.Contains(allocatedAnno, "GPU-a,NVIDIA,5000,10"), "allocated: %q", allocatedAnno)
	assert.Assert(t, strings.Contains(allocatedAnno, "GPU-b,NVIDIA,20000,30"), "allocated: %q", allocatedAnno)

	// In-memory accounting is rebuilt collapsed (one aggregate entry), the
	// same shape Filter and the informer store: GPU-a keeps container 0's
	// usage, GPU-b carries the refitted container 1.
	pi, _ := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	collapsed := pi.Devices[nvidia.NvidiaGPUDevice]
	assert.Equal(t, len(collapsed), 1)
	byUUID := map[string]device.ContainerDevice{}
	for _, d := range collapsed[0] {
		byUUID[d.UUID] = d
	}
	assert.Equal(t, byUUID["GPU-a"].Usedmem, int32(5000))
	assert.Equal(t, byUUID["GPU-b"].Usedmem, int32(20000))
}

func TestRefitNumaAllocationCompetingRefits(t *testing.T) {
	// GPU-b only has room for one of the two reservations: after the first
	// refit moves onto it, the second must be refused on capacity.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 20000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})
	pods := device.NewPodManager()
	makePod := func(uid, name string) *corev1.Pod {
		reserved := device.PodSingleDevice{{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 15000, Usedcores: 10}}}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID: k8stypes.UID(uid), Name: name, Namespace: "default",
				Annotations: map[string]string{
					device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
					device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
				},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
		}
		pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
		return pod
	}
	makePod("pod-1-uid", "pod-1")
	makePod("pod-2-uid", "pod-2")
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	setupRefitAuth(t, s)
	stubRefitPatch(t, nil)

	requestFor := func(uid, name string) device.NumaRefitRequest {
		return device.NumaRefitRequest{
			PodUID: uid, PodNamespace: "default", PodName: name, NodeName: refitNode,
			DeviceType: nvidia.NvidiaGPUDevice, AllowedDeviceUUIDs: []string{"GPU-b"},
		}
	}

	firstResponse := s.RefitNumaAllocation(context.Background(), requestFor("pod-1-uid", "pod-1"), validRefitToken)
	assert.Equal(t, firstResponse.Succeeded, true, "first refit failed: %s", firstResponse.FailureReason)

	secondResponse := s.RefitNumaAllocation(context.Background(), requestFor("pod-2-uid", "pod-2"), validRefitToken)
	assert.Equal(t, secondResponse.Succeeded, false)
	assert.Assert(t, strings.Contains(secondResponse.FailureReason, "no allowed device fits"),
		"reason: %s", secondResponse.FailureReason)

	// The loser keeps its original reservation.
	pi, _ := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-2-uid"}})
	assert.Equal(t, pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID, "GPU-a")
}

func TestRefitNumaAllocationContainerNameMismatch(t *testing.T) {
	s, _ := refitFixture(t, 40000)
	_, calls := stubRefitPatch(t, nil)

	request := refitTestRequestFor("GPU-b")
	request.ContainerName = "sidecar"
	response := s.RefitNumaAllocation(context.Background(), request, validRefitToken)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, `is "main", not "sidecar"`), "reason: %s", response.FailureReason)
	assert.Equal(t, *calls, 0)

	// The matching name is accepted.
	request.ContainerName = "main"
	response = s.RefitNumaAllocation(context.Background(), request, validRefitToken)
	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)
}

func TestRefitNumaAllocationRejectsMigDevice(t *testing.T) {
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 7, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Mode: nvidia.MigMode, Health: true},
		}},
	})
	reserved := device.PodSingleDevice{{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	setupRefitAuth(t, s)
	stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-b"), validRefitToken)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "MIG"), "reason: %s", response.FailureReason)
}

func TestRefitNumaAllocationHeterogeneousReservation(t *testing.T) {
	// Two devices in one container with differing reserved amounts (possible
	// with percentage requests on mixed GPUs) cannot be re-fit faithfully.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 20000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-c", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-d", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})
	reserved := device.PodSingleDevice{{
		{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 40000, Usedcores: 30},
		{UUID: "GPU-b", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30},
	}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	setupRefitAuth(t, s)
	_, calls := stubRefitPatch(t, nil)

	response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-c", "GPU-d"), validRefitToken)

	assert.Equal(t, response.Succeeded, false)
	assert.Assert(t, strings.Contains(response.FailureReason, "heterogeneous"), "reason: %s", response.FailureReason)
	assert.Equal(t, *calls, 0)
}

func TestRefitNumaAllocationKeepsShrunkAccounting(t *testing.T) {
	// A pod whose init-container usage was already released must not have its
	// reservation re-inflated back to the init peak by a later refit.
	nodes := newNodeManager()
	nodes.addNode(refitNode, &device.NodeInfo{
		ID: refitNode, Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: refitNode}},
		Devices: map[string][]device.DeviceInfo{nvidia.NvidiaGPUDevice: {
			{ID: "GPU-a", Count: 10, Devmem: 40000, Devcore: 100, Numa: 1, Type: nvidia.NvidiaGPUDevice, Health: true},
			{ID: "GPU-b", Count: 10, Devmem: 40000, Devcore: 100, Numa: 0, Type: nvidia.NvidiaGPUDevice, Health: true},
		}},
	})

	initDevices := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 30000, Usedcores: 60}}
	appDevices := device.ContainerDevices{{UUID: "GPU-a", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 30}}
	reserved := device.PodSingleDevice{initDevices, appDevices}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: refitPodUID, Name: refitPodName, Namespace: "default",
			Annotations: map[string]string{
				device.InRequestDevices[nvidia.NvidiaGPUDevice]: device.EncodePodSingleDevice(reserved),
				device.SupportDevices[nvidia.NvidiaGPUDevice]:   device.EncodePodSingleDevice(reserved),
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "main"}},
		},
	}
	pods := device.NewPodManager()
	pods.AddPod(pod, refitNode, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	// Simulate the informer's post-init shrink, which arms the released flag.
	steady := device.SteadyStateDeviceUsage(pod, device.PodDevices{nvidia.NvidiaGPUDevice: reserved})
	pods.UpdatePodDevice(pod, steady)

	s := &Scheduler{nodeManager: nodes, podManager: pods, quotaManager: device.NewQuotaManager()}
	s.quotaManager.Quotas = map[string]*device.DeviceQuota{}
	setupRefitAuth(t, s)
	stubRefitPatch(t, nil)

	request := refitTestRequestFor("GPU-b")
	request.ContainerIndex = 1
	request.ContainerName = "main"
	response := s.RefitNumaAllocation(context.Background(), request, validRefitToken)
	assert.Equal(t, response.Succeeded, true, "refit failed: %s", response.FailureReason)

	pi, ok := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: refitPodUID}})
	assert.Equal(t, ok, true)
	assert.Equal(t, pi.InitContainerResourceReleased, true)
	// Steady state is app-only: the refitted GPU-b at the app amount, with no
	// re-inflated init-container reservation on GPU-a.
	total := int32(0)
	for _, containerDevices := range pi.Devices[nvidia.NvidiaGPUDevice] {
		for _, d := range containerDevices {
			total += d.Usedmem
			assert.Equal(t, d.UUID, "GPU-b", "unexpected device %s after refit", d.UUID)
		}
	}
	assert.Equal(t, total, int32(20000))
}

func TestRefitNumaAllocationCallerAuthentication(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "wrong service account", token: wrongSARefitToken},
		{name: "right service account wrong node", token: wrongNodeRefitToken},
		{name: "missing token", token: ""},
		{name: "invalid token", token: "garbage"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := refitFixture(t, 40000)
			_, calls := stubRefitPatch(t, nil)

			response := s.RefitNumaAllocation(context.Background(), refitTestRequestFor("GPU-b"), test.token)

			assert.Equal(t, response.Succeeded, false)
			assert.Assert(t, strings.Contains(response.FailureReason, "caller authentication"), "reason: %s", response.FailureReason)
			assert.Equal(t, *calls, 0)
			assert.Equal(t, trackedUUID(t, s), "GPU-a")
		})
	}
}
