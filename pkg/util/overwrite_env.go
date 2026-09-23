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

package util

import (
	"encoding/json"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// Annotation keys for the OverwriteEnv opt-out. Pod-level is a single bool
// string; container-level is a JSON map of container-name -> bool-string.
const (
	OverwriteEnvAnnotationKey           = "hami.io/overwrite-env"
	OverwriteEnvContainersAnnotationKey = "hami.io/overwrite-env-containers"
)

// OverwriteEnvMode is the three-state decision returned by OverwriteEnvDecision.
// Backends switch on it: Unset falls back to their global config, On forces
// injection, Off leaves any existing *_VISIBLE_DEVICES untouched.
type OverwriteEnvMode int

const (
	OverwriteEnvUnset OverwriteEnvMode = iota // no annotation — fall back to global config
	OverwriteEnvOn                            // force inject the clearing env var
	OverwriteEnvOff                           // do not touch existing env var
)

// String renders the mode for logs and debugging.
func (m OverwriteEnvMode) String() string {
	switch m {
	case OverwriteEnvOn:
		return "On"
	case OverwriteEnvOff:
		return "Off"
	default:
		return "Unset"
	}
}

// parseOverwriteEnvValue parses a single bool-string annotation value.
func parseOverwriteEnvValue(val string) (OverwriteEnvMode, bool) {
	b, err := strconv.ParseBool(val)
	if err != nil {
		return OverwriteEnvUnset, false
	}
	if b {
		return OverwriteEnvOn, true
	}
	return OverwriteEnvOff, true
}

// ParsePodOverwriteEnv parses the pod-level annotation; an empty or invalid value returns (Unset, false).
func ParsePodOverwriteEnv(podVal string) (OverwriteEnvMode, bool) {
	mode, parsed := parseOverwriteEnvValue(podVal)
	if podVal != "" && !parsed {
		klog.Warningf("OverwriteEnv: invalid pod-level annotation value %q, falling back to global config", podVal)
	}
	return mode, parsed
}

// DecodeContainerOverwriteEnvJSON decodes the container-level annotation into
// a name→mode map; malformed JSON and invalid values are logged and skipped.
func DecodeContainerOverwriteEnvJSON(rawJSON string) (map[string]OverwriteEnvMode, error) {
	if rawJSON == "" {
		return nil, nil
	}
	raw := map[string]string{}
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		klog.Warningf("OverwriteEnv: could not parse container-level annotation %q as JSON map[string]string (values must be quoted bool strings like \"true\"), falling back to pod-level decision: %v", rawJSON, err)
		return nil, err
	}
	entries := make(map[string]OverwriteEnvMode, len(raw))
	for name, val := range raw {
		mode, parsed := parseOverwriteEnvValue(val)
		if !parsed {
			klog.Warningf("OverwriteEnv: invalid container-level value %q for container %q, falling back to pod-level decision", val, name)
			continue
		}
		entries[name] = mode
	}
	return entries, nil
}

// ResolveOverwriteEnv returns the container's mode: a listed container
// overrides the pod level. "*" is a literal container name, not a wildcard.
func ResolveOverwriteEnv(podMode OverwriteEnvMode, entries map[string]OverwriteEnvMode, ctr *corev1.Container) OverwriteEnvMode {
	if entries == nil || ctr == nil {
		return podMode
	}
	if mode, ok := entries[ctr.Name]; ok {
		return mode
	}
	return podMode
}

// OverwriteEnvDecision is the composed resolver for backends that call once
// per container (nvidia); backends invoked once per chip (ascend) should use
// the split functions plus a cache instead. Priority: container entry >
// pod value > Unset; a malformed annotation never denies admission.
func OverwriteEnvDecision(pod *corev1.Pod, ctr *corev1.Container) OverwriteEnvMode {
	if pod == nil || ctr == nil || pod.Annotations == nil {
		return OverwriteEnvUnset
	}
	podMode, _ := ParsePodOverwriteEnv(pod.Annotations[OverwriteEnvAnnotationKey])
	rawJSON := pod.Annotations[OverwriteEnvContainersAnnotationKey]
	entries, err := DecodeContainerOverwriteEnvJSON(rawJSON)
	if err != nil {
		return podMode
	}
	return ResolveOverwriteEnv(podMode, entries, ctr)
}
