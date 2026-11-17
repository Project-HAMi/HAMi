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

package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

const (
	moveNodeX = "node-x"
	moveNodeY = "node-y"
	moveMem   = "hami.io/gpumem"
)

// initMoveDevices registers the NVIDIA backend with the resource names the
// move tests use.
func initMoveDevices(t *testing.T) {
	t.Helper()
	require.NoError(t, config.InitDevicesWithConfig(&config.Config{NvidiaConfig: nvidia.NvidiaConfig{
		ResourceCountName: "hami.io/gpu", ResourceMemoryName: moveMem,
		ResourceCoreName: "hami.io/gpucores", DefaultGPUNum: 1,
	}}))
}

// movePod returns a pod that asks for one GPU with 1000 MiB.
func movePod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "move-uid", Name: "move", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "main",
			Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
				"hami.io/gpu": *resource.NewQuantity(1, resource.BinarySI),
				moveMem:       *resource.NewQuantity(1000, resource.BinarySI),
			}},
		}}},
	}
}

// assignedTo returns pod as a Filter leaves it: on node, with mem MiB of
// node's first GPU.
func assignedTo(pod *corev1.Pod, node string, mem int64) *corev1.Pod {
	devices := device.EncodePodSingleDevice(device.PodSingleDevice{{{
		UUID: node + "-gpu-0", Type: nvidia.NvidiaGPUDevice, Usedmem: mem,
	}}})
	assigned := pod.DeepCopy()
	assigned.Annotations = map[string]string{
		util.AssignedNodeAnnotations:                    node,
		device.InRequestDevices[nvidia.NvidiaGPUDevice]: devices,
		device.SupportDevices[nvidia.NvidiaGPUDevice]:   devices,
	}
	return assigned
}

// moveReplica returns a replica that sees pods only through informer
// updates, with node-x and node-y holding one GPU each.
func moveReplica(t *testing.T) *Scheduler {
	t.Helper()
	initMoveDevices(t)
	nodes := newNodeManager()
	nodes.addNode(moveNodeX, testNodeInfo(moveNodeX, 1))
	nodes.addNode(moveNodeY, testNodeInfo(moveNodeY, 1))
	// NewQuotaManager is shared by the whole process, so each replica needs
	// its own.
	quotas := &device.QuotaManager{Quotas: map[string]*device.DeviceQuota{}}
	return &Scheduler{nodeManager: nodes, podManager: device.NewPodManager(), quotaManager: quotas}
}

// filterLeader returns a scheduler that runs Filter for pod against a fake
// API server, with node-x and node-y holding gpus GPUs each.
func filterLeader(t *testing.T, pod *corev1.Pod, gpus int) (*Scheduler, *fake.Clientset) {
	t.Helper()
	initMoveDevices(t)
	s := NewScheduler()
	s.quotaManager = &device.QuotaManager{Quotas: map[string]*device.DeviceQuota{}}
	kubeClient := fake.NewClientset()
	oldClient := client.KubeClient
	client.KubeClient = kubeClient
	t.Cleanup(func() { client.KubeClient = oldClient })
	s.kubeClient = kubeClient
	s.addNode(moveNodeX, testNodeInfo(moveNodeX, gpus))
	s.addNode(moveNodeY, testNodeInfo(moveNodeY, gpus))
	seedPods(t, s, pod)
	return s, kubeClient
}

// filterTo runs Filter for pod on node and returns the pod the API server
// holds afterwards.
func filterTo(t *testing.T, s *Scheduler, kubeClient *fake.Clientset, pod *corev1.Pod, node string) *corev1.Pod {
	t.Helper()
	res, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{node}})
	require.NoError(t, err)
	require.Equal(t, []string{node}, *res.NodeNames)
	live, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return live
}

// trackedNode returns the node s tracks pod on, or "" if it does not track it.
func trackedNode(s *Scheduler, pod *corev1.Pod) string {
	pi, ok := s.podManager.GetPod(pod)
	if !ok {
		return ""
	}
	return pi.NodeID
}

// trackedGPU returns the GPU s tracks pod on.
func trackedGPU(t *testing.T, s *Scheduler, pod *corev1.Pod) string {
	t.Helper()
	pi, ok := s.podManager.GetPod(pod)
	require.True(t, ok)
	return pi.Devices[nvidia.NvidiaGPUDevice][0][0].UUID
}

