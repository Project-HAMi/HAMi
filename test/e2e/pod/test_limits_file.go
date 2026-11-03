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
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/test/utils"
)

const (
	// checkLimitsFile prints whether the container has the limits file.
	checkLimitsFile = "[ -e /overrideEnv ] && echo present || echo absent"

	// freshNvidiaSMI runs nvidia-smi with an empty environment, the way SSH,
	// su -, sudo and cron start a session, and prints the GPU's UUID and total
	// memory, such as "GPU-..., 300 MiB". It removes the pod's shared cache
	// first, so the limit can't come from a process that ran before.
	freshNvidiaSMI = `rm -f /usr/local/vgpu/usage.cache && ` +
		`env -i "$(command -v nvidia-smi)" --query-gpu=uuid,memory.total --format=csv,noheader 2>/dev/null`
)

var _ = ginkgo.Describe("Limits file E2E Tests", ginkgo.Ordered, func() {
	var (
		clientSet *kubernetes.Clientset
		newPod    *corev1.Pod
		nodeName  string
		// oldLabel and hadLabel keep the node's gpu label from before the
		// tests, so AfterAll can leave the node as it found it. labelRead
		// stays false if BeforeAll failed before it could read the label.
		oldLabel  string
		hadLabel  bool
		labelRead bool
	)

	podLimit := utils.GPUPodMemory + " " + utils.GPUPodMemoryUnit

	ginkgo.BeforeAll(func() {
		clientSet = utils.GetClientSet()

		var err error
		nodeName, err = utils.GetGPUNode(clientSet)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to find GPU node")
		fmt.Printf("Using GPU node: %s\n", nodeName)

		node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		oldLabel, hadLabel = node.Labels[utils.GPUNodeLabelKey]
		labelRead = true

		ginkgo.By("Adding node labeling")
		_, err = utils.AddNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey, utils.GPUNodeLabelValue)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Waiting for device-plugin to register healthy GPU devices")
		err = utils.WaitForDevicePluginReady(clientSet, nodeName, 5*time.Minute, 2*time.Second)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "device-plugin did not become ready on node %s", nodeName)
	})

	ginkgo.AfterEach(func() {
		if newPod != nil {
			ginkgo.By("Cleanup pod after each test")
			cleanupPod(newPod, clientSet)
		}
	})

	ginkgo.AfterAll(func() {
		if !labelRead {
			// The label was never read, so its old value is unknown. Leave it alone.
			return
		}
		if hadLabel {
			ginkgo.By("Restoring node labeling")
			_, err := utils.AddNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey, oldLabel)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			return
		}
		ginkgo.By("Deleting node labeling")
		_, err := utils.RemoveNodeLabel(clientSet, nodeName, utils.GPUNodeLabelKey)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("mounts the limits file and limits a process with an empty environment", func() {
		newPod = limitsTestPod("")
		createAndVerifyPod(newPod, clientSet)

		ginkgo.By("Reading /overrideEnv")
		output, err := utils.KubectlExecInPod(newPod.Namespace, newPod.Name, "cat /overrideEnv")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(string(output)).To(gomega.ContainSubstring("CUDA_DEVICE_MEMORY_LIMIT_0=" + utils.GPUPodMemory + "m"))
		gomega.Expect(string(output)).To(gomega.ContainSubstring("CUDA_DEVICE_MEMORY_SHARED_CACHE=/usr/local/vgpu/usage.cache"))
		gomega.Expect(string(output)).To(gomega.ContainSubstring("HAMI_LIMITS_FORMAT=1"))

		ginkgo.By("Checking /overrideEnv is read-only")
		_, err = utils.KubectlExecInPod(newPod.Namespace, newPod.Name, "echo x >> /overrideEnv")
		gomega.Expect(err).To(gomega.HaveOccurred(), "/overrideEnv should not be writable")

		ginkgo.By("Checking the limit with an empty environment")
		_, memory := freshGPUMemory(newPod)
		gomega.Expect(memory).To(gomega.Equal(podLimit))
	})

	ginkgo.It("mounts no limits file when the pod asks for env", func() {
		newPod = limitsTestPod(string(nvidia.LimitDeliveryEnv))
		createAndVerifyPod(newPod, clientSet)

		output, err := utils.KubectlExecInPod(newPod.Namespace, newPod.Name, checkLimitsFile)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(strings.TrimSpace(string(output))).To(gomega.Equal("absent"))

		ginkgo.By("Checking an empty environment sees the whole GPU in env mode")
		uuid, memory := freshGPUMemory(newPod)
		wholeGPU := fmt.Sprintf("%d %s", registeredMemory(clientSet, newPod, uuid), utils.GPUPodMemoryUnit)
		gomega.Expect(memory).To(gomega.Equal(wholeGPU))
	})

	ginkgo.It("ignores an unknown annotation value and mounts the limits file", func() {
		newPod = limitsTestPod("ENV")
		createAndVerifyPod(newPod, clientSet)

		output, err := utils.KubectlExecInPod(newPod.Namespace, newPod.Name, checkLimitsFile)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(strings.TrimSpace(string(output))).To(gomega.Equal("present"))
	})
})

// limitsTestPod returns the default test pod, with the limit-delivery
// annotation set when delivery is not empty.
func limitsTestPod(delivery string) *corev1.Pod {
	pod := utils.Pod.DeepCopy()
	pod.Name += utils.GetRandom()
	if delivery != "" {
		pod.Annotations = map[string]string{nvidia.LimitDeliveryAnnotation: delivery}
	}
	return pod
}

func freshGPUMemory(pod *corev1.Pod) (string, string) {
	output, err := utils.KubectlExecInPod(pod.Namespace, pod.Name, freshNvidiaSMI)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	uuid, memory, found := strings.Cut(strings.TrimSpace(string(output)), ", ")
	gomega.Expect(found).To(gomega.BeTrue(), "unexpected nvidia-smi output: %q", output)
	return uuid, memory
}

func registeredMemory(clientSet *kubernetes.Clientset, pod *corev1.Pod, uuid string) int64 {
	created, err := clientSet.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	node, err := clientSet.CoreV1().Nodes().Get(context.TODO(), created.Spec.NodeName, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	devices, err := device.UnMarshalNodeDevices(node.Annotations[nvidia.RegisterAnnos])
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	for _, d := range devices {
		if d.ID == uuid {
			return d.Devmem
		}
	}
	ginkgo.Fail(fmt.Sprintf("GPU %s is not registered on node %s", uuid, node.Name))
	return 0
}
