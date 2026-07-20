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
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "non-JSON input",
			input:    "not json",
			expected: "not json",
		},
		{
			name:     "empty JSON object",
			input:    "{}",
			expected: "{}",
		},
		{
			name:     "no sensitive data",
			input:    `{"name":"test","value":123}`,
			expected: `{"name":"test","value":123}`,
		},
		{
			name:     "redact token in env",
			input:    `{"env":[{"name":"MY_TOKEN","value":"secret123"}]}`,
			expected: `{"env":[{"name":"MY_TOKEN","value":"[REDACTED]"}]}`,
		},
		{
			name:     "redact secret volumes",
			input:    `{"volumes":[{"name":"vol1","secret":{"secretName":"my-secret"}}]}`,
			expected: `{"volumes":[{"name":"vol1","secret":"[REDACTED]"}]}`,
		},
		{
			name:     "redact password in env",
			input:    `{"env":[{"name":"DB_PASSWORD","value":"pass123"}]}`,
			expected: `{"env":[{"name":"DB_PASSWORD","value":"[REDACTED]"}]}`,
		},
		{
			name:     "preserve apiVersion (not a secret)",
			input:    `{"apiVersion":"v1","kind":"Pod"}`,
			expected: `{"apiVersion":"v1","kind":"Pod"}`,
		},
		{
			name:     "configMap volume preserved (only secret volumes redacted)",
			input:    `{"volumes":[{"name":"cm","configMap":{"name":"my-cm"}}]}`,
			expected: `{"volumes":[{"configMap":{"name":"my-cm"},"name":"cm"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Redact(tt.input)
			if result != tt.expected {
				t.Errorf("Redact() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestRedact_Annotations verifies that sensitive annotation keys are masked
// while ordinary keys pass through.
func TestRedact_Annotations(t *testing.T) {
	in := `{"annotations":{"my-api-token":"abc","app":"foo","kubectl.kubernetes.io/last-applied-configuration":"x"}}`
	out := Redact(in)
	if !strings.Contains(out, `"my-api-token":"[REDACTED]"`) {
		t.Errorf("expected my-api-token to be redacted, got %s", out)
	}
	if !strings.Contains(out, `"app":"foo"`) {
		t.Errorf("expected non-sensitive annotation 'app' to pass through, got %s", out)
	}
}

// TestRedact_ImagePullSecrets verifies imagePullSecrets entries keep their
// shape but the name is masked.
func TestRedact_ImagePullSecrets(t *testing.T) {
	in := `{"imagePullSecrets":[{"name":"regcred"},{"name":"other","extra":1}]}`
	got := Redact(in)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got)
	}
	arr, ok := parsed["imagePullSecrets"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expected array of length 2, got %T %v", parsed["imagePullSecrets"], parsed["imagePullSecrets"])
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatal("expected first element to be a map")
	}
	if first["name"] != "[REDACTED]" {
		t.Errorf("expected name to be redacted, got %v", first["name"])
	}
	second, ok := arr[1].(map[string]any)
	if !ok {
		t.Fatal("expected second element to be a map")
	}
	if second["name"] != "[REDACTED]" {
		t.Errorf("expected second name to be redacted, got %v", second["name"])
	}
	if second["extra"] != float64(1) {
		t.Errorf("expected extra to pass through, got %v", second["extra"])
	}
}

// TestRedact_EnvValueFrom verifies that valueFrom is also redacted when the
// env name is sensitive — the secret name in valueFrom.secretKeyRef would
// otherwise leak through.
func TestRedact_EnvValueFrom(t *testing.T) {
	in := `{"env":[{"name":"DB_PASSWORD","valueFrom":{"secretKeyRef":{"name":"db-secret","key":"password"}}}]}`
	got := Redact(in)
	if strings.Contains(got, "db-secret") {
		t.Errorf("expected valueFrom to be redacted (no 'db-secret' leak), got %s", got)
	}
	if !strings.Contains(got, `"valueFrom":"[REDACTED]"`) {
		t.Errorf("expected valueFrom to be replaced, got %s", got)
	}
}

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"token", true},
		{"secret", true},
		{"password", true},
		{"api-key", true},
		{"api_key", true},
		{"apikey", true},
		{"auth-token", true},
		{"credential", true},
		{"my_token", true},
		{"DB_PASSWORD", true},
		{"my-api-token", true},
		// Whole-word boundaries: substring matches no longer trigger.
		{"app-name", false},
		{"version", false},
		{"apiVersion", false},
		{"keyName", false}, // 'key' is at word start but followed by alphanumeric — not a separator
		{"monkey", false},
		{"keynote", false},
		{"authority", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := isSensitiveKey(tt.key)
			if result != tt.expected {
				t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, result, tt.expected)
			}
		})
	}
}
