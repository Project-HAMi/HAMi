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
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
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

func TestMaxRequestSizePrioritize(t *testing.T) {
	hugePayload := `{"Pod":{"metadata":{"name":"` + strings.Repeat("a", maxRequestSize+100) + `"}}}`
	req := httptest.NewRequest("POST", "/prioritize", strings.NewReader(hugePayload))
	w := httptest.NewRecorder()
	handler := PrioritizeRoute(&scheduler.Scheduler{})

	handler(w, req, nil)

	if w.Code != 400 {
		t.Fatalf("expected invalid oversized JSON to return 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unexpected EOF") {
		t.Fatalf("expected truncation error, got %q", w.Body.String())
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

	checkBody(w, req)

	if w.Code != 400 {
		t.Errorf("Expected status 400 for nil body, got %d", w.Code)
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

func TestPrioritizeRoute_DecodeError(t *testing.T) {
	req := httptest.NewRequest("POST", "/prioritize", strings.NewReader("{not-json"))
	w := httptest.NewRecorder()

	handler := PrioritizeRoute(&scheduler.Scheduler{})
	handler(w, req, nil)

	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid prioritize request, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid character") {
		t.Fatalf("expected decode error in response, got %q", w.Body.String())
	}
}

func TestPrioritizeRoute_MissingPod(t *testing.T) {
	req := httptest.NewRequest("POST", "/prioritize", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	handler := PrioritizeRoute(&scheduler.Scheduler{})
	handler(w, req, nil)

	if w.Code != 400 {
		t.Fatalf("expected 400 for request without a pod, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "extender args must contain a pod") {
		t.Fatalf("expected missing pod error in response, got %q", w.Body.String())
	}
}

func TestPrioritizeRoute_CacheNotSynced(t *testing.T) {
	prioritizeCalled := false
	handler := prioritizeRoute(
		func(context.Context) bool { return false },
		func(extenderv1.ExtenderArgs) (*extenderv1.HostPriorityList, error) {
			prioritizeCalled = true
			return nil, nil
		},
	)
	req := httptest.NewRequest("POST", "/prioritize", strings.NewReader(`{"Pod":{"metadata":{"name":"test"}}}`))
	w := httptest.NewRecorder()

	handler(w, req, nil)

	if w.Code != 503 {
		t.Fatalf("expected 503 while cache is not synced, got %d", w.Code)
	}
	if prioritizeCalled {
		t.Fatal("prioritize should not be called before cache sync")
	}
}

func TestPrioritizeRoute_Success(t *testing.T) {
	want := extenderv1.HostPriorityList{{Host: "node-a", Score: 7}}
	handler := prioritizeRoute(
		func(context.Context) bool { return true },
		func(args extenderv1.ExtenderArgs) (*extenderv1.HostPriorityList, error) {
			if args.Pod == nil || args.Pod.Name != "test" {
				t.Fatalf("unexpected pod in prioritize request: %#v", args.Pod)
			}
			return &want, nil
		},
	)
	req := httptest.NewRequest("POST", "/prioritize", strings.NewReader(`{"Pod":{"metadata":{"name":"test"}}}`))
	w := httptest.NewRecorder()

	handler(w, req, nil)

	if w.Code != 200 {
		t.Fatalf("expected 200 for successful prioritize request, got %d: %s", w.Code, w.Body.String())
	}
	var got extenderv1.HostPriorityList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode prioritize response: %v", err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("unexpected prioritize response: %#v", got)
	}
}

func TestPrioritizeRoute_SchedulerError(t *testing.T) {
	handler := prioritizeRoute(
		func(context.Context) bool { return true },
		func(extenderv1.ExtenderArgs) (*extenderv1.HostPriorityList, error) {
			return nil, errors.New("scoring failed")
		},
	)
	req := httptest.NewRequest("POST", "/prioritize", strings.NewReader(`{"Pod":{"metadata":{"name":"test"}}}`))
	w := httptest.NewRecorder()

	handler(w, req, nil)

	if w.Code != 500 {
		t.Fatalf("expected 500 for prioritize failure, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "scoring failed") {
		t.Fatalf("expected scoring error in response, got %q", w.Body.String())
	}
}
