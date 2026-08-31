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
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

func newTestScheduler(t *testing.T, leaderElect bool) *scheduler.Scheduler {
	t.Helper()
	previous := config.LeaderElect
	config.LeaderElect = leaderElect
	s := scheduler.NewScheduler()
	config.LeaderElect = previous
	t.Cleanup(s.Stop)
	return s
}

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

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler(w, req, nil)

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

func TestReadyzRouteNotLeader(t *testing.T) {
	// Force NewScheduler to use real leader manager (no observed lease => IsLeader() == false)
	origLeaderElect := config.LeaderElect
	config.LeaderElect = true
	t.Cleanup(func() {
		config.LeaderElect = origLeaderElect
	})

	s := scheduler.NewScheduler()

	handler := ReadyzRoute(s)
	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()

	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("Expected status 200 for readyz (not leader), got %d", w.Code)
	}
}

func TestCheckBodyNil(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	if checkBody(w, req) {
		t.Error("expected checkBody to reject a nil body")
	}

	if w.Code != 400 {
		t.Errorf("Expected status 400 for nil body, got %d", w.Code)
	}
}

func TestCheckBody_ValidBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", strings.NewReader("some-body"))
	w := httptest.NewRecorder()

	valid := checkBody(w, req)

	if !valid {
		t.Error("Expected checkBody to return true for valid body, got false")
	}
	if w.Code != 200 {
		t.Errorf("Expected status 200 (default) for valid body, got %d", w.Code)
	}
}

func TestPredicateRoute_NilBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/predicate", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{}
	handler := PredicateRoute(s)
	handler(w, req, nil)

	if w.Code != 400 {
		t.Errorf("expected 400 for nil body, got %d", w.Code)
	}
}

func TestBind_NilBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/bind", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{}
	handler := Bind(s)

	// This should not panic
	handler(w, req, nil)

	if w.Code != 400 {
		t.Errorf("Expected status 400 for nil body in Bind, got %d", w.Code)
	}
}

func TestPredicateRoute_DecodeError(t *testing.T) {
	req := httptest.NewRequest("POST", "/predicate", strings.NewReader("{not-json"))
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{}
	handler := PredicateRoute(s)
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("expected 200 (error reported in body, not status), got %d", w.Code)
	}

	var result extenderv1.ExtenderFilterResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if result.Error == "" {
		t.Error("expected a decode error to be reported in the filter result")
	}
}

func TestPredicateRoute_NilPod(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "pod field omitted", body: `{"NodeNames":["node1"]}`},
		{name: "pod field explicitly null", body: `{"Pod":null,"NodeNames":["node1"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/predicate", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			s := &scheduler.Scheduler{}
			handler := PredicateRoute(s)

			// Must not panic when Pod is missing/null.
			handler(w, req, nil)

			if w.Code != 200 {
				t.Errorf("expected 200 (error reported in body, not status), got %d", w.Code)
			}

			var result extenderv1.ExtenderFilterResult
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}
			if result.Error == "" {
				t.Error("expected a missing-pod error to be reported in the filter result")
			}
		})
	}
}

func TestPredicateRoute_CacheNotSynced(t *testing.T) {
	args := extenderv1.ExtenderArgs{Pod: &corev1.Pod{}}
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}

	req := httptest.NewRequest("POST", "/predicate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s := newTestScheduler(t, false)
	handler := PredicateRoute(s)
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result extenderv1.ExtenderFilterResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !strings.Contains(result.Error, "scheduler cache is not synced") {
		t.Errorf("expected cache-not-synced error, got %q", result.Error)
	}
}

func TestPredicateRoute_NotLeaderFailsFast(t *testing.T) {
	args := extenderv1.ExtenderArgs{Pod: &corev1.Pod{}}
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}

	req := httptest.NewRequest("POST", "/predicate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler := PredicateRoute(newTestScheduler(t, true))
	handler(w, req, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 with in-band error, got %d", w.Code)
	}
	var result extenderv1.ExtenderFilterResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !strings.Contains(result.Error, "scheduler is not leader") {
		t.Errorf("expected not-leader error, got %q", result.Error)
	}
}

func TestBind_DecodeError(t *testing.T) {
	req := httptest.NewRequest("POST", "/bind", strings.NewReader("{not-json"))
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{}
	handler := Bind(s)
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("expected 200 (error reported in body, not status), got %d", w.Code)
	}

	var result extenderv1.ExtenderBindingResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if result.Error == "" {
		t.Error("expected a decode error to be reported in the bind result")
	}
}

func TestBindRouteRejectsUnavailableScheduler(t *testing.T) {
	body, err := json.Marshal(extenderv1.ExtenderBindingArgs{
		PodName:      "pod",
		PodNamespace: "default",
		Node:         "node",
	})
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}

	tests := []struct {
		name        string
		leaderElect bool
		wantError   string
	}{
		{name: "not leader", leaderElect: true, wantError: "scheduler is not leader"},
		{name: "cache not synced", leaderElect: false, wantError: "scheduler cache is not synced"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/bind", bytes.NewReader(body))
			w := httptest.NewRecorder()
			handler := Bind(newTestScheduler(t, tt.leaderElect))
			handler(w, req, nil)

			if w.Code != http.StatusOK {
				t.Fatalf("expected HTTP 200 with in-band error, got %d", w.Code)
			}
			var result extenderv1.ExtenderBindingResult
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}
			if !strings.Contains(result.Error, tt.wantError) {
				t.Errorf("expected %q, got %q", tt.wantError, result.Error)
			}
		})
	}
}

type errorResponseWriter struct {
	http.ResponseWriter
}

func (w *errorResponseWriter) Write(b []byte) (int, error) {
	return 0, errors.New("mock write error")
}
func TestPredicateRoute_WriteError(t *testing.T) {
	var buf bytes.Buffer
	klog.SetOutput(&buf)
	klog.LogToStderr(false)
	defer func() {
		klog.SetOutput(os.Stderr)
		klog.LogToStderr(true)
	}()
	req := httptest.NewRequest("POST", "/predicate", strings.NewReader("{not-json"))
	w := httptest.NewRecorder()
	ew := &errorResponseWriter{ResponseWriter: w}
	s := &scheduler.Scheduler{}
	handler := PredicateRoute(s)
	handler(ew, req, nil)
	klog.Flush()
	if !strings.Contains(buf.String(), "Failed to write response") {
		t.Errorf("Expected 'Failed to write response' in log output, but got: %s", buf.String())
	}
}
func TestBind_WriteError(t *testing.T) {
	var buf bytes.Buffer
	klog.SetOutput(&buf)
	klog.LogToStderr(false)
	defer func() {
		klog.SetOutput(os.Stderr)
		klog.LogToStderr(true)
	}()
	req := httptest.NewRequest("POST", "/bind", strings.NewReader("{not-json"))
	w := httptest.NewRecorder()
	ew := &errorResponseWriter{ResponseWriter: w}
	s := &scheduler.Scheduler{}
	handler := Bind(s)
	handler(ew, req, nil)
	klog.Flush()
	if !strings.Contains(buf.String(), "Failed to write response") {
		t.Errorf("Expected 'Failed to write response' in log output, but got: %s", buf.String())
	}
}
