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
	"os"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/test/utils"
)

// registered is the part of a node's register annotation these tests read. The
// field set comes from pkg/device.DeviceInfo, which every vendor's registration
// JSON shares, so the same struct decodes both the NVIDIA and AMD mock
// registrations hack/e2e-nvidia-mock.sh and hack/e2e-amd-mock.sh write.
type registered struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// cordonAnnotation is the node annotation these specs set and clear. Reusing the
// constant from pkg/device/common, rather than a copy, means a rename there
// can't silently leave these specs testing the wrong key.
const cordonAnnotation = common.DeviceCordonAnnotation

// vendorProfile names one backend's cordon wiring: its registration annotation,
// its allocation annotation, and the resource name a pod requests it with.
type vendorProfile struct {
	name           string
	registerAnnos  string
	allocatedAnnos string
	resourceName   string
}

var vendors = map[string]vendorProfile{
	"nvidia": {
		name:           "nvidia",
		registerAnnos:  "hami.io/node-nvidia-register",
		allocatedAnnos: "hami.io/vgpu-devices-allocated",
		resourceName:   "nvidia.com/gpu",
	},
	"amd": {
		name:           "amd",
		registerAnnos:  "hami.io/node-amd-register",
		allocatedAnnos: "hami.io/amd-devices-allocated",
		resourceName:   "amd.com/gpu",
	},
}

