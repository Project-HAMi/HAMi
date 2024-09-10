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

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/lister"
	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"k8s.io/klog/v2"
)

//var addr = flag.String("listen-address", ":9394", "The address to listen on for HTTP requests.")

//const shared_directory = "/usr/local/vgpu/shared"

func main() {

	if err := ValidateEnvVars(); err != nil {
		klog.Fatalf("Failed to validate environment variables: %v", err)
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		klog.Fatalf("Failed to build kubeconfig: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to build clientset: %v", err)
	}

	cgroupDriver = 0
	stopChan := make(chan struct{})
	//go serveInfo(errchannel)

	podInformer := lister.NewPodInformer(clientset)
	go podInformer.Run(stopChan)
	ctx, _ := context.WithTimeout(context.Background(), 2*time.Minute)
	if !cache.WaitForCacheSync(ctx.Done(), podInformer.HasSynced) {
		klog.Fatalf("Timed out waiting for caches to sync.")
	}
	podLister := lister.NewPodLister(podInformer.GetIndexer())
	containerLister, err := nvidia.NewContainerLister(podLister)
	if err != nil {
		klog.Fatalf("Failed to create container lister: %v", err)
	}
	go watchAndFeedback(containerLister, stopChan)
	go initMetrics(podLister, containerLister, stopChan)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	select {
	case <-stopChan:
		klog.Fatalf("Service terminated abnormally")
	case s := <-sigChan:
		klog.Infof("Received signal %v, shutting down.", s)
	}
}
