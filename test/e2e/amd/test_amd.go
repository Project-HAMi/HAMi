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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/test/utils"
)

// registered is the part of a node's hami.io/node-amd-register entry these tests read.
type registered struct {
	ID      string `json:"id"`
	Devmem  int32  `json:"devmem"`
	Devcore int32  `json:"devcore"`
	Count   int32  `json:"count"`
	Type    string `json:"type"`
	Numa    int    `json:"numa"`
}

const (
	registerAnnotation  = "hami.io/node-amd-register"
	allocatedAnnotation = "hami.io/amd-devices-allocated"
)

// These tests need a cluster with HAMi installed and at least one node where
// amd-device-plugin registered a GPU. They are skipped on any other cluster.
var _ = ginkgo.Describe("AMD GPU E2E Tests", ginkgo.Ordered, func() {
	const namespace = "hami-amd-e2e"

	var (
		clientSet *kubernetes.Clientset
		nodeName  string
		gpu       registered
		all       []registered
		mock      = os.Getenv("AMD_E2E_MOCK") == "true"
		scheduler = envOr("HAMI_SCHEDULER_NAME", utils.HamiScheduler)
	)

	ginkgo.BeforeAll(func() {
		clientSet = utils.GetClientSet()

		nodes, err := clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		for _, n := range nodes.Items {
			raw, ok := n.Annotations[registerAnnotation]
			if !ok {
				continue
			}
			var devices []registered
			gomega.Expect(json.Unmarshal([]byte(raw), &devices)).To(gomega.Succeed(), "node %s has an undecodable %s", n.Name, registerAnnotation)
			if len(devices) > 0 {
				nodeName, gpu, all = n.Name, devices[0], devices
				break
			}
		}
		if nodeName == "" {
			ginkgo.Skip("no node registered an AMD GPU in " + registerAnnotation)
		}
		fmt.Printf("Using AMD GPU node %s, GPU %s (%s, %d MiB, %d CUs, shared %d times)\n", nodeName, gpu.ID, gpu.Type, gpu.Devmem, gpu.Devcore, gpu.Count)

		_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{"pod-security.kubernetes.io/enforce": "privileged"},
		}}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}
	})

	ginkgo.AfterAll(func() {
		if clientSet != nil && nodeName != "" {
			_ = clientSet.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
		}
	})

	// newPod prints the environment the GPU shows up with and exits.
	newPod := func(name string, limits corev1.ResourceList, annotations map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: annotations},
			Spec: corev1.PodSpec{
				SchedulerName: scheduler,
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name:            "rocm",
					Image:           envOr("AMD_E2E_IMAGE", "rocm/dev-ubuntu-24.04:7.2.4"),
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sh", "-c", "env | grep -E '^(HSA_CU_MASK|HIP_DEVICE_MEMORY_LIMIT[_0-9]*|LD_AUDIT)=' ; sleep 2"},
					Resources:       corev1.ResourceRequirements{Limits: limits},
				}},
			},
		}
	}
	gpus := func(extra corev1.ResourceList) corev1.ResourceList {
		limits := corev1.ResourceList{"amd.com/gpu": resource.MustParse("1")}
		maps.Copy(limits, extra)
		return limits
	}
	create := func(pod *corev1.Pod) {
		_, err := utils.CreatePod(clientSet, pod, namespace)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ginkgo.DeferCleanup(func() { _ = utils.DeletePod(clientSet, namespace, pod.Name) })
	}
	phaseOf := func(name string) corev1.PodPhase {
		p, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return p.Status.Phase
	}
	waitFor := func(name string, phase corev1.PodPhase) {
		err := wait.PollUntilContextTimeout(context.TODO(), 3*time.Second, 5*time.Minute, true, func(context.Context) (bool, error) {
			got := phaseOf(name)
			if got == corev1.PodFailed && phase != corev1.PodFailed {
				return false, fmt.Errorf("pod %s failed", name)
			}
			return got == phase, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pod %s never reached %s", name, phase)
	}
	logs := func(name string) string {
		stream, err := clientSet.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{}).Stream(context.TODO())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() { _ = stream.Close() }()
		b, err := io.ReadAll(stream)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return string(b)
	}
	// stuckWith waits for a pod the scheduler cannot place and returns its FailedScheduling message.
	stuckWith := func(name, reason string) {
		gomega.Eventually(func() string {
			events, err := clientSet.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{
				FieldSelector: "involvedObject.name=" + name + ",reason=FailedScheduling",
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			var msgs []string
			for _, e := range events.Items {
				msgs = append(msgs, e.Message)
			}
			return strings.Join(msgs, "\n")
		}, 2*time.Minute, 3*time.Second).Should(gomega.ContainSubstring(reason))
		gomega.Expect(phaseOf(name)).To(gomega.Equal(corev1.PodPending))
	}

	ginkgo.It("registers the GPU with its capacity", func() {
		gomega.Expect(gpu.ID).NotTo(gomega.BeEmpty())
		gomega.Expect(gpu.Devmem).To(gomega.BeNumerically(">", 0))
		gomega.Expect(gpu.Devcore).To(gomega.BeNumerically(">", 0))
		gomega.Expect(gpu.Count).To(gomega.BeNumerically(">", 0))
		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		allocatable := node.Status.Allocatable["amd.com/gpu"]
		gomega.Expect(allocatable.Value()).To(gomega.BeNumerically(">", 0))
	})

	ginkgo.It("runs a whole-GPU pod without a CU mask or memory limit", func() {
		skipWithoutPlugin(mock)
		create(newPod("amd-whole", gpus(nil), nil))
		waitFor("amd-whole", corev1.PodSucceeded)
		out := logs("amd-whole")
		gomega.Expect(out).NotTo(gomega.ContainSubstring("HIP_DEVICE_MEMORY_LIMIT"))
		gomega.Expect(out).NotTo(gomega.ContainSubstring("LD_AUDIT"))
	})

	ginkgo.It("slices memory and cores", func() {
		skipWithoutPlugin(mock)
		create(newPod("amd-slice", gpus(corev1.ResourceList{
			"amd.com/gpumem":   resource.MustParse("2048"),
			"amd.com/gpucores": resource.MustParse("25"),
		}), nil))
		waitFor("amd-slice", corev1.PodSucceeded)
		out := logs("amd-slice")
		gomega.Expect(out).To(gomega.ContainSubstring("HSA_CU_MASK="))
		// older plugin builds name the variable without the device index
		gomega.Expect(out).To(gomega.MatchRegexp(`HIP_DEVICE_MEMORY_LIMIT(_0)?=2048m`))
		gomega.Expect(out).To(gomega.ContainSubstring("LD_AUDIT="))

		pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), "amd-slice", metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(pod.Annotations[allocatedAnnotation]).To(gomega.HavePrefix(gpu.ID + ","))
	})

	ginkgo.It("books a share of the memory for gpumem-percentage", func() {
		skipWithoutPlugin(mock)
		create(newPod("amd-percentage", gpus(corev1.ResourceList{"amd.com/gpumem-percentage": resource.MustParse("25")}), nil))
		waitFor("amd-percentage", corev1.PodSucceeded)
		gomega.Expect(logs("amd-percentage")).To(gomega.MatchRegexp(fmt.Sprintf(`HIP_DEVICE_MEMORY_LIMIT(_0)?=%dm`, gpu.Devmem*25/100)))
	})

	// allocation waits for the scheduler to write the pod's device allocation and returns one entry per device.
	allocation := func(name string) []allocated {
		var out []allocated
		gomega.Eventually(func() string {
			p, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			return p.Annotations[allocatedAnnotation]
		}, 2*time.Minute, 2*time.Second).ShouldNot(gomega.BeEmpty(), "pod %s was never allocated", name)
		p, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		out, err = parseAllocation(p.Annotations[allocatedAnnotation])
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return out
	}
	numaOf := func(uuid string) int {
		for _, d := range all {
			if d.ID == uuid {
				return d.Numa
			}
		}
		return -2
	}

	ginkgo.It("records the requested memory and cores in the allocation", func() {
		create(newPod("amd-alloc", gpus(corev1.ResourceList{
			"amd.com/gpumem":   resource.MustParse("1000"),
			"amd.com/gpucores": resource.MustParse("25"),
		}), nil))
		got := allocation("amd-alloc")
		gomega.Expect(got).To(gomega.HaveLen(1))
		gomega.Expect(got[0].Mem).To(gomega.BeEquivalentTo(1000))
		// the allocation counts CUs: 25% of the GPU, rounded up to whole WGPs on RDNA
		want := int64(gpu.Devcore) * 25 / 100
		gomega.Expect(got[0].Cores).To(gomega.BeNumerically(">=", want))
		gomega.Expect(got[0].Cores).To(gomega.BeNumerically("<=", want+1))
	})

	ginkgo.It("converts gpumem-percentage into memory in the allocation", func() {
		create(newPod("amd-alloc-pct", gpus(corev1.ResourceList{"amd.com/gpumem-percentage": resource.MustParse("50")}), nil))
		got := allocation("amd-alloc-pct")
		gomega.Expect(got).To(gomega.HaveLen(1))
		gomega.Expect(got[0].Mem).To(gomega.BeEquivalentTo(gpu.Devmem * 50 / 100))
	})

	ginkgo.It("gives a two-GPU request two different GPUs", func() {
		if len(all) < 2 {
			ginkgo.Skip("the node registered fewer than two GPUs")
		}
		create(newPod("amd-two", corev1.ResourceList{"amd.com/gpu": resource.MustParse("2")}, nil))
		got := allocation("amd-two")
		gomega.Expect(got).To(gomega.HaveLen(2))
		gomega.Expect(got[0].UUID).NotTo(gomega.Equal(got[1].UUID))
	})

	ginkgo.It("keeps numa-bind GPUs on one NUMA node", func() {
		perNuma := map[int]int{}
		for _, d := range all {
			perNuma[d.Numa]++
		}
		if len(perNuma) < 2 {
			ginkgo.Skip("the node registered GPUs on fewer than two NUMA nodes")
		}
		create(newPod("amd-numa", corev1.ResourceList{"amd.com/gpu": resource.MustParse("2")}, map[string]string{"amd.com/numa-bind": "true"}))
		got := allocation("amd-numa")
		gomega.Expect(got).To(gomega.HaveLen(2))
		gomega.Expect(numaOf(got[0].UUID)).To(gomega.Equal(numaOf(got[1].UUID)))
	})

	ginkgo.It("leaves a pod pending that wants more memory than the GPU has", func() {
		create(newPod("amd-too-big", gpus(corev1.ResourceList{"amd.com/gpumem": *resource.NewQuantity(int64(gpu.Devmem)+1, resource.DecimalSI)}), nil))
		stuckWith("amd-too-big", "CardInsufficientMemory")
	})

	ginkgo.It("leaves a pod pending that asks for a GPU type nothing has", func() {
		create(newPod("amd-wrong-type", gpus(nil), map[string]string{"amd.com/use-gputype": "no-such-gpu"}))
		stuckWith("amd-wrong-type", "CardTypeMismatch")
	})

	ginkgo.It("leaves a pod pending that excludes every GPU by UUID", func() {
		ids := make([]string, 0, len(all))
		for _, d := range all {
			ids = append(ids, d.ID)
		}
		create(newPod("amd-no-uuid", gpus(nil), map[string]string{"amd.com/nouse-gpu-uuid": strings.Join(ids, ",")}))
		stuckWith("amd-no-uuid", "CardUuidMismatch")
	})
})

// skipWithoutPlugin skips specs that read what amd-device-plugin injects into a running container.
func skipWithoutPlugin(mock bool) {
	if mock {
		ginkgo.Skip("AMD_E2E_MOCK: no amd-device-plugin to inject the container environment")
	}
}

// allocated is one device of a pod's hami.io/amd-devices-allocated annotation.
type allocated struct {
	UUID  string
	Mem   int64
	Cores int64
}

// parseAllocation reads the first container of "uuid,type,mem,cores:uuid,...;" annotations.
func parseAllocation(raw string) ([]allocated, error) {
	var out []allocated
	for dev := range strings.SplitSeq(strings.SplitN(raw, ";", 2)[0], ":") {
		if dev == "" {
			continue
		}
		f := strings.Split(dev, ",")
		if len(f) < 4 {
			return nil, fmt.Errorf("device %q in %q has fewer than four fields", dev, raw)
		}
		var a allocated
		a.UUID = f[0]
		if _, err := fmt.Sscan(f[2], &a.Mem); err != nil {
			return nil, err
		}
		if _, err := fmt.Sscan(f[3], &a.Cores); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
