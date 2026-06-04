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

package preempt

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func init() {
	if device.DevicesMap == nil {
		device.DevicesMap = make(map[string]device.Devices)
	}
}

type mockDevice struct {
	devices            []*device.DeviceInfo
	failGetNodeDevices bool
	fitReturnsTrue     bool
}

func (m *mockDevice) CommonWord() string { return "nvidia" }

func (m *mockDevice) MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error) {
	return false, nil
}

func (m *mockDevice) CheckHealth(devType string, n *corev1.Node) (bool, bool) { return true, false }
func (m *mockDevice) NodeCleanUp(nn string) error                             { return nil }

func (m *mockDevice) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  "hami.io/vgpu",
		ResourceMemoryName: "hami.io/vgpu-memory",
		ResourceCoreName:   "hami.io/vgpu-cores",
	}
}

func (m *mockDevice) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	if m.failGetNodeDevices {
		return nil, errors.New("simulated hardware driver error")
	}
	return m.devices, nil
}

func (m *mockDevice) LockNode(n *corev1.Node, p *corev1.Pod) error        { return nil }
func (m *mockDevice) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error { return nil }

func (m *mockDevice) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	req := device.ContainerDeviceRequest{}
	if qty, ok := ctr.Resources.Requests["nvidia.com/gpu"]; ok {
		req.Nums = int32(qty.Value())
	}
	if qty, ok := ctr.Resources.Requests["hami.io/vgpu"]; ok {
		req.Nums = int32(qty.Value())
	}
	if qty, ok := ctr.Resources.Requests["hami.io/vgpu-memory"]; ok {
		req.Memreq = int32(qty.Value())
	}
	if qty, ok := ctr.Resources.Requests["hami.io/vgpu-cores"]; ok {
		req.Coresreq = int32(qty.Value())
	}
	return req
}

func (m *mockDevice) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	return nil
}

func (m *mockDevice) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (m *mockDevice) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	return nil
}

func (m *mockDevice) Fit(
	usages []*device.DeviceUsage,
	req device.ContainerDeviceRequest,
	pod *corev1.Pod,
	nodeInfo *device.NodeInfo,
	allocated *device.PodDevices,
) (bool, map[string]device.ContainerDevices, string) {
	if !m.fitReturnsTrue {
		return false, nil, "insufficient mock device resources"
	}
	for _, du := range usages {
		if du.Count-du.Used >= 1 &&
			du.Totalmem-du.Usedmem >= req.Memreq &&
			du.Totalcore-du.Usedcores >= req.Coresreq {
			alloc := device.ContainerDevices{{
				UUID:      du.ID,
				Type:      "nvidia",
				Usedmem:   req.Memreq,
				Usedcores: req.Coresreq,
			}}
			return true, map[string]device.ContainerDevices{"compute-container": alloc}, ""
		}
	}
	return false, nil, "no device with enough resources"
}

func newMockDevice(count, mem, cores int32) *mockDevice {
	return &mockDevice{
		fitReturnsTrue: true,
		devices: []*device.DeviceInfo{
			{
				ID:      "mock-gpu-uuid-1",
				Index:   0,
				Count:   count,
				Devmem:  mem,
				Devcore: cores,
				Type:    "nvidia",
				Health:  true,
			},
		},
	}
}

func newDefaultMockDevice() *mockDevice {
	return newMockDevice(10, 20000, 100)
}

func registerMock(m *mockDevice) func() {
	devs := device.GetDevices()
	old, existed := devs["nvidia"]
	devs["nvidia"] = m
	return func() {
		if existed {
			devs["nvidia"] = old
		} else {
			delete(devs, "nvidia")
		}
	}
}

func newTestNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceName("hami.io/vgpu"):        resource.MustParse("2"),
				corev1.ResourceName("hami.io/vgpu-memory"): resource.MustParse("20000"),
				corev1.ResourceName("hami.io/vgpu-cores"):  resource.MustParse("200"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceName("hami.io/vgpu"):        resource.MustParse("2"),
				corev1.ResourceName("hami.io/vgpu-memory"): resource.MustParse("20000"),
				corev1.ResourceName("hami.io/vgpu-cores"):  resource.MustParse("200"),
			},
		},
	}
}

type podOpt func(*corev1.Pod)

