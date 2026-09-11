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

package remotegpu

// RemoteGPUConfig describes the resource names a client pod uses to ask for a
// fraction of the lupine GPU pool, and the port lupine listens on when a
// server node does not override it through the LupineServerLabel value.
type RemoteGPUConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	DefaultPort        int    `yaml:"defaultPort"`
	// LibImage carries HAMi-core into a client pod, which has no device plugin
	// to load it. Leave it empty to schedule remote GPUs without enforcing the
	// memory request, which is then only a placement filter.
	LibImage string `yaml:"libImage"`
}
