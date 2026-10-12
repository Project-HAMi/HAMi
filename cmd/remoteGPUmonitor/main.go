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
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/monitor/remotegpu"
)

func main() {
	klog.InitFlags(nil)
	node := flag.String("node", os.Getenv("NODE_NAME"), "GPU server node name")
	source := flag.String("lupine-metrics-url", "http://127.0.0.1:14833/metrics", "Lupine metrics endpoint")
	bind := flag.String("metrics-bind-address", ":31993", "HAMi metrics listen address")
	flag.Parse()
	if err := run(*node, *source, *bind); err != nil {
		klog.ErrorS(err, "remote GPU monitor stopped")
		os.Exit(1)
	}
}
func run(node, source, bind string) error {
	if node == "" {
		return fmt.Errorf("node name is required")
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	factory := informers.NewSharedInformerFactory(client, 0)
	pods := factory.Core().V1().Pods()
	informer := pods.Informer()
	factory.Start(ctx.Done())
	registry := prometheus.NewRegistry()
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": "vGPU"}, registry).MustRegister(&remotegpu.Collector{Node: node, URL: source, Pods: func() ([]*corev1.Pod, error) {
		if !informer.HasSynced() {
			return nil, fmt.Errorf("pod cache not synchronized")
		}
		return pods.Lister().List(labels.Everything())
	}})
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !informer.HasSynced() {
			http.Error(w, "Pod cache not synchronized", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: bind, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	// Start HTTP before synchronization so readiness can report startup progress.
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
