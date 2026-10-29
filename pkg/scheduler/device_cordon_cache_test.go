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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// These tests check that hami.io/device-cordon reaches Fit() when it changes
// after the node's GPUs were registered.
//
// Fit() reads the annotation from the Node stored in the scheduler's own node
// cache. register() replaces that Node only when the vendor's CheckHealth()
// reports needUpdate, and for NVIDIA that happens only when the allocatable GPU
// count or the hami.io/node-nvidia-register value changes. The device plugin
// does not rewrite the register annotation while its device list is unchanged,
// so on a steady node a cordon (or uncordon) may never reach the cache.
//
// The node lister is seeded directly through its indexer and the informer is
// never started, so register() sees exactly the Node the test puts there.

const cordonTestNodeName = "cordon-node"

// cordonTestNode returns a node with two registered NVIDIA GPUs, GPU-0 and
// GPU-1. An empty cordon leaves the annotation off. The register annotation and
// allocatable are the same in every call, as on a node whose GPUs are steady.
func cordonTestNode(cordon string) *corev1.Node {
	var devs string
	for i := range 2 {
		if i > 0 {
			devs += ","
		}
		devs += fmt.Sprintf(`{"id":"GPU-%d","count":10,"devmem":40960,"devcore":100,"type":"NVIDIA","health":true,"mode":"hami-core","numa":0,"index":%d,"devicevendor":"NVIDIA"}`, i, i)
	}
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cordonTestNodeName,
			Annotations: map[string]string{nvidia.RegisterAnnos: "[" + devs + "]"},
		},
	}
	if cordon != "" {
		n.Annotations[common.DeviceCordonAnnotation] = cordon
	}
	n.Status.Allocatable = corev1.ResourceList{"hami.io/gpu": resource.MustParse("2")}
	return n
}

func newCordonTestScheduler(t *testing.T) (*Scheduler, cache.Indexer) {
	t.Helper()

	previousClient := client.KubeClient
	client.KubeClient = fake.NewClientset()
	t.Cleanup(func() { client.KubeClient = previousClient })
	previousDevices := device.DevicesMap
	t.Cleanup(func() { device.DevicesMap = previousDevices })

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
	// Only the NVIDIA backend, so no other vendor reconciles the fake node.
	device.DevicesMap = map[string]device.Devices{
		nvidia.NvidiaGPUDevice: device.GetDevices()[nvidia.NvidiaGPUDevice],
	}
	return s, indexer
}

// fitsOneGPU reports whether a pod asking for one whole GPU fits on the test
// node, and the failure reason when it does not. It runs the same node usage
// and scoring path Filter uses, without recording events.
func fitsOneGPU(t *testing.T, s *Scheduler) (bool, string) {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "cordon-probe", Namespace: "default", UID: "cordon-probe"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "probe",
				Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					"hami.io/gpu": resource.MustParse("1"),
				}},
			}},
		},
	}
	reqs, err := device.Resourcereqs(pod)
	require.NoError(t, err)

	names := []string{cordonTestNodeName}
	usage, _, failed, err := s.getNodesUsage(&names, pod)
	require.NoError(t, err)
	scores, err := s.calcScoreWithOptions(usage, reqs, pod, failed, false, true)
	require.NoError(t, err)
	if len(scores.NodeList) == 1 {
		return true, ""
	}
	return false, failed[cordonTestNodeName]
}

func cachedCordon(t *testing.T, s *Scheduler) string {
	t.Helper()
	info, err := s.GetNode(cordonTestNodeName)
	require.NoError(t, err)
	require.NotNil(t, info.Node)
	return info.Node.Annotations[common.DeviceCordonAnnotation]
}

// A cordon written after the GPUs were registered must stop new allocations on
// the next register pass.
func Test_register_CordonAfterRegistrationReachesFit(t *testing.T) {
	s, indexer := newCordonTestScheduler(t)

	require.NoError(t, indexer.Add(cordonTestNode("")))
	s.register(labels.Everything())
	fit, reason := fitsOneGPU(t, s)
	require.True(t, fit, "before any cordon a one-GPU pod must fit; reason %q", reason)

	// Cordon both GPUs. Only the cordon annotation changes.
	require.NoError(t, indexer.Update(cordonTestNode("GPU-0,GPU-1")))
	s.register(labels.Everything())

	require.Equal(t, "GPU-0,GPU-1", cachedCordon(t, s),
		"the scheduler cache still holds the Node from before the cordon")
	fit, reason = fitsOneGPU(t, s)
	require.False(t, fit, "both GPUs are cordoned, so the pod must not fit")
	require.Contains(t, reason, common.CardCordoned)
}

// Removing a cordon must make the GPUs schedulable again on the next register
// pass, even when the cordon was already present at registration time.
func Test_register_UncordonAfterRegistrationReachesFit(t *testing.T) {
	s, indexer := newCordonTestScheduler(t)

	require.NoError(t, indexer.Add(cordonTestNode("GPU-0,GPU-1")))
	s.register(labels.Everything())
	fit, reason := fitsOneGPU(t, s)
	require.False(t, fit, "both GPUs are cordoned at registration, so the pod must not fit")
	require.Contains(t, reason, common.CardCordoned)

	require.NoError(t, indexer.Update(cordonTestNode("")))
	s.register(labels.Everything())

	require.Empty(t, cachedCordon(t, s),
		"the scheduler cache still holds the Node from before the uncordon")
	fit, reason = fitsOneGPU(t, s)
	require.True(t, fit, "the cordon was removed, so the pod must fit; reason %q", reason)
}
