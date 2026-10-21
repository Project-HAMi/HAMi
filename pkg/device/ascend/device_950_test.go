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

package ascend

import (
	"encoding/json"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestIsAscend950DT(t *testing.T) {
	for _, tc := range []struct {
		family string
		want   bool
	}{
		{"Ascend950DT", true},
		{"Ascend950PR", false},
		{"Ascend910B", false},
	} {
		dev := &Devices{config: VNPUConfig{CommonWord: tc.family}}
		if got := dev.isAscend950DT(); got != tc.want {
			t.Fatalf("isAscend950DT(%q) = %v, want %v", tc.family, got, tc.want)
		}
	}
}

func Test950AdmissionFitAndAllocation(t *testing.T) {
	const family = "Ascend950DT"
	dev := &Devices{config: VNPUConfig{CommonWord: family, ResourceName: "huawei.com/" + family, ResourceMemoryName: "huawei.com/" + family + "-memory", ResourceCoreName: "huawei.com/" + family + "-core", MemoryAllocatable: 98304}, vnpuMode: VNPUModeHamiCore}
	pod := enpuTestPod(VNPUModeHamiCore, "")
	ctr := &pod.Spec.Containers[0]
	ctr.Resources.Limits = corev1.ResourceList{
		corev1.ResourceName(dev.config.ResourceName):       resource.MustParse("1"),
		corev1.ResourceName(dev.config.ResourceMemoryName): resource.MustParse("8192"),
		corev1.ResourceName(dev.config.ResourceCoreName):   resource.MustParse("50"),
	}
	if ok, err := dev.MutateAdmission(ctr, pod); !ok || err != nil {
		t.Fatalf("admission %v,%v", ok, err)
	}
	req, err := dev.GenerateResourceRequests(ctr)
	if err != nil {
		t.Fatal(err)
	}
	usage := enpuTestUsage("950-die", nil)
	usage.Type = family
	usage.Totalmem = 98304
	usage.Count = 4
	fit, allocation, reason := dev.Fit([]*device.DeviceUsage{usage}, req, pod, enpuTestNode(true, false), nil)
	if !fit {
		t.Fatalf("fit rejected: %s", reason)
	}
	oldIn, hadIn := device.InRequestDevices[family]
	oldOut, hadOut := device.SupportDevices[family]
	device.InRequestDevices[family] = "hami.io/" + family + "-devices-to-allocate"
	device.SupportDevices[family] = "hami.io/" + family + "-devices-allocated"
	t.Cleanup(func() {
		if hadIn {
			device.InRequestDevices[family] = oldIn
		} else {
			delete(device.InRequestDevices, family)
		}
		if hadOut {
			device.SupportDevices[family] = oldOut
		} else {
			delete(device.SupportDevices, family)
		}
	})
	annotations := map[string]string{}
	dev.PatchAnnotations(pod, &annotations, device.PodDevices{family: {allocation[family]}})
	var rt []RuntimeInfo
	if err = json.Unmarshal([]byte(annotations["huawei.com/"+family]), &rt); err != nil {
		t.Fatal(err)
	}
	if len(rt) != 1 || rt[0].Memory != 8192 || rt[0].Core != 50 || rt[0].Temp != "" {
		t.Fatalf("incorrect runtime allocation: %+v", rt)
	}
	usage.Usedcores = 60
	if fit, _, _ := dev.Fit([]*device.DeviceUsage{usage}, req, pod, enpuTestNode(true, false), nil); fit {
		t.Fatal("accepted insufficient core")
	}
	usage.Usedcores = 0
	usage.Usedmem = usage.Totalmem - req.Memreq + 1
	if fit, _, _ := dev.Fit([]*device.DeviceUsage{usage}, req, pod, enpuTestNode(true, false), nil); fit {
		t.Fatal("accepted insufficient memory")
	}
	usage.Usedmem = 0
	if fit, _, _ := dev.Fit([]*device.DeviceUsage{usage}, req, pod, enpuTestNode(false, false), nil); fit {
		t.Fatal("accepted non-core node")
	}
	pod.Annotations[VNPUModeAnnotation] = VNPUModeTemplate
	if _, err = dev.MutateAdmission(ctr, pod); err == nil {
		t.Fatal("accepted template mode")
	}
	pod.Annotations[VNPUModeAnnotation] = VNPUModeHamiCore
	ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)] = resource.MustParse("2")
	if _, err = dev.MutateAdmission(ctr, pod); err == nil {
		t.Fatal("accepted multi-device allocation")
	}
	ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)] = resource.MustParse("1")
	delete(ctr.Resources.Limits, corev1.ResourceName(dev.config.ResourceCoreName))
	if _, err = dev.MutateAdmission(ctr, pod); err == nil {
		t.Fatal("accepted missing core quota")
	}
}
