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
	"context"
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

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "well formed", header: "Bearer abc.def.ghi", want: "abc.def.ghi"},
		{name: "missing", header: "", want: ""},
		{name: "wrong scheme", header: "Basic dXNlcjpwYXNz", want: ""},
		{name: "no token after prefix", header: "Bearer ", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/refit", nil)
			if test.header != "" {
				req.Header.Set("Authorization", test.header)
			}
			if got := bearerToken(req); got != test.want {
				t.Errorf("bearerToken() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNumaRefitRoute_CacheNotSynced(t *testing.T) {
	body, err := json.Marshal(device.NumaRefitRequest{PodUID: "uid"})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so WaitForCacheSync fails fast instead of polling forever

	req := httptest.NewRequest("POST", "/refit", strings.NewReader(string(body))).WithContext(ctx)
	w := httptest.NewRecorder()

	handler := NumaRefit(&scheduler.Scheduler{}) // zero value: synced defaults to false
	handler(w, req, nil)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var response device.NumaRefitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if response.Succeeded || !strings.Contains(response.FailureReason, "context cancelled") {
		t.Errorf("expected cache-not-synced failure, got %+v", response)
	}
}
