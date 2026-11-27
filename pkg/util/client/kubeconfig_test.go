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

package client

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const kubeconfigContext = `apiVersion: v1
kind: Config
current-context: selected
contexts:
- name: selected
  context:
    cluster: selected
    user: selected
`

const kubeconfigResources = `apiVersion: v1
kind: Config
clusters:
- name: selected
  cluster:
    server: https://selected.example.com
users:
- name: selected
  user:
    token: synthetic-token
`

func writeTestKubeconfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	assert.NilError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func stubInClusterConfig(t *testing.T, config *rest.Config, err error) *int {
	t.Helper()
	previous := inClusterConfig
	calls := 0
	inClusterConfig = func() (*rest.Config, error) {
		calls++
		return config, err
	}
	t.Cleanup(func() { inClusterConfig = previous })
	return &calls
}

func TestNewClientKubeconfigFiles(t *testing.T) {
	contextPath := writeTestKubeconfig(t, kubeconfigContext)
	resourcesPath := writeTestKubeconfig(t, kubeconfigResources)
	completePath := writeTestKubeconfig(t, kubeconfigContext+strings.TrimPrefix(kubeconfigResources, "apiVersion: v1\nkind: Config\n"))
	otherPath := writeTestKubeconfig(t, strings.ReplaceAll(kubeconfigResources, "selected.example.com", "other.example.com"))
	missingPath := filepath.Join(t.TempDir(), "missing.yaml")

	for _, tt := range []struct {
		name  string
		paths []string
		host  string
	}{
		{name: "single file", paths: []string{completePath}, host: "https://selected.example.com"},
		{name: "merge complementary files", paths: []string{contextPath, resourcesPath}, host: "https://selected.example.com"},
		{name: "first file wins", paths: []string{contextPath, resourcesPath, otherPath}, host: "https://selected.example.com"},
		{name: "reverse conflicting files", paths: []string{contextPath, otherPath, resourcesPath}, host: "https://other.example.com"},
		{name: "missing entry", paths: []string{missingPath, completePath}, host: "https://selected.example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUBECONFIG", strings.Join(tt.paths, string(os.PathListSeparator)))
			calls := stubInClusterConfig(t, nil, errors.New("unexpected in-cluster fallback"))
			c, err := NewClient()
			assert.NilError(t, err)
			assert.Equal(t, c.config.Host, tt.host)
			assert.Equal(t, c.config.BearerToken, "synthetic-token")
			assert.Equal(t, *calls, 0)
		})
	}
}

func TestLoadKubeconfigRelativePaths(t *testing.T) {
	contextPath := writeTestKubeconfig(t, kubeconfigContext)
	resourcesPath := writeTestKubeconfig(t, strings.Replace(kubeconfigResources,
		"    server:", "    certificate-authority: ca.crt\n    server:", 1))
	assert.NilError(t, os.WriteFile(filepath.Join(filepath.Dir(resourcesPath), "ca.crt"), []byte("synthetic CA"), 0600))
	t.Setenv("KUBECONFIG", strings.Join([]string{contextPath, resourcesPath}, string(os.PathListSeparator)))
	calls := stubInClusterConfig(t, nil, errors.New("unexpected in-cluster fallback"))
	config, err := LoadKubeConfig()
	assert.NilError(t, err)
	assert.Equal(t, config.CAFile, filepath.Join(filepath.Dir(resourcesPath), "ca.crt"))
	assert.Equal(t, *calls, 0)
}

func TestLoadKubeconfigHome(t *testing.T) {
	path := writeTestKubeconfig(t, kubeconfigContext+strings.TrimPrefix(kubeconfigResources, "apiVersion: v1\nkind: Config\n"))
	t.Setenv("KUBECONFIG", "")
	// client-go computes this path during package initialization, before t.Setenv.
	previous := clientcmd.RecommendedHomeFile
	clientcmd.RecommendedHomeFile = path
	t.Cleanup(func() { clientcmd.RecommendedHomeFile = previous })
	calls := stubInClusterConfig(t, nil, errors.New("unexpected in-cluster fallback"))
	config, err := LoadKubeConfig()
	assert.NilError(t, err)
	assert.Equal(t, config.Host, "https://selected.example.com")
	assert.Equal(t, *calls, 0)
}

func TestNewClientKubeconfigFallback(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing.yaml")
	malformedPath := writeTestKubeconfig(t, "clusters: [")
	unusablePath := writeTestKubeconfig(t, kubeconfigContext)
	emptyPath := writeTestKubeconfig(t, "apiVersion: v1\nkind: Config\n")
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "missing", path: missingPath},
		{name: "malformed", path: malformedPath},
		{name: "unusable context", path: unusablePath},
		{name: "empty", path: emptyPath},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUBECONFIG", tt.path)
			t.Run("fallback succeeds", func(t *testing.T) {
				calls := stubInClusterConfig(t, &rest.Config{Host: "https://in-cluster.example.com"}, nil)
				c, err := NewClient()
				assert.NilError(t, err)
				assert.Equal(t, c.config.Host, "https://in-cluster.example.com")
				assert.Equal(t, *calls, 1)
			})
			t.Run("fallback fails", func(t *testing.T) {
				fallbackErr := errors.New("in-cluster configuration unavailable")
				calls := stubInClusterConfig(t, nil, fallbackErr)
				_, err := NewClient()
				assert.Assert(t, errors.Is(err, fallbackErr))
				assert.ErrorContains(t, err, "failed to load kubeconfig")
				assert.Equal(t, *calls, 1)
			})
		})
	}
}

func TestNewClientKubeconfigFallbackPreservesErrors(t *testing.T) {
	kubeconfigErr := errors.New("invalid kubeconfig context")
	fallbackErr := errors.New("in-cluster configuration unavailable")
	previous := fileConfig
	fileConfig = func() (*rest.Config, error) { return nil, kubeconfigErr }
	t.Cleanup(func() { fileConfig = previous })
	calls := stubInClusterConfig(t, nil, fallbackErr)
	_, err := NewClient()
	assert.Assert(t, errors.Is(err, kubeconfigErr))
	assert.Assert(t, errors.Is(err, fallbackErr))
	assert.ErrorContains(t, err, kubeconfigErr.Error())
	assert.ErrorContains(t, err, fallbackErr.Error())
	assert.Equal(t, *calls, 1)
}
