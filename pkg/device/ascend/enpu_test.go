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

package ascend

import (
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

func enpuTestDevice() *Devices {
	return &Devices{
		config: VNPUConfig{
			CommonWord: Ascend910CType, ResourceName: "huawei.com/Ascend910C",
			ResourceMemoryName: "huawei.com/Ascend910C-memory", ResourceCoreName: "huawei.com/Ascend910C-core",
			MemoryAllocatable: 65536, MemoryCapacity: 65536, AICore: 20,
			Templates: []Template{{Name: "vir05_1c_16g", Memory: 16384}, {Name: "vir10_3c_32g", Memory: 32768}},
		},
		enpuPolicy: "elastic",
	}
}

func enpuTestPod(mode, policy string) *corev1.Pod {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod"}}
	if mode != "" || policy != "" {
		pod.Annotations = map[string]string{}
		if mode != "" {
			pod.Annotations[VNPUModeAnnotation] = mode
		}
		if policy != "" {
			pod.Annotations["huawei.com/enpu-policy"] = policy
		}
	}
	pod.Spec.Containers = []corev1.Container{{Name: "workload", Resources: corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			"huawei.com/Ascend910C":        resource.MustParse("1"),
			"huawei.com/Ascend910C-memory": resource.MustParse("20480"),
		},
	}}}
	return pod
}

func enpuTestNode(core, enpu bool) *device.NodeInfo {
	return &device.NodeInfo{Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		VNPUNodeSelectorAnnotation: fmt.Sprint(core), VNPUNodeENPUAnnotation: fmt.Sprint(enpu),
	}}}}
}

func enpuTestUsage(id string, occupant *corev1.Pod) *device.DeviceUsage {
	usage := &device.DeviceUsage{ID: id, Count: 100, Totalmem: 65536, Totalcore: 100, Health: true, Type: Ascend910CType}
	if occupant != nil {
		usage.Used, usage.Usedmem, usage.Usedcores = 1, 20480, 20
		usage.PodInfos = []*device.PodInfo{{Pod: occupant}}
	}
	return usage
}

func enpuTestRequest(mode string) device.ContainerDeviceRequest {
	cores := int32(0)
	if mode != "" {
		cores = 20
	}
	return device.ContainerDeviceRequest{Nums: 1, Type: Ascend910CType, Memreq: 20480, Coresreq: cores}
}

func TestENPUAdmissionFreezesPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, defaultPolicy, annotation, upstreamAnnotation, label, want string
		invalid                                                          bool
	}{
		{name: "default", defaultPolicy: "fixed-share", want: "fixed-share"},
		{name: "empty default", want: "elastic"},
		{name: "canonical annotation", annotation: "best-effort", want: "best-effort"},
		{name: "annotation alias", annotation: " FIXED_SHARE ", want: "fixed-share"},
		{name: "upstream annotation", upstreamAnnotation: "3", want: "best-effort"},
		{name: "upstream label", label: "2", want: "elastic"},
		{name: "annotation precedence", annotation: "fixed", upstreamAnnotation: "2", label: "3", want: "fixed-share"},
		{name: "whitespace uses default", defaultPolicy: "best-effort", annotation: " ", want: "best-effort"},
		{name: "invalid annotation", annotation: "invalid", invalid: true},
		{name: "invalid label", label: "invalid", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := enpuTestDevice()
			dev.enpuPolicy = tc.defaultPolicy
			pod := enpuTestPod(VNPUModeENPU, tc.annotation)
			if tc.upstreamAnnotation != "" {
				pod.Annotations["huawei.com/scheduler.softShareDev.policy"] = tc.upstreamAnnotation
			}
			if tc.label != "" {
				pod.Labels = map[string]string{"huawei.com/scheduler.softShareDev.policy": tc.label}
			}
			_, err := dev.MutateAdmission(&pod.Spec.Containers[0], pod)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid ENPU policy was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := pod.Annotations["huawei.com/enpu-policy"]; got != tc.want {
				t.Fatalf("stored policy = %q, want %q", got, tc.want)
			}
			dev.enpuPolicy = "changed-after-admission"
			if got := dev.enpuPolicyForPod(pod); got != tc.want {
				t.Fatalf("stored policy changed with default: %q, want %q", got, tc.want)
			}
		})
	}
}

