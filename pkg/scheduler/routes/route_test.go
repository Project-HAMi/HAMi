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

package routes

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/scheduler"
)

func TestMaxRequestSize(t *testing.T) {
	hugePayload := strings.Repeat(" ", maxRequestSize+100)

	req := httptest.NewRequest("POST", "/predicate", strings.NewReader(hugePayload))
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{}
	handler := PredicateRoute(s)
	handler(w, req, nil)
	respBody := w.Body.String()

	if !strings.Contains(respBody, "EOF") && !strings.Contains(respBody, "unexpected EOF") {
		t.Errorf("LimitReader failed to trigger EOF. Response body: %s", respBody)
	} else {
		t.Logf("Success! Caught expected error: %s", respBody)
	}
}
func TestHealthzRoute(t *testing.T) {
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler := HealthzRoute()
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("Expected status 200 for health check, got %d", w.Code)
	}
}

func TestMaxRequestSizeBind(t *testing.T) {
	hugePayload := strings.Repeat(" ", maxRequestSize+100)
	req := httptest.NewRequest("POST", "/bind", strings.NewReader(hugePayload))
	w := httptest.NewRecorder()
	s := &scheduler.Scheduler{}
	handler := Bind(s)

	handler(w, req, nil)

	respBody := w.Body.String()
	if !strings.Contains(respBody, "EOF") && !strings.Contains(respBody, "unexpected EOF") {
		t.Errorf("LimitReader failed in Bind. Response: %s", respBody)
	}
}

func TestWebHookRoute(t *testing.T) {
	handler := WebHookRoute()
	if handler == nil {
		t.Fatal("WebHookRoute returned nil handler")
	}

	// Send a request to the webhook handler - even an invalid request exercises the handler path
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req, nil)

	// The webhook handler should respond (any status is fine - we're testing the route layer)
	if w.Code == 0 {
		t.Error("Expected a non-zero status code from webhook handler")
	}
}

func TestReadyzRouteLeader(t *testing.T) {
	// NewScheduler initializes with DummyLeaderManager(true) by default
	s := scheduler.NewScheduler()

	handler := ReadyzRoute(s)
	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()

	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("Expected status 200 for readyz (leader), got %d", w.Code)
	}
}

func TestCheckBodyNil(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	checkBody(w, req)

	if w.Code != 400 {
		t.Errorf("Expected status 400 for nil body, got %d", w.Code)
	}
}
