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

import (
	yamlv2 "go.yaml.in/yaml/v2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// RemoteGPUConfig describes the resource names a client pod uses to ask for a
// fraction of the lupine GPU pool, and the Lupine server pod started for it.
type RemoteGPUConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	// DefaultPort is the node port of a server node with an empty
	// LupineServerLabel. Each client pod's own server listens above it.
	DefaultPort int `yaml:"defaultPort"`
	// LibImage carries HAMi-core into each Lupine server pod, which enforces
	// the memory a client pod asked for. Leave it empty to schedule remote
	// GPUs without enforcing the memory request, which is then only a
	// placement filter.
	LibImage string `yaml:"libImage"`
	// Server is the Lupine server pod started for each client pod.
	Server ServerConfig `yaml:"server"`
	// ClientImage delivers Lupine client libraries. Empty disables automatic delivery.
	ClientImage string `yaml:"clientImage"`
	// SessionImage relays a port-forwarded session to the lupine server beside
	// it, so a user never forwards to the server itself. It needs socat on the
	// path. Leave it empty to run no session stubs.
	SessionImage string `yaml:"sessionImage"`
	// SessionImagePullSecrets names image pull Secrets in the scheduler's
	// namespace for session relay Pods. Global chart secrets precede
	// RemoteGPU-specific secrets in this list.
	SessionImagePullSecrets []string `yaml:"sessionImagePullSecrets"`
}

// ServerConfig shapes the Lupine server pod the scheduler starts on a server
// node for each client pod. It uses the Kubernetes field names, so the chart
// can render its values unchanged.
type ServerConfig struct {
	Image            string                      `json:"image"`
	ImagePullPolicy  corev1.PullPolicy           `json:"imagePullPolicy,omitempty"`
	ImagePullSecrets []string                    `json:"imagePullSecrets,omitempty"`
	RuntimeClassName string                      `json:"runtimeClassName,omitempty"`
	Tolerations      []corev1.Toleration         `json:"tolerations,omitempty"`
	SecurityContext  *corev1.SecurityContext     `json:"securityContext,omitempty"`
	Resources        corev1.ResourceRequirements `json:"resources,omitempty"`
	LivenessProbe    *corev1.Probe               `json:"livenessProbe,omitempty"`
	ReadinessProbe   *corev1.Probe               `json:"readinessProbe,omitempty"`
}

// UnmarshalYAML reads the Kubernetes types by their JSON names, which the
// YAML decoder of the device config does not know.
func (s *ServerConfig) UnmarshalYAML(unmarshal func(any) error) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return err
	}
	data, err := yamlv2.Marshal(raw)
	if err != nil {
		return err
	}
	type plain ServerConfig
	return yaml.UnmarshalStrict(data, (*plain)(s))
}
