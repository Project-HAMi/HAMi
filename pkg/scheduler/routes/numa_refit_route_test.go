/*
Copyright 2026 The HAMi Authors.

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
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler"
)

func TestNumaRefitRoute_NilBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/refit", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	handler := NumaRefit(&scheduler.Scheduler{})
	handler(w, req, nil)

	if w.Code != 400 {
		t.Errorf("expected 400 for nil body, got %d", w.Code)
	}
}

func TestNumaRefitRoute_DecodeError(t *testing.T) {
	req := httptest.NewRequest("POST", "/refit", strings.NewReader("{not-json"))
	w := httptest.NewRecorder()

	handler := NumaRefit(&scheduler.Scheduler{})
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var response device.NumaRefitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if response.Succeeded || response.FailureReason == "" {
		t.Errorf("expected in-band decode failure, got %+v", response)
	}
}

func TestNumaRefitRoute_CacheNotSynced(t *testing.T) {
	body, err := json.Marshal(device.NumaRefitRequest{PodUID: "uid"})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest("POST", "/refit", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	handler := NumaRefit(newTestScheduler(t, false))
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var response device.NumaRefitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if response.Succeeded || !strings.Contains(response.FailureReason, "scheduler cache is not synced") {
		t.Errorf("expected cache-not-synced failure, got %+v", response)
	}
}

func TestNumaRefitRoute_NotLeaderFailsFast(t *testing.T) {
	body, err := json.Marshal(device.NumaRefitRequest{PodUID: "uid"})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest("POST", "/refit", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler := NumaRefit(newTestScheduler(t, true))
	handler(w, req, nil)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200 with in-band error, got %d", w.Code)
	}
	var response device.NumaRefitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if response.Succeeded || !strings.Contains(response.FailureReason, "scheduler is not leader") {
		t.Errorf("expected not-leader failure, got %+v", response)
	}
}