func TestENPUAdmissionSingleDieAndLegacySuperPod(t *testing.T) {
	for _, tc := range []struct {
		name, mode, count     string
		wantCount, wantMemory int64
		invalid               bool
	}{
		{name: "ENPU single DIE", mode: VNPUModeENPU, count: "1", wantCount: 1, wantMemory: 20480},
		{name: "ENPU alias", mode: "vcann-rt", count: "1", wantCount: 1, wantMemory: 20480},
		{name: "ENPU multiple DIEs rejected", mode: VNPUModeENPU, count: "2", invalid: true},
		{name: "ENPU odd count rejected", mode: VNPUModeENPU, count: "3", invalid: true},
		{name: "hami-core keeps module pair", mode: VNPUModeHamiCore, count: "1", wantCount: 2, wantMemory: 20480},
		{name: "template keeps module pair and rounding", mode: VNPUModeTemplate, count: "1", wantCount: 2, wantMemory: 32768},
		{name: "unannotated keeps module pair and rounding", count: "1", wantCount: 2, wantMemory: 32768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := enpuTestDevice()
			dev.config.SuperPod = true
			pod := enpuTestPod(tc.mode, "")
			ctr := &pod.Spec.Containers[0]
			ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)] = resource.MustParse(tc.count)
			_, err := dev.MutateAdmission(ctr, pod)
			if tc.invalid {
				if err == nil {
					t.Fatal("multiple ENPU DIEs were accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			count := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)]
			memory := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
			if count.Value() != tc.wantCount || memory.Value() != tc.wantMemory {
				t.Fatalf("count/memory = %d/%d, want %d/%d", count.Value(), memory.Value(), tc.wantCount, tc.wantMemory)
			}
			if !isENPUMode(tc.mode) && pod.Annotations["huawei.com/enpu-policy"] != "" {
				t.Fatal("non-ENPU Pod received an ENPU policy")
			}
		})
	}
}

