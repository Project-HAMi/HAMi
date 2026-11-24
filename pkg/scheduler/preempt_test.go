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
	"encoding/json"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func preemptorPod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "high", Namespace: "default", UID: "preemptor-uid"}}
}

// preemptScheduler returns a scheduler that can resolve pod, which Preempt
// does before it answers a request.
func preemptScheduler(t *testing.T, pod *corev1.Pod) *Scheduler {
	t.Helper()
	s := NewScheduler()
	seedPods(t, s, pod)
	return s
}

// For a pod that asks for no device of HAMi, the preempt verb must be
// invisible to kube-scheduler: every candidate node goes back with the victims
// it came with. A node that carries no victim is a candidate too. It is how
// kube-scheduler 1.38 and later hands over a node whose in-tree filters
// already fit, and leaving it out of the answer would reject the node.
func Test_Preempt_ReturnsCandidatesUnchanged(t *testing.T) {
	tests := []struct {
		name string
		args extenderv1.ExtenderPreemptionArgs
		want map[string]*extenderv1.MetaVictims
	}{
		{
			name: "meta victims go back as they came",
			args: extenderv1.ExtenderPreemptionArgs{
				Pod: preemptorPod(),
				NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
					"node-1": {
						Pods:             []*extenderv1.MetaPod{{UID: "victim-a"}, {UID: "victim-b"}},
						NumPDBViolations: 1,
					},
				},
			},
			want: map[string]*extenderv1.MetaVictims{
				"node-1": {
					Pods:             []*extenderv1.MetaPod{{UID: "victim-a"}, {UID: "victim-b"}},
					NumPDBViolations: 1,
				},
			},
		},
		{
			name: "a candidate without victims is kept",
			args: extenderv1.ExtenderPreemptionArgs{
				Pod: preemptorPod(),
				NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
					"node-1": {Pods: []*extenderv1.MetaPod{{UID: "victim-a"}}},
					"node-2": {Pods: []*extenderv1.MetaPod{}},
					"node-3": {},
				},
			},
			want: map[string]*extenderv1.MetaVictims{
				"node-1": {Pods: []*extenderv1.MetaPod{{UID: "victim-a"}}},
				"node-2": {Pods: []*extenderv1.MetaPod{}},
				"node-3": {Pods: []*extenderv1.MetaPod{}},
			},
		},
		{
			// An extender that is not nodeCacheCapable gets whole pods, and
			// kube-scheduler still reads meta victims back.
			name: "victims are answered as meta victims",
			args: extenderv1.ExtenderPreemptionArgs{
				Pod: preemptorPod(),
				NodeNameToVictims: map[string]*extenderv1.Victims{
					"node-1": {
						Pods: []*corev1.Pod{
							{ObjectMeta: metav1.ObjectMeta{Name: "low-a", UID: "victim-a"}},
							{ObjectMeta: metav1.ObjectMeta{Name: "low-b", UID: "victim-b"}},
						},
						NumPDBViolations: 2,
					},
					"node-2": {Pods: []*corev1.Pod{}},
				},
			},
			want: map[string]*extenderv1.MetaVictims{
				"node-1": {
					Pods:             []*extenderv1.MetaPod{{UID: "victim-a"}, {UID: "victim-b"}},
					NumPDBViolations: 2,
				},
				"node-2": {Pods: []*extenderv1.MetaPod{}},
			},
		},
		{
			name: "meta victims win when a request carries both forms",
			args: extenderv1.ExtenderPreemptionArgs{
				Pod: preemptorPod(),
				NodeNameToVictims: map[string]*extenderv1.Victims{
					"node-1": {Pods: []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "from-victims"}}}},
					"node-2": {Pods: []*corev1.Pod{{ObjectMeta: metav1.ObjectMeta{UID: "only-in-victims"}}}},
				},
				NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
					"node-1": {Pods: []*extenderv1.MetaPod{{UID: "from-meta-victims"}}},
				},
			},
			want: map[string]*extenderv1.MetaVictims{
				"node-1": {Pods: []*extenderv1.MetaPod{{UID: "from-meta-victims"}}},
				"node-2": {Pods: []*extenderv1.MetaPod{{UID: "only-in-victims"}}},
			},
		},
		{
			// kube-scheduler dereferences the victims of every node in the
			// answer, and every pod among them.
			name: "the answer holds no nil entry",
			args: extenderv1.ExtenderPreemptionArgs{
				Pod: preemptorPod(),
				NodeNameToVictims: map[string]*extenderv1.Victims{
					"nil-victims": nil,
					"nil-pod":     {Pods: []*corev1.Pod{nil, {ObjectMeta: metav1.ObjectMeta{UID: "victim-a"}}}},
				},
				NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
					"nil-meta-victims": nil,
					"nil-meta-pod":     {Pods: []*extenderv1.MetaPod{nil, {UID: "victim-b"}}},
				},
			},
			want: map[string]*extenderv1.MetaVictims{
				"nil-victims":      {Pods: []*extenderv1.MetaPod{}},
				"nil-pod":          {Pods: []*extenderv1.MetaPod{{UID: "victim-a"}}},
				"nil-meta-victims": {Pods: []*extenderv1.MetaPod{}},
				"nil-meta-pod":     {Pods: []*extenderv1.MetaPod{{UID: "victim-b"}}},
			},
		},
		{
			name: "no candidates",
			args: extenderv1.ExtenderPreemptionArgs{Pod: preemptorPod()},
			want: map[string]*extenderv1.MetaVictims{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := preemptScheduler(t, tt.args.Pod).Preempt(tt.args)
			assert.NilError(t, err)
			assert.DeepEqual(t, res.NodeNameToMetaVictims, tt.want)
		})
	}
}

