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
package device

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// TestInitContainerUsageLifecycle walks a pod with an init container that
// requests more memory than its app container through the accounting
// lifecycle, mirroring design Cases 3 and 4 (docs/develop/initContainer-design.md):
//
//   - init requests 20 on uuid1, app requests 10 on uuid1
//   - recorded usage while init runs = max(sum(app)=10, max(init)=20) = 20 (Case 3)
//   - once the init container terminates with exit 0, usage shrinks to
//     app-only = 10, and the quota delta is applied symmetrically (Case 4)
func TestInitContainerUsageLifecycle(t *testing.T) {
	initTest()
	const (
		ns      = "default"
		memName = "nvidia.com/gpumem"
	)

	// Raw per-container device list: index 0 = init (20), index 1 = app (10),
	// both on uuid1. DecodePodDevices preserves this init-first ordering.
	rawDevices := PodDevices{
		"NVIDIA": PodSingleDevice{
			ContainerDevices{{UUID: "uuid1", Type: "NVIDIA", Usedmem: 20}},
			ContainerDevices{{UUID: "uuid1", Type: "NVIDIA", Usedmem: 10}},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "p", UID: k8stypes.UID("uid-1")},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "app"}},
		},
	}

	pm := NewPodManager()
	qm := NewQuotaManager()
	qm.Quotas[ns] = &DeviceQuota{memName: &Quota{Used: 0, Limit: 24}}

	// --- bind time: record collapsed usage ---
	collapsed := CollapseInitContainerUsage(pod, rawDevices)
	if pm.AddPod(pod, "node1", collapsed) {
		qm.AddUsage(pod, collapsed)
	}
	if got := (*qm.Quotas[ns])[memName].Used; got != 20 {
		t.Fatalf("Case 3: recorded mem usage = %d, want 20 (max(app 10, init 20))", got)
	}

	// --- init container not yet finished: no shrink ---
	if InitContainersAllSucceeded(pod) {
		t.Fatal("init container has no status yet; should not be considered succeeded")
	}

	// --- init container terminates with exit 0: shrink to app-only ---
	pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
		{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
	}
	if !InitContainersAllSucceeded(pod) {
		t.Fatal("init container terminated exit 0 should be considered succeeded")
	}
	appOnly := AppContainersOnly(pod, rawDevices)
	old, didShrink := pm.ShrinkToAppOnly(pod, appOnly)
	if !didShrink {
		t.Fatal("expected shrink to occur on first call after init success")
	}
	qm.RmUsage(pod, old)
	qm.AddUsage(pod, appOnly)
	if got := (*qm.Quotas[ns])[memName].Used; got != 10 {
		t.Fatalf("Case 4: shrunk mem usage = %d, want 10 (app only)", got)
	}

	// --- shrink is idempotent ---
	if _, again := pm.ShrinkToAppOnly(pod, appOnly); again {
		t.Fatal("shrink should be idempotent; second call must be a no-op")
	}

	// --- a routine informer update must not clobber the shrunk value ---
	pm.AddPod(pod, "node1", collapsed)
	pi, _ := pm.GetPod(pod)
	if got := pi.Devices["NVIDIA"][0][0].Usedmem; got != 10 {
		t.Fatalf("post-shrink update clobbered usage: mem = %d, want 10", got)
	}

	// --- deletion subtracts exactly the currently-stored (shrunk) value ---
	taken, ok := pm.TakeAndDeletePod(pod)
	if !ok {
		t.Fatal("expected pod present for deletion")
	}
	qm.RmUsage(pod, taken.Devices)
	if got := (*qm.Quotas[ns])[memName].Used; got != 0 {
		t.Fatalf("after delete mem usage = %d, want 0 (no drift)", got)
	}
}
