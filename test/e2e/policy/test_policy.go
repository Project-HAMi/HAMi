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

package policy

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/test/utils"
)

const (
	policyTestNamespacePrefix = "hami-policy-e2e-"
	policyTestImage           = "nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0"
	policyTestScheduler       = "hami-scheduler"
	policyTestTimeout         = 5 * time.Minute
	policyTestInterval        = 2 * time.Second

	gpuResourceName       = corev1.ResourceName("nvidia.com/gpu")
	gpuCoreResourceName   = corev1.ResourceName("nvidia.com/gpucores")
	gpuMemoryPercentName  = corev1.ResourceName("nvidia.com/gpumem-percentage")
	allocatedDevicesKey   = "hami.io/vgpu-devices-allocated"
	schedulerComponentKey = "app.kubernetes.io/component"
	schedulerComponent    = "hami-scheduler"
	devicePluginComponent = "hami-device-plugin"
	schedulerContainer    = "vgpu-scheduler-extender"
)

type gpuNode struct {
	name string
	gpus []device.DeviceInfo
}

func (n gpuNode) gpu(index int) string {
	return n.gpus[index].ID
}

type policyFixture struct {
	cluster   *policyCluster
	client    kubernetes.Interface
	namespace string
	pods      []*corev1.Pod
}

type policyCluster struct {
	client kubernetes.Interface
}

type policyMetricsTarget struct {
	pod  string
	port string
}

