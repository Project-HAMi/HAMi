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
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/awsneuron"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func TestFilterNeuronInitReservationPreservesCoreMasks(t *testing.T) {
	oldDevices, oldSupport, oldClient := device.DevicesMap, maps.Clone(device.SupportDevices), client.KubeClient
	t.Cleanup(func() {
		device.DevicesMap, device.SupportDevices, client.KubeClient = oldDevices, oldSupport, oldClient
	})
	neuron := awsneuron.InitAWSNeuronDevice(awsneuron.AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron", ResourceCoreName: "aws.amazon.com/neuroncore",
	})
	device.DevicesMap = map[string]device.Devices{awsneuron.AWSNeuronDevice: neuron}
	container := func(name, cores string) corev1.Container {
		return corev1.Container{Name: name, Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{"aws.amazon.com/neuroncore": resource.MustParse(cores)},
		}}
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "init-and-app", Namespace: "neuron-mask-reservation", UID: "init-and-app"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{container("init", "2")},
			Containers:     []corev1.Container{container("app", "1")},
		},
	}
	follower := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "follower", Namespace: pod.Namespace, UID: "follower"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{container("app", "1")}},
	}
	s := schedulerWithPods(t, pod, follower)
	s.eventRecorder = record.NewFakeRecorder(100)
	client.KubeClient = s.kubeClient
	t.Cleanup(func() { delete(s.quotaManager.Quotas, pod.Namespace) })
	// One Inferentia1 device, with core 1 already occupied: the only contiguous
	// two-core range is 2-3, but the app's one-core request selects core 0.
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "neuron-node", Labels: map[string]string{"node.kubernetes.io/instance-type": "inf1.xlarge"}},
		Status: corev1.NodeStatus{Capacity: corev1.ResourceList{
			"aws.amazon.com/neuron": resource.MustParse("1"), "aws.amazon.com/neuroncore": resource.MustParse("4"),
		}},
	}
	inventory, err := neuron.GetNodeDevices(*node)
	require.NoError(t, err)
	s.addNode(node.Name, &device.NodeInfo{ID: node.Name, Node: node,
		Devices: map[string][]device.DeviceInfo{awsneuron.AWSNeuronDevice: {*inventory[0]}},
	})
	background := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "background", Namespace: pod.Namespace, UID: "background"}}
	backgroundDevices := device.PodDevices{awsneuron.AWSNeuronDevice: {{{
		UUID: inventory[0].ID, Type: awsneuron.AWSNeuronDevice, Usedcores: 0b0010,
	}}}}
	require.True(t, s.podManager.AddPod(background, node.Name, backgroundDevices))
	s.quotaManager.AddUsage(background, backgroundDevices)
	names := []string{node.Name}
	result, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &names})
	require.NoError(t, err)
	require.Equal(t, &names, result.NodeNames)
	stored, err := s.kubeClient.CoreV1().Pods(pod.Namespace).Get(t.Context(), pod.Name, metav1.GetOptions{})
	require.NoError(t, err)
	allocations, err := device.DecodePodDevices(device.SupportDevices, stored.Annotations)
	require.NoError(t, err)
	rows := allocations[awsneuron.AWSNeuronDevice]
	require.GreaterOrEqual(t, len(rows), 2)
	require.Equal(t, int64(0b1100), rows[0][0].Usedcores)
	require.Equal(t, int64(0b0001), rows[1][0].Usedcores)
	pi, ok := s.podManager.GetPod(pod)
	require.True(t, ok)
	t.Logf("init mask=%04b, app mask=%04b, cached reservation mask=%04b", rows[0][0].Usedcores,
		rows[1][0].Usedcores, pi.Devices[awsneuron.AWSNeuronDevice][0][0].Usedcores)
	result, err = s.Filter(extenderv1.ExtenderArgs{Pod: follower, NodeNames: &names})
	require.NoError(t, err)
	require.True(t, result.NodeNames == nil || len(*result.NodeNames) == 0,
		"all four cores are reserved; follower must not receive the app's core 0: %+v", result)
	require.Equal(t, int64(0b1101), pi.Devices[awsneuron.AWSNeuronDevice][0][0].Usedcores)
	require.Equal(t, node.Name, stored.Annotations[util.AssignedNodeAnnotations])
	// Replay the persisted annotation into an empty cache, as after a scheduler
	// restart. Reconstructing usage must preserve the same reservation.
	s.removePodAllocation(stored)
	s.onAddPod(stored)
	pi, ok = s.podManager.GetPod(stored)
	require.True(t, ok)
	require.Equal(t, int64(0b1101), pi.Devices[awsneuron.AWSNeuronDevice][0][0].Usedcores)
	require.Equal(t, int64(4), replayQuotaUsage(s, pod.Namespace, "aws.amazon.com/neuroncore"))
	result, err = s.Filter(extenderv1.ExtenderArgs{Pod: follower, NodeNames: &names})
	require.NoError(t, err)
	require.True(t, result.NodeNames == nil || len(*result.NodeNames) == 0)

	// Only the completed init container's exclusive cores become available.
	completed := stored.DeepCopy()
	completed.Spec.NodeName = node.Name
	completed.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
	}}}
	s.onUpdatePod(stored, completed)
	pi, ok = s.podManager.GetPod(completed)
	require.True(t, ok)
	require.Equal(t, int64(0b0001), pi.Devices[awsneuron.AWSNeuronDevice][0][0].Usedcores)
	require.Equal(t, int64(2), replayQuotaUsage(s, pod.Namespace, "aws.amazon.com/neuroncore"))
	result, err = s.Filter(extenderv1.ExtenderArgs{Pod: follower, NodeNames: &names})
	require.NoError(t, err)
	require.Equal(t, &names, result.NodeNames)
	followerInfo, ok := s.podManager.GetPod(follower)
	require.True(t, ok)
	require.Equal(t, int64(0b0100), followerInfo.Devices[awsneuron.AWSNeuronDevice][0][0].Usedcores)
	s.onDelPod(completed)
	s.onDelPod(completed)
	require.Equal(t, int64(2), replayQuotaUsage(s, pod.Namespace, "aws.amazon.com/neuroncore"))
}

