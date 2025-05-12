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
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type Client struct {
	// Embedded kubernetes.Interface to avoid name conflicts.
	kubernetes.Interface
	config *rest.Config
}

var (
	KubeClient kubernetes.Interface
	once       sync.Once
)

func init() {
	KubeClient = nil
}

// GetClient returns the global Kubernetes client.
func GetClient() kubernetes.Interface {
	return KubeClient
}

// NewClient creates a new Kubernetes client with the given options.
func NewClient(opts ...Option) (*Client, error) {
	restConfig, err := loadKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Apply WithDefaults option first to set default values.
	WithDefaults()(restConfig)

	// Then apply user-provided options that will override defaults if specified.
	for _, opt := range opts {
		opt(restConfig)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &Client{
		Interface: clientset,
		config:    restConfig,
	}, nil
}

// InitGlobalClient initializes the global Kubernetes client with the given options.
func InitGlobalClient(opts ...Option) {
	once.Do(func() {
		client, err := NewClient(opts...)
		if err != nil {
			klog.Fatalf("Failed to initialize global client: %v", err)
		}
		KubeClient = client.Interface
	})
}

// loadKubeConfig loads Kubernetes configuration from the environment or in-cluster.
func loadKubeConfig() (*rest.Config, error) {
	kubeConfigPath := os.Getenv("KUBECONFIG")
	if kubeConfigPath == "" {
		kubeConfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		klog.Infof("BuildConfigFromFlags failed for file %s: %v. Using in-cluster config.", kubeConfigPath, err)
		return rest.InClusterConfig()
	}
	return config, nil
}