func Test_Preempt_RejectsRequestWithoutPod(t *testing.T) {
	res, err := NewScheduler().Preempt(extenderv1.ExtenderPreemptionArgs{
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			"node-1": {Pods: []*extenderv1.MetaPod{{UID: "victim-a"}}},
		},
	})
	assert.ErrorContains(t, err, "missing pod")
	assert.Assert(t, res == nil)
}

// Victim selection adds to the answer, and must not write into the request
// while doing so.
func Test_Preempt_AnswerDoesNotAliasRequest(t *testing.T) {
	requested := &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{{UID: "victim-a"}}}
	pod := preemptorPod()

	res, err := preemptScheduler(t, pod).Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{"node-1": requested},
	})
	assert.NilError(t, err)

	answered := res.NodeNameToMetaVictims["node-1"]
	answered.Pods[0].UID = "changed"
	answered.Pods = append(answered.Pods, &extenderv1.MetaPod{UID: "added"})
	answered.NumPDBViolations = 3

	assert.DeepEqual(t, requested, &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{{UID: "victim-a"}}})
}

// The request and the answer as kube-scheduler puts them on the wire for a
// nodeCacheCapable extender. The extender types carry no JSON tags, so the Go
// field names are the keys.
func Test_Preempt_WireFormat(t *testing.T) {
	const request = `{
		"Pod": {"metadata": {"name": "high", "namespace": "default", "uid": "preemptor-uid"}},
		"NodeNameToVictims": null,
		"NodeNameToMetaVictims": {
			"node-1": {"Pods": [{"UID": "victim-a"}], "NumPDBViolations": 1},
			"node-2": {"Pods": [], "NumPDBViolations": 0}
		}
	}`
	const want = `{"NodeNameToMetaVictims":{` +
		`"node-1":{"Pods":[{"UID":"victim-a"}],"NumPDBViolations":1},` +
		`"node-2":{"Pods":[],"NumPDBViolations":0}}}`

	var args extenderv1.ExtenderPreemptionArgs
	assert.NilError(t, json.Unmarshal([]byte(request), &args))

	res, err := preemptScheduler(t, preemptorPod()).Preempt(args)
	assert.NilError(t, err)

	answer, err := json.Marshal(res)
	assert.NilError(t, err)
	assert.Equal(t, string(answer), want)
}