func withPriority(p int32) podOpt     { return func(pod *corev1.Pod) { pod.Spec.Priority = &p } }
func withNodeName(node string) podOpt { return func(pod *corev1.Pod) { pod.Spec.NodeName = node } }
func withDeletionTimestamp() podOpt {
	return func(pod *corev1.Pod) {
		now := metav1.NewTime(time.Now())
		pod.DeletionTimestamp = &now
	}
}
func withOwner(kind string) podOpt {
	return func(pod *corev1.Pod) {
		ctrl := true
		pod.OwnerReferences = []metav1.OwnerReference{{
			Kind: kind, Name: "test-owner", UID: types.UID(uuid.NewString()), APIVersion: "apps/v1", Controller: &ctrl,
		}}
	}
}
func withNamespace(ns string) podOpt { return func(pod *corev1.Pod) { pod.Namespace = ns } }
func withLabels(labels map[string]string) podOpt {
	return func(pod *corev1.Pod) { pod.Labels = labels }
}
func withAnnotations(annot map[string]string) podOpt {
	return func(pod *corev1.Pod) { pod.Annotations = annot }
}

type vgpuRequest struct{ vgpu, mem, cores int64 }

func newVGPUPod(name string, req vgpuRequest, opts ...podOpt) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(uuid.NewString())},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "compute-container",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("hami.io/vgpu"):        resource.MustParse(fmt.Sprintf("%d", req.vgpu)),
						corev1.ResourceName("hami.io/vgpu-memory"): resource.MustParse(fmt.Sprintf("%d", req.mem)),
						corev1.ResourceName("hami.io/vgpu-cores"):  resource.MustParse(fmt.Sprintf("%d", req.cores)),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, o := range opts {
		o(pod)
	}
	return pod
}

func newNativeGPUPod(name string, gpuCount int, opts ...podOpt) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(uuid.NewString())},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "gpu-container",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse(fmt.Sprintf("%d", gpuCount)),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, o := range opts {
		o(pod)
	}
	return pod
}

func newMultiContainerPod(name string, containers []corev1.Container, opts ...podOpt) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(uuid.NewString())},
		Spec:       corev1.PodSpec{Containers: containers},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, o := range opts {
		o(pod)
	}
	return pod
}

func newPDB(name, namespace string, selector *metav1.LabelSelector, minAvailable int32) *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: minAvailable},
			Selector:     selector,
		},
		Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
	}
}

func newPreemptPluginWithSync(t *testing.T, pods []*corev1.Pod, nodes []*corev1.Node, pdbs ...*policyv1.PodDisruptionBudget) (*VgpuPreempt, context.CancelFunc) {
	t.Helper()
	objs := make([]runtime.Object, 0, len(pods)+len(nodes)+len(pdbs))
	for _, p := range pods {
		objs = append(objs, p)
	}
	for _, n := range nodes {
		objs = append(objs, n)
	}
	for _, pdb := range pdbs {
		objs = append(objs, pdb)
	}
	k8sClient := fake.NewClientset(objs...)
	factory := informers.NewSharedInformerFactory(k8sClient, 0)
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "test-scheduler-extender"})

	plugin, err := New(factory, recorder)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	cleanup := func() {
		cancel()
		broadcaster.Shutdown()
	}
	return plugin, cleanup
}

func TestIsProtectedFromPreemption(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{"normal pod", newVGPUPod("p", vgpuRequest{1, 1000, 10}, withPriority(10), withNodeName("n1")), false},
		{"terminating", newVGPUPod("p", vgpuRequest{1, 1000, 10}, withPriority(10), withNodeName("n1"), withDeletionTimestamp()), true},
		{"DaemonSet owned", newVGPUPod("p", vgpuRequest{1, 1000, 10}, withPriority(10), withNodeName("n1"), withOwner("DaemonSet")), true},
		{"ReplicaSet owned", newVGPUPod("p", vgpuRequest{1, 1000, 10}, withPriority(10), withNodeName("n1"), withOwner("ReplicaSet")), false},
		{"critical priority", newVGPUPod("p", vgpuRequest{1, 1000, 10}, withPriority(2000000001), withNodeName("n1")), true},
		{"kube-system", newVGPUPod("p", vgpuRequest{1, 1000, 10}, withNamespace("kube-system"), withNodeName("n1")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isProtectedFromPreemption(tt.pod))
		})
	}
}

