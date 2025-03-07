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

var (
	KubeClient kubernetes.Interface
	once       sync.Once
)

func init() {
	KubeClient = nil
}

func GetClient() kubernetes.Interface {
	return KubeClient
}

// Client is a kubernetes client.
type Client struct {
	Client kubernetes.Interface
	QPS    float32
	Burst  int
}

// WithQPS sets the QPS of the client.
func WithQPS(qps float32) func(*Client) {
	return func(c *Client) {
		c.QPS = qps
	}
}

func WithBurst(burst int) func(*Client) {
	return func(c *Client) {
		c.Burst = burst
	}
}

// NewClientWithConfig creates a new client with a given config.
func NewClientWithConfig(config *rest.Config, opts ...func(*Client)) (*Client, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	c := &Client{
		Client: client,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// NewClient creates a new client.
func NewClient(ops ...func(*Client)) (*Client, error) {
	kubeConfigPath := os.Getenv("KUBECONFIG")
	if kubeConfigPath == "" {
		kubeConfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		klog.Infof("BuildConfigFromFlags failed for file %s: %v. Using in-cluster config.", kubeConfigPath, err)
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
	}
	c, err := NewClientWithConfig(config, ops...)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return c, err
}

// InitGlobalClient creates a new global client.
func InitGlobalClient(ops ...func(*Client)) {
	c, err := NewClient(ops...)
	if err != nil {
		klog.Fatalf("new client error %s", err.Error())
	}
	KubeClient = c.Client
}
