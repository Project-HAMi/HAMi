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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/test/utils"
)

var _ = ginkgo.Describe("Pod E2E Tests", ginkgo.Ordered, func() {
	const (
		Namespace      = utils.GPUNameSpace
		NodeName       = utils.GPUNode
		NodeLabelKey   = utils.GPUNodeLabelKey
		NodeLabelValue = utils.GPUNodeLabelValue
		DeleteTimeout  = 300 * time.Second
		DeleteInterval = 10 * time.Second
	)

	var (
		clientSet = utils.GetClientSet()
		newPod    *corev1.Pod
	)

	ginkgo.BeforeAll(func() {
		ginkgo.By("Adding node labeling")
		_, err := utils.AddNodeLabel(clientSet, NodeName, NodeLabelKey, NodeLabelValue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		ginkgo.By("Cleanup pod after each test")
		cleanupPod(newPod, clientSet)
	})

	ginkgo.AfterAll(func() {
		ginkgo.By("Deleting node labeling")
		_, err := utils.RemoveNodeLabel(clientSet, NodeName, NodeLabelKey)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("creates a single pod with CUDA configuration", func() {
		newPod = utils.Pod.DeepCopy()
		newPod.Name += utils.GetRandom()

		// Ensure cleanup even if the test fails
		ginkgo.DeferCleanup(func() {
			ginkgo.By("DeferCleanup: Deleting pod after test")
			cleanupPod(newPod, clientSet)
		})

		createAndVerifyPod(newPod, clientSet)

		ginkgo.By("Verifying GPU memory in pod by executing: " + utils.GPUExecuteNvidiaSMI)
		output, err := utils.KubectlExecInPod(newPod.Namespace, newPod.Name, utils.GPUExecuteNvidiaSMI)
		if err != nil {
			fmt.Printf("nvidia-smi execution error: %v\n", err)
			fmt.Printf("nvidia-smi output: %s\n", output)
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "nvidia-smi command failed")

		fmt.Println("nvidia-smi output:")
		fmt.Println(string(output))

		ginkgo.By("Verifying CUDA execution in pod by executing: " + utils.GPUExecuteCudaSample)
		output, err = utils.KubectlExecInPod(newPod.Namespace, newPod.Name, utils.GPUExecuteCudaSample)
		if err != nil {
			fmt.Printf("CUDA sample execution error: %v\n", err)
			fmt.Printf("CUDA sample output: %s\n", output)
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "CUDA sample command failed")

		fmt.Println("CUDA sample output:")
		fmt.Println(string(output))
	})

	ginkgo.It("create overcommit pods", func() {
		newPod = prepareOvercommitPod(utils.Pod.DeepCopy(), Namespace) // Pass the namespace to the helper

		ginkgo.By("Creating overcommit pod in namespace " + Namespace)
		createdPod, err := utils.CreatePod(clientSet, newPod, Namespace)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(createdPod.Name).To(gomega.Equal(newPod.Name), "Pod was not created successfully")

		ginkgo.By("Verifying pod is pending due to filtering")
		gomega.Eventually(func() bool {
			return checkPodPendingDueToFiltering(clientSet, newPod)
		}, DeleteTimeout, DeleteInterval).Should(gomega.BeTrue())
	})
})

func cleanupPod(pod *corev1.Pod, clientSet *kubernetes.Clientset) {
	if podExists(pod.Namespace, pod.Name, clientSet) {
		ginkgo.By("Deleting pod " + pod.Name + " in namespace " + pod.Namespace)
		err := utils.DeletePod(clientSet, pod.Namespace, pod.Name)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Verifying pod " + pod.Name + " is deleted")
		gomega.Eventually(func() bool {
			return !podExists(pod.Namespace, pod.Name, clientSet)
		}, 300*time.Second, 10*time.Second).Should(gomega.BeTrue())
	}
}

func podExists(namespace, podName string, clientSet *kubernetes.Clientset) bool {
	pod, err := clientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	return err == nil && pod != nil
}

func createAndVerifyPod(pod *corev1.Pod, clientSet *kubernetes.Clientset) {
	ginkgo.By("Creating pod " + pod.Name + " in namespace " + pod.Namespace)
	createdPod, err := utils.CreatePod(clientSet, pod, pod.Namespace)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	gomega.Expect(createdPod.Name).To(gomega.Equal(pod.Name), "Pod was not created successfully")

	ginkgo.By("Verifying pod " + pod.Name + " is in running status")
	err = utils.WaitForPodRunning(clientSet, pod.Namespace, pod.Name)
	if err != nil {
		p, _ := clientSet.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		fmt.Printf("Pod %s/%s status: %v\n", pod.Namespace, pod.Name, p.Status)
		events, _ := clientSet.CoreV1().Events(pod.Namespace).List(context.TODO(), metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
		})
		for _, event := range events.Items {
			fmt.Printf("Event: %s - %s\n", event.Reason, event.Message)
		}
	}
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func prepareOvercommitPod(pod *corev1.Pod, namespace string) *corev1.Pod {
	pod.Name += utils.GetRandom()
	pod.Namespace = namespace // Ensure the pod's namespace is correctly set

	// Modify pod spec for overcommit scenario
	pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
	pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
	pod.Spec.Containers[1].Name = pod.Spec.Containers[0].Name + utils.GetRandom()
	pod.Spec.Containers[2].Name = pod.Spec.Containers[0].Name + utils.GetRandom()

	return pod
}

func checkPodPendingDueToFiltering(clientSet *kubernetes.Clientset, pod *corev1.Pod) bool {
	events, err := utils.GetPodEvents(clientSet, pod.Namespace, pod.Name)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	for _, event := range events {
		fmt.Printf("Event: Reason=%s, Message=%s\n", event.Reason, event.Message)
		if strings.Contains(event.Reason, utils.ErrReasonFilteringFailed) &&
			strings.Contains(event.Message, utils.ErrMessageFilteringFailed) {
			return true
		}
	}
	return false
}
