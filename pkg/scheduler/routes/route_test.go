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
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/scheduler"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

func setUnexportedField(target interface{}, fieldName string, value interface{}) {
	rv := reflect.ValueOf(target).Elem()
	field := rv.FieldByName(fieldName)
	if !field.IsValid() {
		panic(fmt.Sprintf("no such field %q on %T", fieldName, target))
	}
	addr := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	addr.Set(reflect.ValueOf(value))
}

func setSynced(s *scheduler.Scheduler, synced bool) {
	setUnexportedField(s, "synced", synced)
}

func setPodLister(s *scheduler.Scheduler, pl listerscorev1.PodLister) {
	setUnexportedField(s, "podLister", pl)
}

// fakePodLister/fakePodNamespaceLister let us simulate "pod not found" from
// Scheduler.Bind without wiring up a full informer + fake clientset.
type fakePodLister struct{}

func (fakePodLister) List(_ labels.Selector) ([]*corev1.Pod, error) { return nil, nil }
func (fakePodLister) Pods(_ string) listerscorev1.PodNamespaceLister {
	return fakePodNamespaceLister{}
}

type fakePodNamespaceLister struct{}

func (fakePodNamespaceLister) List(_ labels.Selector) ([]*corev1.Pod, error) { return nil, nil }
func (fakePodNamespaceLister) Get(name string) (*corev1.Pod, error) {
	return nil, fmt.Errorf("pod %q not found", name)
}

// ---------------------------------------------------------------------------
// Existing tests (unchanged)
// ---------------------------------------------------------------------------

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

// TestPredicateRoute_CacheNotSynced exercises the "cache not synced" branch:
// a valid, decodable request but a context that is already cancelled, so
// WaitForCacheSync returns false quickly instead of blocking.
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

// TestPredicateRoute_FilterSuccess exercises the successful decode + synced +
// Filter() + marshal path end-to-end. A pod with no device resource requests
// takes the early-return branch inside Scheduler.Filter, which needs no
// podLister/nodeLister/kubeClient, so only "synced" has to be forced to true.
func TestPredicateRoute_FilterSuccess(t *testing.T) {
	s := scheduler.NewScheduler()
	setSynced(s, true)

	nodeNames := []string{"node1"}
	args := extenderv1.ExtenderArgs{
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "c", Image: "nginx"}},
			},
		},
		NodeNames: &nodeNames,
	}
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}

	req := httptest.NewRequest("POST", "/predicate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler := PredicateRoute(s)
	handler(w, req, nil)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var result extenderv1.ExtenderFilterResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Error != "" {
		t.Errorf("expected no error from Filter, got %q", result.Error)
	}

	if result.NodeNames == nil {
		t.Fatal("expected NodeNames to be non-nil in successful filter result")
	}
	if !reflect.DeepEqual(*result.NodeNames, nodeNames) {
		t.Errorf("expected NodeNames %v, got %v", nodeNames, *result.NodeNames)
	}
}

// TestBind_DecodeError exercises Bind's JSON-decode-failure branch with a
// well-formed (small) request, complementing the existing oversized-payload
// EOF test.
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

// TestBind_PodNotFound exercises Scheduler.Bind's "pod not found" error path,
// which flows back through routes.Bind's success-marshal branch with a
// populated Error field. It only requires a fake podLister, since that error
// return happens before anything else (node lookup, locking, kube client)
// is touched.
func TestBind_PodNotFound(t *testing.T) {
	s := scheduler.NewScheduler()
	setPodLister(s, fakePodLister{})

	args := extenderv1.ExtenderBindingArgs{
		PodName:      "missing-pod",
		PodNamespace: "default",
		Node:         "node1",
	}
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}

	req := httptest.NewRequest("POST", "/bind", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler := Bind(s)
	handler(w, req, nil)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var result extenderv1.ExtenderBindingResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if result.Error == "" {
		t.Error("expected Bind to report a pod-not-found error")
	}
}
