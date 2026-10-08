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
	"fmt"
	"strings"
)

type Template struct {
	Name   string `yaml:"name"`
	Memory int64  `yaml:"memory"`
	AICore int32  `yaml:"aiCore,omitempty"`
	AICPU  int32  `yaml:"aiCPU,omitempty"`
}

type VNPUConfig struct {
	CommonWord         string     `yaml:"commonWord"`
	ChipName           string     `yaml:"chipName"`
	ResourceName       string     `yaml:"resourceName"`
	ResourceMemoryName string     `yaml:"resourceMemoryName"`
	ResourceCoreName   string     `yaml:"resourceCoreName"`
	MemoryAllocatable  int64      `yaml:"memoryAllocatable"`
	MemoryCapacity     int64      `yaml:"memoryCapacity"`
	MemoryFactor       int32      `yaml:"memoryFactor"`
	AICore             int32      `yaml:"aiCore"`
	AICPU              int32      `yaml:"aiCPU"`
	Templates          []Template `yaml:"templates"`
	SuperPod           bool       `yaml:"superPod"`
}

// VNPUs holds the default Ascend VNPU mode and the per-chip config list.
// OverwriteEnv and RuntimeClassName apply to every chip identically, so they
// live here rather than on each VNPUConfig.
type VNPUs struct {
	HamiVnpuMode     string       `yaml:"hamiVnpuMode,omitempty"`
	EnpuPolicy       string       `yaml:"enpuPolicy,omitempty"`
	OverwriteEnv     bool         `yaml:"overwriteEnv"`
	RuntimeClassName string       `yaml:"runtimeClassName"`
	Configs          []VNPUConfig `yaml:"configs"`

	// Deprecated: use HamiVnpuMode. Only applies when HamiVnpuMode is empty.
	HamiVnpuCore bool `yaml:"hamiVnpuCore,omitempty"`
}

// mode resolves the global default; node capability annotations take precedence.
func (v VNPUs) mode() (string, error) {
	switch mode := strings.ToLower(strings.TrimSpace(v.HamiVnpuMode)); mode {
	case "":
		if v.HamiVnpuCore {
			return VNPUModeHamiCore, nil
		}
		return VNPUModeTemplate, nil
	case "hamicore", VNPUModeHamiCore:
		return VNPUModeHamiCore, nil
	case VNPUModeTemplate, VNPUModeENPU:
		return mode, nil
	default:
		return "", fmt.Errorf("vnpus.hamiVnpuMode must be template, hami-core (or hamiCore), or enpu, got %q", v.HamiVnpuMode)
	}
}

// UnmarshalYAML rejects invalid modes before the scheduler starts.
func (v *VNPUs) UnmarshalYAML(unmarshal func(any) error) error {
	type plainVNPUs VNPUs
	if err := unmarshal((*plainVNPUs)(v)); err != nil {
		return err
	}
	_, err := v.mode()
	return err
}