var _ = ginkgo.Describe("[policy] Scheduler policy E2E tests", ginkgo.Ordered, ginkgo.Serial, func() {
	var (
		cluster *policyCluster
		nodes   []gpuNode
	)

	ginkgo.BeforeAll(func() {
		cluster = newPolicyCluster()
		ensureGPUNodeLabeled(cluster.client)

		// The device-plugin may be Ready (probe passes) before it finishes
		// registering GPU devices in the node annotation, and stale node-lock
		// annotations or terminating pods from earlier suites may temporarily
		// cause a node to be filtered out. Poll until at least one node passes
		// all readiness checks so the specs can make informed skip/run decisions.
		gomega.Eventually(func() []gpuNode {
			nodes = registeredGPUNodes(cluster.client)
			return nodes
		}, policyTestTimeout, policyTestInterval).ShouldNot(gomega.BeEmpty(),
			"timed out waiting for at least one node to register hami-core GPUs")
	})

	// This spec exercises the full schedule -> bind -> metrics -> release path
	// using vGPU sharing on a single GPU, so it runs even on a single-GPU node
	// (e.g. the Tesla P4 CI runner) while remaining valid on richer topologies.
	ginkgo.It("shares one GPU across pods and releases capacity on delete", func() {
		selected := requireNodes(nodes, 1, 1, "single-GPU vGPU sharing")
		node := selected[0]
		fixture := newPolicyFixture(cluster)

		ginkgo.By("placing the first vGPU workload on the only GPU")
		first := fixture.createPod("share-first", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0)},
			cores:      30,
			memoryPct:  30,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(first.Spec.NodeName).To(gomega.Equal(node.name))
		gomega.Expect(allocatedGPU(first)).To(gomega.Equal(node.gpu(0)))
		cluster.waitForSchedulerCache(first)

		ginkgo.By("packing a second vGPU workload onto the same shared GPU")
		second := fixture.createPod("share-second", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0)},
			cores:      30,
			memoryPct:  30,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(allocatedGPU(second)).To(gomega.Equal(node.gpu(0)))
		cluster.waitForSchedulerCache(second)

		ginkgo.By("releasing the first workload frees its share from the scheduler cache")
		fixture.deletePod(first)

		ginkgo.By("a replacement workload reuses the freed capacity on the same GPU")
		third := fixture.createPod("share-third", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0)},
			cores:      30,
			memoryPct:  30,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(allocatedGPU(third)).To(gomega.Equal(node.gpu(0)))
		cluster.waitForSchedulerCache(third)
	})

	ginkgo.It("places workloads according to node binpack and spread policies", func() {
		selected := requireNodes(nodes, 2, 1, "node binpack and spread")

		ginkgo.By("creating an unequal node utilization baseline")
		fixture := newPolicyFixture(cluster)
		seed := fixture.createPod("node-seed", podOptions{
			nodes:      []string{selected[0].name},
			gpuUUIDs:   []string{selected[0].gpu(0)},
			cores:      60,
			memoryPct:  40,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(seed.Spec.NodeName).To(gomega.Equal(selected[0].name))
		cluster.waitForSchedulerCache(seed)

		ginkgo.By("asserting node binpack selects the busier node")
		binpack := fixture.createPod("node-binpack", podOptions{
			nodes:      []string{selected[0].name, selected[1].name},
			gpuUUIDs:   []string{selected[0].gpu(0), selected[1].gpu(0)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  util.GPUSchedulerPolicySpread.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(binpack.Spec.NodeName).To(gomega.Equal(selected[0].name))
		cluster.waitForSchedulerCache(binpack)

		ginkgo.By("asserting node spread selects the less utilized node")
		spread := fixture.createPod("node-spread", podOptions{
			nodes:      []string{selected[0].name, selected[1].name},
			gpuUUIDs:   []string{selected[0].gpu(0), selected[1].gpu(0)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  util.GPUSchedulerPolicySpread.String(),
			nodePolicy: util.NodeSchedulerPolicySpread.String(),
		})
		gomega.Expect(spread.Spec.NodeName).To(gomega.Equal(selected[1].name))
	})

	ginkgo.It("places workloads according to GPU binpack and spread policies", func() {
		selected := requireNodes(nodes, 1, 2, "GPU binpack and spread")
		node := selected[0]

		ginkgo.By("creating an unequal GPU utilization baseline")
		fixture := newPolicyFixture(cluster)
		seed := fixture.createPod("gpu-seed", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0)},
			cores:      60,
			memoryPct:  40,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(allocatedGPU(seed)).To(gomega.Equal(node.gpu(0)))
		cluster.waitForSchedulerCache(seed)

		ginkgo.By("asserting GPU binpack selects the busier GPU")
		binpack := fixture.createPod("gpu-binpack", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0), node.gpu(1)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(allocatedGPU(binpack)).To(gomega.Equal(node.gpu(0)))
		cluster.waitForSchedulerCache(binpack)

		ginkgo.By("asserting a comma-separated mutex policy excludes the busy GPU")
		mutex := fixture.createPod("gpu-binpack-mutex", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0), node.gpu(1)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  "binpack,mutex",
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(allocatedGPU(mutex)).To(gomega.Equal(node.gpu(1)))
		cluster.waitForSchedulerCache(mutex)

		ginkgo.By("asserting GPU spread selects the less utilized GPU")
		spread := fixture.createPod("gpu-spread", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0), node.gpu(1)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  util.GPUSchedulerPolicySpread.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(allocatedGPU(spread)).To(gomega.Equal(node.gpu(1)))
	})

	ginkgo.It("honors combined node and GPU policy annotations", func() {
		selected := requireNodes(nodes, 2, 2, "combined node and GPU policies")
		low, high := selected[0], selected[1]
		fixture := newPolicyFixture(cluster)

		ginkgo.By("creating distinct baseline utilization on both nodes")
		lowSeed := fixture.createPod("combined-low", podOptions{
			nodes:      []string{low.name},
			gpuUUIDs:   []string{low.gpu(0)},
			cores:      20,
			memoryPct:  20,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		highSeed := fixture.createPod("combined-high", podOptions{
			nodes:      []string{high.name},
			gpuUUIDs:   []string{high.gpu(0)},
			cores:      60,
			memoryPct:  60,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		cluster.waitForSchedulerCache(lowSeed)
		cluster.waitForSchedulerCache(highSeed)

		ginkgo.By("asserting binpack node and binpack GPU policy chains")
		binpackBinpack := fixture.createPod("combined-binpack-binpack", podOptions{
			nodes:      []string{low.name, high.name},
			gpuUUIDs:   []string{low.gpu(0), low.gpu(1), high.gpu(0), high.gpu(1)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  "binpack,spread",
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(binpackBinpack.Spec.NodeName).To(gomega.Equal(high.name))
		gomega.Expect(allocatedGPU(binpackBinpack)).To(gomega.Equal(high.gpu(0)))
		cluster.waitForSchedulerCache(binpackBinpack)
		fixture.deletePod(binpackBinpack)

		ginkgo.By("asserting binpack node and spread GPU policy chains")
		binpackSpread := fixture.createPod("combined-binpack-spread", podOptions{
			nodes:      []string{low.name, high.name},
			gpuUUIDs:   []string{low.gpu(0), low.gpu(1), high.gpu(0), high.gpu(1)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  "spread,binpack",
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(binpackSpread.Spec.NodeName).To(gomega.Equal(high.name))
		gomega.Expect(allocatedGPU(binpackSpread)).To(gomega.Equal(high.gpu(1)))
		cluster.waitForSchedulerCache(binpackSpread)
		fixture.deletePod(binpackSpread)

		ginkgo.By("asserting spread node and binpack GPU policy chains")
		spreadBinpack := fixture.createPod("combined-spread-binpack", podOptions{
			nodes:      []string{low.name, high.name},
			gpuUUIDs:   []string{low.gpu(0), low.gpu(1), high.gpu(0), high.gpu(1)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  "binpack,spread",
			nodePolicy: util.NodeSchedulerPolicySpread.String(),
		})
		gomega.Expect(spreadBinpack.Spec.NodeName).To(gomega.Equal(low.name))
		gomega.Expect(allocatedGPU(spreadBinpack)).To(gomega.Equal(low.gpu(0)))
		cluster.waitForSchedulerCache(spreadBinpack)
		fixture.deletePod(spreadBinpack)

		ginkgo.By("asserting spread node and spread GPU policy chains")
		spreadSpread := fixture.createPod("combined-spread-spread", podOptions{
			nodes:      []string{low.name, high.name},
			gpuUUIDs:   []string{low.gpu(0), low.gpu(1), high.gpu(0), high.gpu(1)},
			cores:      10,
			memoryPct:  10,
			gpuPolicy:  "spread,binpack",
			nodePolicy: util.NodeSchedulerPolicySpread.String(),
		})
		gomega.Expect(spreadSpread.Spec.NodeName).To(gomega.Equal(low.name))
		gomega.Expect(allocatedGPU(spreadSpread)).To(gomega.Equal(low.gpu(1)))
	})

	ginkgo.It("applies per-Pod device scoring weight overrides", func() {
		selected := requireNodes(nodes, 1, 2, "per-Pod device scoring weights")
		node := selected[0]
		fixture := newPolicyFixture(cluster)

		ginkgo.By("creating GPU utilization that default and memory-heavy weights rank differently")
		coreSeed := fixture.createPod("weight-core", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0)},
			cores:      60,
			memoryPct:  10,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		memorySeed := fixture.createPod("weight-memory", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(1)},
			cores:      1,
			memoryPct:  40,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		cluster.waitForSchedulerCache(coreSeed)
		cluster.waitForSchedulerCache(memorySeed)

		ginkgo.By("asserting default weights prefer the core-heavy GPU")
		defaultWeights := fixture.createPod("weight-default", podOptions{
			nodes:      []string{node.name},
			gpuUUIDs:   []string{node.gpu(0), node.gpu(1)},
			memoryPct:  10,
			gpuPolicy:  util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy: util.NodeSchedulerPolicyBinpack.String(),
		})
		gomega.Expect(allocatedGPU(defaultWeights)).To(gomega.Equal(node.gpu(0)))
		cluster.waitForSchedulerCache(defaultWeights)
		fixture.deletePod(defaultWeights)

		ginkgo.By("asserting memory-heavy weights prefer the memory-heavy GPU")
		memoryWeighted := fixture.createPod("weight-memory-heavy", podOptions{
			nodes:              []string{node.name},
			gpuUUIDs:           []string{node.gpu(0), node.gpu(1)},
			memoryPct:          10,
			gpuPolicy:          util.GPUSchedulerPolicyBinpack.String(),
			nodePolicy:         util.NodeSchedulerPolicyBinpack.String(),
			deviceScoreWeights: "slot=1,core=1,memory=3",
		})
		gomega.Expect(allocatedGPU(memoryWeighted)).To(gomega.Equal(node.gpu(1)))
	})
})

type podOptions struct {
	nodes              []string
	gpuUUIDs           []string
	cores              int64
	memoryPct          int64
	nodePolicy         string
	gpuPolicy          string
	deviceScoreWeights string
}

func newPolicyFixture(cluster *policyCluster) *policyFixture {
	client := cluster.client
	namespace := fmt.Sprintf("%s%s", policyTestNamespacePrefix, utils.GetRandom())
	_, err := client.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	fixture := &policyFixture{cluster: cluster, client: client, namespace: namespace}
	ginkgo.DeferCleanup(fixture.cleanup)
	return fixture
}

func (f *policyFixture) createPod(name string, options podOptions) *corev1.Pod {
	pod := newPolicyPod(f.namespace, name, options)
	_, err := f.client.CoreV1().Pods(f.namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	allocated := waitForAllocatedPod(f.client, f.namespace, pod.Name)
	f.pods = append(f.pods, allocated)
	return allocated
}

func (f *policyFixture) deletePod(pod *corev1.Pod) {
	err := f.client.CoreV1().Pods(f.namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
	if !apierrors.IsNotFound(err) {
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	gomega.Eventually(func() bool {
		_, err := f.client.CoreV1().Pods(f.namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, policyTestTimeout, policyTestInterval).Should(gomega.BeTrue())

	// The API object is gone, but the scheduler releases the pod's GPU/node
	// usage asynchronously via an informer delete event. Wait for that release
	// so a subsequent placement assertion does not observe stale utilization.
	f.cluster.waitForSchedulerCacheRelease(pod)
}

func (f *policyFixture) cleanup() {
	err := f.client.CoreV1().Namespaces().Delete(context.Background(), f.namespace, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	gomega.Eventually(func() bool {
		_, err := f.client.CoreV1().Namespaces().Get(context.Background(), f.namespace, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, policyTestTimeout, policyTestInterval).Should(gomega.BeTrue())

	// Namespace deletion removes the pods from the API, but the scheduler's
	// cache still counts their usage until the delete events are processed.
	// Wait for release so the next It block starts from a clean cache.
	for _, pod := range f.pods {
		f.cluster.waitForSchedulerCacheRelease(pod)
	}
}

func newPolicyCluster() *policyCluster {
	return &policyCluster{client: utils.GetClientSet()}
}

// waitForSchedulerCache blocks until the scheduler's in-memory cache reflects
// the pod's allocation (its resource-usage metric line has appeared on at least
// one running scheduler replica).
func (c *policyCluster) waitForSchedulerCache(pod *corev1.Pod) {
	needle := schedulerCacheNeedle(pod)
	gomega.Eventually(func() bool {
		return strings.Contains(c.schedulerMetrics(), needle)
	}, policyTestTimeout, policyTestInterval).Should(gomega.BeTrue(),
		"scheduler cache never reflected pod %s/%s", pod.Namespace, pod.Name)
}

// waitForSchedulerCacheRelease blocks until the scheduler's in-memory cache has
// released the pod's allocation on every running scheduler replica, so freed
// GPU/node resources are no longer counted against subsequent placements.
func (c *policyCluster) waitForSchedulerCacheRelease(pod *corev1.Pod) {
	needle := schedulerCacheNeedle(pod)
	gomega.Eventually(func() bool {
		return !strings.Contains(c.schedulerMetrics(), needle)
	}, policyTestTimeout, policyTestInterval).Should(gomega.BeTrue(),
		"scheduler cache never released pod %s/%s", pod.Namespace, pod.Name)
}

func schedulerCacheNeedle(pod *corev1.Pod) string {
	return fmt.Sprintf("namespace=\"%s\",node=\"%s\",pod=\"%s\"", pod.Namespace, pod.Spec.NodeName, pod.Name)
}

// schedulerMetricsTargets returns the metrics endpoint of every running
// scheduler-extender replica. Scraping all replicas (rather than guessing the
// leader) keeps the cache assertions correct under HA and across leader changes.
func (c *policyCluster) schedulerMetricsTargets() []policyMetricsTarget {
	pods, err := c.client.CoreV1().Pods(hamiNamespace()).List(context.Background(), metav1.ListOptions{
		LabelSelector: schedulerComponentKey + "=" + schedulerComponent,
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	targets := make([]policyMetricsTarget, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, container := range pod.Spec.Containers {
			if container.Name != schedulerContainer {
				continue
			}
			for _, port := range container.Ports {
				if port.Name == "metrics" {
					targets = append(targets, policyMetricsTarget{pod: pod.Name, port: fmt.Sprint(port.ContainerPort)})
				}
			}
		}
	}
	gomega.Expect(targets).NotTo(gomega.BeEmpty(), "no running HAMi scheduler metrics endpoint found")
	return targets
}

// schedulerMetrics returns the concatenated /metrics output of every running
// scheduler replica, re-resolving the endpoints on each call so leader changes
// and replica restarts are tolerated.
func (c *policyCluster) schedulerMetrics() string {
	var builder strings.Builder
	for _, target := range c.schedulerMetricsTargets() {
		response := c.client.CoreV1().Pods(hamiNamespace()).ProxyGet("http", target.pod, target.port, "metrics", nil)
		stream, err := response.Stream(context.Background())
		if err != nil {
			continue
		}
		metrics, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			continue
		}
		builder.Write(metrics)
	}
	return builder.String()
}

func newPolicyPod(namespace, name string, options podOptions) *corev1.Pod {
	annotations := map[string]string{
		nvidia.GPUUseUUID:                     strings.Join(options.gpuUUIDs, ","),
		util.NodeSchedulerPolicyAnnotationKey: options.nodePolicy,
		util.GPUSchedulerPolicyAnnotationKey:  options.gpuPolicy,
	}
	if options.deviceScoreWeights != "" {
		annotations[util.DeviceScoringWeightsAnnotationKey] = options.deviceScoreWeights
	}

	limits := corev1.ResourceList{
		gpuResourceName:      resource.MustParse("1"),
		gpuMemoryPercentName: *resource.NewQuantity(options.memoryPct, resource.DecimalSI),
	}
	if options.cores > 0 {
		limits[gpuCoreResourceName] = *resource.NewQuantity(options.cores, resource.DecimalSI)
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name + "-" + utils.GetRandom(),
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			SchedulerName: policyTestScheduler,
			RestartPolicy: corev1.RestartPolicyNever,
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							MatchFields: []corev1.NodeSelectorRequirement{{
								Key:      "metadata.name",
								Operator: corev1.NodeSelectorOpIn,
								Values:   options.nodes,
							}},
						}},
					},
				},
			},
			Containers: []corev1.Container{{
				Name:      "cuda",
				Image:     policyTestImage,
				Command:   []string{"/bin/sh", "-c", "sleep 86400"},
				Resources: corev1.ResourceRequirements{Limits: limits},
			}},
		},
	}
}

func waitForAllocatedPod(client kubernetes.Interface, namespace, name string) *corev1.Pod {
	var latest *corev1.Pod
	gomega.Eventually(func(g gomega.Gomega) {
		pod, err := client.CoreV1().Pods(namespace).Get(context.Background(), name, metav1.GetOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		latest = pod
		g.Expect(pod.Spec.SchedulerName).To(gomega.Equal(policyTestScheduler))
		g.Expect(pod.Spec.NodeName).NotTo(gomega.BeEmpty())
		g.Expect(pod.Status.Phase).To(gomega.Equal(corev1.PodRunning))
		g.Expect(pod.Annotations[util.AssignedNodeAnnotations]).To(gomega.Equal(pod.Spec.NodeName))
		g.Expect(pod.Annotations[util.DeviceBindPhase]).To(gomega.Equal(util.DeviceBindSuccess))
		g.Expect(pod.Annotations[allocatedDevicesKey]).NotTo(gomega.BeEmpty())
	}, policyTestTimeout, policyTestInterval).Should(gomega.Succeed())
	return latest
}

func allocatedGPU(pod *corev1.Pod) string {
	return allocatedGPUs(pod)[0]
}

func allocatedGPUs(pod *corev1.Pod) []string {
	devices, err := device.DecodePodDevices(map[string]string{
		nvidia.NvidiaGPUDevice: allocatedDevicesKey,
	}, pod.Annotations)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// The encoded annotation carries a trailing separator, so the decode may
	// yield an empty container entry after the real one. Keep only the
	// containers that were actually allocated a device rather than depending
	// on that separator quirk.
	allocated := devices[nvidia.NvidiaGPUDevice]
	containers := make([][]device.ContainerDevice, 0, len(allocated))
	for _, container := range allocated {
		if len(container) > 0 {
			containers = append(containers, container)
		}
	}
	gomega.Expect(containers).To(gomega.HaveLen(1), "expected exactly one allocated container")
	gomega.Expect(containers[0]).To(gomega.HaveLen(1), "expected exactly one allocated GPU")

	uuids := make([]string, 0, len(containers[0]))
	for _, allocatedDevice := range containers[0] {
		uuids = append(uuids, allocatedDevice.UUID)
	}
	return uuids
}

func registeredGPUNodes(client kubernetes.Interface) []gpuNode {
	nodeList, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	registered := make([]gpuNode, 0)
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if !nodeReady(node) || node.Spec.Unschedulable {
			continue
		}
		if !hasReadyDevicePlugin(client, node.Name) {
			continue
		}

		raw := node.Annotations[nvidia.RegisterAnnos]
		devices, err := device.UnMarshalNodeDevices(raw)
		if err != nil {
			continue
		}

		gpus := make([]device.DeviceInfo, 0, len(devices))
		for _, gpu := range devices {
			if gpu != nil && gpu.Health && gpu.Mode == nvidia.HamiCoreMode && gpu.Count >= 2 && gpu.Devcore == 100 && gpu.Devmem > 0 {
				gpus = append(gpus, *gpu)
			}
		}
		if len(gpus) == 0 {
			continue
		}
		sort.Slice(gpus, func(i, j int) bool {
			return gpus[i].Index < gpus[j].Index
		})
		registered = append(registered, gpuNode{name: node.Name, gpus: gpus})
	}

	sort.Slice(registered, func(i, j int) bool {
		return registered[i].name < registered[j].name
	})
	return registered
}

func requireNodes(nodes []gpuNode, count, minimumGPUs int, feature string) []gpuNode {
	selected := make([]gpuNode, 0, count)
	for _, node := range nodes {
		if len(node.gpus) >= minimumGPUs {
			selected = append(selected, node)
		}
		if len(selected) == count {
			return selected
		}
	}

	message := fmt.Sprintf("%s requires %d idle registered NVIDIA GPU node(s) with at least %d healthy hami-core GPU(s) each", feature, count, minimumGPUs)
	if os.Getenv("HAMI_E2E_REQUIRE_POLICY_TOPOLOGY") == "true" {
		gomega.Expect(selected).To(gomega.HaveLen(count), message)
	}
	ginkgo.Skip(message)
	return nil
}

// ensureGPUNodeLabeled adds the gpu=on label (if missing) to the first GPU-capable
// node and waits for the device-plugin DaemonSet pod to become Ready on that node.
// This makes the policy suite independent of earlier test suites that may have
// removed the label during their cleanup.
func ensureGPUNodeLabeled(client kubernetes.Interface) {
	clientSet, ok := client.(*kubernetes.Clientset)
	if !ok {
		// Non-standard client (e.g. fake in unit tests); skip labeling.
		return
	}

	nodeName, err := utils.GetGPUNode(clientSet)
	if err != nil {
		// No GPU node discovered — requireNodes will skip later.
		return
	}

	node, err := client.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	if node.Labels[utils.GPUNodeLabelKey] != utils.GPUNodeLabelValue {
		ginkgo.By(fmt.Sprintf("labeling node %s with %s=%s for device-plugin scheduling", nodeName, utils.GPUNodeLabelKey, utils.GPUNodeLabelValue))
		_, err = utils.AddNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey, utils.GPUNodeLabelValue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	// Wait for the device-plugin pod to become Ready on this node.
	gomega.Eventually(func() bool {
		return hasReadyDevicePlugin(client, nodeName)
	}, policyTestTimeout, policyTestInterval).Should(gomega.BeTrue(),
		"device-plugin did not become ready on node %s after labeling", nodeName)
}

func nodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func hasReadyDevicePlugin(client kubernetes.Interface, nodeName string) bool {
	pods, err := client.CoreV1().Pods(hamiNamespace()).List(context.Background(), metav1.ListOptions{
		LabelSelector: schedulerComponentKey + "=" + devicePluginComponent,
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return false
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == "device-plugin" && status.Ready {
				return true
			}
		}
	}
	return false
}

func hamiNamespace() string {
	if namespace := os.Getenv("HAMI_NAMESPACE"); namespace != "" {
		return namespace
	}
	return utils.GPUNameSpace
}
