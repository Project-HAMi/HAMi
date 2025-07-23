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

package metax

import "flag"

type MetaxConfig struct {
	// GPU
	ResourceCountName string `yaml:"resourceCountName"`

	// SGPU
	ResourceVCountName  string `yaml:"resourceVCountName"`
	ResourceVMemoryName string `yaml:"resourceVMemoryName"`
	ResourceVCoreName   string `yaml:"resourceVCoreName"`
	TopologyAware       bool   `yaml:"sgpuTopologyAware"`
}

func ParseConfig(fs *flag.FlagSet) {
	// GPU
	fs.StringVar(&MetaxResourceCount, "metax-name", "metax-tech.com/gpu", "metax resource count")

	// SGPU
	fs.StringVar(&MetaxResourceNameVCount, "metax-vcount", "metax-tech.com/sgpu", "metax vcount name")
	fs.StringVar(&MetaxResourceNameVCore, "metax-vcore", "metax-tech.com/vcore", "metax vcore name")
	fs.StringVar(&MetaxResourceNameVMemory, "metax-vmemory", "metax-tech.com/vmemory", "metax vmemory name")
	fs.BoolVar(&MetaxTopologyAware, "sgpu-topology-aware", false, "sGPU topology aware enable")
}
