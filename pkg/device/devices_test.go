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

	"gopkg.in/yaml.v2"
	"gotest.tools/v3/assert"

	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/iluvatar"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
)

func Test_LoadConfig(t *testing.T) {
	configMapdata := `
nvidia:
  resourceCountName: nvidia.com/gpu
  resourceMemoryName: nvidia.com/gpumem
  resourceMemoryPercentageName: nvidia.com/gpumem-percentage
  resourceCoreName: nvidia.com/gpucores
  resourcePriorityName: nvidia.com/priority
  overwriteEnv: false
  defaultMemory: 0
  defaultCores: 0
  defaultGPUNum: 1
cambricon:
  resourceCountName: cambricon.com/vmlu
  resourceMemoryName: cambricon.com/mlu.smlu.vmemory
  resourceCoreName: cambricon.com/mlu.smlu.vcore
hygon:
  resourceCountName: hygon.com/dcunum
  resourceMemoryName: hygon.com/dcumem
  resourceCoreName: hygon.com/dcucores
metax:
  resourceCountName: "metax-tech.com/gpu"
mthreads:
  resourceCountName: "mthreads.com/vgpu"
  resourceMemoryName: "mthreads.com/sgpu-memory"
  resourceCoreName: "mthreads.com/sgpu-core"
iluvatar: 
  resourceCountName: iluvatar.ai/vgpu
  resourceMemoryName: iluvatar.ai/vcuda-memory
  resourceCoreName: iluvatar.ai/vcuda-core
vnpus:
- chipName: 910B
  commonWord: Ascend910A
  resourceName: huawei.com/Ascend910A
  resourceMemoryName: huawei.com/Ascend910A-memory
  memoryAllocatable: 32768
  memoryCapacity: 32768
  aiCore: 30
  templates:
    - name: vir02
      memory: 2184
      aiCore: 2
    - name: vir04
      memory: 4369
      aiCore: 4
    - name: vir08
      memory: 8738
      aiCore: 8
    - name: vir16
      memory: 17476
      aiCore: 16
- chipName: 910B3
  commonWord: Ascend910B
  resourceName: huawei.com/Ascend910B
  resourceMemoryName: huawei.com/Ascend910B-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_16g
      memory: 16384
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_32g
      memory: 32768
      aiCore: 10
      aiCPU: 3
- chipName: 310P3
  commonWord: Ascend310P
  resourceName: huawei.com/Ascend310P
  resourceMemoryName: huawei.com/Ascend310P-memory
  memoryAllocatable: 21527
  memoryCapacity: 24576
  aiCore: 8
  aiCPU: 7
  templates:
    - name: vir01
      memory: 3072
      aiCore: 1
      aiCPU: 1
    - name: vir02
      memory: 6144
      aiCore: 2
      aiCPU: 2
    - name: vir04
      memory: 12288
      aiCore: 4
      aiCPU: 4`

	var yamlData Config
	err := yaml.Unmarshal([]byte(configMapdata), &yamlData)
	assert.NilError(t, err)

	t.Run("NVIDIA Config", func(t *testing.T) {
		nvidiaConfig := yamlData.NvidiaConfig
		assert.Equal(t, nvidiaConfig.ResourceCountName, "nvidia.com/gpu")
		assert.Equal(t, nvidiaConfig.ResourceMemoryName, "nvidia.com/gpumem")
		assert.Equal(t, nvidiaConfig.ResourceMemoryPercentageName, "nvidia.com/gpumem-percentage")
		assert.Equal(t, nvidiaConfig.ResourceCoreName, "nvidia.com/gpucores")
		assert.Equal(t, nvidiaConfig.ResourcePriority, "nvidia.com/priority")
		assert.Equal(t, nvidiaConfig.OverwriteEnv, false)
		assert.Equal(t, nvidiaConfig.DefaultMemory, int32(0))
		assert.Equal(t, nvidiaConfig.DefaultCores, int32(0))
		assert.Equal(t, nvidiaConfig.DefaultGPUNum, int32(1))
	})

	tests := []struct {
		name         string
		expected     interface{}
		actualGetter func() interface{}
	}{
		{"Cambricon Config", createCambriconConfig(), func() interface{} { return yamlData.CambriconConfig }},
		{"Hygon Config", createHygonConfig(), func() interface{} { return yamlData.HygonConfig }},
		{"Iluvatar Config", createIluvatarConfig(), func() interface{} { return yamlData.IluvatarConfig }},
		{"Mthreads Config", createMthreadsConfig(), func() interface{} { return yamlData.MthreadsConfig }},
		{"Metax Config", createMetaxConfig(), func() interface{} { return yamlData.MetaxConfig }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.DeepEqual(t, tt.expected, tt.actualGetter())
		})
	}

	expectedVNPUs := createVNPUConfigs()
	assert.DeepEqual(t, yamlData.VNPUs, expectedVNPUs)
}