func TestSortVictimsByPreference(t *testing.T) {
	now := time.Now()
	mkPod := func(name string, prio int32, createdAgo time.Duration) *corev1.Pod {
		p := newVGPUPod(name, vgpuRequest{1, 1000, 10}, withPriority(prio), withNodeName("n1"))
		p.CreationTimestamp = metav1.NewTime(now.Add(-createdAgo))
		return p
	}
	t.Run("priority order", func(t *testing.T) {
		pods := []*corev1.Pod{mkPod("high", 100, time.Minute), mkPod("low", 10, time.Minute), mkPod("mid", 50, time.Minute)}
		sortVictimsByPreference(pods)
		assert.Equal(t, "low", pods[0].Name)
		assert.Equal(t, "mid", pods[1].Name)
		assert.Equal(t, "high", pods[2].Name)
	})
	t.Run("timestamp tiebreak", func(t *testing.T) {
		older := mkPod("old", 10, time.Hour)
		newer := mkPod("new", 10, time.Second)
		pods := []*corev1.Pod{older, newer}
		sortVictimsByPreference(pods)
		assert.Equal(t, "new", pods[0].Name)
	})
	t.Run("identical timestamp tiebreak", func(t *testing.T) {
		commonTime := metav1.NewTime(now)
		p1 := mkPod("b-pod", 10, time.Minute)
		p1.CreationTimestamp = commonTime
		p2 := mkPod("a-pod", 10, time.Minute)
		p2.CreationTimestamp = commonTime

		pods := []*corev1.Pod{p1, p2}
		sortVictimsByPreference(pods)
		assert.Len(t, pods, 2)
	})
}

func TestExtractDeviceTypeFromResourceName(t *testing.T) {
	tests := []struct {
		name, resourceName, expected string
	}{
		{"nvidia gpu", "nvidia.com/gpu", "nvidia"},
		{"nvidia uppercase", "NVIDIA.com/gpu", "nvidia"},
		{"nvidia mixed internal", "NviDia.CoM/vGpu", "nvidia"},
		{"amd gpu", "amd.com/gpu", "amd"},
		{"amd mixed", "Amd.Com/Gpu", "amd"},
		{"huawei ascend", "huawei.com/Ascend910", "huawei"},
		{"huawei mixed case", "Huawei.Com/ascend910", "huawei"},
		{"cambricon", "cambricon.com/mlu", "cambricon"},
		{"unknown", "unknown.com/device", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDeviceTypeFromResourceName(tt.resourceName)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIsVGPUResourcePod(t *testing.T) {
	t.Run("hami annotation marks pod as vgpu", func(t *testing.T) {
		pod := newVGPUPod("p", vgpuRequest{1, 1000, 10})
		pod.Annotations = map[string]string{"hami.io/container-devices": "{}"}
		assert.True(t, isVGPUResourcePod(pod))
	})

	t.Run("hami vgpu resource request", func(t *testing.T) {
		pod := newVGPUPod("p", vgpuRequest{1, 1000, 10})
		assert.True(t, isVGPUResourcePod(pod))
	})

	t.Run("native nvidia resource", func(t *testing.T) {
		pod := newNativeGPUPod("p", 1)
		assert.True(t, isVGPUResourcePod(pod))
	})

	t.Run("no gpu resources", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "c",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				}},
			},
		}
		assert.False(t, isVGPUResourcePod(pod))
	})
}

func TestViolatePDB(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	pod := newVGPUPod("victim", vgpuRequest{1, 1000, 10}, withLabels(map[string]string{"app": "critical"}), withPriority(5), withNodeName("n1"))

	pdbSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "critical"},
	}

	// Case 1: PDB has 0 disruptions allowed — pod must violate it.
	pdbRestricted := newPDB("critical-pdb", "default", pdbSelector, 1)
	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{pod}, nil, pdbRestricted)
	defer cleanup()
	assert.True(t, plugin.violatesPDB(pod))

	// Case 2: PDB allows 1 disruption — pod must NOT violate it.
	pdbPermissive := newPDB("critical-pdb", "default", pdbSelector, 1)
	pdbPermissive.Status.DisruptionsAllowed = 1
	plugin2, cleanup2 := newPreemptPluginWithSync(t, []*corev1.Pod{pod}, nil, pdbPermissive)
	defer cleanup2()
	assert.False(t, plugin2.violatesPDB(pod))
}

