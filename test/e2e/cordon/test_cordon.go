package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/test/utils"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	cordonAnnotation   = "hami.io/device-cordon"
	cordonNamespace    = "hami-cordon-e2e"
	schedulerNamespace = "hami-system"
	schedulerSelector  = "app.kubernetes.io/component=hami-scheduler"
)

type vendorProfile struct {
	name                string
	registerAnnotation  string
	allocatedAnnotation string
	resourceName        corev1.ResourceName
}

// The specs read the vendor from the node instead of taking a flag, so the same
// suite runs against the NVIDIA and the AMD mock.
var vendorProfiles = []vendorProfile{
	{"NVIDIA", "hami.io/node-nvidia-register", "hami.io/vgpu-devices-allocated", "nvidia.com/gpu"},
	{"AMD", "hami.io/node-amd-register", "hami.io/amd-devices-allocated", "amd.com/gpu"},
}

type registeredDevice struct {
	ID string `json:"id"`
}

type allocationEntry struct {
	UUID  string
	Mem   int64
	Cores int64
}

var _ = ginkgo.Describe("device cordon", ginkgo.Ordered, func() {
	var (
		ctx       = context.Background()
		clientSet *kubernetes.Clientset
		profile   vendorProfile
		nodeName  string
		register  string
		devices   []registeredDevice
		scheduler string
	)

	patchNodeAnnotations := func(annotations map[string]any) {
		payload, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": annotations}})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		_, err = clientSet.CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, payload, metav1.PatchOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	cordonRaw := func(value string) {
		patchNodeAnnotations(map[string]any{cordonAnnotation: value})
	}
	cordon := func(ids ...string) {
		cordonRaw(strings.Join(ids, ","))
	}
	clearCordon := func() {
		patchNodeAnnotations(map[string]any{cordonAnnotation: nil})
	}
	ids := func() []string {
		out := make([]string, 0, len(devices))
		for _, device := range devices {
			out = append(out, device.ID)
		}
		return out
	}
	idsExcept := func(skip string) []string {
		out := make([]string, 0, len(devices)-1)
		for _, device := range devices {
			if device.ID != skip {
				out = append(out, device.ID)
			}
		}
		return out
	}

	newPod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cordonNamespace},
			Spec: corev1.PodSpec{
				SchedulerName: scheduler,
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name:            "sleep",
					Image:           envOr("CORDON_E2E_IMAGE", "ubuntu:22.04"),
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sh", "-c", "sleep 600"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{profile.resourceName: resource.MustParse("1")},
					},
				}},
			},
		}
	}
	create := func(name string) {
		_, err := utils.CreatePod(clientSet, newPod(name), cordonNamespace)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ginkgo.DeferCleanup(func() { _ = utils.DeletePod(clientSet, cordonNamespace, name) })
	}
	phaseOf := func(name string) corev1.PodPhase {
		pod, err := clientSet.CoreV1().Pods(cordonNamespace).Get(ctx, name, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return pod.Status.Phase
	}
	waitFor := func(name string, phase corev1.PodPhase) {
		err := wait.PollUntilContextTimeout(ctx, 3*time.Second, 5*time.Minute, true, func(context.Context) (bool, error) {
			got := phaseOf(name)
			if got == corev1.PodFailed && phase != corev1.PodFailed {
				return false, fmt.Errorf("pod %s failed", name)
			}
			return got == phase, nil
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	allocation := func(name string) []allocationEntry {
		var raw string
		gomega.Eventually(func() string {
			pod, err := clientSet.CoreV1().Pods(cordonNamespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return ""
			}
			raw = pod.Annotations[profile.allocatedAnnotation]
			return raw
		}, 2*time.Minute, 2*time.Second).ShouldNot(gomega.BeEmpty())
		parsed, err := parseAllocation(raw)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		return parsed
	}
	stuckWith := func(name, reason string) {
		gomega.Eventually(func() string {
			events, err := clientSet.CoreV1().Events(cordonNamespace).List(ctx, metav1.ListOptions{
				FieldSelector: "involvedObject.name=" + name + ",reason=FailedScheduling",
			})
			if err != nil {
				return ""
			}
			messages := make([]string, 0, len(events.Items))
			for _, event := range events.Items {
				messages = append(messages, event.Message)
			}
			return strings.Join(messages, "\n")
		}, 2*time.Minute, 3*time.Second).Should(gomega.ContainSubstring(reason))
		gomega.Expect(phaseOf(name)).To(gomega.Equal(corev1.PodPending))
	}
	restartScheduler := func() {
		pods, err := clientSet.CoreV1().Pods(schedulerNamespace).List(ctx, metav1.ListOptions{LabelSelector: schedulerSelector})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(pods.Items).NotTo(gomega.BeEmpty(), "no scheduler pod matching "+schedulerSelector)
		for _, pod := range pods.Items {
			err := clientSet.CoreV1().Pods(schedulerNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if !apierrors.IsNotFound(err) {
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}
		gomega.Eventually(func() int {
			pods, err := clientSet.CoreV1().Pods(schedulerNamespace).List(ctx, metav1.ListOptions{LabelSelector: schedulerSelector})
			if err != nil {
				return 0
			}
			ready := 0
			for _, pod := range pods.Items {
				if pod.DeletionTimestamp != nil {
					continue
				}
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						ready++
					}
				}
			}
			return ready
		}, 3*time.Minute, 3*time.Second).Should(gomega.BeNumerically(">=", 1))
	}

	ginkgo.BeforeAll(func() {
		clientSet = utils.GetClientSet()
		scheduler = envOr("HAMI_SCHEDULER_NAME", utils.HamiScheduler)

		nodes, err := clientSet.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		found := false
		for _, node := range nodes.Items {
			for _, candidate := range vendorProfiles {
				if value := node.Annotations[candidate.registerAnnotation]; value != "" {
					profile, nodeName, register, found = candidate, node.Name, value, true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			ginkgo.Skip("no node registered a GPU in " + vendorProfiles[0].registerAnnotation + " or " + vendorProfiles[1].registerAnnotation)
		}
		gomega.Expect(json.Unmarshal([]byte(register), &devices)).To(gomega.Succeed())
		gomega.Expect(devices).NotTo(gomega.BeEmpty())
		ginkgo.GinkgoWriter.Printf("Using %s node %s with %d registered devices\n", profile.name, nodeName, len(devices))

		_, err = clientSet.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   cordonNamespace,
				Labels: map[string]string{"pod-security.kubernetes.io/enforce": "privileged"},
			},
		}, metav1.CreateOptions{})
		if !apierrors.IsAlreadyExists(err) {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}
	})

	ginkgo.AfterAll(func() {
		clearCordon()
		_ = clientSet.CoreV1().Namespaces().Delete(ctx, cordonNamespace, metav1.DeleteOptions{})
	})

	ginkgo.It("E1: allocates only devices that are not cordoned", func() {
		ginkgo.DeferCleanup(clearCordon)
		free := devices[0].ID
		cordon(idsExcept(free)...)
		create("cordon-partial")
		waitFor("cordon-partial", corev1.PodRunning)
		gomega.Expect(allocation("cordon-partial")[0].UUID).To(gomega.Equal(free))
	})

	ginkgo.It("E2: reports CardCordoned when every device is cordoned", func() {
		ginkgo.DeferCleanup(clearCordon)
		cordon(ids()...)
		create("cordon-all")
		stuckWith("cordon-all", "CardCordoned")
	})

	ginkgo.It("E3: schedules again after the devices are uncordoned", func() {
		ginkgo.DeferCleanup(clearCordon)
		cordon(ids()...)
		create("cordon-uncordon")
		stuckWith("cordon-uncordon", "CardCordoned")
		clearCordon()
		waitFor("cordon-uncordon", corev1.PodRunning)
	})

	ginkgo.It("E4: leaves pods that are already running alone", func() {
		ginkgo.DeferCleanup(clearCordon)
		create("cordon-running")
		waitFor("cordon-running", corev1.PodRunning)
		cordon(ids()...)
		gomega.Consistently(func() corev1.PodPhase {
			return phaseOf("cordon-running")
		}, 20*time.Second, 2*time.Second).Should(gomega.Equal(corev1.PodRunning))
	})

	ginkgo.It("E5: tolerates whitespace and empty entries in the annotation", func() {
		ginkgo.DeferCleanup(clearCordon)
		cordoned := devices[0].ID
		cordonRaw(fmt.Sprintf(" , %s ,,", cordoned))
		create("cordon-messy")
		waitFor("cordon-messy", corev1.PodRunning)
		gomega.Expect(allocation("cordon-messy")[0].UUID).NotTo(gomega.Equal(cordoned))
	})

	ginkgo.It("E6: keeps the cordon across a scheduler restart", func() {
		ginkgo.DeferCleanup(clearCordon)
		free := devices[0].ID
		cordon(idsExcept(free)...)
		restartScheduler()
		// The AMD backend drops the register annotation when the fake devices fail a
		// health check; a real device plugin re-registers by itself.
		patchNodeAnnotations(map[string]any{profile.registerAnnotation: register})
		create("cordon-restart")
		waitFor("cordon-restart", corev1.PodRunning)
		gomega.Expect(allocation("cordon-restart")[0].UUID).To(gomega.Equal(free))
	})
})

func parseAllocation(raw string) ([]allocationEntry, error) {
	// uuid,type,mem,cores:...;  — only the first container is needed here.
	container := strings.SplitN(raw, ";", 2)[0]
	var out []allocationEntry
	for _, group := range strings.Split(container, ":") {
		fields := strings.Split(group, ",")
		if len(fields) < 4 {
			if strings.TrimSpace(group) == "" {
				continue
			}
			return nil, fmt.Errorf("unexpected allocation %q", group)
		}
		var entry allocationEntry
		entry.UUID = strings.TrimSpace(fields[0])
		if _, err := fmt.Sscan(fields[2], &entry.Mem); err != nil {
			return nil, err
		}
		if _, err := fmt.Sscan(fields[3], &entry.Cores); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