func createCambriconConfig() cambricon.CambriconConfig {
	return cambricon.CambriconConfig{
		ResourceCountName:  "cambricon.com/vmlu",
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
	}
}

func createHygonConfig() hygon.HygonConfig {
	return hygon.HygonConfig{
		ResourceCountName:  "hygon.com/dcunum",
		ResourceMemoryName: "hygon.com/dcumem",
		ResourceCoreName:   "hygon.com/dcucores",
	}
}

func createIluvatarConfig() iluvatar.IluvatarConfig {
	return iluvatar.IluvatarConfig{
		ResourceCountName:  "iluvatar.ai/vgpu",
		ResourceMemoryName: "iluvatar.ai/vcuda-memory",
		ResourceCoreName:   "iluvatar.ai/vcuda-core",
	}
}

func createMthreadsConfig() mthreads.MthreadsConfig {
	return mthreads.MthreadsConfig{
		ResourceCountName:  "mthreads.com/vgpu",
		ResourceMemoryName: "mthreads.com/sgpu-memory",
		ResourceCoreName:   "mthreads.com/sgpu-core",
	}
}

func createMetaxConfig() metax.MetaxConfig {
	return metax.MetaxConfig{
		ResourceCountName: "metax-tech.com/gpu",
	}
}

func createVNPUConfigs() []ascend.VNPUConfig {
	return []ascend.VNPUConfig{
		{
			ChipName:           "910B",
			CommonWord:         "Ascend910A",
			ResourceName:       "huawei.com/Ascend910A",
			ResourceMemoryName: "huawei.com/Ascend910A-memory",
			MemoryAllocatable:  32768,
			MemoryCapacity:     32768,
			AICore:             30,
			Templates: []ascend.Template{
				{Name: "vir02", Memory: 2184, AICore: 2},
				{Name: "vir04", Memory: 4369, AICore: 4},
				{Name: "vir08", Memory: 8738, AICore: 8},
				{Name: "vir16", Memory: 17476, AICore: 16},
			},
		},
		{
			ChipName:           "910B3",
			CommonWord:         "Ascend910B",
			ResourceName:       "huawei.com/Ascend910B",
			ResourceMemoryName: "huawei.com/Ascend910B-memory",
			MemoryAllocatable:  65536,
			MemoryCapacity:     65536,
			AICore:             20,
			AICPU:              7,
			Templates: []ascend.Template{
				{Name: "vir05_1c_16g", Memory: 16384, AICore: 5, AICPU: 1},
				{Name: "vir10_3c_32g", Memory: 32768, AICore: 10, AICPU: 3},
			},
		},
		{
			ChipName:           "310P3",
			CommonWord:         "Ascend310P",
			ResourceName:       "huawei.com/Ascend310P",
			ResourceMemoryName: "huawei.com/Ascend310P-memory",
			MemoryAllocatable:  21527,
			MemoryCapacity:     24576,
			AICore:             8,
			AICPU:              7,
			Templates: []ascend.Template{
				{Name: "vir01", Memory: 3072, AICore: 1, AICPU: 1},
				{Name: "vir02", Memory: 6144, AICore: 2, AICPU: 2},
				{Name: "vir04", Memory: 12288, AICore: 4, AICPU: 4},
			},
		},
	}
}
