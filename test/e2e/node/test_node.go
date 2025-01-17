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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/test/utils"
)

var _ = ginkgo.Describe("[Node] Node E2E Tests", ginkgo.Ordered, func() {
	var clientSet = utils.GetClientSet()
	var nodeName string

	ginkgo.BeforeAll(func() {
		// Get all nodes in the cluster
		nodes, err := utils.GetNodes(clientSet)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(len(nodes.Items)).To(gomega.BeNumerically(">", 0), "No nodes available for testing")

		// Select a node with GPU resources
		for _, node := range nodes.Items {
			if _, exists := node.Status.Capacity["nvidia.com/gpu"]; exists {
				nodeName = node.Name
				break
			}
		}
		gomega.Expect(nodeName).NotTo(gomega.BeEmpty(), "No GPU node found")
	})

	ginkgo.It("verify node with labeling", func() {
		// Add label to the node
		ginkgo.By("Updating node " + nodeName + " by labeling " + utils.GPUNodeLabelKey + "=" + utils.GPUNodeLabelValue)
		_, err := utils.AddNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey, utils.GPUNodeLabelValue)
		if err != nil {
			fmt.Printf("Failed to add label to node %s: %v\n", nodeName, err)
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify that the label was added correctly
		ginkgo.By("Checking node " + nodeName + " label")
		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(node.Labels[utils.GPUNodeLabelKey]).To(gomega.Equal(utils.GPUNodeLabelValue), "Label was not correctly added")

		// Verify that the HamiDevicePlugin pods are running after labeling
		ginkgo.By("Checking pods " + utils.HamiDevicePlugin + " running after labeling")
		gomega.Eventually(func() bool {
			pods, err := utils.GetPods(clientSet, utils.GPUNameSpace)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, pod := range pods.Items {
				if strings.Contains(pod.Name, utils.HamiDevicePlugin) {
					err := utils.WaitForPodRunning(clientSet, utils.GPUNameSpace, pod.Name)
					if err != nil {
						fmt.Printf("Pod %s is not running: %v\n", pod.Name, err)
						return false
					}
				}
			}
			return true
		}, 300*time.Second, 10*time.Second).Should(gomega.BeTrue())
	})

	ginkgo.It("verify node after removing label", func() {
		// Remove label from the node
		ginkgo.By("Updating node " + nodeName + " by removing label " + utils.GPUNodeLabelKey + "=" + utils.GPUNodeLabelValue)
		_, err := utils.RemoveNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey)
		if err != nil {
			fmt.Printf("Failed to remove label from node %s: %v\n", nodeName, err)
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify that the label was removed correctly
		ginkgo.By("Checking node " + nodeName + " label")
		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, exists := node.Labels[utils.GPUNodeLabelKey]
		gomega.Expect(exists).To(gomega.BeFalse(), "Label was not correctly removed")

		// Verify that the HamiDevicePlugin pods are deleted after removing the label
		ginkgo.By("Checking pods " + utils.HamiDevicePlugin + " deleted after removing label")
		gomega.Eventually(func() bool {
			pods, err := utils.GetPods(clientSet, utils.GPUNameSpace)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, pod := range pods.Items {
				if strings.Contains(pod.Name, utils.HamiDevicePlugin) {
					fmt.Printf("Pod %s still exists\n", pod.Name)
					return false
				}
			}
			return true
		}, 300*time.Second, 10*time.Second).Should(gomega.BeTrue())
	})
})
