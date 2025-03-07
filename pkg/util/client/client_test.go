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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// MockClientConfig is a mock implementation of clientcmd.ClientConfig.
type MockClientConfig struct {
	config *rest.Config
	err    error
}

func (m *MockClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, nil
}

func (m *MockClientConfig) ClientConfig() (*rest.Config, error) {
	return m.config, m.err
}

func (m *MockClientConfig) Namespace() (string, bool, error) {
	return "", false, nil
}

func (m *MockClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}

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

// Mock functions for testing.
var (
	buildConfigFromFlags = clientcmd.BuildConfigFromFlags
	inClusterConfig      = rest.InClusterConfig
)
