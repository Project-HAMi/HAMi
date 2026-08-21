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

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// unrecognizedPolicy is an unrecognized value for hami.io/gpu-scheduler-policy.
const unrecognizedPolicy = "binpakc"

// fallbackPod builds a Pod carrying the given scheduler-policy annotation. An empty
// value means "no annotation", i.e. the configured cluster default applies.
func fallbackPod(annotationValue string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fallback-pod", Namespace: "default"},
	}
	if annotationValue != "" {
		pod.Annotations = map[string]string{
			util.GPUSchedulerPolicyAnnotationKey: annotationValue,
		}
	}
	return pod
}

// newFallbackNode builds a single node holding three NVIDIA cards at different
// utilisation levels, with its device list carrying the policy that
// GetGPUSchedulerPolicyByPod resolves for task. The spread of utilisation is
// what makes binpack and spread pick different cards.
func newFallbackNode(task *corev1.Pod, clusterDefault string) *NodeUsage {
	mk := func(id string, used, usedcores, usedmem int32) *policy.DeviceListsScore {
		return &policy.DeviceListsScore{
			Device: &device.DeviceUsage{
				ID:        id,
				Index:     0,
				Type:      nvidia.NvidiaGPUDevice,
				Count:     10,
				Used:      used,
				Totalcore: 100,
				Usedcores: usedcores,
				Totalmem:  8192,
				Usedmem:   usedmem,
				Numa:      0,
				Health:    true,
			},
		}
	}

	return &NodeUsage{
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "fallback-node"}},
		Devices: policy.DeviceUsageList{
			Policy: util.GetGPUSchedulerPolicyByPod(clusterDefault, task),
			DeviceLists: []*policy.DeviceListsScore{
				mk("gpu-idle", 0, 0, 0),
				mk("gpu-mid", 2, 50, 4096),
				mk("gpu-busy", 3, 75, 6144),
			},
		},
	}
}

// placeWithPolicy runs one real scheduling pass - ComputeScore, sort.Sort, and the
// NVIDIA backend's Fit - and returns the UUID of the card actually selected.
func placeWithPolicy(t *testing.T, annotationValue, clusterDefault string) string {
	t.Helper()

	task := fallbackPod(annotationValue)
	node := newFallbackNode(task, clusterDefault)

	requests := device.ContainerDeviceRequests{
		nvidia.NvidiaGPUDevice: {
			Nums:             1,
			Type:             nvidia.NvidiaGPUDevice,
			Memreq:           1024,
			MemPercentagereq: 101,
			Coresreq:         10,
		},
	}
	devinput := &device.PodDevices{}

	fit, reason := fitInDevices(node, requests, task, nil, devinput, util.DefaultDeviceScoringWeights())
	assert.Assert(t, fit, "expected the pod to fit; reason=%s", reason)

	containers := (*devinput)[nvidia.NvidiaGPUDevice]
	assert.Equal(t, len(containers), 1)
	assert.Equal(t, len(containers[0]), 1)
	return containers[0][0].UUID
}

func TestPlacementUnderUnrecognizedPolicy(t *testing.T) {
	binpackPolicy := util.GPUSchedulerPolicyBinpack.String()
	spreadPolicy := util.GPUSchedulerPolicySpread.String()

	binpack := placeWithPolicy(t, binpackPolicy, binpackPolicy)
	spread := placeWithPolicy(t, spreadPolicy, binpackPolicy)

	t.Logf("  annotation %-9q -> placed on %s", binpackPolicy, binpack)
	t.Logf("  annotation %-9q -> placed on %s", spreadPolicy, spread)

	// Sanity: the two valid policies must disagree, otherwise the fixture is
	// not discriminating and nothing below means anything.
	assert.Assert(t, binpack != spread,
		"fixture is not discriminating: binpack and spread chose the same card")

	// Both configured defaults are exercised, so an implementation that fell
	// back to a fixed policy instead of the configured one would still fail.
	tests := []struct {
		clusterDefault string
		want           string
	}{
		{binpackPolicy, binpack},
		{spreadPolicy, spread},
	}
	for _, tt := range tests {
		t.Run("cluster default "+tt.clusterDefault, func(t *testing.T) {
			noAnnotation := placeWithPolicy(t, "", tt.clusterDefault)
			unrecognized := placeWithPolicy(t, unrecognizedPolicy, tt.clusterDefault)

			t.Logf("  no annotation        -> placed on %s", noAnnotation)
			t.Logf("  annotation %-9q -> placed on %s", unrecognizedPolicy, unrecognized)

			// With no annotation the configured cluster default is honoured.
			assert.Equal(t, noAnnotation, tt.want,
				"absent annotation should use the configured cluster default")

			// An unrecognized value is ignored, so placement matches the
			// configured default rather than the comparator's hardcoded spread
			// fallback.
			assert.Equal(t, unrecognized, noAnnotation,
				"unrecognized policy should place identically to no annotation at all")
		})
	}
}

// TestResolveNodeSchedulerPolicy covers the node-policy half of the change,
// which the placement test above does not reach: an annotation replaces the
// configured policy only when it names a policy NodeScoreList.Less handles.
func TestResolveNodeSchedulerPolicy(t *testing.T) {
	original := config.NodeSchedulerPolicy
	t.Cleanup(func() { config.NodeSchedulerPolicy = original })

	podWith := func(annotations map[string]string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name:        "node-policy-pod",
			Namespace:   "default",
			Annotations: annotations,
		}}
	}
	withPolicy := func(value string) map[string]string {
		return map[string]string{util.NodeSchedulerPolicyAnnotationKey: value}
	}

	// Both configured policies are exercised, so "keeps the configured policy"
	// cannot pass by coincidence against a fixed fallback.
	for _, clusterDefault := range []string{
		util.NodeSchedulerPolicyBinpack.String(),
		util.NodeSchedulerPolicySpread.String(),
	} {
		override := util.NodeSchedulerPolicySpread.String()
		if clusterDefault == override {
			override = util.NodeSchedulerPolicyBinpack.String()
		}

		tests := []struct {
			name        string
			annotations map[string]string
			want        string
		}{
			{"valid value overrides the configured policy", withPolicy(override), override},
			{"unrecognized value keeps the configured policy", withPolicy("spraed"), clusterDefault},
			{"empty value keeps the configured policy", withPolicy(""), clusterDefault},
			{"chain form is not a node policy", withPolicy("binpack,spread"), clusterDefault},
			{"gpu-only policy is not a node policy", withPolicy("numa"), clusterDefault},
			{"unrelated annotation is ignored", map[string]string{"hami.io/unrelated": "spread"}, clusterDefault},
			{"no annotations at all", nil, clusterDefault},
		}
		t.Run("configured "+clusterDefault, func(t *testing.T) {
			config.NodeSchedulerPolicy = clusterDefault
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					assert.Equal(t, tt.want, resolveNodeSchedulerPolicy(podWith(tt.annotations)))
				})
			}
		})
	}
}
