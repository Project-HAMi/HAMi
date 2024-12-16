/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package plugin

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"gotest.tools/v3/assert"
	"k8s.io/klog/v2"
)

func TestGenerateMigTemplate(t *testing.T) {
	sconfig := nvidia.NvidiaConfig{
		MigGeometriesList: make([]util.AllowedMigGeometries, 0),
	}
	sconfig.MigGeometriesList = append(sconfig.MigGeometriesList, util.AllowedMigGeometries{
		Models: []string{"A30"},
		Geometries: []util.Geometry{
			{
				util.MigTemplate{
					Name:   "1g.6gb",
					Memory: 6144,
					Count:  4,
				},
			},
			{
				util.MigTemplate{
					Name:   "2g.12gb",
					Memory: 12288,
					Count:  2,
				},
			},
			{
				util.MigTemplate{
					Name:   "4g.24gb",
					Memory: 24576,
					Count:  1,
				},
			},
		},
	})
	sconfig.MigGeometriesList = append(sconfig.MigGeometriesList, util.AllowedMigGeometries{
		Models: []string{"A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB", "A100-SXM4-40GB"},
		Geometries: []util.Geometry{
			{
				util.MigTemplate{
					Name:   "1g.5gb",
					Memory: 5120,
					Count:  7,
				},
			},
			{
				util.MigTemplate{
					Name:   "2g.10gb",
					Memory: 10240,
					Count:  3,
				},
				util.MigTemplate{
					Name:   "1g.5gb",
					Memory: 5120,
					Count:  1,
				},
			},
			{
				util.MigTemplate{
					Name:   "3g.20gb",
					Memory: 20480,
					Count:  2,
				},
			},
			{
				util.MigTemplate{
					Name:   "7g.40gb",
					Memory: 40960,
					Count:  1,
				},
			},
		},
	})
	sconfig.MigGeometriesList = append(sconfig.MigGeometriesList, util.AllowedMigGeometries{
		Models: []string{"A100-SXM4-80GB", "A100-80GB-PCIe", "A100-PCIE-80GB"},
		Geometries: []util.Geometry{
			{
				util.MigTemplate{
					Name:   "1g.10gb",
					Memory: 10240,
					Count:  7,
				},
			},
			{
				util.MigTemplate{
					Name:   "2g.20gb",
					Memory: 20480,
					Count:  3,
				},
				util.MigTemplate{
					Name:   "1g.10gb",
					Memory: 10240,
					Count:  1,
				},
			},
			{
				util.MigTemplate{
					Name:   "3g.40gb",
					Memory: 40960,
					Count:  2,
				},
			},
			{
				util.MigTemplate{
					Name:   "7g.80gb",
					Memory: 81920,
					Count:  1,
				},
			},
		},
	})
	for _, val := range sconfig.MigGeometriesList {
		klog.Infoln("val=", val)
	}
	plugin := NvidiaDevicePlugin{
		operatingMode:   "mig",
		schedulerConfig: sconfig,
	}
	plugin.migCurrent = nvidia.MigPartedSpec{
		Version:    "v1",
		MigConfigs: make(map[string]nvidia.MigConfigSpecSlice),
	}
	plugin.migCurrent.MigConfigs["current"] = nvidia.MigConfigSpecSlice{
		nvidia.MigConfigSpec{
			Devices:    []int32{0, 1},
			MigEnabled: true,
			MigDevices: make(map[string]int32),
		},
	}

	pos, needsreset := plugin.GenerateMigTemplate("A100-SXM4-40GB", 0, util.ContainerDevice{
		Idx:     0,
		UUID:    "aaaaabbbb[1-1]",
		Usedmem: 3000,
	})
	assert.Equal(t, plugin.migCurrent.MigConfigs["current"][0].MigDevices["2g.10gb"], int32(3))
	assert.Equal(t, pos, 1)
	assert.Check(t, needsreset)

	pos, needsreset = plugin.GenerateMigTemplate("A100-SXM4-40GB", 0, util.ContainerDevice{
		Idx:     0,
		UUID:    "aaaaabbbb[0-1]",
		Usedmem: 3000,
	})
	assert.Equal(t, plugin.migCurrent.MigConfigs["current"][0].MigDevices["1g.5gb"], int32(7))
	assert.Equal(t, pos, 1)
	assert.Check(t, needsreset)

	pos, needsreset = plugin.GenerateMigTemplate("A100-SXM4-40GB", 0, util.ContainerDevice{
		Idx:     0,
		UUID:    "aaaaabbbb[0-2]",
		Usedmem: 3000,
	})
	assert.Equal(t, plugin.migCurrent.MigConfigs["current"][0].MigDevices["1g.5gb"], int32(7))
	assert.Equal(t, pos, 2)
	assert.Check(t, !needsreset)
}