func TestENPUAdmissionRequestsOnly(t *testing.T) {
	for _, tc := range []struct {
		name, mode, count, limitCount, memory, limitMemory, policy string
		wantMemory                                                 int64
		wantMutated, invalid                                       bool
	}{
		{name: "requests only", mode: VNPUModeENPU, count: "1", memory: "20480", wantMemory: 20480, wantMutated: true},
		{name: "alias requests only", mode: "vcann-rt", count: "1", memory: "20480", wantMemory: 20480, wantMutated: true},
		{name: "default memory", mode: VNPUModeENPU, count: "1", wantMemory: 65536, wantMutated: true},
		{name: "memory limit takes precedence", mode: VNPUModeENPU, count: "1", memory: "16384", limitMemory: "20480", wantMemory: 20480, wantMutated: true},
		{name: "multiple devices", mode: VNPUModeENPU, count: "2", invalid: true},
		{name: "zero devices", mode: VNPUModeENPU, count: "0", invalid: true},
		{name: "limit count takes precedence", mode: VNPUModeENPU, count: "1", limitCount: "2", invalid: true},
		{name: "invalid policy", mode: VNPUModeENPU, count: "1", policy: "unknown", invalid: true},
		{name: "no Ascend count", mode: VNPUModeENPU},
		{name: "hami-core unchanged", mode: VNPUModeHamiCore, count: "1", memory: "20480"},
		{name: "template unchanged", mode: VNPUModeTemplate, count: "1", memory: "20480"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := enpuTestDevice()
			dev.config.SuperPod = true
			dev.runtimeClassName = "ascend"
			countName := corev1.ResourceName(dev.config.ResourceName)
			memoryName := corev1.ResourceName(dev.config.ResourceMemoryName)
			dev.allAscendResourceNames = []corev1.ResourceName{countName}
			pod := enpuTestPod(tc.mode, tc.policy)
			ctr := &pod.Spec.Containers[0]
			ctr.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{}}
			if tc.count != "" {
				ctr.Resources.Requests[countName] = resource.MustParse(tc.count)
			}
			if tc.memory != "" {
				ctr.Resources.Requests[memoryName] = resource.MustParse(tc.memory)
			}
			if tc.limitCount != "" || tc.limitMemory != "" {
				ctr.Resources.Limits = corev1.ResourceList{}
			}
			if tc.limitCount != "" {
				ctr.Resources.Limits[countName] = resource.MustParse(tc.limitCount)
			}
			if tc.limitMemory != "" {
				ctr.Resources.Limits[memoryName] = resource.MustParse(tc.limitMemory)
			}
			original := pod.DeepCopy()
			mutated, err := dev.MutateAdmission(ctr, pod)
			if (err != nil) != tc.invalid || mutated != tc.wantMutated {
				t.Fatalf("MutateAdmission = %v, %v; want mutated=%v, invalid=%v", mutated, err, tc.wantMutated, tc.invalid)
			}
			if !tc.wantMutated {
				if !reflect.DeepEqual(pod, original) {
					t.Fatal("rejected or unrelated Pod was modified")
				}
				return
			}
			if pod.Annotations["huawei.com/enpu-policy"] != "elastic" || pod.Spec.RuntimeClassName == nil || *pod.Spec.RuntimeClassName != "ascend" {
				t.Fatalf("policy or runtime class not persisted: %+v", pod)
			}
			for _, resources := range []corev1.ResourceList{ctr.Resources.Limits, ctr.Resources.Requests} {
				count, memory := resources[countName], resources[memoryName]
				if count.Value() != 1 || memory.Value() != tc.wantMemory {
					t.Fatalf("count/memory = %d/%d, want 1/%d", count.Value(), memory.Value(), tc.wantMemory)
				}
			}
			request := dev.GenerateResourceRequests(ctr)
			if request.Nums != 1 || int64(request.Memreq) != tc.wantMemory {
				t.Fatalf("scheduler request does not match admitted resources: %+v", request)
			}
		})
	}
}

func TestENPUFitBackendIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, incomingMode, existingMode string
		emptyAnnotations, separateDIE    bool
		wantFit                          bool
	}{
		{name: "ENPU rejects implicit core with nil annotations", incomingMode: VNPUModeENPU},
		{name: "ENPU rejects implicit core with empty annotations", incomingMode: VNPUModeENPU, emptyAnnotations: true},
		{name: "implicit core rejects ENPU", existingMode: VNPUModeENPU},
		{name: "empty-annotation core rejects ENPU", existingMode: VNPUModeENPU, emptyAnnotations: true},
		{name: "explicit core rejects ENPU", incomingMode: VNPUModeHamiCore, existingMode: VNPUModeENPU},
		{name: "ENPU rejects explicit core", incomingMode: VNPUModeENPU, existingMode: VNPUModeHamiCore},
		{name: "implicit core can use a separate DIE", existingMode: VNPUModeENPU, separateDIE: true, wantFit: true},
		{name: "ENPU can use a separate DIE", incomingMode: VNPUModeENPU, separateDIE: true, wantFit: true},
		{name: "legacy implicit core still shares with core", wantFit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := enpuTestDevice()
			incoming := enpuTestPod(tc.incomingMode, "")
			existing := enpuTestPod(tc.existingMode, "")
			for _, pod := range []*corev1.Pod{incoming, existing} {
				if isENPUMode(pod.Annotations[VNPUModeAnnotation]) {
					pod.Annotations["huawei.com/enpu-policy"] = "elastic"
				} else if tc.emptyAnnotations && pod.Annotations == nil {
					pod.Annotations = map[string]string{}
				}
			}
			usages := []*device.DeviceUsage{enpuTestUsage("occupied", existing)}
			if tc.separateDIE {
				usages = append([]*device.DeviceUsage{enpuTestUsage("free", nil)}, usages...)
			}
			fit, allocation, reason := dev.Fit(usages, enpuTestRequest(tc.incomingMode), incoming, enpuTestNode(true, true), nil)
			if fit != tc.wantFit {
				t.Fatalf("Fit = %v, want %v; reason = %s", fit, tc.wantFit, reason)
			}
			if tc.separateDIE && (len(allocation[Ascend910CType]) != 1 || allocation[Ascend910CType][0].UUID != "free") {
				t.Fatalf("unexpected shared allocation: %+v", allocation)
			}
		})
	}
}

