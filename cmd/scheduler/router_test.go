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

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Malformed on purpose: every handler rejects it while decoding, before it
// reaches the scheduler, so routing can be asserted against a nil one.
const undecodableBody = "{"

// The extender verbs authenticate no caller, so reaching them must require
// being inside this pod's network namespace. Putting either back on the
// router the Service publishes hands every pod in the cluster the ability to
// drop another pod's GPU reservation and to patch arbitrary pod annotations.
func TestClusterRouterDoesNotServeExtenderVerbs(t *testing.T) {
	router := clusterRouter(nil)

	for _, path := range []string{"/filter", "/bind"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(undecodableBody)))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s is served on the cluster-facing router (status %d), it must only be on the extender router", path, rec.Code)
		}
	}
}

// The complement: the extender router carries the verbs and nothing else, so
// a caller that reaches loopback cannot use it to talk to /refit or /webhook.
func TestExtenderRouterServesOnlyExtenderVerbs(t *testing.T) {
	router := extenderRouter(nil)

	for _, path := range []string{"/filter", "/bind"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(undecodableBody)))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not served on the extender router, kube-scheduler cannot reach it", path)
		}
	}

	for _, path := range []string{"/refit", "/webhook"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(undecodableBody)))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s is served on the extender router (status %d), its callers are outside this pod", path, rec.Code)
		}
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:9444", true},
		{"127.0.0.2:9444", true},
		{"localhost:9444", true},
		{"[::1]:9444", true},
		{"0.0.0.0:9444", false},
		{"[::]:9444", false},
		{":9444", false},
		{"10.0.0.5:9444", false},
		{"127.0.0.1", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := isLoopbackAddr(tt.addr); got != tt.want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}