// These specs need a cluster with HAMi installed and at least four devices
// registered, by one backend, on one node; hack/e2e-nvidia-mock.sh and
// hack/e2e-amd-mock.sh fake exactly that, with no real GPU. CORDON_E2E_VENDOR
// picks which backend's annotations to read ("nvidia", the default, or "amd").
// On any other cluster the specs are skipped rather than failed.
//
// No spec here waits for a pod to reach Running: the scheduler extender writes
// the allocation annotation, and a FailedScheduling event, during Filter/Bind
// alone, so every case can assert on those without a container ever starting.
var _ = ginkgo.Describe("Device Cordon E2E Tests", ginkgo.Ordered, func() {
	const namespace = "hami-cordon-e2e"

	vendorName := envOr("CORDON_E2E_VENDOR", "nvidia")
	v, vendorKnown := vendors[vendorName]

	var (
		clientSet *kubernetes.Clientset
		nodeName  string
		all       []registered
		scheduler = envOr("HAMI_SCHEDULER_NAME", utils.HamiScheduler)
	)

	ginkgo.BeforeAll(func() {
		if !vendorKnown {
			ginkgo.Skip("unknown CORDON_E2E_VENDOR " + vendorName)
		}
		clientSet = utils.GetClientSet()

		nodes, err := clientSet.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		for _, n := range nodes.Items {
			raw, ok := n.Annotations[v.registerAnnos]
			if !ok {
				continue
			}
			var devices []registered
			gomega.Expect(json.Unmarshal([]byte(raw), &devices)).To(gomega.Succeed(), "node %s has an undecodable %s", n.Name, v.registerAnnos)
			if len(devices) > 0 {
				nodeName, all = n.Name, devices
				break
			}
		}
		if nodeName == "" {
			ginkgo.Skip("no node registered a " + v.name + " GPU in " + v.registerAnnos)
		}
		if len(all) < 4 {
			ginkgo.Skip(fmt.Sprintf("node %s registered only %d %s device(s), these specs need at least 4", nodeName, len(all), v.name))
		}
		fmt.Printf("Using %s node %s with %d registered devices\n", v.name, nodeName, len(all))

		_, err = clientSet.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: map[string]string{"pod-security.kubernetes.io/enforce": "privileged"},
		}}, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}
	})

	ginkgo.AfterAll(func() {
		if clientSet == nil || nodeName == "" {
			return
		}
		_, _ = utils.RemoveNodeAnnotation(clientSet, nodeName, cordonAnnotation)
		_ = clientSet.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
	})

	// resetCordon clears the annotation so each It starts from a known, uncordoned
	// node regardless of what an earlier spec left behind.
	resetCordon := func() {
		_, err := utils.RemoveNodeAnnotation(clientSet, nodeName, cordonAnnotation)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	cordon := func(uuids ...string) {
		_, err := utils.SetNodeAnnotation(clientSet, nodeName, cordonAnnotation, strings.Join(uuids, ","))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	idsOf := func(devices []registered) []string {
		ids := make([]string, len(devices))
		for i, d := range devices {
			ids[i] = d.ID
		}
		return ids
	}

	newPod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: corev1.PodSpec{
				SchedulerName: scheduler,
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name:    "sleep",
					Image:   envOr("CORDON_E2E_IMAGE", "busybox:1.36"),
					Command: []string{"sh", "-c", "sleep 3600"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{corev1.ResourceName(v.resourceName): resource.MustParse("1")},
					},
				}},
			},
		}
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
	// stuckWith waits for a pod the scheduler cannot place and asserts its
	// FailedScheduling events contain reason, the same pattern the AMD mock
	// suite uses for its own rejection reasons.
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
	// annotationOf waits for the scheduler to Bind name and returns its raw
	// allocation annotation.
	annotationOf := func(name string) string {
		var raw string
		gomega.Eventually(func() string {
			p, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			raw = p.Annotations[v.allocatedAnnos]
			return raw
		}, 2*time.Minute, 2*time.Second).ShouldNot(gomega.BeEmpty(), "pod %s was never allocated", name)
		return raw
	}
	// allocatedUUID waits for name to be bound and returns the UUID of the one
	// device it was given; every pod in these specs requests exactly one.
	allocatedUUID := func(name string) string {
		got, err := parseAllocation(annotationOf(name))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(got).To(gomega.HaveLen(1))
		return got[0].UUID
	}

	// E1: cordoning three of the four registered devices after they're already
	// registered (and so already cached by the scheduler) still lands a pod on
	// the one left uncordoned. This is the regression case for the stale-cache
	// bug WI-1 fixed: without a live refresh, the scheduler would keep scoring
	// against the pre-cordon state.
	ginkgo.It("E1: lands a pod on the only uncordoned GPU after cordoning the rest", func() {
		resetCordon()
		ids := idsOf(all)
		want := ids[len(ids)-1]
		cordon(ids[:len(ids)-1]...)

		create(newPod("cordon-e1"))
		gomega.Expect(allocatedUUID("cordon-e1")).To(gomega.Equal(want))
	})

	// E2+E3: with every device cordoned, a pod stays Pending with a cordon
	// reason; uncordoning one of them then lets that same pod schedule, within
	// a generous bound. How long that takes is printed rather than asserted
	// tightly, since the exact figure depends on the informer/poll cadence, not
	// on correctness.
	ginkgo.It("E2+E3: a pod stays pending while every GPU is cordoned, then schedules once one is uncordoned", func() {
		resetCordon()
		ids := idsOf(all)
		cordon(ids...)

		create(newPod("cordon-e2"))
		stuckWith("cordon-e2", common.CardCordoned)

		start := time.Now()
		cordon(ids[1:]...) // uncordon ids[0], leave the rest cordoned
		gomega.Expect(allocatedUUID("cordon-e2")).To(gomega.Equal(ids[0]))
		fmt.Printf("uncordon-to-scheduled latency: %s\n", time.Since(start))
	})

	// E4: cordoning the device under a pod the scheduler has already bound must
	// not disturb that pod. Binding happens before any container starts, so
	// this only needs the allocation annotation to survive unchanged and the
	// pod to still exist, not for it to reach Running.
	ginkgo.It("E4: cordoning a GPU under a bound pod doesn't disturb it", func() {
		resetCordon()
		create(newPod("cordon-e4"))
		before := annotationOf("cordon-e4")
		uuid, err := parseAllocation(before)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(uuid).To(gomega.HaveLen(1))

		cordon(uuid[0].UUID)
		time.Sleep(10 * time.Second) // give register()'s refresh a chance to run

		after, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), "cordon-e4", metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(after.DeletionTimestamp).To(gomega.BeNil())
		gomega.Expect(after.Annotations[v.allocatedAnnos]).To(gomega.Equal(before))
	})

	// E5: a cordon value with stray whitespace, empty entries, and a UUID that
	// matches no registered device must not break scheduling. If parsing it
	// errored, allocatedUUID below would time out, since the extender would be
	// failing every pod on this node, not only ones that ask for the cordoned
	// device.
	ginkgo.It("E5: whitespace, empty entries, and unknown UUIDs are tolerated", func() {
		resetCordon()
		valid := idsOf(all)[0]
		_, err := utils.SetNodeAnnotation(clientSet, nodeName, cordonAnnotation,
			"  , ,"+valid+", not-a-real-uuid-"+utils.GetRandom()+" , ,")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		create(newPod("cordon-e5"))
		gomega.Expect(allocatedUUID("cordon-e5")).NotTo(gomega.Equal(valid))
	})

	// E6: the cordon is read from the node's live annotation, not cached only in
	// the scheduler process, so it must still be honored after the scheduler
	// restarts.
	ginkgo.It("E6: cordon state survives a scheduler restart", func() {
		resetCordon()
		ids := idsOf(all)
		cordon(ids...)

		create(newPod("cordon-e6a"))
		stuckWith("cordon-e6a", common.CardCordoned)

		gomega.Expect(utils.RestartHamiScheduler(clientSet, utils.GPUNameSpace)).NotTo(gomega.HaveOccurred())
		gomega.Expect(utils.WaitForHamiSchedulerReady(clientSet, utils.GPUNameSpace, 3*time.Minute)).NotTo(gomega.HaveOccurred())

		create(newPod("cordon-e6b"))
		stuckWith("cordon-e6b", common.CardCordoned)
	})
})

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// allocated is one device of a pod's allocation annotation.
type allocated struct {
	UUID  string
	Mem   int64
	Cores int64
}

// parseAllocation reads the first container's "uuid,type,mem,cores[,slots]:...;"
// allocation annotation. The encoding is generic across every vendor backend
// (pkg/device.EncodeContainerDevices), so this is the same parser the AMD mock
// suite uses for its own allocation annotation.
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
