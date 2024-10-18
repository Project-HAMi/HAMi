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

package lister

import (
	"math/rand"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

type PodLister struct {
	cache.Indexer
	listerv1.PodLister
}

func NewPodLister(indexer cache.Indexer) PodLister {
	return PodLister{
		PodLister: listerv1.NewPodLister(indexer),
		Indexer:   indexer,
	}
}

func (p *PodLister) GetByIndex(indexerKey, indexedValue string) ([]*corev1.Pod, error) {
	objs, err := p.ByIndex(indexerKey, indexedValue)
	if err != nil {
		return nil, err
	}
	pods := make([]*corev1.Pod, 0, len(objs))
	for _, obj := range objs {
		if pod, ok := obj.(*corev1.Pod); ok {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}

// resyncPeriod computes the time interval a shared informer waits before resyncing with the api server .
func resyncPeriod(minResyncPeriod time.Duration) time.Duration {
	factor := rand.Float64() + 1
	return time.Duration(float64(minResyncPeriod.Nanoseconds()) * factor)
}

const PodIndexerKey = ".spec.nodeName"

func NewPodInformer(clientSet *kubernetes.Clientset) cache.SharedIndexInformer {
	lw := cache.NewListWatchFromClient(clientSet.CoreV1().RESTClient(),
		"pods", corev1.NamespaceAll, fields.Everything())
	// Resulting resync period will be between 12 and 24 hours, like the default for k8s .
	resync := resyncPeriod(12 * time.Hour)
	podInformer := cache.NewSharedIndexInformer(lw, &corev1.Pod{}, resync, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		PodIndexerKey: func(obj interface{}) ([]string, error) {
			var indexValues []string
			if pod, ok := obj.(*corev1.Pod); ok {
				indexValues = append(indexValues, pod.Spec.NodeName)
			}
			return indexValues, nil
		},
	})
	// Trimming managed fields to reduce memory usage .
	_ = podInformer.SetTransform(func(in any) (any, error) {
		if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
			obj.SetManagedFields(nil)
		}
		return in, nil
	})
	return podInformer
}
