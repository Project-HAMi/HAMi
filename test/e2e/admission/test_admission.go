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

// Package admission covers the decisions the HAMi mutating webhook makes when
// a Pod is created. The webhook is the first component every HAMi workload
// meets and several of its guards fail silently when they regress: a quota
// bypass (#2558), non-idempotent mutation under reinvocation (#2929, #2936),
// an empty resource name injected as a map key (#2889) and a valid resource
// rejected outright (#2975).
//
// The specs assert admission outcomes only, so they need no physical
// accelerator. Pods that are expected to be admitted carry a node selector
// that matches nothing, which keeps them Pending and stops the suite from
// competing for real GPUs.
package admission

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/test/utils"
)

const (
	admissionNamespacePrefix = "hami-admission-e2e-"
	admissionTestImage       = "registry.k8s.io/pause:3.9"
	admissionScheduler       = "hami-scheduler"
	admissionTimeout         = 2 * time.Minute
	admissionInterval        = 2 * time.Second

	gpuResourceName    = corev1.ResourceName("nvidia.com/gpu")
	gpuMemResourceName = corev1.ResourceName("nvidia.com/gpumem")

	// unschedulableNodeSelectorKey keeps an admitted Pod Pending so the suite
	// never consumes a real device. Admission has already happened by the time
	// scheduling is attempted, which is all these specs assert on.
	unschedulableNodeSelectorKey = "hami.io/e2e-admission-never-matches"
)

// newPod returns a Pod that is admitted but never scheduled.
func newPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			NodeSelector:  map[string]string{unschedulableNodeSelectorKey: "true"},
			Containers: []corev1.Container{{
				Name:  "app",
				Image: admissionTestImage,
			}},
		},
	}
}

// withDeviceRequest makes the Pod one HAMi recognises, which is what turns on
// the webhook's resource-dependent guards.
func withDeviceRequest(pod *corev1.Pod, gpus int64) *corev1.Pod {
	pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			gpuResourceName: *resource.NewQuantity(gpus, resource.DecimalSI),
		},
	}
	return pod
}

// withMemoryRequest adds a device memory request, which is what brings the
// Pod within the webhook's ResourceQuota check.
func withMemoryRequest(pod *corev1.Pod, mem int64) *corev1.Pod {
	if pod.Spec.Containers[0].Resources.Limits == nil {
		pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{}
	}
	pod.Spec.Containers[0].Resources.Limits[gpuMemResourceName] = *resource.NewQuantity(mem, resource.DecimalSI)
	return pod
}

// withPrivileged marks the first container privileged.
func withPrivileged(pod *corev1.Pod) *corev1.Pod {
	privileged := true
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: &privileged}
	return pod
}

// withAnnotation sets a single annotation on the Pod.
func withAnnotation(pod *corev1.Pod, key, value string) *corev1.Pod {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[key] = value
	return pod
}

func createNamespace(client kubernetes.Interface, name string) {
	ginkgo.GinkgoHelper()
	_, err := client.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}, metav1.CreateOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "create namespace %s", name)
}

func deleteNamespace(client kubernetes.Interface, name string) {
	ginkgo.GinkgoHelper()
	err := client.CoreV1().Namespaces().Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "delete namespace %s", name)
	}
}

// createAndCleanup creates the Pod and registers its deletion, returning the
// admitted object.
func createAndCleanup(client kubernetes.Interface, namespace string, pod *corev1.Pod) *corev1.Pod {
	ginkgo.GinkgoHelper()
	created, err := client.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "pod %s should have been admitted", pod.Name)
	ginkgo.DeferCleanup(func() {
		_ = client.CoreV1().Pods(namespace).Delete(context.TODO(), created.Name, metav1.DeleteOptions{})
	})
	return created
}

