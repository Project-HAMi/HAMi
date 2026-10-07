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
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	versionmetrics "github.com/Project-HAMi/HAMi/pkg/metrics"
	"github.com/Project-HAMi/HAMi/pkg/monitor/amd"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	vendorNVIDIA = "nvidia"
	vendorAMD    = "amd"
)

// startAMD serves the AMD metrics of this node. There is no libvgpu cache to
// map and no MIG to follow, so it needs only the node's pods and its node object.
func startAMD() error {
	nodeName := os.Getenv(util.NodeNameEnvName)
	if nodeName == "" {
		return fmt.Errorf("env %s not set", util.NodeNameEnvName)
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to build clientset: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Bind before the informer starts: a taken port never heals, so fail fast.
	listener, err := net.Listen("tcp", metricsBindAddress)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", metricsBindAddress, err)
	}
	defer listener.Close()

	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 5*time.Minute,
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.FieldSelector = "spec.nodeName=" + nodeName }))
	podLister := factory.Core().V1().Pods().Lister()
	factory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), factory.Core().V1().Pods().Informer().HasSynced) {
		return fmt.Errorf("failed to sync pod informer cache")
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(versionmetrics.NewBuildInfoCollector())
	reg.MustRegister(&amd.Collector{
		NodeName:   nodeName,
		CgroupRoot: amd.DefaultCgroupRoot,
		DRMRoot:    amd.DefaultDRMRoot,
		Pods: func() ([]*corev1.Pod, error) {
			return podLister.List(labels.Everything())
		},
		Node: func() (*corev1.Node, error) {
			return clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		},
	})
	klog.Infof("Serving AMD metrics for node %s on %s", nodeName, metricsBindAddress)
	return serveMetrics(ctx, listener, reg)
}