// gpuUsedmem returns the memory s accounts on each GPU of both nodes.
func gpuUsedmem(t *testing.T, s *Scheduler) map[string]int64 {
	t.Helper()
	probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "usage-probe", Namespace: "default"}}
	usage, _, _, err := s.getNodesUsage(&[]string{moveNodeX, moveNodeY}, probe)
	assert.NilError(t, err)
	used := map[string]int64{}
	for _, node := range *usage {
		for _, deviceList := range node.Devices.DeviceLists {
			used[deviceList.Device.ID] = deviceList.Device.Usedmem
		}
	}
	return used
}

// quotaUsed returns the GPU memory s counts against the default namespace.
func quotaUsed(s *Scheduler) int64 {
	quota := s.quotaManager.GetResourceQuota()["default"]
	if quota == nil || (*quota)[moveMem] == nil {
		return 0
	}
	return (*quota)[moveMem].Used
}

// TestOnUpdatePodFollowsMoveToAnotherNode is the regression test for #3435:
// a standby must follow a pod that a Filter moved to another node.
func TestOnUpdatePodFollowsMoveToAnotherNode(t *testing.T) {
	standby := moveReplica(t)
	// A percentage request can book a different amount on another GPU.
	onX := assignedTo(movePod(), moveNodeX, 1000)
	onY := assignedTo(movePod(), moveNodeY, 2000)

	standby.onAddPod(onX)
	assert.DeepEqual(t, gpuUsedmem(t, standby), map[string]int64{"node-x-gpu-0": 1000, "node-y-gpu-0": 0})
	// A refit write for the old allocation must not outlive it.
	standby.refitWrites = map[k8stypes.UID]refitWrite{onX.UID: {}}

	standby.onUpdatePod(onX, onY)
	assert.Equal(t, trackedNode(standby, onY), moveNodeY)
	assert.DeepEqual(t, gpuUsedmem(t, standby), map[string]int64{"node-x-gpu-0": 0, "node-y-gpu-0": 2000})
	assert.Equal(t, quotaUsed(standby), int64(2000))
	assert.Equal(t, len(standby.refitWrites), 0)
}

// TestOnUpdatePodResyncFollowsMove checks that an update whose old and new
// objects both show the new node still moves the pod, so a missed update
// heals on the next one.
func TestOnUpdatePodResyncFollowsMove(t *testing.T) {
	standby := moveReplica(t)
	onX := assignedTo(movePod(), moveNodeX, 1000)
	onY := assignedTo(movePod(), moveNodeY, 1000)

	standby.onAddPod(onX)
	standby.onUpdatePod(onY, onY)
	assert.Equal(t, trackedNode(standby, onY), moveNodeY)
	assert.DeepEqual(t, gpuUsedmem(t, standby), map[string]int64{"node-x-gpu-0": 0, "node-y-gpu-0": 1000})
}

// TestOnUpdatePodDropsUndecodableMove checks that a move to an allocation
// that cannot be decoded drops the old one, as a restart would not add the
// pod either.
func TestOnUpdatePodDropsUndecodableMove(t *testing.T) {
	standby := moveReplica(t)
	onX := assignedTo(movePod(), moveNodeX, 1000)
	broken := assignedTo(movePod(), moveNodeY, 1000)
	broken.Annotations[device.SupportDevices[nvidia.NvidiaGPUDevice]] = "node-y-gpu-0,NVIDIA,bad,0:;"

	standby.onAddPod(onX)
	standby.onUpdatePod(onX, broken)
	assert.Equal(t, trackedNode(standby, broken), "")
	assert.DeepEqual(t, gpuUsedmem(t, standby), map[string]int64{"node-x-gpu-0": 0, "node-y-gpu-0": 0})
	assert.Equal(t, quotaUsed(standby), int64(0))
}

