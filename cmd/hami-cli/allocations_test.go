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

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// nvidiaPod builds a pod annotated the way nvidia.PatchAnnotations leaves it once the pod
// is fully running: hami.io/vgpu-devices-to-allocate (the pending work queue) has already
// been erased container-by-container by the device plugin's Allocate(), while
// hami.io/vgpu-devices-allocated (the stable record) still holds the full value. Fixtures
// intentionally mirror this post-bind state rather than the transient bind-time state,
// since that's the state hami-cli must be able to read.
func nvidiaPod(namespace, name, node string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				"hami.io/vgpu-node":                node,
				"hami.io/vgpu-devices-to-allocate": "",
				"hami.io/vgpu-devices-allocated":   "GPU-0fc3eda5-e98b-a25b-5b0d-cf5c855d1448,NVIDIA,3000,0:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "trainer"}},
		},
	}
}

func cambriconPod(namespace, name, node string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				"hami.io/vgpu-node":                       node,
				"hami.io/cambricon-mlu-devices-allocated": "MLU-45013011-2257-0000-0000-000000000000,MLU,23308,0:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "infer"}},
		},
	}
}

// kunlunPod exercises the one vendor whose SupportDevices key breaks the common
// "-devices-allocated" suffix: kunlun registers "hami.io/kunlun-allocated"
// (pkg/device/kunlun/device.go), with no "-devices-" segment.
func kunlunPod(namespace, name, node string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				"hami.io/vgpu-node":        node,
				"hami.io/kunlun-allocated": "XPU-1,KUNLUN,4096,0:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "worker"}},
		},
	}
}

func TestCollectAllocationRows_MultiVendor(t *testing.T) {
	pods := []corev1.Pod{
		nvidiaPod("default", "nvidia-job", "node-a"),
		cambriconPod("default", "mlu-job", "node-b"),
	}

	rows := collectAllocationRows(pods)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}

	// rows are sorted by node, so node-a (nvidia) comes before node-b (cambricon).
	nvidiaRow := rows[0]
	if nvidiaRow.Node != "node-a" || nvidiaRow.Pod != "nvidia-job" || nvidiaRow.Container != "trainer" {
		t.Errorf("unexpected nvidia row: %+v", nvidiaRow)
	}
	if nvidiaRow.DeviceUUID != "GPU-0fc3eda5-e98b-a25b-5b0d-cf5c855d1448" || nvidiaRow.DeviceType != "NVIDIA" {
		t.Errorf("unexpected nvidia device fields: %+v", nvidiaRow)
	}
	if nvidiaRow.RequestedMem != 3000 || nvidiaRow.RequestedCore != 0 {
		t.Errorf("unexpected nvidia usage fields: %+v", nvidiaRow)
	}

	mluRow := rows[1]
	if mluRow.Node != "node-b" || mluRow.Pod != "mlu-job" || mluRow.Container != "infer" {
		t.Errorf("unexpected cambricon row: %+v", mluRow)
	}
	if mluRow.DeviceUUID != "MLU-45013011-2257-0000-0000-000000000000" || mluRow.DeviceType != "MLU" {
		t.Errorf("unexpected cambricon device fields: %+v", mluRow)
	}
	if mluRow.RequestedMem != 23308 {
		t.Errorf("unexpected cambricon memory: %+v", mluRow)
	}
}

func TestCollectAllocationRows_SkipsPodsWithoutHamiAnnotations(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "plain-pod"}},
	}

	rows := collectAllocationRows(pods)
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for a pod with no HAMi annotations, got %d", len(rows))
	}
}

