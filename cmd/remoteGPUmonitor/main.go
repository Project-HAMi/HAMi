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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	remote "github.com/Project-HAMi/HAMi/pkg/device/remotegpu"
	"github.com/Project-HAMi/HAMi/pkg/monitor/remotegpu"
	"github.com/Project-HAMi/HAMi/pkg/util"
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
	return serve(ctx, client, node, source, bind)
}

func serve(ctx context.Context, client kubernetes.Interface, node, source, bind string) error {
	factory := informers.NewSharedInformerFactoryWithOptions(client, 0, informers.WithTweakListOptions(func(options *metav1.ListOptions) {
		options.LabelSelector = remote.RemoteGPUPodLabel + "=true"
	}))
	pods := factory.Core().V1().Pods()
	informer := pods.Informer()
	if err := informer.SetTransform(func(obj any) (any, error) {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			return obj, nil
		}
		return trimPod(pod), nil
	}); err != nil {
		return err
	}
	factory.Start(ctx.Done())
	registry := prometheus.NewRegistry()
	prometheus.WrapRegistererWith(prometheus.Labels{"zone": "vGPU"}, registry).MustRegister(&remotegpu.Collector{Node: node, URL: source, Pods: func() ([]*corev1.Pod, error) {
		if !informer.HasSynced() {
			return nil, fmt.Errorf("pod cache not synchronized")
		}
		return pods.Lister().List(labels.Everything())
	}})
	server := &http.Server{Addr: bind, Handler: newServeMux(informer.HasSynced, registry), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	// Start HTTP before synchronization so readiness can report startup progress.
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func newServeMux(ready func() bool, registry *prometheus.Registry) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready == nil || !ready() {
			http.Error(w, "Pod cache not synchronized", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// trimPod keeps the fields the collector reads. The informer still receives
// every labeled Pod; this only shrinks the local cache.
func trimPod(pod *corev1.Pod) *corev1.Pod {
	out := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pod.Name,
			Namespace:       pod.Namespace,
			UID:             pod.UID,
			ResourceVersion: pod.ResourceVersion,
			Annotations:     map[string]string{},
		},
		Spec:   corev1.PodSpec{NodeName: pod.Spec.NodeName},
		Status: corev1.PodStatus{Phase: pod.Status.Phase},
	}
	if v := pod.Annotations[util.AssignedNodeAnnotations]; v != "" {
		out.Annotations[util.AssignedNodeAnnotations] = v
	}
	if v := pod.Annotations[remote.AllocatedAnnos]; v != "" {
		out.Annotations[remote.AllocatedAnnos] = v
	}
	out.Spec.InitContainers = trimContainers(pod.Spec.InitContainers)
	out.Spec.Containers = trimContainers(pod.Spec.Containers)
	for _, st := range pod.Status.InitContainerStatuses {
		trimmed := corev1.ContainerStatus{Name: st.Name}
		if st.State.Terminated != nil {
			trimmed.State.Terminated = &corev1.ContainerStateTerminated{ExitCode: st.State.Terminated.ExitCode}
		}
		out.Status.InitContainerStatuses = append(out.Status.InitContainerStatuses, trimmed)
	}
	return out
}

func trimContainers(in []corev1.Container) []corev1.Container {
	if len(in) == 0 {
		return nil
	}
	out := make([]corev1.Container, len(in))
	for i, c := range in {
		out[i] = corev1.Container{Name: c.Name, RestartPolicy: c.RestartPolicy}
		for _, env := range c.Env {
			if env.Name == "CUDA_DEVICE_MEMORY_LIMIT" {
				out[i].Env = append(out[i].Env, corev1.EnvVar{Name: env.Name, Value: env.Value})
			}
		}
	}
	return out
}
