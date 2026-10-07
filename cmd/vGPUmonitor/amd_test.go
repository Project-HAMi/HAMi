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

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

func withVendor(t *testing.T, v string) {
	t.Helper()
	original := vendor
	vendor = v
	t.Cleanup(func() { vendor = original })
}

func TestStartRejectsAnUnknownVendor(t *testing.T) {
	withVendor(t, "intel")
	require.ErrorContains(t, start(), `unsupported --vendor "intel"`)
}

func TestStartAMDNeedsTheNodeName(t *testing.T) {
	withVendor(t, vendorAMD)
	t.Setenv(util.NodeNameEnvName, "")
	require.ErrorContains(t, start(), util.NodeNameEnvName)
}

func TestStartAMDNeedsAUsableKubeconfig(t *testing.T) {
	withVendor(t, vendorAMD)
	t.Setenv(util.NodeNameEnvName, "gpu-1")
	t.Setenv("KUBECONFIG", "/nonexistent/kubeconfig")
	require.ErrorContains(t, start(), "kubeconfig")
}

// serveMetrics is shared by both vendors: it must serve /metrics and stop when
// its context ends.
func TestServeMetricsServesAndStops(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	original := metricsBindAddress
	metricsBindAddress = listener.Addr().String()
	t.Cleanup(func() { metricsBindAddress = original })

	reg := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "amd_test_gauge", Help: "h"})
	gauge.Set(7)
	reg.MustRegister(gauge)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveMetrics(ctx, listener, reg) }()

	resp, err := http.Get("http://" + listener.Addr().String() + "/metrics")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.True(t, strings.Contains(string(body), "amd_test_gauge 7"))

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serveMetrics did not stop after its context ended")
	}
}
