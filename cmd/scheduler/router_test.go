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
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/klog/v2"
)

func TestProfilingNonLoopbackWarning(t *testing.T) {
	state := klog.CaptureState()
	t.Cleanup(state.Restore)
	var logs bytes.Buffer
	klog.LogToStderr(false)
	klog.SetOutput(&logs)
	for _, tt := range []struct {
		address string
		warn    bool
	}{
		{"127.0.0.1:0", false},
		{"0.0.0.0:0", true},
	} {
		logs.Reset()
		listener, err := listenProfiling(true, tt.address)
		if err != nil {
			t.Fatal(err)
		}
		listener.Close()
		klog.Flush()
		if warned := strings.Contains(logs.String(), "is not a loopback address"); warned != tt.warn {
			t.Errorf("listenProfiling(%q) warning = %v, want %v", tt.address, warned, tt.warn)
		}
	}
}

func TestProfilingDefaults(t *testing.T) {
	for name, want := range map[string]string{"profiling": "false", "profiling-bind-address": "127.0.0.1:6060"} {
		if got := rootCmd.Flags().Lookup(name).DefValue; got != want {
			t.Errorf("%s default = %q, want %q", name, got, want)
		}
	}
}

func TestProfilingRouteIsolation(t *testing.T) {
	previous := enableProfiling
	enableProfiling = true
	t.Cleanup(func() { enableProfiling = previous })
	for name, router := range map[string]http.Handler{"cluster": clusterRouter(nil), "extender": extenderRouter(nil)} {
		for _, path := range []string{"/debug/pprof/", "/debug/pprof/goroutine", "/debug/pprof/heap", "/debug/pprof/cmdline", "/debug/pprof/symbol", "/debug/pprof/profile", "/debug/pprof/trace"} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s router serves %s: status %d", name, path, rec.Code)
			}
		}
	}

	mux := profilingMux()
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/goroutine", "/debug/pprof/heap", "/debug/pprof/cmdline", "/debug/pprof/symbol", "/debug/pprof/profile?seconds=1", "/debug/pprof/trace?seconds=0.01"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("profiling route %s: status %d, body length %d", path, rec.Code, rec.Body.Len())
		}
	}
	for _, path := range []string{"/", "/healthz", "/readyz", "/metrics", "/webhook", "/refit", "/filter", "/bind", "/debug/pprof/unknown"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("profiling mux serves %s %s: status %d", method, path, rec.Code)
			}
		}
	}
}

func TestListenProfiling(t *testing.T) {
	listener, err := listenProfiling(true, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().String()
	for _, addr := range []string{address, "invalid address"} {
		if disabled, err := listenProfiling(false, addr); disabled != nil || err != nil {
			t.Fatalf("disabled profiling tried to listen on %q: %v, %v", addr, disabled, err)
		}
	}
	duplicate, err := listenProfiling(true, address)
	if duplicate != nil {
		duplicate.Close()
		t.Fatal("profiling unexpectedly bound an occupied port")
	}
	var networkError *net.OpError
	if !errors.As(err, &networkError) || !strings.Contains(err.Error(), "profiling address") {
		t.Fatalf("expected wrapped profiling listener error, got %v", err)
	}
}

// Malformed on purpose: every handler rejects it while decoding, before it
// reaches the scheduler, so routing can be asserted against a nil one.
const undecodableBody = "{"

// The extender verbs authenticate no caller, so reaching them must require
// being inside this pod's network namespace. Putting either back on the
// router the Service publishes hands every pod in the cluster the ability to
// drop another pod's GPU reservation and to patch arbitrary pod annotations.
//
// Reproduce on a cluster (issue #3030): before this fix the extender served
// both verbs on 0.0.0.0, routed through the Service, with no NetworkPolicy by
// default. From any pod in another namespace:
//
//	curl -sk -X POST https://hami-scheduler.kube-system.svc/bind \
//	  -H 'Content-Type: application/json' \
//	  -d '{"PodName":"does-not-exist","PodNamespace":"team-b","PodUID":"<victim-uid>","Node":"gpu-node-1"}'
//
// reached the scheduler and forged a reservation drop; see #3040's tests for
// what that request then did once it arrived. This test only asserts the
// reachability half: that the router the Service exposes never carries these
// two paths in the first place, so that request has nowhere to land.
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

func TestServeAnswersOnTheGivenListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open a listener: %v", err)
	}
	defer listener.Close()

	served := make(chan error, 1)
	go func() { served <- serve(listener, clusterRouter(nil), nil) }()

	resp, err := http.Get("http://" + listener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz returned %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Closing the listener ends Serve, which is how the caller learns a
	// listener stopped accepting.
	listener.Close()
	select {
	case err := <-served:
		if err == nil {
			t.Error("serve returned no error after its listener closed")
		}
	case <-time.After(5 * time.Second):
		t.Error("serve did not return after its listener closed")
	}
}