func TestPreemptHappyPath(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(2, 20000, 200))
	defer cleanupMock()

	node := newTestNode("node1")
	lowA := newVGPUPod("low-a", vgpuRequest{1, 10000, 100}, withPriority(10), withNodeName(node.Name))
	lowB := newVGPUPod("low-b", vgpuRequest{1, 10000, 100}, withPriority(10), withNodeName(node.Name))
	preemptor := newVGPUPod("high-preemptor", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{lowA, lowB, preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(lowB.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Len(t, res.NodeNameToMetaVictims[node.Name].Pods, 1)
	assert.Equal(t, string(lowB.UID), res.NodeNameToMetaVictims[node.Name].Pods[0].UID)
}

func TestPreemptNodeWithZeroVictims(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(1, 10000, 100))
	defer cleanupMock()

	node := newTestNode("node1")
	preemptor := newVGPUPod("fit", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Empty(t, res.NodeNameToMetaVictims[node.Name].Pods, "node must be returned with empty victims")
}

func TestPreemptProtectedPodDropsNode(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(1, 10000, 100))
	defer cleanupMock()

	node := newTestNode("node1")
	dsA := newVGPUPod("ds", vgpuRequest{1, 10000, 100}, withPriority(10), withNodeName(node.Name), withOwner("DaemonSet"))
	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{dsA, preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(dsA.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	assert.NotContains(t, res.NodeNameToMetaVictims, node.Name)
}

func TestPreemptAddsAdditionalVictim(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(2, 20000, 200))
	defer cleanupMock()

	node := newTestNode("node1")
	protected := newVGPUPod("protected", vgpuRequest{1, 10000, 100}, withPriority(10), withNodeName(node.Name), withOwner("DaemonSet"))
	evictable := newVGPUPod("evict", vgpuRequest{1, 10000, 100}, withPriority(5), withNodeName(node.Name))
	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{protected, evictable, preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(protected.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	meta := res.NodeNameToMetaVictims[node.Name]
	require.Len(t, meta.Pods, 1)
	assert.Equal(t, string(evictable.UID), meta.Pods[0].UID)
}

func TestPreemptNativeGPUAccounting(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(1, 10000, 100))
	defer cleanupMock()

	node := newTestNode("node1")
	nativePod := newNativeGPUPod("native", 1, withPriority(5), withNodeName(node.Name))
	preemptor := newVGPUPod("vgpu", vgpuRequest{1, 10000, 50}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{nativePod, preemptor}, []*corev1.Node{node})
	defer cleanup()

	// Case 1: native pod is proposed as victim → preemptor fits after eviction.
	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(nativePod.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Len(t, res.NodeNameToMetaVictims[node.Name].Pods, 1)

	// Case 2: no victims proposed → preemptor cannot be scheduled (device monopolised by native pod).
	args2 := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{}},
		},
	}
	res2 := plugin.Preempt(context.Background(), args2)
	assert.NotContains(t, res2.NodeNameToMetaVictims, node.Name)
}

func TestPreemptMultiContainerPod(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(3, 30000, 300))
	defer cleanupMock()

	node := newTestNode("node1")

	// Multi-container victim with different resource requests per container.
	victim := newMultiContainerPod("victim", []corev1.Container{
		{
			Name: "gpu-1",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("hami.io/vgpu"):        resource.MustParse("1"),
					corev1.ResourceName("hami.io/vgpu-memory"): resource.MustParse("10000"),
					corev1.ResourceName("hami.io/vgpu-cores"):  resource.MustParse("50"),
				},
			},
		},
		{
			Name: "gpu-2",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName("hami.io/vgpu"):        resource.MustParse("1"),
					corev1.ResourceName("hami.io/vgpu-memory"): resource.MustParse("10000"),
					corev1.ResourceName("hami.io/vgpu-cores"):  resource.MustParse("50"),
				},
			},
		},
	}, withPriority(5), withNodeName(node.Name))

	preemptor := newVGPUPod("preemptor", vgpuRequest{2, 20000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{victim, preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(victim.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Len(t, res.NodeNameToMetaVictims[node.Name].Pods, 1)
}

func TestPreemptPDBViolation(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(2, 20000, 200))
	defer cleanupMock()

	node := newTestNode("node1")

	// Create a protected pod with PDB.
	protectedPod := newVGPUPod("protected", vgpuRequest{1, 10000, 100},
		withLabels(map[string]string{"app": "critical"}),
		withPriority(5),
		withNodeName(node.Name))

	evictablePod := newVGPUPod("evictable", vgpuRequest{1, 10000, 100},
		withPriority(5),
		withNodeName(node.Name))

	preemptor := newVGPUPod("preemptor", vgpuRequest{1, 10000, 100}, withPriority(100))

	pdb := newPDB("critical-pdb", "default", &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "critical"},
	}, 1)
	pdb.Status.DisruptionsAllowed = 0 // Zero disruptions allowed.

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{protectedPod, evictablePod, preemptor}, []*corev1.Node{node}, pdb)
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(evictablePod.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Len(t, res.NodeNameToMetaVictims[node.Name].Pods, 1)
	assert.Equal(t, string(evictablePod.UID), res.NodeNameToMetaVictims[node.Name].Pods[0].UID)
}

