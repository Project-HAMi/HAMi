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

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

func preemptorPod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "high", Namespace: "default", UID: "preemptor-uid"}}
}

// Until HAMi picks victims itself, the preempt verb must be invisible to
// kube-scheduler: every candidate node goes back with the victims it came
// with. A node that carries no victim is a candidate too. It is how
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
			res, err := NewScheduler().Preempt(tt.args)
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

// Victim selection will add to the answer, and must not write into the
// request while doing so.
func Test_Preempt_AnswerDoesNotAliasRequest(t *testing.T) {
	requested := &extenderv1.MetaVictims{Pods: []*extenderv1.MetaPod{{UID: "victim-a"}}}

	res, err := NewScheduler().Preempt(extenderv1.ExtenderPreemptionArgs{
		Pod:                   preemptorPod(),
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

	res, err := NewScheduler().Preempt(args)
	assert.NilError(t, err)

	answer, err := json.Marshal(res)
	assert.NilError(t, err)
	assert.Equal(t, string(answer), want)
}