// trackedOn returns what the pod cache holds for a pod that is bound to
// nodeName and has 1000 MiB of its first GPU.
func trackedOn(nodeName, uid string, priority int32) *device.PodInfo {
	return &device.PodInfo{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{UID: k8stypes.UID(uid), Name: uid, Namespace: "default"},
			Spec:       corev1.PodSpec{NodeName: nodeName, Priority: &priority},
		},
		NodeID: nodeName,
		Devices: device.PodDevices{nvidia.NvidiaGPUDevice: {{{
			UUID: nodeName + "-gpu-0", Type: nvidia.NvidiaGPUDevice, Usedmem: 1000,
		}}}},
	}
}

// Only a pod kube-scheduler can evict from this node for this pod is a
// candidate, and the candidates come out most important first, which is the
// order they are spared in.
func Test_victimCandidates(t *testing.T) {
	preemptor := preemptorPod()
	high := int32(1000)
	preemptor.Spec.Priority = &high

	started := func(pi *device.PodInfo, unix int64) *device.PodInfo {
		at := metav1.NewTime(time.Unix(unix, 0))
		pi.Status.StartTime = &at
		return pi
	}
	changed := func(pi *device.PodInfo, change func(*device.PodInfo)) *device.PodInfo {
		change(pi)
		return pi
	}

	nodePods := []*device.PodInfo{
		started(trackedOn("node-1", "low-new", 10), 200),
		started(trackedOn("node-1", "low-old", 10), 100),
		trackedOn("node-1", "mid", 500),
		trackedOn("node-1", "low-not-started", 10),
		trackedOn("node-1", "same-priority", 1000),
		trackedOn("node-1", "higher-priority", 2000),
		trackedOn("node-1", "already-proposed", 10),
		trackedOn("node-2", "other-node", 10),
		changed(trackedOn("node-1", "not-bound", 10), func(pi *device.PodInfo) { pi.Spec.NodeName = "" }),
		changed(trackedOn("node-1", "terminating", 10), func(pi *device.PodInfo) {
			now := metav1.Now()
			pi.DeletionTimestamp = &now
		}),
		changed(trackedOn("node-1", "no-devices", 10), func(pi *device.PodInfo) { pi.Devices = nil }),
	}
	taken := map[k8stypes.UID]struct{}{"already-proposed": {}}

	var got []string
	for _, pi := range victimCandidates(nodePods, "node-1", preemptor, taken) {
		got = append(got, pi.Name)
	}
	assert.DeepEqual(t, got, []string{"mid", "low-old", "low-new", "low-not-started"})
}

// The fit check answers for the node as it would be with some pods gone, and
// leaves the caches alone.
func Test_fitsWithout(t *testing.T) {
	pod := movePod()
	s, _ := filterLeader(t, pod, 1)
	resourceReqs, err := device.Resourcereqs(pod)
	assert.NilError(t, err)

	// The only GPU of the node is nearly full.
	holder := trackedOn(moveNodeX, "holder", 10)
	holder.Devices[nvidia.NvidiaGPUDevice][0][0].Usedmem = 8000
	assert.Assert(t, s.podManager.AddPod(holder.Pod, moveNodeX, holder.Devices))
	nodePods := s.podManager.ListPodsInfo()

	_, fits := s.fitsWithout(pod, resourceReqs, moveNodeX, nodePods, nil)
	assert.Assert(t, !fits, "8000 of 8192 MiB are taken, 1000 more must not fit")

	devices, fits := s.fitsWithout(pod, resourceReqs, moveNodeX, nodePods, map[k8stypes.UID]struct{}{"holder": {}})
	assert.Assert(t, fits, "the pod must fit once the holder is gone")
	assert.Equal(t, devices[nvidia.NvidiaGPUDevice][0][0].UUID, moveNodeX+"-gpu-0")

	_, fits = s.fitsWithout(pod, resourceReqs, "no-such-node", nodePods, nil)
	assert.Assert(t, !fits, "a node HAMi does not know cannot fit the pod")

	_, tracked := s.podManager.GetPod(pod)
	assert.Assert(t, !tracked, "a fit check must not assign the pod")
	_, tracked = s.podManager.GetPod(holder.Pod)
	assert.Assert(t, tracked, "a fit check must not drop the pod it left out")
}

