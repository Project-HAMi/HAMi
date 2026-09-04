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
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

// Simulation callers such as the cluster autoscaler send Nodes instead of
// NodeNames. A pod without any HAMi resource must get the template nodes
// echoed back; dropping them makes the extender veto scale up for every
// ordinary non GPU workload.
func Test_Filter_SimulationEchoesNodesForNonHAMiPod(t *testing.T) {
	s := NewScheduler()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}
	nodes := &corev1.NodeList{Items: []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "template-node-1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "template-node-2"}},
	}}

	res, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, Nodes: nodes})
	assert.NilError(t, err)
	assert.Assert(t, res.Nodes != nil, "template nodes were dropped")
	assert.Equal(t, len(res.Nodes.Items), 2)
	assert.Equal(t, res.Error, "")
}