func TestResolveVictimsMapLegacyFormat(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	pod := newVGPUPod("victim", vgpuRequest{1, 1000, 10}, withPriority(5), withNodeName("n1"))
	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{pod}, nil)
	defer cleanup()

	// Legacy format: NodeNameToVictims.
	args := extenderv1.ExtenderPreemptionArgs{
		NodeNameToVictims: map[string]*extenderv1.Victims{
			"n1": {Pods: []*corev1.Pod{pod}},
		},
	}
	got, err := plugin.resolveVictimsMap(args)
	assert.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, pod.UID, got["n1"].Pods[0].UID)
}

func TestPreemptMetricsIncrement(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(2, 20000, 200))
	defer cleanupMock()

	node := newTestNode("node1")
	lowPod := newVGPUPod("low", vgpuRequest{1, 10000, 100}, withPriority(10), withNodeName(node.Name))
	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{lowPod, preemptor}, []*corev1.Node{node})
	defer cleanup()

	initialMetrics := GetPreemptionMetrics()
	initialAttempts := initialMetrics["preemption_attempts"]

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(lowPod.UID)}}},
		},
	}
	plugin.Preempt(context.Background(), args)

	finalMetrics := GetPreemptionMetrics()
	assert.Greater(t, finalMetrics["preemption_attempts"], initialAttempts)
}

func TestPreemptInitContainerRecognition(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(2, 20000, 200))
	defer cleanupMock()

	node := newTestNode("node1")
	victim := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "victim", Namespace: "default", UID: types.UID(uuid.NewString())},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name: "init",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("hami.io/vgpu"):        resource.MustParse("1"),
						corev1.ResourceName("hami.io/vgpu-memory"): resource.MustParse("10000"),
						corev1.ResourceName("hami.io/vgpu-cores"):  resource.MustParse("50"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	victim.Spec.NodeName = node.Name
	victim.Spec.Priority = &[]int32{5}[0]

	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 50}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{victim, preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(victim.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Len(t, res.NodeNameToMetaVictims[node.Name].Pods, 1)
}

func TestPassthroughMetaVictims(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	plugin, cleanup := newPreemptPluginWithSync(t, nil, nil)
	defer cleanup()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu-only", Namespace: "default", UID: types.UID(uuid.NewString())},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "c",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
				},
			}},
		},
	}

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: pod,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			"node1": {Pods: []*extenderv1.MetaPod{{UID: "some-uid"}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, "node1")
	assert.Len(t, res.NodeNameToMetaVictims["node1"].Pods, 1)
}

