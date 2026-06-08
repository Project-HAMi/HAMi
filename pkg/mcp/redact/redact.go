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

package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

// sensitivePattern matches environment variable names and annotation keys that
// likely carry secrets. We use word-boundary anchors to avoid false positives
// like "apiVersion" / "keyName" / "credentialPolicy" / "authority" being
// matched purely on substring.
var sensitivePattern = regexp.MustCompile(`(?i)(?:^|[._\-/])(token|secret|password|passwd|credential|cred|auth|api[_\-]?key|key)(?:$|[._\-/])`)

// nonSensitiveKeys are JSON keys that pattern-match the regex above but are
// known never to carry secrets, so they bypass redaction entirely.
var nonSensitiveKeys = map[string]struct{}{
	"apiVersion": {},
}

// Redact removes sensitive information from a JSON string.
// It strips:
// - Container env entries whose name matches sensitive pattern
// - Annotation keys that match sensitive pattern
// - imagePullSecrets
// - Pod spec.volumes[*].secret
func Redact(input string) string {
	// Try to parse as JSON
	var data interface{}
	if err := json.Unmarshal([]byte(input), &data); err != nil {
		// If not valid JSON, return as-is
		return input
	}

	// Redact the data
	redacted := redactValue(data)

	// Marshal back to JSON
	output, err := json.Marshal(redacted)
	if err != nil {
		// If marshaling fails, return original
		return input
	}

	return string(output)
}

// redactValue recursively redacts sensitive information from a value.
func redactValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return redactMap(val)
	case []interface{}:
		return redactSlice(val)
	default:
		return v
	}
}

// redactMap redacts sensitive information from a map.
func redactMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range m {
		// Check if this is a sensitive key
		if isSensitiveKey(key) {
			// Redact the value
			result[key] = "[REDACTED]"
			continue
		}

		// Special handling for specific keys
		switch key {
		case "env":
			// Redact sensitive environment variables
			result[key] = redactEnv(value)
		case "annotations":
			// Redact sensitive annotations
			result[key] = redactAnnotations(value)
		case "imagePullSecrets":
			// Only the secret reference name is sensitive — keep the array
			// shape intact so downstream consumers can still see the count and
			// any non-name fields, but mask each entry's name field.
			result[key] = redactImagePullSecrets(value)
		case "volumes":
			// Redact secret volumes
			result[key] = redactVolumes(value)
		default:
			// Recursively redact nested values
			result[key] = redactValue(value)
		}
	}

	return result
}

// redactSlice redacts sensitive information from a slice.
func redactSlice(s []interface{}) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		result[i] = redactValue(v)
	}
	return result
}

// redactEnv redacts sensitive environment variables.
func redactEnv(env interface{}) interface{} {
	envSlice, ok := env.([]interface{})
	if !ok {
		return env
	}

	var result []interface{}
	for _, item := range envSlice {
		envMap, ok := item.(map[string]interface{})
		if !ok {
			result = append(result, item)
			continue
		}

		// Check if env name is sensitive — if so, mask both the literal value
		// and any valueFrom (which may reference a secret/configmap key whose
		// name leaks information).
		if name, ok := envMap["name"].(string); ok && isSensitiveKey(name) {
			redacted := make(map[string]interface{})
			for k, v := range envMap {
				switch k {
				case "value", "valueFrom":
					redacted[k] = "[REDACTED]"
				default:
					redacted[k] = v
				}
			}
			result = append(result, redacted)
		} else {
			result = append(result, item)
		}
	}

	return result
}

// redactImagePullSecrets masks the name field of each pull-secret reference
// while preserving the array shape.
func redactImagePullSecrets(v interface{}) interface{} {
	slice, ok := v.([]interface{})
	if !ok {
		return v
	}
	result := make([]interface{}, len(slice))
	for i, item := range slice {
		m, ok := item.(map[string]interface{})
		if !ok {
			result[i] = item
			continue
		}
		redacted := make(map[string]interface{}, len(m))
		for k, val := range m {
			if k == "name" {
				redacted[k] = "[REDACTED]"
			} else {
				redacted[k] = val
			}
		}
		result[i] = redacted
	}
	return result
}

// redactAnnotations redacts sensitive annotations.
func redactAnnotations(annotations interface{}) interface{} {
	annotMap, ok := annotations.(map[string]interface{})
	if !ok {
		return annotations
	}

	result := make(map[string]interface{})
	for key, value := range annotMap {
		if isSensitiveKey(key) {
			result[key] = "[REDACTED]"
		} else {
			result[key] = value
		}
	}

	return result
}

// redactVolumes redacts secret volumes.
func redactVolumes(volumes interface{}) interface{} {
	volumeSlice, ok := volumes.([]interface{})
	if !ok {
		return volumes
	}

	var result []interface{}
	for _, item := range volumeSlice {
		volumeMap, ok := item.(map[string]interface{})
		if !ok {
			result = append(result, item)
			continue
		}

		// Check if volume has secret
		if _, hasSecret := volumeMap["secret"]; hasSecret {
			// Redact the entire volume
			redacted := make(map[string]interface{})
			for k, v := range volumeMap {
				if k == "secret" {
					redacted[k] = "[REDACTED]"
				} else {
					redacted[k] = v
				}
			}
			result = append(result, redacted)
		} else {
			result = append(result, item)
		}
	}

	return result
}

// isSensitiveKey checks if a key matches the sensitive pattern. A short,
// hard-coded allowlist (e.g. apiVersion) is honored before pattern matching to
// avoid false positives on common Kubernetes JSON shapes.
func isSensitiveKey(key string) bool {
	if _, ok := nonSensitiveKeys[key]; ok {
		return false
	}
	return sensitivePattern.MatchString(strings.ToLower(key))
}
