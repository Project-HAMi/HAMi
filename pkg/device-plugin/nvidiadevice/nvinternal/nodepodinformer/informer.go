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

// Package nodepodinformer owns the device-plugin process's node-scoped Pod
// informer. Consumers receive snapshots through List instead of depending on
// Kubernetes lister types.
package nodepodinformer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// ErrNotSynced is returned until the initial Pod list has populated the local
// informer cache. Callers must fail closed while this error is returned.
var ErrNotSynced = errors.New("node Pod informer cache is not synced")

// Informer is the concrete, process-local node Pod cache used by the NVIDIA
// device plugin. It intentionally does not expose the underlying Kubernetes
// informer or lister.
type Informer struct {
	factory   informers.SharedInformerFactory
	informer  cache.SharedIndexInformer
	lister    corelisters.PodLister
	startOnce sync.Once
	client    kubernetes.Interface
	nodeName  string
}

// New constructs a node-scoped Pod informer. Start must be called separately;
// this keeps construction non-blocking and lets process lifecycle own startup.
func New(kubeClient kubernetes.Interface, nodeName string) (*Informer, error) {
	if kubeClient == nil {
		return nil, errors.New("Kubernetes client is not initialized")
	}
	if nodeName == "" {
		return nil, errors.New("node name is empty")
	}

	factory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		0,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = fmt.Sprintf("spec.nodeName=%s", nodeName)
		}),
	)
	pods := factory.Core().V1().Pods()
	return &Informer{
		factory:  factory,
		client:   kubeClient,
		nodeName: nodeName,
		informer: pods.Informer(),
		lister:   pods.Lister(),
	}, nil
}

// Start starts the informer at most once and returns immediately. Call
// WaitForSync before starting consumers that require an initial Pod snapshot.
func (i *Informer) Start(ctx context.Context) {
	i.startOnce.Do(func() {
		i.factory.Start(ctx.Done())
	})
}

// WaitForSync waits for the initial Pod list or returns the context error.
// Start must be called first, and callers should provide a bounded context.
// Successful initial sync does not guarantee freshness; destructive consumers
// must still use ListFresh to confirm Pod state with the API server.
func (i *Informer) WaitForSync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cache.WaitForCacheSync(ctx.Done(), i.informer.HasSynced) {
		return fmt.Errorf("initial Pod cache sync failed: %w", ctx.Err())
	}
	return ctx.Err()
}

// List returns the Pods currently held in the local node-scoped cache. The Pod
// pointers are read-only and must not be mutated by callers. HasSynced only
// covers initial synchronization; absence here does not authorize deletion.
func (i *Informer) List() ([]*corev1.Pod, error) {
	if !i.informer.HasSynced() {
		return nil, ErrNotSynced
	}
	pods, err := i.lister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list node Pods from informer cache: %w", err)
	}
	return pods, nil
}

// ListFresh confirms node Pod state with the API server before destructive
// cleanup. It never falls back to the informer cache when the request fails.
// The initial-sync gate is retained, but is not treated as a freshness proof.
func (i *Informer) ListFresh(ctx context.Context) ([]*corev1.Pod, error) {
	if !i.informer.HasSynced() {
		return nil, ErrNotSynced
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pods, err := i.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + i.nodeName,
		// An unset resourceVersion requests Most Recent semantics. Do not use
		// "0", which permits an arbitrarily stale API-server cache result.
	})
	if err != nil {
		return nil, fmt.Errorf("confirm node Pods with API server: %w", err)
	}
	result := make([]*corev1.Pod, 0, len(pods.Items))
	for index := range pods.Items {
		result = append(result, &pods.Items[index])
	}
	return result, nil
}
