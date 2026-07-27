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

package scheduler

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// Test_register_NodeCacheConcurrency reproduces the s.nodes data race. It models a
// node whose GPU flaps between healthy and unhealthy -- the case HAMi's node health
// check exists for -- so the scheduler keeps re-registering it. register() reads
// s.nodes for that node while the informer delete callback onDelNode -> rmNode
// deletes from the same map under the manager lock. register()'s unlocked read and
// rmNode's locked delete race. Run with -race: it fails on the unsynchronised access
// and passes once the read is removed.
//
// Design notes:
//   - The register annotation is fixed and realistic except for the GPU health flag;
//     a flip changes the annotation, which is what makes CheckHealth report
//     needUpdate and drives register() back into the cache write on every cycle.
//   - Keeping one node name keeps that map key hot, so the read and the delete
//     overlap reliably instead of depending on timing luck.
//   - The lister is seeded directly through its indexer, so the informer is never
//     started and no background goroutine leaks past the test.
//   - onDelNode does extra per-call work, so several delete goroutines are needed to
//     delete often enough to overlap register()'s brief cache access.
//   - Work is bounded by an iteration count rather than a time.Sleep window.
func Test_register_NodeCacheConcurrency(t *testing.T) {
	const nodeName = "gpu-node-0"
	const rounds = 3000
	const deleters = 6

	client.KubeClient = fake.NewClientset()
	t.Cleanup(func() { client.KubeClient = nil })

	s := NewScheduler()
	s.kubeClient = client.KubeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client.KubeClient, time.Hour)
	s.nodeLister = informerFactory.Core().V1().Nodes().Lister()
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	indexer := informerFactory.Core().V1().Nodes().Informer().GetIndexer()

	require.NoError(t, config.InitDevicesWithConfig(&config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultGPUNum:                1,
		},
	}))

	// One GPU node with fixed, realistic hardware (a 40 GiB card). Only the GPU
	// health flag flips, as it would on a node with an intermittently failing GPU.
	mkNode := func(healthy bool) *corev1.Node {
		reg := fmt.Sprintf(`[{"id":"GPU-0","count":10,"devmem":40960,"devcore":100,"type":"NVIDIA","health":%t,"mode":"hami-core","numa":0,"index":0,"devicevendor":"NVIDIA"}]`, healthy)
		n := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Annotations: map[string]string{
					nvidia.RegisterAnnos:     reg,
					"hami.io/node-handshake": "Requesting_2999-01-01 00:00:00",
				},
			},
		}
		n.Status.Allocatable = corev1.ResourceList{"hami.io/gpu": resource.MustParse("1")}
		return n
	}
	require.NoError(t, indexer.Add(mkNode(true)))

	var wg sync.WaitGroup

	// register loop: the flapping health flips the register annotation each round, so
	// CheckHealth reports needUpdate and register() reaches the s.nodes read/write.
	// indexer.Update never errors for the default store and require/FailNow must not
	// run outside the test goroutine, so the error is ignored.
	wg.Go(func() {
		printed := map[string]bool{}
		sel := labels.Everything()
		for v := 1; v <= rounds; v++ {
			_ = indexer.Update(mkNode(v%2 == 0))
			s.register(sel, printed)
		}
	})

	// delete loop(s): the real informer delete callback removes the node from s.nodes.
	for range deleters {
		wg.Go(func() {
			for v := 1; v <= rounds; v++ {
				s.onDelNode(mkNode(true))
			}
		})
	}

	wg.Wait()
}
