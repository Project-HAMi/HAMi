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
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var kubeConfig string

func init() {
	flag.StringVar(&kubeConfig, "kubeconfig", defaultKubeConfigPath(), "Path to the kubeConfig file")
}

func defaultKubeConfigPath() string {
	configPath := os.Getenv("KUBE_CONF")
	if configPath == "" {
		klog.Fatalf("Environment variable KUBE_CONF is not set or empty. Please set it to a valid kubeconfig file path.")
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		klog.Fatalf("Kubeconfig file does not exist at path: %s", configPath)
	}
	return configPath
}

func DefaultKubeConfigPath() string {
	configPath := os.Getenv("KUBE_CONF")
	if configPath == "" {
		klog.Fatalf("Environment variable KUBE_CONF is not set or empty. Please set it to a valid kubeconfig file path.")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		klog.Fatalf("lalala Kubeconfig file does not exist at path: %s, error is %s", configPath, err)
	}
	return configPath
}

func GetClientSet() *kubernetes.Clientset {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
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

func KubectlExecInPod(namespace, podName, cmdshell string) ([]byte, error) {
	time.Sleep(30 * time.Second)
	cmd := exec.Command("kubectl", "exec", "-it", "-n", namespace, podName, "--", "/bin/bash", "-c", cmdshell)
	output, err := cmd.Output()
	if err != nil {
		return output, err
	}

	return output, nil
}
