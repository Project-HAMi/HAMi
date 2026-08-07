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

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"token", "secret", "password", "passwd", "credential", "cred", "auth",
		"api_key", "api-key", "apikey", "key",
		"my-api-token", "AUTH_TOKEN", "db.password",
		// camelCase must be caught via the boundary normalizer.
		"authToken", "clientSecret", "accessKeyId", "apiToken",
	}
	for _, k := range sensitive {
		if !isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = false, want true", k)
		}
	}

	notSensitive := []string{
		"apiVersion", "keyName", "credentialPolicy", "authority",
		"name", "namespace", "image", "replicas", "nodeName",
	}
	for _, k := range notSensitive {
		if isSensitiveKey(k) {
			t.Errorf("isSensitiveKey(%q) = true, want false", k)
		}
	}
}

func TestRedact_JSONObject(t *testing.T) {
	in := `{"name":"pod-1","authToken":"super-secret","nested":{"apiVersion":"v1","password":"hunter2"}}`
	out := Redact(in)

	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("Redact produced invalid JSON: %v\nout=%s", err, out)
	}
	if doc["authToken"] != redactedPlaceholder {
		t.Errorf("authToken = %v, want %q", doc["authToken"], redactedPlaceholder)
	}
	if doc["name"] != "pod-1" {
		t.Errorf("name was modified: %v", doc["name"])
	}
	nested, ok := doc["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested field missing or wrong type: %v", doc["nested"])
	}
	if nested["password"] != redactedPlaceholder {
		t.Errorf("nested.password = %v, want %q", nested["password"], redactedPlaceholder)
	}
	if nested["apiVersion"] != "v1" {
		t.Errorf("nested.apiVersion was modified: %v", nested["apiVersion"])
	}
}

func TestRedact_JSONArray(t *testing.T) {
	in := `[{"token":"abc"},{"name":"ok"}]`
	out := Redact(in)

	var doc []map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("Redact produced invalid JSON: %v", err)
	}
	if doc[0]["token"] != redactedPlaceholder {
		t.Errorf("doc[0].token = %v, want %q", doc[0]["token"], redactedPlaceholder)
	}
	if doc[1]["name"] != "ok" {
		t.Errorf("doc[1].name was modified: %v", doc[1]["name"])
	}
}

func TestRedact_MalformedJSONFallsBackToBlob(t *testing.T) {
	in := "api-token: should-be-redacted\nreplicas: 3"
	out := Redact(in)
	if strings.Contains(out, "should-be-redacted") {
		t.Errorf("expected blob fallback to redact api-token, got: %s", out)
	}
	if !strings.Contains(out, "replicas: 3") {
		t.Errorf("expected non-sensitive line preserved, got: %s", out)
	}
}

func TestRedactBlob(t *testing.T) {
	in := "policy: binpack\napi-token: should-be-redacted-by-mcp\n- password=hunter2\nplain: value"
	out := RedactBlob(in)

	if strings.Contains(out, "should-be-redacted-by-mcp") {
		t.Errorf("api-token value leaked: %s", out)
	}
	if strings.Contains(out, "hunter2") {
		t.Errorf("password value leaked: %s", out)
	}
	if !strings.Contains(out, "policy: binpack") {
		t.Errorf("non-sensitive key/value was altered: %s", out)
	}
	if !strings.Contains(out, "plain: value") {
		t.Errorf("non-sensitive key/value was altered: %s", out)
	}
}

func TestRedactAnnotations(t *testing.T) {
	in := map[string]string{
		"my-api-token":                 "super-secret",
		"hami.io/node-nvidia-register": "[]",
		"nodeName":                     "gpu-node-1",
	}
	out := RedactAnnotations(in)

	if out["my-api-token"] != redactedPlaceholder {
		t.Errorf("my-api-token = %v, want %q", out["my-api-token"], redactedPlaceholder)
	}
	if out["hami.io/node-nvidia-register"] != "[]" {
		t.Errorf("unrelated annotation was modified: %v", out["hami.io/node-nvidia-register"])
	}
	if out["nodeName"] != "gpu-node-1" {
		t.Errorf("unrelated annotation was modified: %v", out["nodeName"])
	}

	if RedactAnnotations(nil) != nil {
		t.Errorf("RedactAnnotations(nil) should return nil")
	}
}
