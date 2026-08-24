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
	"maps"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func initReplayDevices(t *testing.T) {
	t.Helper()
	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}
	assert.NilError(t, config.InitDevicesWithConfig(sConfig))
}

func replayQuotaUsage(s *Scheduler, namespace, resourceName string) int64 {
	dq, ok := s.quotaManager.GetResourceQuota()[namespace]
	if !ok {
		return 0
	}
	quota, ok := (*dq)[resourceName]
	if !ok {
		return 0
	}
	return quota.Used
}

func newTerminatingAllocatedPod(uid, name, namespace string) *corev1.Pod {
	podDevices := device.PodDevices{
		nvidia.NvidiaGPUDevice: device.PodSingleDevice{
			[]device.ContainerDevice{{Idx: 0, UUID: "GPU0", Type: nvidia.NvidiaGPUDevice, Usedmem: 20000, Usedcores: 100}},
		},
	}
	now := metav1.Now()
	grace := int64(3600)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:               k8stypes.UID(uid),
			Name:              name,
			Namespace:         namespace,
			Annotations:       map[string]string{util.AssignedNodeAnnotations: "node1"},
			DeletionTimestamp: &now,
		},
		Spec:   corev1.PodSpec{TerminationGracePeriodSeconds: &grace},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	maps.Copy(pod.Annotations, device.EncodePodDevices(device.SupportDevices, podDevices))
	return pod
}

// After a scheduler restart the informer's initial sync replays every pod as
// an add. A pod that is terminating with a long grace period still runs and
// holds its devices, so it must land in the cache; dropping it made its GPU
// look free and allowed overcommit.
func Test_onAddPod_TerminatingPodReplayedIntoEmptyCache(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	pod := newTerminatingAllocatedPod("replay-add-uid", "long-graceful-add", "replay-add-ns")
	// The quota manager is a process wide singleton; release the usage so
	// later tests start from a clean slate.
	t.Cleanup(func() { s.onDelPod(pod) })

	s.onAddPod(pod)

	pi, ok := s.podManager.GetPod(pod)
	assert.Equal(t, ok, true, "terminating pod replayed on initial sync must be cached")
	assert.Equal(t, pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID, "GPU0")
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"), int64(20000))
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpucores"), int64(100))
}

// The periodic resync delivers the same situation as an update.
func Test_onUpdatePod_TerminatingPodMissingFromCache(t *testing.T) {
	initReplayDevices(t)
	s := NewScheduler()
	pod := newTerminatingAllocatedPod("replay-update-uid", "long-graceful-update", "replay-update-ns")
	t.Cleanup(func() { s.onDelPod(pod) })

	s.onUpdatePod(pod.DeepCopy(), pod)

	pi, ok := s.podManager.GetPod(pod)
	assert.Equal(t, ok, true, "terminating pod resynced into an empty cache must be cached")
	assert.Equal(t, pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID, "GPU0")
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpumem"), int64(20000))
	assert.Equal(t, replayQuotaUsage(s, pod.Namespace, "hami.io/gpucores"), int64(100))
}