func TestPreemptHardwareDriverError(t *testing.T) {
	mockDev := newDefaultMockDevice()
	mockDev.failGetNodeDevices = true
	cleanupMock := registerMock(mockDev)
	defer cleanupMock()

	node := newTestNode("node1")
	lowPod := newVGPUPod("low", vgpuRequest{1, 10000, 100}, withPriority(10), withNodeName(node.Name))
	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{lowPod, preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(lowPod.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	assert.NotContains(t, res.NodeNameToMetaVictims, node.Name, "Node must be excluded when device driver retrieval errors out")
}

func TestPreemptDeviceFitFails(t *testing.T) {
	mockDev := newDefaultMockDevice()
	mockDev.fitReturnsTrue = false
	cleanupMock := registerMock(mockDev)
	defer cleanupMock()

	node := newTestNode("node1")
	lowPod := newVGPUPod("low", vgpuRequest{1, 10000, 100}, withPriority(10), withNodeName(node.Name))
	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{lowPod, preemptor}, []*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(lowPod.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	assert.NotContains(t, res.NodeNameToMetaVictims, node.Name, "Node must be dropped if preemption scenario still does not satisfy device placement constraints")
}

func TestPreemptNodeNotFoundInCache(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))
	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{preemptor}, nil) // Cache has no nodes registered
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			"ghost-node": {Pods: []*extenderv1.MetaPod{{UID: "any-uid"}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	assert.NotContains(t, res.NodeNameToMetaVictims, "ghost-node", "Missing nodes from informer cache must be skipped gracefully")
}

func TestPreemptVictimNotFoundInCache(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	node := newTestNode("node1")
	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{preemptor}, []*corev1.Node{node}) // Cache lacks proposed victim pods
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: "ghost-pod-uid"}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Empty(t, res.NodeNameToMetaVictims[node.Name].Pods, "ghost pod should be ignored")
}

func TestResolveVictimsMapEmptyAndMinimal(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	plugin, cleanup := newPreemptPluginWithSync(t, nil, nil)
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{}
	got, err := plugin.resolveVictimsMap(args)
	assert.NoError(t, err)
	assert.Empty(t, got, "Empty inputs must resolve to an empty map cleanly")
}

func TestViolatePDBWithDummyLister(t *testing.T) {
	plugin := &VgpuPreempt{pdbLister: &dummyPDBLister{}}
	pod := newVGPUPod("p", vgpuRequest{1, 1000, 10})
	assert.False(t, plugin.violatesPDB(pod), "dummy lister should never report a PDB violation")
}

func TestAccountVGPURequestsReturnErrorOnFailure(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	plugin, cleanup := newPreemptPluginWithSync(t, nil, nil)
	defer cleanup()

	// Test case 1: No device state available for requested type
	state := make(map[string][]*device.DeviceUsage)
	pod := newVGPUPod("test", vgpuRequest{1, 1000, 10})

	err := plugin.accountVGPURequests(state, pod, nil)
	assert.NotNil(t, err, "Should return error when device type has no state")
	assert.Contains(t, err.Error(), "no device state available")
}

func TestAccountVGPURequestsNoSatisfyingDevice(t *testing.T) {
	cleanupMock := registerMock(newDefaultMockDevice())
	defer cleanupMock()

	plugin, cleanup := newPreemptPluginWithSync(t, nil, nil)
	defer cleanup()

	// Create state with insufficient resources (device fully used)
	state := map[string][]*device.DeviceUsage{
		"nvidia": {
			{
				ID:        "gpu-0",
				Count:     10,
				Used:      10,
				Totalmem:  10000,
				Usedmem:   10000, // Fully used
				Totalcore: 100,
				Usedcores: 100, // Fully used
			},
		},
	}

	pod := newVGPUPod("test", vgpuRequest{1, 1000, 10})
	err := plugin.accountVGPURequests(state, pod, nil)
	assert.NotNil(t, err, "Should return error when no device satisfies request")
	assert.Contains(t, err.Error(), "no device could satisfy vGPU request")
}

func TestPDBViolationsUpperBoundSignature(t *testing.T) {
	// Verify function works correctly with 2-parameter signature
	result := pdbViolationsUpperBound(100, 50)
	assert.Equal(t, int64(50), result)

	result = pdbViolationsUpperBound(30, 100)
	assert.Equal(t, int64(30), result)
}

func TestPDBViolationsUpperBoundEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		originalCount int64
		keptLen       int
		expected      int64
	}{
		{"original smaller", 10, 100, 10},
		{"kept smaller", 100, 10, 10},
		{"equal", 50, 50, 50},
		{"zero original", 0, 50, 0},
		{"zero kept", 50, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pdbViolationsUpperBound(tt.originalCount, tt.keptLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractDeviceTypeNewVendors(t *testing.T) {
	tests := []struct {
		name, resourceName, expected string
	}{
		// New vendors
		{"metax-tech gpu", "metax-tech.com/gpu", "metax-tech"},
		{"metax mixed case", "Metax-Tech.CoM/gpu", "metax-tech"},
		{"mthreads gpu", "mthreads.com/gpu", "mthreads"},
		{"mthreads mixed", "MThreads.Com/Gpu", "mthreads"},
		{"kunlun gpu", "kunlun.com/gpu", "kunlun"},
		{"kunlun mixed", "Kunlun.CoM/gpu", "kunlun"},
		{"aws neuron", "aws.amazon.com/neuron", "aws"},
		{"neuron mixed", "AWS.Amazon.CoM/neuron", "aws"},
		{"vastai gpu", "vastai.com/gpu", "vastai"},
		{"vastai mixed", "VastAI.Com/gpu", "vastai"},
		{"enflame gpu", "enflame.com/gpu", "enflame"},
		{"enflame mixed", "Enflame.CoM/gpu", "enflame"},
		{"intel gpu", "intel.com/gpu", "intel"},
		// Original vendors still work
		{"nvidia gpu", "nvidia.com/gpu", "nvidia"},
		{"amd gpu", "amd.com/gpu", "amd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDeviceTypeFromResourceName(tt.resourceName)
			assert.Equal(t, tt.expected, got, "Failed to extract device type for %s", tt.resourceName)
		})
	}
}

func TestIsVGPUResourcePodNewVendors(t *testing.T) {
	tests := []struct {
		name     string
		vendor   string
		resource string
		want     bool
	}{
		{"metax-tech pod", "metax-tech.com/", "gpu", true},
		{"mthreads pod", "mthreads.com/", "gpu", true},
		{"kunlun pod", "kunlun.com/", "gpu", true},
		{"aws neuron pod", "aws.amazon.com/", "neuron", true},
		{"vastai pod", "vastai.com/", "gpu", true},
		{"enflame pod", "enflame.com/", "gpu", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "c",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceName(tt.vendor + tt.resource): resource.MustParse("1"),
							},
						},
					}},
				},
			}
			assert.Equal(t, tt.want, isVGPUResourcePod(pod), "Should recognize %s as GPU pod", tt.name)
		})
	}
}

func TestGetNativeWholeGPUResourcesCompleteness(t *testing.T) {
	// Verify all expected vendors are present in the map
	resources := getNativeWholeGPUResources()
	expectedVendors := []string{
		"nvidia.com/gpu",
		"amd.com/gpu",
		"intel.com/gpu",
		"huawei.com/Ascend310",
		"huawei.com/Ascend910",
		"cambricon.com/mlu",
		"hygon.com/dcu",
		"iluvatar.ai/gpu",
		"metax-tech.com/gpu",
		"mthreads.com/gpu",
		"kunlun.com/gpu",
		"aws.amazon.com/neuron",
		"vastai.com/gpu",
		"enflame.com/gpu",
	}

	for _, vendor := range expectedVendors {
		assert.True(t, resources[vendor], "Missing vendor: %s", vendor)
	}
}

func TestNewWithNilFactory(t *testing.T) {
	// Pass nil factory — should return error instead of panicking
	plugin, err := New(nil, nil)
	assert.Nil(t, plugin)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "informerFactory cannot be nil")
}

func TestNewWithValidFactory(t *testing.T) {
	// Create valid factory and confirm no error
	k8sClient := fake.NewClientset()
	factory := informers.NewSharedInformerFactory(k8sClient, 0)
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "test"})

	plugin, err := New(factory, recorder)
	assert.NoError(t, err)
	assert.NotNil(t, plugin)
	broadcaster.Shutdown()
}

func TestIsCriticalPodKubeSystemProtected(t *testing.T) {
	pod := newVGPUPod("critical", vgpuRequest{1, 1000, 10}, withNamespace("kube-system"))
	assert.True(t, isCriticalPod(pod), "kube-system pods should be protected")
}

func TestIsCriticalPodKubePublicNotProtected(t *testing.T) {
	pod := newVGPUPod("info", vgpuRequest{1, 1000, 10}, withNamespace("kube-public"))
	assert.False(t, isCriticalPod(pod), "kube-public pods should NOT be protected")
}

func TestIsCriticalPodPriorityClassProtected(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "critical", Namespace: "my-app"},
		Spec: corev1.PodSpec{
			PriorityClassName: "system-cluster-critical",
			Containers:        []corev1.Container{{Name: "c"}},
		},
	}
	assert.True(t, isCriticalPod(pod), "system-cluster-critical pods should be protected")
}

