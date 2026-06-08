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

package resources

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

func TestConfigResource_Resource(t *testing.T) {
	r := NewConfigResource(client.NewK8sClientFromInterface(fake.NewClientset()))
	def := r.Resource()
	if def.URI != "hami://config/scheduler" {
		t.Errorf("unexpected URI: %s", def.URI)
	}
	if def.MIMEType != "application/json" {
		t.Errorf("unexpected MIMEType: %s", def.MIMEType)
	}
}

func TestConfigResource_Handler_Success(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "hami-scheduler-config", Namespace: "hami-system"},
		Data: map[string]string{
			"scheduler-config.yaml": "{policy: binpack}",
			"api-token":             "should-be-redacted",
		},
	}
	cs := fake.NewClientset(cm)
	r := NewConfigResource(client.NewK8sClientFromInterface(cs))

	res, err := r.Handler()(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "hami://config/scheduler"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(res.Contents))
	}
	c := res.Contents[0]
	if c.URI != "hami://config/scheduler" || c.MIMEType != "application/json" {
		t.Errorf("unexpected metadata: %+v", c)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(c.Text), &data); err != nil {
		t.Fatalf("invalid JSON: %v\nbody=%s", err, c.Text)
	}
	if data["scheduler-config.yaml"] != "{policy: binpack}" {
		t.Errorf("expected config preserved, got %v", data["scheduler-config.yaml"])
	}
	// Sensitive key should be redacted by redact.Redact
	if v, ok := data["api-token"].(string); !ok || !strings.Contains(v, "REDACTED") {
		t.Errorf("expected api-token to be redacted, got %v", data["api-token"])
	}
}

func TestConfigResource_Handler_Missing(t *testing.T) {
	cs := fake.NewClientset()
	r := NewConfigResource(client.NewK8sClientFromInterface(cs))

	if _, err := r.Handler()(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "hami://config/scheduler"},
	}); err == nil {
		t.Errorf("expected error when configmap is missing")
	}
}