func TestCollectAllocationRows_SkipsMalformedAnnotationWithoutFailingOthers(t *testing.T) {
	malformed := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "broken-job",
			Annotations: map[string]string{
				"hami.io/vgpu-node":              "node-a",
				"hami.io/vgpu-devices-allocated": "not-a-valid-device-record",
			},
		},
	}
	good := nvidiaPod("default", "good-job", "node-a")

	rows := collectAllocationRows([]corev1.Pod{malformed, good})
	if len(rows) != 1 {
		t.Fatalf("expected the malformed pod to be skipped and the good pod kept, got %d rows: %+v", len(rows), rows)
	}
	if rows[0].Pod != "good-job" {
		t.Errorf("expected surviving row to belong to good-job, got %q", rows[0].Pod)
	}
}

func TestCollectAllocationRows_InitContainerNameMapping(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "multi-ctr",
			Annotations: map[string]string{
				"hami.io/vgpu-node": "node-a",
				"hami.io/vgpu-devices-allocated": "GPU-init,NVIDIA,1000,0:;" +
					"GPU-main,NVIDIA,2000,0:;",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "setup"}},
			Containers:     []corev1.Container{{Name: "trainer"}},
		},
	}

	rows := collectAllocationRows([]corev1.Pod{pod})
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].Container != "setup" {
		t.Errorf("expected first row's container to resolve to init container %q, got %q", "setup", rows[0].Container)
	}
	if rows[1].Container != "trainer" {
		t.Errorf("expected second row's container to resolve to %q, got %q", "trainer", rows[1].Container)
	}
}

// TestCollectAllocationRows_IgnoresErasedPendingAnnotation reproduces the state of an
// already-running pod: the NVIDIA device plugin's Allocate() has erased
// hami.io/vgpu-devices-to-allocate down to an empty value as each container started
// (pkg/device-plugin/nvidiadevice/nvinternal/plugin/util.go, patchErasedAnnotation), while
// hami.io/vgpu-devices-allocated still holds the original allocation. hami-cli must read
// the row from the -allocated annotation and must not be confused by the empty -to-allocate
// one sitting alongside it.
func TestCollectAllocationRows_IgnoresErasedPendingAnnotation(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "running-job",
			Annotations: map[string]string{
				"hami.io/vgpu-node":                "node-a",
				"hami.io/vgpu-devices-to-allocate": "",
				"hami.io/vgpu-devices-allocated":   "GPU-live,NVIDIA,4000,0:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "trainer"}},
		},
	}

	rows := collectAllocationRows([]corev1.Pod{pod})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row from the -allocated annotation, got %d: %+v", len(rows), rows)
	}
	if rows[0].DeviceUUID != "GPU-live" {
		t.Errorf("expected device UUID from -allocated annotation, got %+v", rows[0])
	}
}

// TestCollectAllocationRows_KunlunIrregularSuffix guards against a regression to a suffix
// match on "-devices-allocated", which would silently miss kunlun's
// "hami.io/kunlun-allocated" key (pkg/device/kunlun/device.go).
func TestCollectAllocationRows_KunlunIrregularSuffix(t *testing.T) {
	rows := collectAllocationRows([]corev1.Pod{kunlunPod("default", "xpu-job", "node-c")})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row for kunlun's irregular annotation key, got %d: %+v", len(rows), rows)
	}
	if rows[0].DeviceUUID != "XPU-1" || rows[0].DeviceType != "KUNLUN" {
		t.Errorf("unexpected kunlun row: %+v", rows[0])
	}
}

func TestRunAllocations_NamespaceAndNodeFilters(t *testing.T) {
	nvPod := nvidiaPod("team-a", "job-1", "node-a")
	cbPod := cambriconPod("team-b", "job-2", "node-b")
	client := fake.NewSimpleClientset(&nvPod, &cbPod)

	namespaceFilter = "team-a"
	nodeFilter = ""
	defer func() { namespaceFilter = ""; nodeFilter = "" }()

	var out bytes.Buffer
	if err := runAllocations(context.Background(), client, &out); err != nil {
		t.Fatalf("runAllocations returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "job-1") {
		t.Errorf("expected output to contain job-1, got:\n%s", got)
	}
	if strings.Contains(got, "job-2") {
		t.Errorf("expected namespace filter to exclude job-2, got:\n%s", got)
	}
}