func TestIsCriticalPodPriorityValueProtected(t *testing.T) {
	priority := criticalPriorityThreshold + 100
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "critical", Namespace: "my-app"},
		Spec: corev1.PodSpec{
			Priority:   &priority,
			Containers: []corev1.Container{{Name: "c"}},
		},
	}
	assert.True(t, isCriticalPod(pod), "Pods with high priority value should be protected")
}

func TestIsCriticalPodRegularNamespaceNotProtected(t *testing.T) {
	pod := newVGPUPod("regular", vgpuRequest{1, 1000, 10}, withNamespace("default"), withPriority(5))
	assert.False(t, isCriticalPod(pod), "Regular pods should not be protected")
}

func TestRefineForNodeWithAccountingError(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(1, 5000, 50)) // Very limited resources
	defer cleanupMock()

	node := newTestNode("node1")

	// Pod requesting more resources than the device provides
	highRequest := newVGPUPod("demanding", vgpuRequest{1, 20000, 100},
		withPriority(10), withNodeName(node.Name))

	preemptor := newVGPUPod("high", vgpuRequest{1, 20000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{highRequest, preemptor}, []*corev1.Node{node})
	defer cleanup()

	// Even with the victim proposed, the preemptor cannot fit (device only has 5000 mem, needs 20000)
	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(highRequest.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	assert.NotContains(t, res.NodeNameToMetaVictims, node.Name,
		"Node should be dropped when preemptor cannot fit even after victim removal")
}

func TestPreemptAfterAllFixes(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(2, 20000, 200))
	defer cleanupMock()

	node := newTestNode("node1")

	// lowVGPU is the target victim on node1.
	lowVGPU := newVGPUPod("low-vgpu", vgpuRequest{1, 10000, 100},
		withPriority(10), withNodeName(node.Name))

	// lowNative exists in the cluster but is NOT bound to node1.
	// Binding it to node1 would cause accountNativeWholeGPUs to monopolise the
	// single device entry (Used=Count), making the preemptor fail to fit after
	// evicting only lowVGPU and incorrectly pulling in a second victim.
	lowNative := newNativeGPUPod("low-native", 1,
		withPriority(10))

	preemptor := newVGPUPod("high", vgpuRequest{1, 10000, 100}, withPriority(100))

	plugin, cleanup := newPreemptPluginWithSync(t,
		[]*corev1.Pod{lowVGPU, lowNative, preemptor},
		[]*corev1.Node{node})
	defer cleanup()

	args := extenderv1.ExtenderPreemptionArgs{
		Pod: preemptor,
		NodeNameToMetaVictims: map[string]*extenderv1.MetaVictims{
			node.Name: {Pods: []*extenderv1.MetaPod{{UID: string(lowVGPU.UID)}}},
		},
	}
	res := plugin.Preempt(context.Background(), args)
	require.Contains(t, res.NodeNameToMetaVictims, node.Name)
	assert.Len(t, res.NodeNameToMetaVictims[node.Name].Pods, 1)
}

func TestAllNewVendorsInRealScenario(t *testing.T) {
	cleanupMock := registerMock(newMockDevice(10, 50000, 500))
	defer cleanupMock()

	node := newTestNode("node1")

	// Create pods from all new vendors
	vendors := []struct {
		name   string
		vendor string
	}{
		{"metax", "metax-tech.com/gpu"},
		{"mthreads", "mthreads.com/gpu"},
		{"kunlun", "kunlun.com/gpu"},
		{"aws", "aws.amazon.com/neuron"},
		{"vastai", "vastai.com/gpu"},
		{"enflame", "enflame.com/gpu"},
	}

	var pods []*corev1.Pod
	for i, v := range vendors {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      v.name,
				Namespace: "default",
				UID:       types.UID(fmt.Sprintf("uid-%d", i)),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "compute",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName(v.vendor): resource.MustParse("1"),
						},
					},
				}},
				NodeName: node.Name,
				Priority: &[]int32{5}[0],
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
		pods = append(pods, pod)
	}

	preemptor := newVGPUPod("high", vgpuRequest{1, 5000, 50}, withPriority(100))

	_, cleanup := newPreemptPluginWithSync(t, append(pods, preemptor), []*corev1.Node{node})
	defer cleanup()

	// Verify all vendor pods are recognized as GPU pods
	for _, pod := range pods {
		assert.True(t, isVGPUResourcePod(pod), "Pod %s should be recognized as GPU pod", pod.Name)
	}
}
