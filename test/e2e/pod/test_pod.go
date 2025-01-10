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

package e2e

import (
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/test/utils"
)

var _ = ginkgo.Describe("Pod E2E Tests", ginkgo.Ordered, func() {
	var clientSet = utils.GetClientSet()
	var newPod *corev1.Pod

	ginkgo.BeforeAll(func() {
		ginkgo.By("Add node labeling")
		_, err := utils.AddNodeLabel(clientSet, utils.GPUNode, utils.GPUNodeLabelKey, utils.GPUNodeLabelValue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Deleting pod " + newPod.Name + " in namespace " + newPod.Namespace)
		err := utils.DeletePod(clientSet, newPod.Namespace, newPod.Name)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Verifying pod " + newPod.Name + " is deleted")
		gomega.Eventually(func() bool {
			pods, err := utils.GetPods(clientSet, utils.GPUNameSpace)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, pod := range pods.Items {
				if strings.Contains(pod.Name, newPod.Name) {
					return false
				}
				return true
			}
			return false
		}, 300*time.Second, 10*time.Second).Should(gomega.BeTrue())
	})

	ginkgo.AfterAll(func() {
		ginkgo.By("Delete node labeling")
		_, err := utils.RemoveNodeLabel(clientSet, utils.GPUNode, utils.GPUNodeLabelKey)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("create single pod with CUDA configuration", func() {
		newPod = utils.Pod.DeepCopy()
		newPod.Name = newPod.Name + utils.GetRandom()

		ginkgo.By("Creating pod " + newPod.Name + " in namespace " + newPod.Namespace)
		createdPod, err := utils.CreatePod(clientSet, newPod, newPod.Namespace)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdPod.Name).To(gomega.Equal(newPod.Name), "Pod was not created successfully")

		ginkgo.By("Verifying pod " + newPod.Name + " in running status")
		err = utils.WaitForPodRunning(clientSet, newPod.Namespace, newPod.Name)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Verifying GPU memory in pod " + newPod.Name + " by executing: " + utils.GPUExecuteNvidiaSMI)
		output, err := utils.KubectlExecInPod(newPod.Namespace, newPod.Name, utils.GPUExecuteNvidiaSMI)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(string(output)).To(gomega.ContainSubstring(utils.GPUPodMemory + utils.GPUPodMemoryUnit))

		ginkgo.By("Verifying CUDA execution status in pod " + newPod.Name + " by executing: " + utils.GPUExecuteCudaSample)
		output, err = utils.KubectlExecInPod(newPod.Namespace, newPod.Name, utils.GPUExecuteCudaSample)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(string(output)).To(gomega.ContainSubstring(utils.GPUCudaTestPass))
	})

	ginkgo.It("create overcommit pods", func() {
		newPod = utils.Pod.DeepCopy()
		newPod.Name = newPod.Name + utils.GetRandom()
		newPod.Spec.Containers = append(newPod.Spec.Containers, newPod.Spec.Containers[0])
		newPod.Spec.Containers = append(newPod.Spec.Containers, newPod.Spec.Containers[0])
		newPod.Spec.Containers[1].Name = newPod.Spec.Containers[0].Name + utils.GetRandom()
		newPod.Spec.Containers[2].Name = newPod.Spec.Containers[0].Name + utils.GetRandom()

		ginkgo.By("Creating pod " + newPod.Name + " within multiple containers in namespace " + newPod.Namespace)
		createdPod, err := utils.CreatePod(clientSet, newPod, newPod.Namespace)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdPod.Name).To(gomega.Equal(newPod.Name), "Pod was not created successfully")

		ginkgo.By("Verifying pod " + newPod.Name + " is pending due to " + utils.ErrReasonFilteringFailed + utils.ErrMessageFilteringFailed)
		gomega.Eventually(func() bool {
			events, err := utils.GetPodEvents(clientSet, newPod.Namespace, newPod.Name)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, event := range events {
				if strings.Contains(event.Reason, utils.ErrReasonFilteringFailed) && strings.Contains(event.Message, utils.ErrMessageFilteringFailed) {
					return true
				}
			}
			return false
		}, 300*time.Second, 10*time.Second).Should(gomega.BeTrue())

	})
})