func TestENPUFitPolicyChangeAndLegacyPolicy(t *testing.T) {
	dev := enpuTestDevice()
	old := enpuTestPod(VNPUModeENPU, "")
	if _, err := dev.MutateAdmission(&old.Spec.Containers[0], old); err != nil {
		t.Fatal(err)
	}
	dev.enpuPolicy = "fixed-share"
	for _, tc := range []struct {
		name, oldPolicy, incomingPolicy string
		wantFit                         bool
	}{
		{name: "changed default rejects old elastic", oldPolicy: old.Annotations["huawei.com/enpu-policy"]},
		{name: "explicit old policy can still share", oldPolicy: old.Annotations["huawei.com/enpu-policy"], incomingPolicy: "elastic", wantFit: true},
		{name: "legacy missing policy rejected", incomingPolicy: "elastic"},
		{name: "legacy invalid policy rejected", oldPolicy: "unknown", incomingPolicy: "elastic"},
		{name: "same normalized policy accepted", oldPolicy: "fixed", incomingPolicy: "fixed-share", wantFit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := enpuTestPod(VNPUModeENPU, tc.oldPolicy)
			incoming := enpuTestPod(VNPUModeENPU, tc.incomingPolicy)
			if _, err := dev.MutateAdmission(&incoming.Spec.Containers[0], incoming); err != nil {
				t.Fatal(err)
			}
			fit, _, reason := dev.Fit([]*device.DeviceUsage{enpuTestUsage("occupied", existing)}, enpuTestRequest(VNPUModeENPU), incoming, enpuTestNode(false, true), nil)
			if fit != tc.wantFit {
				t.Fatalf("Fit = %v, want %v; reason = %s", fit, tc.wantFit, reason)
			}
		})
	}
}

func TestENPUNodeOverridesAndDefaultCoreAccounting(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		globalENPU bool
		node       *device.NodeInfo
		wantFit    bool
		wantCore   int32
	}{
		{name: "node enables ENPU", mode: VNPUModeENPU, node: enpuTestNode(false, true), wantFit: true, wantCore: 100},
		{name: "node disables global ENPU", mode: VNPUModeENPU, globalENPU: true, node: enpuTestNode(false, false)},
		{name: "global ENPU default", mode: VNPUModeENPU, globalENPU: true, node: &device.NodeInfo{}, wantFit: true, wantCore: 100},
		{name: "ENPU disabled by default", mode: VNPUModeENPU, node: &device.NodeInfo{}},
		{name: "implicit core keeps zero reservation", node: enpuTestNode(true, false), wantFit: true},
		{name: "unannotated rejected on ENPU only", node: enpuTestNode(false, true)},
		{name: "unknown mode rejected on ENPU only", mode: "typo", node: enpuTestNode(false, true)},
		{name: "core rejected on ENPU only", mode: VNPUModeHamiCore, node: enpuTestNode(false, true)},
		{name: "ENPU alias accepted", mode: "vcann-rt", node: enpuTestNode(false, true), wantFit: true, wantCore: 100},
		{name: "unknown mode on legacy template node unchanged", mode: "typo", node: enpuTestNode(false, false), wantFit: true},
		{name: "unknown mode on legacy core node unchanged", mode: "typo", node: enpuTestNode(true, false), wantFit: true},
		{name: "core accepted on dual backend node", mode: VNPUModeHamiCore, node: enpuTestNode(true, true), wantFit: true},
		{name: "template rejected on dual backend node", mode: VNPUModeTemplate, node: enpuTestNode(true, true)},
		{name: "template rejected on ENPU", mode: VNPUModeTemplate, node: enpuTestNode(false, true)},
		{name: "template default unchanged", mode: VNPUModeTemplate, node: enpuTestNode(false, false), wantFit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := enpuTestDevice()
			dev.enpu = tc.globalENPU
			pod := enpuTestPod(tc.mode, "")
			request := enpuTestRequest("")
			fit, allocation, reason := dev.Fit([]*device.DeviceUsage{enpuTestUsage("free", nil)}, request, pod, tc.node, nil)
			if fit != tc.wantFit {
				t.Fatalf("Fit = %v, want %v; reason = %s", fit, tc.wantFit, reason)
			}
			if fit && (len(allocation[Ascend910CType]) != 1 || allocation[Ascend910CType][0].Usedcores != tc.wantCore) {
				t.Fatalf("unexpected core accounting: %+v", allocation)
			}
		})
	}
}

