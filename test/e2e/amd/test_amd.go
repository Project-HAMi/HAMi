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
				nodeName, gpu = n.Name, devices[0]
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
		for k, v := range extra {
			limits[k] = v
		}
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
		create(newPod("amd-whole", gpus(nil), nil))
		waitFor("amd-whole", corev1.PodSucceeded)
		out := logs("amd-whole")
		gomega.Expect(out).NotTo(gomega.ContainSubstring("HIP_DEVICE_MEMORY_LIMIT"))
		gomega.Expect(out).NotTo(gomega.ContainSubstring("LD_AUDIT"))
	})

	ginkgo.It("slices memory and cores", func() {
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
		create(newPod("amd-percentage", gpus(corev1.ResourceList{"amd.com/gpumem-percentage": resource.MustParse("25")}), nil))
		waitFor("amd-percentage", corev1.PodSucceeded)
		gomega.Expect(logs("amd-percentage")).To(gomega.MatchRegexp(fmt.Sprintf(`HIP_DEVICE_MEMORY_LIMIT(_0)?=%dm`, gpu.Devmem*25/100)))
	})

	ginkgo.It("leaves a pod pending that wants more memory than the GPU has", func() {
		create(newPod("amd-too-big", gpus(corev1.ResourceList{"amd.com/gpumem": *resource.NewQuantity(int64(gpu.Devmem)+1, resource.DecimalSI)}), nil))
		stuckWith("amd-too-big", "CardInsufficientMemory")
	})

	ginkgo.It("leaves a pod pending that asks for a GPU type nothing has", func() {
		create(newPod("amd-wrong-type", gpus(nil), map[string]string{"amd.com/use-gputype": "no-such-gpu"}))
		stuckWith("amd-wrong-type", "CardTypeMismatch")
	})

	ginkgo.It("leaves a pod pending that excludes the only GPU by UUID", func() {
		create(newPod("amd-no-uuid", gpus(nil), map[string]string{"amd.com/nouse-gpu-uuid": gpu.ID}))
		stuckWith("amd-no-uuid", "CardUuidMismatch")
	})
})

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
