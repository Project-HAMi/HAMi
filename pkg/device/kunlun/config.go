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

package kunlun

import "flag"

var (
	// XPU
	KunlunResourceCount string

	// VXPU
	KunlunResourceVCount  string
	KunlunResourceVMemory string
)

type KunlunConfig struct {
	ResourceCountName   string `yaml:"resourceCountName"`
	ResourceVCountName  string `yaml:"resourceVCountName"`
	ResourceVMemoryName string `yaml:"resourceVMemoryName"`
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&KunlunResourceCount, "kunlun-name", "kunlunxin.com/xpu", "kunlunxin resource count")
	fs.StringVar(&KunlunResourceVCount, "kunlun-vcount", "kunlunxin.com/vxpu", "kunlunxin resource vcount")
	fs.StringVar(&KunlunResourceVMemory, "kunlun-vmemory", "kunlunxin.com/vxpu-memory", "kunlunxin resource vmemory")
}
