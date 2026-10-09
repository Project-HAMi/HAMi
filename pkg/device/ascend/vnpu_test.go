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
	"testing"

	"gopkg.in/yaml.v2"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestVNPUModeConfig(t *testing.T) {
	previous := enableAscend
	enableAscend = true
	t.Cleanup(func() { enableAscend = previous })
	configs := []VNPUConfig{
		{CommonWord: "AscendModeTestA", ResourceName: "huawei.com/AscendModeTestA"},
		{CommonWord: "AscendModeTestB", ResourceName: "huawei.com/AscendModeTestB"},
	}
	for _, config := range configs {
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
	}

	for _, tc := range []struct {
		name, config, wantMode string
	}{
		{name: "default", config: "{}", wantMode: VNPUModeTemplate},
		{name: "empty", config: "hamiVnpuMode: ''", wantMode: VNPUModeTemplate},
		{name: "template", config: "hamiVnpuMode: template", wantMode: VNPUModeTemplate},
		{name: "core", config: "hamiVnpuMode: hami-core", wantMode: VNPUModeHamiCore},
		{name: "core alias", config: "hamiVnpuMode: hamiCore", wantMode: VNPUModeHamiCore},
		{name: "ENPU", config: "hamiVnpuMode: enpu", wantMode: VNPUModeENPU},
		{name: "case and whitespace", config: "hamiVnpuMode: ' ENPU '", wantMode: VNPUModeENPU},
		{name: "legacy true", config: "hamiVnpuCore: true", wantMode: VNPUModeHamiCore},
		{name: "legacy false", config: "hamiVnpuCore: false", wantMode: VNPUModeTemplate},
		{name: "empty with legacy", config: "hamiVnpuMode: ''\nhamiVnpuCore: true", wantMode: VNPUModeHamiCore},
		{name: "null with legacy", config: "hamiVnpuMode: null\nhamiVnpuCore: true", wantMode: VNPUModeHamiCore},
		{name: "template overrides legacy", config: "hamiVnpuMode: template\nhamiVnpuCore: true", wantMode: VNPUModeTemplate},
		{name: "ENPU overrides legacy", config: "hamiVnpuMode: enpu\nhamiVnpuCore: true", wantMode: VNPUModeENPU},
		{name: "core overrides legacy false", config: "hamiVnpuMode: hamiCore\nhamiVnpuCore: false", wantMode: VNPUModeHamiCore},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var config VNPUs
			assert.NilError(t, yaml.UnmarshalStrict([]byte(tc.config), &config))
			config.Configs = configs
			devs := InitDevices(config)
			assert.Equal(t, len(devs), len(configs))
			for _, dev := range devs {
				assert.Equal(t, dev.vnpuMode, tc.wantMode)
				for _, node := range []*corev1.Node{nil, {}} {
					assert.Equal(t, dev.nodeSupportsHamiCore(node), tc.wantMode == VNPUModeHamiCore)
					assert.Equal(t, dev.nodeSupportsENPU(node), tc.wantMode == VNPUModeENPU)
				}
				for _, core := range []string{"true", "false"} {
					for _, enpu := range []string{"true", "false"} {
						node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
							VNPUNodeSelectorAnnotation: core,
							VNPUNodeENPUAnnotation:     enpu,
						}}}
						assert.Equal(t, dev.nodeSupportsHamiCore(node), core == "true")
						assert.Equal(t, dev.nodeSupportsENPU(node), enpu == "true")
					}
				}
			}
		})
	}
}

func TestVNPUModeRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name, config, wantErr string
	}{
		{name: "unknown mode", config: "hamiVnpuMode: enup", wantErr: "vnpus.hamiVnpuMode"},
		{name: "boolean mode", config: "hamiVnpuMode: false", wantErr: "vnpus.hamiVnpuMode"},
		{name: "list mode", config: "hamiVnpuMode: [enpu]", wantErr: "cannot unmarshal"},
		{name: "invalid mode with legacy", config: "hamiVnpuMode: invalid\nhamiVnpuCore: true", wantErr: "vnpus.hamiVnpuMode"},
		{name: "removed ENPU flag", config: "enpu: true", wantErr: "field enpu not found"},
		{name: "unknown field", config: "hamiVnpuMdoe: enpu", wantErr: "field hamiVnpuMdoe not found"},
		{name: "duplicate mode", config: "hamiVnpuMode: enpu\nhamiVnpuMode: template", wantErr: "hamiVnpuMode already set"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var config VNPUs
			assert.ErrorContains(t, yaml.UnmarshalStrict([]byte(tc.config), &config), tc.wantErr)
		})
	}
}