func TestNeuronInitReservationQuota(t *testing.T) {
	oldDevices, oldSupport := device.DevicesMap, maps.Clone(device.SupportDevices)
	t.Cleanup(func() { device.DevicesMap, device.SupportDevices = oldDevices, oldSupport })
	device.DevicesMap = map[string]device.Devices{awsneuron.AWSNeuronDevice: awsneuron.InitAWSNeuronDevice(awsneuron.AWSNeuronConfig{
		ResourceCountName: "aws.amazon.com/neuron", ResourceCoreName: "aws.amazon.com/neuroncore",
	})}
	quota := device.NewQuotaManager()
	const namespace = "neuron-init-reservation-quota"
	t.Cleanup(func() { delete(quota.Quotas, namespace) })
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace},
		Spec:       corev1.PodSpec{InitContainers: []corev1.Container{{Name: "init"}}, Containers: []corev1.Container{{Name: "app"}}},
	}
	requests := device.PodDeviceRequests{
		{awsneuron.AWSNeuronDevice: {Type: awsneuron.AWSNeuronDevice, Nums: 1, TotalCoresreq: 2}},
		{awsneuron.AWSNeuronDevice: {Type: awsneuron.AWSNeuronDevice, Nums: 1, TotalCoresreq: 1}},
	}
	for _, tc := range []struct {
		name                       string
		usedMask, usedCores, limit int64
		fit                        bool
	}{
		{name: "disjoint masks exceed quota", usedMask: 0b0010, usedCores: 1, limit: 3},
		{name: "disjoint masks fit quota", usedMask: 0b0010, usedCores: 1, limit: 4, fit: true},
		{name: "reused cores counted once", limit: 2, fit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quota.Quotas[namespace] = &device.DeviceQuota{
				"aws.amazon.com/neuroncore": &device.Quota{Used: tc.usedCores, Limit: tc.limit, LimitSet: true},
			}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "neuron-node"}}
			usage := &NodeUsage{
				Node: node, NodeInfo: &device.NodeInfo{ID: node.Name, Node: node},
				Devices: policy.DeviceUsageList{DeviceLists: []*policy.DeviceListsScore{{
					Device: &device.DeviceUsage{ID: "neuron-0", Type: awsneuron.AWSNeuronDevice,
						Health: true, Count: 4, Totalcore: 0b1111, Usedcores: tc.usedMask},
				}}},
			}
			result := (&Scheduler{quotaManager: quota}).scoreNode(node.Name, usage, requests, pod,
				util.NodeSchedulerPolicyBinpack.String(), nil, util.DeviceScoringWeights{})
			require.Equal(t, tc.fit, result.score != nil, "reason: %s", result.reason)
			if !tc.fit {
				require.Equal(t, "AWS NeuronCore quota exceeded", result.reason)
			}
		})
	}
}