func TestENPUNilPodPolicyDefaults(t *testing.T) {
	if got := enpuPolicyOverride(nil); got != "" {
		t.Fatalf("nil Pod policy override = %q, want empty", got)
	}
	if got := enpuSchedulingPolicy(nil); got != "elastic" {
		t.Fatalf("nil Pod scheduling policy = %q, want elastic", got)
	}
	dev := enpuTestDevice()
	dev.enpuPolicy = "fixed-share"
	if got := dev.enpuPolicyForPod(nil); got != "fixed-share" {
		t.Fatalf("nil Pod configured policy = %q, want fixed-share", got)
	}
	dev.enpuPolicy = ""
	if got := dev.enpuPolicyForPod(nil); got != "elastic" {
		t.Fatalf("nil Pod unconfigured policy = %q, want elastic", got)
	}
}

func TestENPUFitIgnoresMissingPodInfo(t *testing.T) {
	for _, tc := range []struct {
		name, incomingMode, existingMode, existingPolicy string
		wantFit                                          bool
	}{
		{name: "same ENPU policy shares", incomingMode: VNPUModeENPU, existingMode: VNPUModeENPU, existingPolicy: "elastic", wantFit: true},
		{name: "different ENPU policy rejected", incomingMode: VNPUModeENPU, existingMode: VNPUModeENPU, existingPolicy: "fixed-share"},
		{name: "ENPU rejects core tenant", incomingMode: VNPUModeENPU, existingMode: VNPUModeHamiCore},
		{name: "core tenants still share", incomingMode: VNPUModeHamiCore, existingMode: VNPUModeHamiCore, wantFit: true},
		{name: "core rejects ENPU tenant", incomingMode: VNPUModeHamiCore, existingMode: VNPUModeENPU, existingPolicy: "elastic"},
		{name: "implicit core rejects ENPU tenant", existingMode: VNPUModeENPU, existingPolicy: "elastic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := enpuTestDevice()
			incoming := enpuTestPod(tc.incomingMode, "elastic")
			existing := enpuTestPod(tc.existingMode, tc.existingPolicy)
			usage := enpuTestUsage("occupied", existing)
			usage.PodInfos = []*device.PodInfo{nil, {}, {Pod: existing}, nil, {}}
			fit, allocation, reason := dev.Fit([]*device.DeviceUsage{usage}, enpuTestRequest(tc.incomingMode), incoming, enpuTestNode(true, true), nil)
			if fit != tc.wantFit {
				t.Fatalf("Fit = %v, want %v; reason = %s", fit, tc.wantFit, reason)
			}
			if fit && (len(allocation[Ascend910CType]) != 1 || allocation[Ascend910CType][0].UUID != "occupied") {
				t.Fatalf("unexpected allocation: %+v", allocation)
			}
			if !fit && (len(allocation[Ascend910CType]) != 0 || reason != common.GenReason(map[string]int{common.ModeNotFit: 1}, 1)) {
				t.Fatalf("unexpected rejection: allocation = %+v, reason = %s", allocation, reason)
			}
		})
	}
}
