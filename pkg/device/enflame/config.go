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

package enflame

import "flag"

var (
	EnflameResourceNameGCU            string
	EnflameResourceNameDRSGCU         string
	EnflameResourceNameGCUMemory      string
	EnflameResourceNameGCUCore        string
	EnflameResourceNameVGCU           string
	EnflameResourceNameVGCUPercentage string
)

type EnflameConfig struct {
	// GCU
	ResourceNameGCU string `yaml:"resourceNameGCU"`

	// DRS-GCU (new hard-partition mode)
	ResourceNameDRSGCU string `yaml:"resourceNameDRSGCU"`
	ResourceNameMemory string `yaml:"resourceNameGCUMemory"`
	ResourceNameCore   string `yaml:"resourceNameGCUCore"`

	// Legacy shared-GCU key kept for compatibility.
	ResourceNameVGCU           string `yaml:"resourceNameVGCU"`
	ResourceNameVGCUPercentage string `yaml:"resourceNameVGCUPercentage"`
}

func ParseConfig(fs *flag.FlagSet) {
	// GCU
	fs.StringVar(&EnflameResourceNameGCU, "enflame-gcu-resource-name", "enflame.com/gcu", "enflame gcu resource name")

	// DRS-GCU.
	fs.StringVar(&EnflameResourceNameDRSGCU, "enflame-drs-gcu-resource-name", "enflame.com/drs-gcu", "enflame drs gcu resource name")
	fs.StringVar(&EnflameResourceNameGCUMemory, "enflame-gcu-memory-resource-name", "enflame.com/gcu-memory", "enflame gcu memory request resource name")
	fs.StringVar(&EnflameResourceNameGCUCore, "enflame-gcu-core-resource-name", "enflame.com/gcu-core", "enflame gcu core request resource name")
	// Legacy flag alias for backward compatibility.
	fs.StringVar(&EnflameResourceNameDRSGCU, "enflame-vgcu-resource-name", "enflame.com/drs-gcu", "legacy enflame vgcu resource name, now maps to drs-gcu")
	// Legacy shared-GCU related flags.
	fs.StringVar(&EnflameResourceNameVGCU, "enflame-vgcu-legacy-resource-name", "enflame.com/vgcu", "legacy enflame shared gcu count resource name")
	fs.StringVar(&EnflameResourceNameVGCUPercentage, "enflame-vgcu-percentage-resource-name", "enflame.com/vgcu-percentage", "enflame shared gcu percentage resource name")
}