// TestFilterKeepsOwnMoveOverOlderUpdates checks that the replica that moved
// a pod skips updates older than its Filter, then follows a later move by
// another replica.
func TestFilterKeepsOwnMoveOverOlderUpdates(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)

	onX := filterTo(t, s, kubeClient, pod, moveNodeX)
	s.onUpdatePod(pod, onX)
	assert.Equal(t, len(s.filterWrites), 0)

	// Bind marks the pod, the bind on node-x fails, and Filter runs again.
	binding := onX.DeepCopy()
	binding.Annotations[util.DeviceBindPhase] = util.DeviceBindAllocating
	onY := filterTo(t, s, kubeClient, binding, moveNodeY)
	assert.Equal(t, trackedNode(s, pod), moveNodeY)
	assert.Equal(t, len(s.filterWrites), 1)

	// The update for the bind is older than the second Filter.
	s.onUpdatePod(onX, binding)
	assert.Equal(t, trackedNode(s, pod), moveNodeY)
	assert.DeepEqual(t, gpuUsedmem(t, s), map[string]int64{"node-x-gpu-0": 0, "node-y-gpu-0": 1000})
	assert.Equal(t, len(s.filterWrites), 1)

	// Its own patch ends the wait.
	s.onUpdatePod(binding, onY)
	assert.Equal(t, len(s.filterWrites), 0)
	assert.Equal(t, trackedNode(s, pod), moveNodeY)

	// Another replica moves the pod back to node-x.
	back := assignedTo(onY, moveNodeX, 1000)
	s.onUpdatePod(onY, back)
	assert.Equal(t, trackedNode(s, pod), moveNodeX)
	assert.DeepEqual(t, gpuUsedmem(t, s), map[string]int64{"node-x-gpu-0": 1000, "node-y-gpu-0": 0})
	assert.Equal(t, quotaUsed(s), int64(1000))
}

// TestOnUpdatePodFollowsOtherWriteAfterSettle checks that the replica that
// ran Filter follows the pod once its own patch has not shown up for a
// minute, as when another replica wrote the pod since.
func TestOnUpdatePodFollowsOtherWriteAfterSettle(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)
	onX := filterTo(t, s, kubeClient, pod, moveNodeX)

	// The informer folds this replica's patch into a later one by another
	// replica that put the pod on node-y.
	other := assignedTo(onX, moveNodeY, 1000)
	s.onUpdatePod(pod, other)
	assert.Equal(t, trackedNode(s, pod), moveNodeX)

	write := s.filterWrites[pod.UID]
	write.at = write.at.Add(-filterWriteSettle)
	s.filterWrites[pod.UID] = write
	s.onUpdatePod(other, other)
	assert.Equal(t, trackedNode(s, pod), moveNodeY)
	assert.Equal(t, len(s.filterWrites), 0)
	assert.DeepEqual(t, gpuUsedmem(t, s), map[string]int64{"node-x-gpu-0": 0, "node-y-gpu-0": 1000})
}

// TestOnUpdatePodFollowsOtherWriteOnSameNodeAfterSettle checks the same when
// the other replica kept the node but picked another GPU.
func TestOnUpdatePodFollowsOtherWriteOnSameNodeAfterSettle(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 2)
	onX := filterTo(t, s, kubeClient, pod, moveNodeX)
	otherGPU := "node-x-gpu-0"
	if trackedGPU(t, s, pod) == otherGPU {
		otherGPU = "node-x-gpu-1"
	}
	devices := device.EncodePodSingleDevice(device.PodSingleDevice{{{
		UUID: otherGPU, Type: nvidia.NvidiaGPUDevice, Usedmem: 1000,
	}}})
	other := onX.DeepCopy()
	other.Annotations[device.InRequestDevices[nvidia.NvidiaGPUDevice]] = devices
	other.Annotations[device.SupportDevices[nvidia.NvidiaGPUDevice]] = devices

	write := s.filterWrites[pod.UID]
	write.at = write.at.Add(-filterWriteSettle)
	s.filterWrites[pod.UID] = write
	s.onUpdatePod(other, other)
	assert.Equal(t, trackedGPU(t, s, pod), otherGPU)
	assert.Equal(t, gpuUsedmem(t, s)[otherGPU], int64(1000))
	assert.Equal(t, quotaUsed(s), int64(1000))
}

// TestFilterPatchFailureRecordsNoWrite checks that a Filter whose patch
// fails leaves no write to wait for.
func TestFilterPatchFailureRecordsNoWrite(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)
	onX := filterTo(t, s, kubeClient, pod, moveNodeX)
	assert.Equal(t, len(s.filterWrites), 1)

	kubeClient.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected patch error")
	})
	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: onX, NodeNames: &[]string{moveNodeY}})
	assert.ErrorContains(t, err, "injected patch error")
	assert.Equal(t, len(s.filterWrites), 0)
	assert.Equal(t, trackedNode(s, pod), "")
}

// TestRemovePodAllocationDropsPendingFilterWrite checks that removing a pod
// does not leave its pending Filter write behind.
func TestRemovePodAllocationDropsPendingFilterWrite(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)
	onX := filterTo(t, s, kubeClient, pod, moveNodeX)
	assert.Check(t, len(s.filterWrites) == 1)

	s.removePodAllocation(onX)
	assert.Equal(t, len(s.filterWrites), 0)
}
