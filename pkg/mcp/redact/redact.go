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

// Package redact masks likely-sensitive values before HAMi MCP tools and
// resources return Kubernetes object data (annotations, env vars, ConfigMap
// data) to an MCP client.
package redact

import (
	"encoding/json"
	"regexp"
	"strings"
)

const redactedPlaceholder = "REDACTED"

// sensitivePattern matches environment variable names, annotation keys, and
// map keys that likely carry secrets. Word-boundary anchors avoid false
// positives like "apiVersion" / "keyName" / "credentialPolicy" / "authority"
// matching purely on substring.
var sensitivePattern = regexp.MustCompile(`(?i)(?:^|[._\-/])(token|secret|password|passwd|credential|cred|auth|api[_\-]?key|key)(?:$|[._\-/])`)

// camelBoundary finds lower-to-upper transitions so camelCase keys
// (authToken, clientSecret, accessKeyId) can be matched by sensitivePattern,
// which is anchored on separators.
var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// isSensitiveKey reports whether key looks like it names a secret.
func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(camelBoundary.ReplaceAllString(key, "${1}_${2}"))
	return sensitivePattern.MatchString("_" + normalized + "_")
}

// Redact walks a JSON document and replaces the value of every object key
// that looks sensitive with REDACTED. Non-JSON input (e.g. a plain string)
// falls back to RedactBlob. Malformed JSON is returned unchanged rather than
// dropped, since callers use this defensively on data they don't control.
func Redact(jsonStr string) string {
	var doc any
	if err := json.Unmarshal([]byte(jsonStr), &doc); err != nil {
		return RedactBlob(jsonStr)
	}
	redactValue(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		return jsonStr
	}
	return string(out)
}

func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = redactedPlaceholder
				continue
			}
			redactValue(val)
		}
	case []any:
		for _, item := range t {
			redactValue(item)
		}
	}
}

// kvLine matches "key: value" or "key=value" lines, the common shape of
// YAML, .env, and INI-style text embedded as a single ConfigMap string
// value. Redact()'s JSON walker never sees inside such a blob, so callers
// that emit raw ConfigMap.Data values must also call RedactBlob on each one.
var kvLine = regexp.MustCompile(`(?m)^(\s*(?:-\s*)?)([A-Za-z0-9_.\-/]+)(\s*[:=]\s*)(.+)$`)

// RedactBlob masks values in YAML/INI/env-style text whose key looks
// sensitive. Keys that don't match sensitivePattern are left untouched.
func RedactBlob(s string) string {
	return kvLine.ReplaceAllStringFunc(s, func(line string) string {
		m := kvLine.FindStringSubmatch(line)
		if m == nil || !isSensitiveKey(m[2]) {
			return line
		}
		return m[1] + m[2] + m[3] + redactedPlaceholder
	})
}

// RedactAnnotations returns a copy of annos with sensitive-looking keys'
// values replaced. Used directly on corev1.Node/Pod annotation maps, where
// building a JSON round-trip would be wasteful.
func RedactAnnotations(annos map[string]string) map[string]string {
	if annos == nil {
		return nil
	}
	out := make(map[string]string, len(annos))
	for k, v := range annos {
		if isSensitiveKey(k) {
			out[k] = redactedPlaceholder
			continue
		}
		out[k] = v
	}
	return out
}