// devicePreemptor returns a pod of priority 1000 that asks for 1000 MiB of one
// GPU, and a scheduler that can resolve it, with node-x and node-y holding one
// GPU of 8192 MiB each.
func devicePreemptor(t *testing.T) (*Scheduler, *corev1.Pod) {
	t.Helper()
	pod := movePod()
	priority := int32(1000)
	pod.Spec.Priority = &priority
	s, _ := filterLeader(t, pod, 1)
	return s, pod
}

// holding sets how much of its GPU a pod of trackedOn has.
func holding(pi *device.PodInfo, mem int64) *device.PodInfo {
	pi.Devices[nvidia.NvidiaGPUDevice][0][0].Usedmem = mem
	return pi
}

// notBound turns a pod of trackedOn into one a Filter has assigned to its
// node and nothing has bound yet.
func notBound(pi *device.PodInfo) *device.PodInfo {
	pi.Spec.NodeName = ""
	return pi
}

// track puts pods into the pod cache of s.
func track(t *testing.T, s *Scheduler, pods ...*device.PodInfo) {
	t.Helper()
	for _, pi := range pods {
		assert.Assert(t, s.podManager.AddPod(pi.Pod, pi.NodeID, pi.Devices), "pod %s is tracked twice", pi.Name)
	}
}

// kube-scheduler does not count the devices HAMi shares, so a candidate node
// comes with the victims its own resources need, or with none at all. Preempt
// adds the pods the devices need, and leaves out a node the devices do not fit
// on whoever is evicted.
func Test_Preempt_SelectsVictims(t *testing.T) {
	victims := func(uids ...string) *extenderv1.MetaVictims {
		meta := &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{}}
		for _, uid := range uids {
			meta.Pods = append(meta.Pods, &extenderv1.MetaPod{UID: uid})
		}
		return meta
	}

	tests := []struct {
		name    string
		tracked []*device.PodInfo
		request map[string]*extenderv1.MetaVictims
		want    map[string]*extenderv1.MetaVictims
	}{
		{
			name: "the less important pod is added to an empty victim list",
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "mid", 500), 4000),
				holding(trackedOn(moveNodeX, "low", 10), 4000),
			},
			request: map[string]*extenderv1.MetaVictims{moveNodeX: victims()},
			want:    map[string]*extenderv1.MetaVictims{moveNodeX: victims("low")},
		},
		{
			name: "every pod in the way is added, most important first",
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "higher", 2000), 7000),
				holding(trackedOn(moveNodeX, "low-b", 10), 500),
				holding(trackedOn(moveNodeX, "low-a", 10), 600),
			},
			request: map[string]*extenderv1.MetaVictims{moveNodeX: victims()},
			want:    map[string]*extenderv1.MetaVictims{moveNodeX: victims("low-a", "low-b")},
		},
		{
			// kube-scheduler drops a node no extender names a victim on, and
			// the pod is scheduled there on its next attempt.
			name: "no pod is added where the devices fit already",
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "low", 10), 4000),
			},
			request: map[string]*extenderv1.MetaVictims{moveNodeX: victims()},
			want:    map[string]*extenderv1.MetaVictims{moveNodeX: victims()},
		},
		{
			name: "the devices of a victim kube-scheduler proposed count as free",
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "proposed", 10), 4000),
				holding(trackedOn(moveNodeX, "low", 10), 4000),
			},
			request: map[string]*extenderv1.MetaVictims{moveNodeX: {
				Pods:             []*extenderv1.MetaPod{{UID: "without-devices"}, {UID: "proposed"}},
				NumPDBViolations: 1,
			}},
			want: map[string]*extenderv1.MetaVictims{moveNodeX: {
				Pods:             []*extenderv1.MetaPod{{UID: "without-devices"}, {UID: "proposed"}},
				NumPDBViolations: 1,
			}},
		},
		{
			name: "pods are added after the victims kube-scheduler proposed",
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "proposed", 10), 100),
				holding(trackedOn(moveNodeX, "low", 10), 8000),
			},
			request: map[string]*extenderv1.MetaVictims{moveNodeX: victims("proposed")},
			want:    map[string]*extenderv1.MetaVictims{moveNodeX: victims("proposed", "low")},
		},
		{
			name: "a node the devices do not fit on is left out",
			tracked: []*device.PodInfo{
				holding(trackedOn(moveNodeX, "higher", 2000), 8000),
			},
			request: map[string]*extenderv1.MetaVictims{
				moveNodeX: victims("without-devices"),
				moveNodeY: victims(),
			},
			want: map[string]*extenderv1.MetaVictims{moveNodeY: victims()},
		},
		{
			name:    "a node without devices of HAMi is left out",
			request: map[string]*extenderv1.MetaVictims{"no-such-node": victims("without-devices")},
			want:    map[string]*extenderv1.MetaVictims{},
		},
		{
			// kube-scheduler would not find the pod on the node, and that
			// fails the preemption on every node.
			name: "a pod that is not bound yet is not offered as a victim",
			tracked: []*device.PodInfo{
				notBound(holding(trackedOn(moveNodeX, "assigned", 10), 8000)),
			},
			request: map[string]*extenderv1.MetaVictims{moveNodeX: victims()},
			want:    map[string]*extenderv1.MetaVictims{},
		},
		{
			name: "what the pod holds from an earlier attempt does not count",
			tracked: []*device.PodInfo{
				notBound(holding(trackedOn(moveNodeX, "move-uid", 1000), 8000)),
			},
			request: map[string]*extenderv1.MetaVictims{moveNodeX: victims()},
			want:    map[string]*extenderv1.MetaVictims{moveNodeX: victims()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, pod := devicePreemptor(t)
			track(t, s, tt.tracked...)

			res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{Pod: pod, NodeNameToMetaVictims: tt.request})
			assert.NilError(t, err)
			assert.DeepEqual(t, res.NodeNameToMetaVictims, tt.want)
		})
	}
}

