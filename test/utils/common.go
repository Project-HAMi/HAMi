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

package utils

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var kubeConfig string

func init() {
	flag.StringVar(&kubeConfig, "kubeconfig", "", "Path to the kubeConfig file")
}

// resolveKubeConfigPath picks kubeconfig in order: --kubeconfig flag, KUBE_CONF, ~/.kube/config.
func resolveKubeConfigPath() string {
	if kubeConfig != "" {
		return validateKubeConfigPath(kubeConfig)
	}
	if configPath := os.Getenv("KUBE_CONF"); configPath != "" {
		kubeConfig = validateKubeConfigPath(configPath)
		return kubeConfig
	}
	home, err := os.UserHomeDir()
	if err == nil {
		defaultPath := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(defaultPath); err == nil {
			kubeConfig = defaultPath
			return kubeConfig
		}
	}
	klog.Fatalf("kubeconfig not set: pass --kubeconfig, set KUBE_CONF, or place config at ~/.kube/config")
	return ""
}

func validateKubeConfigPath(configPath string) string {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		klog.Fatalf("Kubeconfig file does not exist at path: %s", configPath)
	}
	return configPath
}

func DefaultKubeConfigPath() string {
	return resolveKubeConfigPath()
}

func GetClientSet() *kubernetes.Clientset {
	config, err := clientcmd.BuildConfigFromFlags("", resolveKubeConfigPath())
	if err != nil {
		klog.Fatalf("Failed to load kubeConfig: %v", err)
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}
	return clientSet
}

func GetRandom() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	random := strconv.Itoa(r.Intn(9999))
	return random
}

// KubectlExecInPod executes a shell command in a specified Pod using kubectl exec.
func KubectlExecInPod(namespace, podName, command string) ([]byte, error) {
	// Wait for the container to stabilize
	time.Sleep(30 * time.Second)

	// Build the kubectl exec command
	cmd := exec.Command("kubectl", "exec", "-n", namespace, podName, "--", "/bin/bash", "-c", command)

	// Capture the command output (both stdout and stderr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("failed to execute kubectl command: %w. Output: %s", err, output)
	}

	return output, nil
}
