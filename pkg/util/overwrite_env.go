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

package util

import (
	"encoding/json"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// Annotation keys for the OverwriteEnv opt-out (ADR 0002). Pod-level is a single
// bool string; container-level is a JSON map of container-name -> bool-string.
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

// parseOverwriteEnvAnnotation parses an annotation value into a mode using
// strconv.ParseBool semantics (accepts true/True/1/t/T and false/False/0/f/F).
// The second return is false when the value is not a valid bool, so callers
// can decide whether to fall back to a lower-priority layer or treat the
// annotation as absent.
func parseOverwriteEnvAnnotation(val string) (OverwriteEnvMode, bool) {
	b, err := strconv.ParseBool(val)
	if err != nil {
		return OverwriteEnvUnset, false
	}
	if b {
		return OverwriteEnvOn, true
	}
	return OverwriteEnvOff, true
}

// OverwriteEnvDecision resolves the OverwriteEnv opt-out for a container by
// inspecting pod-level (hami.io/overwrite-env) and container-level
// (hami.io/overwrite-env-containers) annotations. Priority: container-level JSON
// entry > pod-level single value > Unset (the caller falls back to
// dev.config.OverwriteEnv). A container listed in the JSON overrides the pod
// level for that container only; unlisted containers fall back to the pod level.
// There is NO wildcard — "*" is a literal container name, not "all containers".
//
// Value vocabulary is strconv.ParseBool at both levels (true/false/1/0/t/f).
// A malformed JSON or an invalid bool value is logged (klog warning) and treated
// as absent — the affected container falls back to the lower layer. Admission is
// never denied for a malformed annotation.
//
// Nil pod / nil ctr / nil Annotations are all treated as "no annotations"
// and return Unset.
func OverwriteEnvDecision(pod *corev1.Pod, ctr *corev1.Container) OverwriteEnvMode {
	if pod == nil || ctr == nil || pod.Annotations == nil {
		return OverwriteEnvUnset
	}
	// An invalid pod-level value is intentionally treated as Unset (the caller
	// then applies its global config); warn so a typo doesn't silently no-op.
	podVal := pod.Annotations[OverwriteEnvAnnotationKey]
	podMode, podParsed := parseOverwriteEnvAnnotation(podVal)
	if podVal != "" && !podParsed {
		klog.Warningf("OverwriteEnv: invalid pod-level annotation value %q, falling back to global config", podVal)
	}
	mode := podMode
	// Container-level JSON overrides pod-level for listed containers only.
	rawJSON, hasContainerAnno := pod.Annotations[OverwriteEnvContainersAnnotationKey]
	if hasContainerAnno {
		entries := map[string]string{}
		if err := json.Unmarshal([]byte(rawJSON), &entries); err != nil {
			klog.Warningf("OverwriteEnv: could not parse container-level annotation %q as JSON map[string]string (values must be quoted bool strings like \"true\"), falling back to pod-level decision: %v", rawJSON, err)
			return mode
		}
		if val, ok := entries[ctr.Name]; ok {
			if ctrMode, parsed := parseOverwriteEnvAnnotation(val); parsed {
				mode = ctrMode
			} else {
				klog.Warningf("OverwriteEnv: invalid container-level value %q for container %q, falling back to pod-level decision", val, ctr.Name)
			}
		}
	}
	return mode
}