// Preempt only answers. It assigns nothing to the pod, keeps the pods it names
// in the pod cache, and writes nothing to the API server.
func Test_Preempt_OnlyAnswers(t *testing.T) {
	pod := movePod()
	priority := int32(1000)
	pod.Spec.Priority = &priority
	s, kubeClient := filterLeader(t, pod, 1)
	low := holding(trackedOn(moveNodeX, "low", 10), 8000)
	track(t, s, low)

	res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: {}},
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, res.NodeNameToMetaVictims, map[string]*extenderv1.MetaVictims{
		moveNodeX: {Pods: []*extenderv1.MetaPod{{UID: "low"}}},
	})

	_, tracked := s.podManager.GetPod(pod)
	assert.Assert(t, !tracked, "Preempt must not assign the pod")
	pi, tracked := s.podManager.GetPod(low.Pod)
	assert.Assert(t, tracked, "Preempt must not drop the pod it names")
	assert.Equal(t, pi.Devices[nvidia.NvidiaGPUDevice][0][0].Usedmem, int64(8000))
	for _, action := range kubeClient.Actions() {
		verb := action.GetVerb()
		assert.Assert(t, verb == "create" || verb == "get", "Preempt must not write to the API server, it sent %s", verb)
	}
}

// The pod in a request is input, as it is for Filter. Preempt answers for the
// live pod, and for nothing if the request is not about that pod.
func Test_Preempt_RejectsRequestForAnotherPod(t *testing.T) {
	s, pod := devicePreemptor(t)
	track(t, s, holding(trackedOn(moveNodeX, "low", 10), 8000))

	other := pod.DeepCopy()
	other.UID = "another-uid"
	res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   other,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: {}},
	})
	assert.ErrorContains(t, err, "UID")
	assert.Assert(t, res == nil)
}

// Filter rejects a pod with an invalid device request on every node, so no pod
// is evicted for it.
func Test_Preempt_RejectsInvalidDeviceRequest(t *testing.T) {
	pod := movePod()
	pod.Spec.Containers[0].Resources.Limits["hami.io/gpucores"] = *resource.NewQuantity(200, resource.DecimalSI)
	s, _ := filterLeader(t, pod, 1)
	track(t, s, holding(trackedOn(moveNodeX, "low", 10), 8000))

	res, err := s.Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{moveNodeX: {}},
	})
	assert.ErrorContains(t, err, "invalid device request")
	assert.Assert(t, res == nil)
}
