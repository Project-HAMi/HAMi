package e2e

import (
	"context"
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
		nodes, err := utils.GetNodes(clientSet)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(len(nodes.Items)).To(gomega.BeNumerically(">", 0), "No nodes available for testing")

		nodeName = nodes.Items[0].Name
	})

	ginkgo.It("verify node with labeling", func() {
		ginkgo.By("Updating node " + nodeName + " by labeling " + utils.GPUNodeLabelKey + "=" + utils.GPUNodeLabelValue)
		_, err := utils.AddNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey, utils.GPUNodeLabelValue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Checking node " + nodeName + " label")
		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(node.Labels[utils.GPUNodeLabelKey]).To(gomega.Equal(utils.GPUNodeLabelValue), "Label was not correctly added")

		ginkgo.By("Checking pods " + utils.HamiDevicePlugin + " running after labeling")
		gomega.Eventually(func() bool {
			pods, err := utils.GetPods(clientSet, utils.GPUNameSpace)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, pod := range pods.Items {
				err := utils.WaitForPodRunning(clientSet, utils.GPUNameSpace, pod.Name)
				if err != nil {
					return false
				}
				return true
			}
			return false
		}, 300*time.Second, 10*time.Second).Should(gomega.BeTrue())
	})

	ginkgo.It("verify node after removing label", func() {
		ginkgo.By("Updating node " + nodeName + " by removing label " + utils.GPUNodeLabelKey + "=" + utils.GPUNodeLabelValue)
		_, err := utils.RemoveNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Checking node " + nodeName + " label")
		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, exists := node.Labels[utils.GPUNodeLabelKey]
		gomega.Expect(exists).To(gomega.BeFalse(), "Label was not correctly removed")

		ginkgo.By("Checking pods " + utils.HamiDevicePlugin + " deleted after removing label")
		gomega.Eventually(func() bool {
			pods, err := utils.GetPods(clientSet, utils.GPUNameSpace)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			for _, pod := range pods.Items {
				if strings.Contains(pod.Name, utils.HamiDevicePlugin) {
					return false
				}
			}
			return true
		}, 300*time.Second, 10*time.Second).Should(gomega.BeTrue())
	})
})
