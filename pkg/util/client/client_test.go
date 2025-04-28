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

package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Mock functions for testing.
var (
	buildConfigFromFlags = clientcmd.BuildConfigFromFlags
	inClusterConfig      = rest.InClusterConfig
)

// TestGetClient tests the GetClient function.
func TestGetClient(t *testing.T) {
	InitGlobalClient()
	tests := []struct {
		name           string
		kubeConfig     string
		buildConfig    *rest.Config
		buildConfigErr error
		inCluster      *rest.Config
		inClusterErr   error
		expectError    bool
	}{
		{
			name:           "Success from kubeconfig",
			kubeConfig:     filepath.Join("testdata", "kubeconfig.yaml"),
			buildConfig:    &rest.Config{Host: "https://example.com"},
			buildConfigErr: nil,
			inCluster:      nil,
			inClusterErr:   nil,
			expectError:    false,
		},
		{
			name:           "Fallback to in-cluster config",
			kubeConfig:     filepath.Join("testdata", "invalid_kubeconfig.yaml"),
			buildConfig:    nil,
			buildConfigErr: errors.New("kubeconfig error"),
			inCluster:      &rest.Config{Host: "https://in-cluster.example.com"},
			inClusterErr:   nil,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock the clientcmd.BuildConfigFromFlags function.
			oldBuildConfigFromFlags := buildConfigFromFlags
			buildConfigFromFlags = func(masterUrl, kubeconfigPath string) (*rest.Config, error) {
				return tt.buildConfig, tt.buildConfigErr
			}
			defer func() { buildConfigFromFlags = oldBuildConfigFromFlags }()

			// Mock the rest.InClusterConfig function.
			oldInClusterConfig := inClusterConfig
			inClusterConfig = func() (*rest.Config, error) {
				return tt.inCluster, tt.inClusterErr
			}
			defer func() { inClusterConfig = oldInClusterConfig }()

			// Set the KUBECONFIG environment variable.
			oldKubeConfig := os.Getenv("KUBECONFIG")
			os.Setenv("KUBECONFIG", tt.kubeConfig)
			defer os.Setenv("KUBECONFIG", oldKubeConfig)

			// Call GetClient and check the result.
			client := GetClient()
			if tt.expectError {
				if client != nil {
					t.Errorf("Expected error, but got a valid client")
				}
			} else {
				if client == nil {
					t.Errorf("Expected a valid client, but got nil")
				}
			}
		})
	}
}

// TestClientWithOptions tests client initialization with options.
func TestClientWithOptions(t *testing.T) {
	KubeClient = nil
	once = sync.Once{}

	timeout := 1
	client, _ := NewClient(WithTimeout(timeout))

	assert.Equal(t, client.config.Timeout, time.Duration(timeout)*time.Second)
	assert.Equal(t, client.config.QPS, DefaultQPS)
	assert.Equal(t, client.config.Burst, DefaultBurst)

	KubeClient = nil
	once = sync.Once{}

	qps := float32(50.0)
	client, _ = NewClient(WithQPS(qps))

	assert.Equal(t, client.config.Timeout, time.Duration(DefaultTimeout)*time.Second)
	assert.Equal(t, client.config.QPS, qps)
	assert.Equal(t, client.config.Burst, DefaultBurst)

	KubeClient = nil
	once = sync.Once{}
	burst := 100
	client, _ = NewClient(WithBurst(burst))

	assert.Equal(t, client.config.Timeout, time.Duration(DefaultTimeout)*time.Second)
	assert.Equal(t, client.config.QPS, DefaultQPS)
	assert.Equal(t, client.config.Burst, burst)

	KubeClient = nil
	once = sync.Once{}
	timeout = 2
	qps = 0.5
	burst = 100
	client, _ = NewClient(WithTimeout(timeout), WithQPS(qps), WithBurst(burst))

	assert.Equal(t, client.config.Timeout, time.Duration(timeout)*time.Second)
	assert.Equal(t, client.config.QPS, qps)
	assert.Equal(t, client.config.Burst, burst)
}

// TestClientRealNodePerformance tests the performance with a real Kubernetes cluster if available.
func TestClientRealNodePerformance(t *testing.T) {

	skipRealClusterTest := true
	// Skip this test by default as it requires a real Kubernetes cluster.
	if skipRealClusterTest == true {
		t.Skip("Skipping real cluster test. Set TEST_WITH_REAL_CLUSTER=true to run this test.")
	}

	tests := []struct {
		name    string
		qps     float32
		burst   int
		updates int
		timeout int
	}{
		{
			name:    "Real Cluster - Low QPS and Burst",
			qps:     1,
			burst:   1,
			updates: 10,
			timeout: 1,
		},
		{
			name:    "Real Cluster - Standard Timeout",
			qps:     5,
			burst:   10,
			updates: 10,
			timeout: 5,
		},
		{
			name:    "Real Cluster - High Timeout",
			qps:     10,
			burst:   20,
			updates: 15,
			timeout: 10,
		},
		{
			name:    "Real Cluster - Very Short Timeout",
			qps:     5,
			burst:   5,
			updates: 5,
			timeout: 1,
		},
	}

	labelKey := "test-performance-label"
	var nodeName string

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(WithQPS(tt.qps), WithBurst(tt.burst), WithTimeout(tt.timeout))
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			if nodeName == "" {
				nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					t.Fatalf("Failed to list nodes: %v", err)
				}
				if len(nodes.Items) == 0 {
					t.Fatal("No nodes found in the cluster")
				}
				nodeName = nodes.Items[0].Name
				t.Logf("Using node %s for testing", nodeName)
			}
			start := time.Now()
			for i := 0; i < tt.updates; i++ {
				labelValue := fmt.Sprintf("perf-test-value-%d", i)
				node, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Failed to get node: %v", err)
				}
				if node.Labels == nil {
					node.Labels = make(map[string]string)
				}
				node.Labels[labelKey] = labelValue
				_, err = client.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("Failed to update node: %v", err)
				}
			}

			elapsed := time.Since(start)

			node, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get node during cleanup: %v", err)
			}
			delete(node.Labels, labelKey)
			_, err = client.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("Failed to cleanup test label: %v", err)
			}

			opsPerSecond := float64(tt.updates) / elapsed.Seconds()

			t.Logf("Real cluster performance test results for %s:", tt.name)
			t.Logf("  - QPS: %.1f, Burst: %d", tt.qps, tt.burst)
			t.Logf("  - Updates performed: %d", tt.updates)
			t.Logf("  - Total time: %v", elapsed)
			t.Logf("  - Operations per second: %.2f", opsPerSecond)
		})
	}
}
