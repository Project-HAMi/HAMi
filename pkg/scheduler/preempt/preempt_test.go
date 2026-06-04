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

// ============================================================================
// Mock Device Implementation
// ============================================================================

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

// ============================================================================
// Test Fixtures
// ============================================================================

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

// ============================================================================
// Unit Tests
// ============================================================================

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
}

func TestExtractDeviceTypeFromResourceName(t *testing.T) {
	tests := []struct {
		name, resourceName, expected string
	}{
		{"nvidia gpu", "nvidia.com/gpu", "nvidia"},
		{"nvidia uppercase", "NVIDIA.com/gpu", "nvidia"},
		{"amd gpu", "amd.com/gpu", "amd"},
		{"huawei ascend", "huawei.com/Ascend910", "huawei"},
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
	// newPDB already sets DisruptionsAllowed = 0; the informer cache is populated
	// with this value when the plugin is created.
	plugin, cleanup := newPreemptPluginWithSync(t, []*corev1.Pod{pod}, nil, pdbRestricted)
	defer cleanup()
	assert.True(t, plugin.violatesPDB(pod))

	// Case 2: PDB allows 1 disruption — pod must NOT violate it.
	// A fresh plugin instance is required because the informer cache is an immutable
	// snapshot taken at WaitForCacheSync time; mutating the Go struct in-place after
	// that point has no effect on the cached copy.
	pdbPermissive := newPDB("critical-pdb", "default", pdbSelector, 1)
	pdbPermissive.Status.DisruptionsAllowed = 1
	plugin2, cleanup2 := newPreemptPluginWithSync(t, []*corev1.Pod{pod}, nil, pdbPermissive)
	defer cleanup2()
	assert.False(t, plugin2.violatesPDB(pod))
}

// ============================================================================
// E2E Preemption Tests
// ============================================================================

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

	// Case 2: no victims proposed → preemptor cannot be scheduled.
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

	// Propose evictablePod as victim, but it's not enough. The algorithm will search
	// for additional victims. It should find protectedPod but skip it due to PDB violation,
	// and accept evictablePod alone.
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
