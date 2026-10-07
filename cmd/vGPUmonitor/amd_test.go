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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

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
	require.Contains(t, string(body), "amd_test_gauge 7")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serveMetrics did not stop after its context ended")
	}
}

// serveAMD must wire the node's pods and registration into the collector: the
// device memory of a card the node registered shows up on /metrics.
func TestServeAMDServesTheRegisteredCard(t *testing.T) {
	const bdf = "0000:06:00.0"
	drm := t.TempDir()
	card := filepath.Join(drm, "card1", "device")
	require.NoError(t, os.MkdirAll(card, 0o755))
	for name, content := range map[string]string{
		"uevent":             "DRIVER=amdgpu\nPCI_SLOT_NAME=" + bdf + "\n",
		"mem_info_vram_used": "1024\n",
		"gpu_busy_percent":   "5\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(card, name), []byte(content), 0o644))
	}
	clientset := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "gpu-1",
		Annotations: map[string]string{
			"hami.io/node-amd-register": `[{"id":"uuid-1","index":0,"type":"AMD","custominfo":{"pciBDF":"` + bdf + `"}}]`,
		},
	}})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	original := metricsBindAddress
	metricsBindAddress = listener.Addr().String()
	t.Cleanup(func() { metricsBindAddress = original })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveAMD(ctx, clientset, "gpu-1", listener, t.TempDir(), drm) }()

	resp, err := http.Get("http://" + listener.Addr().String() + "/metrics")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Contains(t, string(body), `hami_host_gpu_memory_used_bytes{device_index="0",device_type="AMD",device_uuid="uuid-1",node="gpu-1"} 1024`)
	require.Contains(t, string(body), `hami_vgpumonitor_collect_success{node="gpu-1"} 1`)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("serveAMD did not stop after its context ended")
	}
}
