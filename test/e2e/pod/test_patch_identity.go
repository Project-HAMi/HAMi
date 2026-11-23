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
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/test/utils"
)

// util.PatchPodAnnotations addresses a pod by name and pins the UID it was
// computed for in the patch body. Only the API server enforces that pin, so the
// unit tests can check the body but not that a patch computed for a deleted pod
// fails instead of landing on a pod that later took the same name.
var _ = ginkgo.Describe("Pod annotation patch identity", func() {
	const (
		annotation = "hami.io/e2e-patch-identity"
		timeout    = 2 * time.Minute
		interval   = 2 * time.Second
	)

	var (
		clientSet *kubernetes.Clientset
		pod       *corev1.Pod
	)

	ginkgo.BeforeEach(func() {
		clientSet = utils.GetClientSet()
		previous := client.KubeClient
		client.KubeClient = clientSet
		ginkgo.DeferCleanup(func() { client.KubeClient = previous })

		// Left unscheduled on purpose: the test is about the object, so it
		// needs no node, no device and no image pull.
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "patch-identity-" + utils.GetRandom(),
				Namespace: utils.GPUNameSpace,
			},
			Spec: corev1.PodSpec{
				SchedulerName:                 "hami-e2e-unscheduled",
				TerminationGracePeriodSeconds: new(int64),
				Containers: []corev1.Container{{
					Name:  "main",
					Image: utils.Pod.Spec.Containers[0].Image,
				}},
			},
		}
		ginkgo.DeferCleanup(func() {
			err := clientSet.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		})
	})

	ginkgo.It("does not write to a pod recreated under the same name", func() {
		ginkgo.By("Creating pod " + pod.Name)
		stale, err := clientSet.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Deleting it and creating a new pod under the same name")
		err = clientSet.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Eventually(func() bool {
			_, err := clientSet.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, timeout, interval).Should(gomega.BeTrue(), "pod %s was not deleted", pod.Name)
		current, err := clientSet.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(current.UID).NotTo(gomega.Equal(stale.UID))

		ginkgo.By("Patching with the deleted pod's identity")
		err = util.PatchPodAnnotations(stale, map[string]string{annotation: "stale"})
		gomega.Expect(err).To(gomega.HaveOccurred(), "a patch computed for the deleted pod was accepted")

		current, err = clientSet.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(current.Annotations).NotTo(gomega.HaveKey(annotation),
			"a patch computed for the deleted pod landed on the new one")

		ginkgo.By("Patching with the new pod's identity")
		err = util.PatchPodAnnotations(current, map[string]string{annotation: "current"})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		current, err = clientSet.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(current.Annotations).To(gomega.HaveKeyWithValue(annotation, "current"))
	})
})