var _ = ginkgo.Describe("[admission] Mutating webhook admission E2E tests", ginkgo.Ordered, ginkgo.Serial, func() {
	var (
		client    kubernetes.Interface
		namespace string
	)

	ginkgo.BeforeAll(func() {
		client = utils.GetClientSet()
		namespace = admissionNamespacePrefix + utils.GetRandom()
		createNamespace(client, namespace)
	})

	ginkgo.AfterAll(func() {
		deleteNamespace(client, namespace)
	})

	// A privileged container can escape the in-container limits HAMi relies on,
	// so requesting a device from one is refused. Without a device request the
	// Pod is none of HAMi's business and must still be admitted.
	ginkgo.It("denies a privileged Pod that requests a device", func() {
		pod := withPrivileged(withDeviceRequest(newPod("privileged-with-device"), 1))
		_, err := client.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred(), "a privileged Pod requesting a device must be denied")
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("is privileged"))
	})

	ginkgo.It("admits a privileged Pod that requests no device", func() {
		created := createAndCleanup(client, namespace, withPrivileged(newPod("privileged-no-device")))
		gomega.Expect(created.Name).To(gomega.Equal("privileged-no-device"))
	})

	// GetNumaAlignmentModeByPod runs before the per-device mutation, so a typo
	// is reported at admission rather than surfacing later in a device-plugin
	// log.
	ginkgo.It("denies an invalid hami.io/numa-alignment value", func() {
		pod := withAnnotation(withDeviceRequest(newPod("bad-numa-alignment"), 1),
			"hami.io/numa-alignment", "not-a-mode")
		_, err := client.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred(), "an unparsable numa-alignment value must be denied")
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("hami.io/numa-alignment"))
	})

	ginkgo.It("admits a valid hami.io/numa-alignment value", func() {
		pod := withAnnotation(withDeviceRequest(newPod("good-numa-alignment"), 1),
			"hami.io/numa-alignment", "best-effort")
		createAndCleanup(client, namespace, pod)
	})

	// The webhook rewrites schedulerName only for Pods it recognises, which is
	// what routes them to the HAMi extender. Leaving an unrelated Pod alone
	// matters just as much.
	ginkgo.It("rewrites schedulerName for a Pod requesting HAMi resources", func() {
		created := createAndCleanup(client, namespace, withDeviceRequest(newPod("scheduler-rewritten"), 1))
		gomega.Expect(created.Spec.SchedulerName).To(gomega.Equal(admissionScheduler))
	})

	ginkgo.It("leaves schedulerName alone for a Pod requesting no HAMi resources", func() {
		created := createAndCleanup(client, namespace, newPod("scheduler-untouched"))
		gomega.Expect(created.Spec.SchedulerName).NotTo(gomega.Equal(admissionScheduler))
	})

	// Two separate admissions of the same spec must be mutated identically. This
	// is not webhook reinvocation, which the API server only triggers when a
	// later webhook changes the Pod and which cannot be forced from here; the
	// reinvocation case (#2929) belongs in unit tests that feed MutateAdmission
	// its own output.
	ginkgo.It("mutates two admissions of an identical spec the same way", func() {
		first := createAndCleanup(client, namespace, withDeviceRequest(newPod("idempotent-first"), 1))
		second := createAndCleanup(client, namespace, withDeviceRequest(newPod("idempotent-second"), 1))

		gomega.Expect(second.Spec.SchedulerName).To(gomega.Equal(first.Spec.SchedulerName))
		gomega.Expect(second.Spec.Containers[0].Resources.Limits).
			To(gomega.Equal(first.Spec.Containers[0].Resources.Limits))
		gomega.Expect(second.Spec.Containers[0].Resources.Requests).
			To(gomega.Equal(first.Spec.Containers[0].Resources.Requests))
		gomega.Expect(second.Spec.Containers[0].Env).
			To(gomega.Equal(first.Spec.Containers[0].Env))
		gomega.Expect(second.Spec.RuntimeClassName).To(gomega.Equal(first.Spec.RuntimeClassName))
	})

	// The quota guard runs in the webhook against the scheduler's cached
	// ResourceQuotas, so a Pod that would exceed the namespace limit never
	// reaches scheduling. #2558 was a bypass of exactly this path.
	ginkgo.Context("with a device memory ResourceQuota", func() {
		var quotaNamespace string

		ginkgo.BeforeAll(func() {
			quotaNamespace = admissionNamespacePrefix + "quota-" + utils.GetRandom()
			createNamespace(client, quotaNamespace)

			_, err := client.CoreV1().ResourceQuotas(quotaNamespace).Create(context.TODO(), &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: "hami-device-memory"},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceName("limits." + gpuMemResourceName.String()): *resource.NewQuantity(1000, resource.DecimalSI),
					},
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred(), "create ResourceQuota")
		})

		ginkgo.AfterAll(func() {
			deleteNamespace(client, quotaNamespace)
		})

		ginkgo.It("denies a Pod that exceeds the device memory quota", func() {
			// The scheduler learns about the quota through an informer, so the
			// first attempts may be admitted before it has caught up. Retry
			// until the denial appears, cleaning up anything that slips through.
			gomega.Eventually(func() string {
				pod := withMemoryRequest(withDeviceRequest(newPod("quota-"+utils.GetRandom()), 1), 4000)
				created, err := client.CoreV1().Pods(quotaNamespace).Create(context.TODO(), pod, metav1.CreateOptions{})
				if err != nil {
					return err.Error()
				}
				_ = client.CoreV1().Pods(quotaNamespace).Delete(context.TODO(), created.Name, metav1.DeleteOptions{})
				return ""
			}, admissionTimeout, admissionInterval).Should(gomega.ContainSubstring("exceeding resource quota"),
				"a Pod requesting more device memory than the namespace quota allows must be denied")
		})

		ginkgo.It("admits a Pod that fits within the device memory quota", func() {
			pod := withMemoryRequest(withDeviceRequest(newPod("quota-within"), 1), 500)
			createAndCleanup(client, quotaNamespace, pod)
		})
	})
})
