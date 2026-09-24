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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func initENPUPolicyTestDevice(t *testing.T, policy string) *Devices {
	t.Helper()
	previous := enableAscend
	t.Cleanup(func() { enableAscend = previous })
	enableAscend = true
	config := enpuTestDevice().config
	for _, registry := range []map[string]string{device.InRequestDevices, device.SupportDevices, util.HandshakeAnnos} {
		value, exists := registry[config.CommonWord]
		t.Cleanup(func() {
			if exists {
				registry[config.CommonWord] = value
			} else {
				delete(registry, config.CommonWord)
			}
		})
	}
	devs := InitDevices(VNPUs{Enpu: true, EnpuPolicy: policy, Configs: []VNPUConfig{config}})
	if len(devs) != 1 {
		t.Fatalf("InitDevices returned %d devices, want 1", len(devs))
	}
	if devs[0].enpuPolicy != policy {
		t.Fatalf("InitDevices changed configured policy %q to %q", policy, devs[0].enpuPolicy)
	}
	return devs[0]
}

func TestENPUConfiguredPolicyAdmissionAndFit(t *testing.T) {
	for _, tc := range []struct {
		name, configured, override, wantPolicy string
		invalid                                bool
	}{
		{name: "invalid default", configured: "elatsic", invalid: true},
		{name: "invalid padded default", configured: " invalid ", invalid: true},
		{name: "invalid explicit override", configured: "elastic", override: "invalid", invalid: true},
		{name: "blank override keeps invalid default", configured: "invalid", override: " \t ", invalid: true},
		{name: "empty default", wantPolicy: "elastic"},
		{name: "whitespace default", configured: " \t ", wantPolicy: "elastic"},
		{name: "fixed numeric alias", configured: "1", wantPolicy: "fixed-share"},
		{name: "fixed text alias", configured: " FIXED_SHARE ", wantPolicy: "fixed-share"},
		{name: "elastic numeric alias", configured: "2", wantPolicy: "elastic"},
		{name: "elastic text alias", configured: " ELASTIC ", wantPolicy: "elastic"},
		{name: "best effort numeric alias", configured: "3", wantPolicy: "best-effort"},
		{name: "best effort text alias", configured: " BEST_EFFORT ", wantPolicy: "best-effort"},
		{name: "valid override wins over invalid default", configured: "invalid", override: " FIXED_SHARE ", wantPolicy: "fixed-share"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := initENPUPolicyTestDevice(t, tc.configured)
			pod := enpuTestPod(VNPUModeENPU, tc.override)
			fit, allocation, reason := dev.Fit([]*device.DeviceUsage{enpuTestUsage("free", nil)}, enpuTestRequest(VNPUModeENPU), pod, enpuTestNode(false, true), nil)
			if tc.invalid {
				if fit || len(allocation[Ascend910CType]) != 0 || reason != common.GenReason(map[string]int{common.ModeNotFit: 1}, 1) {
					t.Fatalf("invalid policy Fit = %v, allocation = %+v, reason = %q", fit, allocation, reason)
				}
			} else if !fit || len(allocation[Ascend910CType]) != 1 || allocation[Ascend910CType][0].UUID != "free" {
				t.Fatalf("valid policy Fit = %v, allocation = %+v, reason = %q", fit, allocation, reason)
			}
			admitted, err := dev.MutateAdmission(&pod.Spec.Containers[0], pod)
			if tc.invalid {
				if admitted || err == nil || !strings.Contains(err.Error(), "invalid ENPU scheduling policy") {
					t.Fatalf("invalid policy admission = %v, error = %v", admitted, err)
				}
				return
			}
			if !admitted || err != nil {
				t.Fatalf("valid policy admission = %v, error = %v", admitted, err)
			}
			if got := pod.Annotations["huawei.com/enpu-policy"]; got != tc.wantPolicy {
				t.Fatalf("admitted policy = %q, want %q", got, tc.wantPolicy)
			}
		})
	}
}

func TestENPUInvalidConfiguredPolicyLeavesLegacyModesUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		core       bool
		wantMemory int64
	}{
		{name: "hami-core", mode: VNPUModeHamiCore, core: true, wantMemory: 20480},
		{name: "template", mode: VNPUModeTemplate, wantMemory: 32768},
		{name: "unannotated template", wantMemory: 32768},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dev := initENPUPolicyTestDevice(t, "invalid-default")
			pod := enpuTestPod(tc.mode, "invalid-ENPU-only-override")
			ctr := &pod.Spec.Containers[0]
			admitted, err := dev.MutateAdmission(ctr, pod)
			if !admitted || err != nil {
				t.Fatalf("legacy admission = %v, error = %v", admitted, err)
			}
			memory := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
			if memory.Value() != tc.wantMemory {
				t.Fatalf("legacy memory = %d, want %d", memory.Value(), tc.wantMemory)
			}
			if got := pod.Annotations["huawei.com/enpu-policy"]; got != "invalid-ENPU-only-override" {
				t.Fatalf("legacy Pod ENPU-only policy was rewritten: %q", got)
			}
			fit, allocation, reason := dev.Fit([]*device.DeviceUsage{enpuTestUsage("free", nil)}, dev.GenerateResourceRequests(ctr), pod, enpuTestNode(tc.core, false), nil)
			if !fit || len(allocation[Ascend910CType]) != 1 || allocation[Ascend910CType][0].Usedmem != int32(tc.wantMemory) {
				t.Fatalf("legacy Fit = %v, allocation = %+v, reason = %q", fit, allocation, reason)
			}
		})
	}
}
