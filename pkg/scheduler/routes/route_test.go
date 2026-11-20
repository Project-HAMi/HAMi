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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so WaitForCacheSync fails fast instead of polling forever

	req := httptest.NewRequest("POST", "/predicate", bytes.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{} // zero value: synced defaults to false
	handler := PredicateRoute(s)
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var result extenderv1.ExtenderFilterResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if !strings.Contains(result.Error, "context cancelled") {
		t.Errorf("expected 'context cancelled' error, got %q", result.Error)
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

func TestMaxRequestSizePreempt(t *testing.T) {
	hugePayload := strings.Repeat(" ", maxRequestSize+100)
	req := httptest.NewRequest("POST", "/preempt", strings.NewReader(hugePayload))
	w := httptest.NewRecorder()
	s := &scheduler.Scheduler{}
	handler := PreemptRoute(s)

	handler(w, req, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an oversized body, got %d", w.Code)
	}
	if respBody := w.Body.String(); !strings.Contains(respBody, "EOF") {
		t.Errorf("LimitReader failed in PreemptRoute. Response: %s", respBody)
	}
}

func TestPreemptRoute_NilBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/preempt", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{}
	handler := PreemptRoute(s)

	// This should not panic
	handler(w, req, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nil body, got %d", w.Code)
	}
}

func TestPreemptRoute_DecodeError(t *testing.T) {
	req := httptest.NewRequest("POST", "/preempt", strings.NewReader("{not-json"))
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{}
	handler := PreemptRoute(s)
	handler(w, req, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for an undecodable body, got %d", w.Code)
	}
}

func TestPreemptRoute_NilPod(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "pod field omitted", body: `{"NodeNameToMetaVictims":{"node1":{"Pods":[]}}}`},
		{name: "pod field explicitly null", body: `{"Pod":null,"NodeNameToMetaVictims":{"node1":{"Pods":[]}}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/preempt", strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			s := &scheduler.Scheduler{}
			handler := PreemptRoute(s)

			// Must not panic when Pod is missing/null.
			handler(w, req, nil)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for a request with no pod, got %d", w.Code)
			}
			if respBody := w.Body.String(); !strings.Contains(respBody, "missing pod") {
				t.Errorf("expected a missing-pod error in the response, got %q", respBody)
			}
		})
	}
}

func TestPreemptRoute_CacheNotSynced(t *testing.T) {
	args := extenderv1.ExtenderPreemptionArgs{Pod: &corev1.Pod{}}
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so WaitForCacheSync fails fast instead of polling forever

	req := httptest.NewRequest("POST", "/preempt", bytes.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	s := &scheduler.Scheduler{} // zero value: synced defaults to false
	handler := PreemptRoute(s)
	handler(w, req, nil)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 while the cache is not synced, got %d", w.Code)
	}
	if respBody := w.Body.String(); !strings.Contains(respBody, "context cancelled") {
		t.Errorf("expected 'context cancelled' error, got %q", respBody)
	}
}

// syncedScheduler returns a scheduler that has finished its first registration
// pass, which is what WaitForCacheSync waits for.
func syncedScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()

	previousClient := client.KubeClient
	client.KubeClient = fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	t.Cleanup(func() { client.KubeClient = previousClient })
	previousLeaderElect := config.LeaderElect
	config.LeaderElect = false
	t.Cleanup(func() { config.LeaderElect = previousLeaderElect })

	s := scheduler.NewScheduler()
	if err := s.Start(); err != nil {
		t.Fatalf("failed to start the scheduler: %v", err)
	}
	// The node above is announced while the informers sync, and that
	// notification starts the first registration pass.
	registered := make(chan struct{})
	go func() {
		defer close(registered)
		s.RegisterFromNodeAnnotations()
	}()
	t.Cleanup(func() {
		s.Stop()
		<-registered
	})
	return s
}

func TestPreemptRoute_ReturnsCandidates(t *testing.T) {
	s := syncedScheduler(t)

	const request = `{"Pod":{"metadata":{"name":"high","namespace":"default","uid":"preemptor-uid"}},` +
		`"NodeNameToMetaVictims":{"node-1":{"Pods":[{"UID":"victim-a"}],"NumPDBViolations":1}}}`
	const want = `{"NodeNameToMetaVictims":{"node-1":{"Pods":[{"UID":"victim-a"}],"NumPDBViolations":1}}}`

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	req := httptest.NewRequest("POST", "/preempt", strings.NewReader(request)).WithContext(ctx)
	w := httptest.NewRecorder()

	handler := PreemptRoute(s)
	handler(w, req, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != want {
		t.Errorf("unexpected answer:\n got: %s\nwant: %s", got, want)
	}
	if contentType := w.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("expected a JSON answer, got Content-Type %q", contentType)
	}
}
