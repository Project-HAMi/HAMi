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

package nvidia

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func TestDynamicMIGRuntimeIdentity(t *testing.T) {
	raw := `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-parent","profile":"1g.5gb","placement":{"start":0,"size":1},"migUUID":"MIG-real","gpuInstanceID":7,"computeInstanceID":0}]`
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{nv.MigAllocationsAnnotation: raw}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}}
	registry := map[string]*device.DeviceInfo{"GPU-parent": {ID: "GPU-parent", Mode: nv.MigMode, Devmem: 40960, MIGProfiles: []device.MigProfile{{Name: "1g.5gb", MemoryMB: 4864}}}}
	c := wholeGPUCandidate{pod: pod, container: "main", devices: device.ContainerDevices{{UUID: "GPU-parent", Usedmem: 4864, Usedcores: 15}}}
	devices, nodes, ready := resolveDynamicMIGCandidate(c, registry)
	if !ready || devices[0].UUID != "MIG-real" {
		t.Fatalf("unresolved runtime: %v %v", devices, ready)
	}
	if verdict, _ := evaluateContainerWholeGPU(devices, nodes, nil); verdict != confirmedWholeGPU {
		t.Fatalf("verdict=%v", verdict)
	}
	if c.devices[0].UUID != "GPU-parent" || len(registry) != 1 {
		t.Fatal("input mutated")
	}
	pod.Spec.InitContainers = []corev1.Container{{Name: "init"}}
	pod.Annotations[nv.MigAllocationsAnnotation] = strings.Replace(raw, `"containerIndex":0`, `"containerIndex":1`, 1)
	if devices, _, ready := resolveDynamicMIGCandidate(c, registry); !ready || devices[0].UUID != "MIG-real" {
		t.Fatal("init container offset lost")
	}
	pod.Spec.InitContainers = nil
	delete(pod.Annotations, nv.MigAllocationsAnnotation)
	if _, _, ready := resolveDynamicMIGCandidate(c, registry); ready {
		t.Fatal("missing runtime allocation accepted")
	}

	pod.Annotations[nv.MigAllocationsAnnotation] = `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-parent","profile":"1g.5gb","placement":{"start":0,"size":1}}]`
	if _, _, ready := resolveDynamicMIGCandidate(c, registry); ready {
		t.Fatal("pending runtime accepted")
	}
	pod.Annotations[nv.MigAllocationsAnnotation] = raw
	c.devices[0].Usedmem = 9984
	if _, _, ready := resolveDynamicMIGCandidate(c, registry); ready {
		t.Fatal("inconsistent profile accepted")
	}
}
