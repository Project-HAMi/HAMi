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
package device

import (
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MockDevices struct {
	resourceNames ResourceNames
}

func (m *MockDevices) CommonWord() string {
	return "mock"
}

func (m *MockDevices) MemoryFactor() int32 {
	return 1
}

func (m *MockDevices) MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error) {
	return true, nil
}

func (m *MockDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (m *MockDevices) NodeCleanUp(nn string) error {
	return nil
}

func (m *MockDevices) GetResourceNames() ResourceNames {
	return m.resourceNames
}

func (m *MockDevices) GetNodeDevices(n corev1.Node) ([]*DeviceInfo, error) {
	return []*DeviceInfo{}, nil
}

func (m *MockDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (m *MockDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (m *MockDevices) GenerateResourceRequests(ctr *corev1.Container) ContainerDeviceRequest {
	return ContainerDeviceRequest{}
}

func (m *MockDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd PodDevices) map[string]string {
	return map[string]string{}
}

func (m *MockDevices) ScoreNode(node *corev1.Node, podDevices PodSingleDevice, previous []*DeviceUsage, policy string) float32 {
	return 1.0
}

func (m *MockDevices) AddResourceUsage(pod *corev1.Pod, n *DeviceUsage, ctr *ContainerDevice) error {
	return nil
}

func (m *MockDevices) Fit(devices []*DeviceUsage, request ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *NodeInfo, allocated *PodDevices) (bool, map[string]ContainerDevices, string) {
	return true, nil, ""
}

type PodDeviceInfo struct {
	Usedmem   int
	Usedcores int
}

type TestPodDevices map[string]map[string][]PodDeviceInfo

func cleanupNamespaceQuota(t *testing.T, ns string) {
	t.Helper()
	t.Cleanup(func() {
		delete(NewQuotaManager().Quotas, ns)
	})
}

func initTest() {
	DevicesMap = make(map[string]Devices)
	DevicesMap["NVIDIA"] = &MockDevices{
		resourceNames: ResourceNames{
			ResourceMemoryName: "nvidia.com/gpumem",
			ResourceCoreName:   "nvidia.com/gpucore",
		},
	}
}

func TestNewQuotaManagerSingleton(t *testing.T) {
	var wg sync.WaitGroup
	var managers [2]*QuotaManager

	wg.Add(2)
	go func() {
		managers[0] = NewQuotaManager()
		wg.Done()
	}()
	go func() {
		managers[1] = NewQuotaManager()
		wg.Done()
	}()
	wg.Wait()

	if managers[0] != managers[1] {
		t.Error("NewQuotaManager should return the same instance (singleton)")
	}
}

func TestFitQuota(t *testing.T) {
	initTest()
	qm := NewQuotaManager()
	ns := "testns"
	deviceName := "NVIDIA"
	memName := "nvidia.com/gpumem"
	coreName := "nvidia.com/gpucore"

	qm.Quotas[ns] = &DeviceQuota{
		memName:  &Quota{Used: 1000, Limit: 2000},
		coreName: &Quota{Used: 200, Limit: 400},
	}

	// Should fit
	if !qm.FitQuota(ns, 500, 1, 100, deviceName) {
		t.Error("FitQuota should return true when within limits")
	}
	// Should not fit memory
	if qm.FitQuota(ns, 1500, 1, 100, deviceName) {
		t.Error("FitQuota should return false when memory exceeds limit")
	}
	// Should not fit core
	if qm.FitQuota(ns, 500, 1, 300, deviceName) {
		t.Error("FitQuota should return false when core exceeds limit")
	}
	// Should fit memory with factor
	if !qm.FitQuota(ns, 1000, 2, 100, deviceName) {
		t.Error("FitQuota should return true")
	}
	// Should not fit memory with factor
	if qm.FitQuota(ns, 5000, 2, 100, deviceName) {
		t.Error("FitQuota should return false when memory exceeds limit")
	}
	// Should fit if namespace not present
	if !qm.FitQuota("otherns", 1500, 1, 100, deviceName) {
		t.Error("FitQuota should return true if namespace not present")
	}
	// Should fit if device not present
	if !qm.FitQuota(ns, 1000, 1, 100, "unknown-device") {
		t.Error("FitQuota should return true if device not present")
	}
}

func TestPodQuotaRequests(t *testing.T) {
	initTest()
	DevicesMap["Ascend910B"] = &MockDevices{
		resourceNames: ResourceNames{
			ResourceCountName:  "huawei.com/Ascend910B",
			ResourceMemoryName: "huawei.com/Ascend910B-memory",
			ResourceCoreName:   "huawei.com/Ascend910B-core",
		},
	}
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B":        resource.MustParse("1"),
						"huawei.com/Ascend910B-memory": resource.MustParse("2000"),
					},
				},
			}},
		},
	}
	mem, cores := PodQuotaRequests(pod, "Ascend910B")
	if mem != 2000 || cores != 0 {
		t.Fatalf("PodQuotaRequests() = (%d, %d), want (2000, 0)", mem, cores)
	}
}

func TestPodQuotaRequestsUnknownDevice(t *testing.T) {
	initTest()
	mem, cores := PodQuotaRequests(&corev1.Pod{}, "unknown")
	if mem != 0 || cores != 0 {
		t.Fatalf("PodQuotaRequests() = (%d, %d), want (0, 0)", mem, cores)
	}
}

func TestFitPodQuotaNoDeviceRequest(t *testing.T) {
	initTest()
	cleanupNamespaceQuota(t, "default")
	if !FitPodQuota(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default"}}, "NVIDIA", 1) {
		t.Fatal("FitPodQuota should allow pods with no device requests")
	}
}

func TestFitPodQuotaMemoryFactor(t *testing.T) {
	initTest()
	deviceName := "NVIDIA"
	memName := "nvidia.com/gpumem"
	cleanupNamespaceQuota(t, "default")
	qm := NewQuotaManager()
	qm.Quotas["default"] = &DeviceQuota{
		memName: &Quota{Used: 0, Limit: 1000},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu":    resource.MustParse("1"),
						"nvidia.com/gpumem": resource.MustParse("1500"),
					},
				},
			}},
		},
	}
	DevicesMap[deviceName] = &MockDevices{
		resourceNames: ResourceNames{
			ResourceCountName:  "nvidia.com/gpu",
			ResourceMemoryName: memName,
			ResourceCoreName:   "nvidia.com/gpucore",
		},
	}
	if FitPodQuota(pod, deviceName, 2) {
		t.Fatal("FitPodQuota should reject request when memoryFactor doubles usage over limit")
	}
}

func TestFitAllocationQuota(t *testing.T) {
	initTest()
	cleanupNamespaceQuota(t, "default")
	deviceName := "NVIDIA"
	memName := "nvidia.com/gpumem"
	coreName := "nvidia.com/gpucore"
	DevicesMap[deviceName] = &MockDevices{
		resourceNames: ResourceNames{
			ResourceCountName:  "nvidia.com/gpu",
			ResourceMemoryName: memName,
			ResourceCoreName:   coreName,
		},
	}
	qm := NewQuotaManager()
	qm.Quotas["default"] = &DeviceQuota{
		memName:  &Quota{Used: 0, Limit: 2048},
		coreName: &Quota{Used: 0, Limit: 100},
	}

	tests := []struct {
		name      string
		tmpDevs   map[string]ContainerDevices
		allocated *PodDevices
		memreq    int64
		coresreq  int64
		want      bool
	}{
		{
			name:     "within quota",
			memreq:   100,
			coresreq: 1,
			want:     true,
		},
		{
			name:   "request exceeds quota",
			memreq: 3000,
			want:   false,
		},
		{
			name: "tmpdev exceeds quota",
			tmpDevs: map[string]ContainerDevices{
				deviceName: {{Usedmem: 1024, Usedcores: 5}},
			},
			memreq: 2000,
			want:   false,
		},
		{
			name: "allocated devs exceed quota",
			allocated: &PodDevices{
				deviceName: PodSingleDevice{
					ContainerDevices{{Usedmem: 1024, Usedcores: 2}},
				},
			},
			memreq: 2000,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FitAllocationQuota("default", deviceName, 1, tt.memreq, tt.coresreq, tt.tmpDevs, tt.allocated)
			if got != tt.want {
				t.Fatalf("FitAllocationQuota() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFitPodQuotaNonNvidia(t *testing.T) {
	initTest()
	cleanupNamespaceQuota(t, "default")
	deviceName := "Ascend910B"
	memName := "huawei.com/Ascend910B-memory"
	DevicesMap[deviceName] = &MockDevices{
		resourceNames: ResourceNames{
			ResourceCountName:  "huawei.com/Ascend910B",
			ResourceMemoryName: memName,
			ResourceCoreName:   "huawei.com/Ascend910B-core",
		},
	}
	qm := NewQuotaManager()
	qm.Quotas["default"] = &DeviceQuota{
		memName: &Quota{Used: 9000, Limit: 10000},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B":        resource.MustParse("1"),
						"huawei.com/Ascend910B-memory": resource.MustParse("2000"),
					},
				},
			}},
		},
	}
	if FitPodQuota(pod, deviceName, 1) {
		t.Fatal("FitPodQuota should reject over-limit non-NVIDIA request")
	}
}

func TestAddUsageAndRmUsage(t *testing.T) {
	initTest()
	qm := NewQuotaManager()
	ns := "testns"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns}}
	podDev := PodDevices{
		"NVIDIA": PodSingleDevice{
			[]ContainerDevice{
				{
					Idx:       0,
					UUID:      "GPU0",
					Usedmem:   1000,
					Usedcores: 100,
				},
			},
		},
	}

	qm.Quotas[ns] = &DeviceQuota{}
	qm.AddUsage(pod, podDev)

	memName := "nvidia.com/gpumem"
	coreName := "nvidia.com/gpucore"

	if (*qm.Quotas[ns])[memName].Used != 1000 {
		t.Errorf("AddUsage: expected Used memory 1000, got %d", (*qm.Quotas[ns])[memName].Used)
	}
	if (*qm.Quotas[ns])[coreName].Used != 100 {
		t.Errorf("AddUsage: expected Used core 100, got %d", (*qm.Quotas[ns])[coreName].Used)
	}

	qm.RmUsage(pod, podDev)
	if (*qm.Quotas[ns])[memName].Used != 0 {
		t.Errorf("RmUsage: expected Used memory 0, got %d", (*qm.Quotas[ns])[memName].Used)
	}
	if (*qm.Quotas[ns])[coreName].Used != 0 {
		t.Errorf("RmUsage: expected Used core 0, got %d", (*qm.Quotas[ns])[coreName].Used)
	}

	// remove more than tracked: Used must not go below zero
	qm.RmUsage(pod, podDev)
	if (*qm.Quotas[ns])[memName].Used < 0 {
		t.Errorf("RmUsage: Used memory went negative: %d", (*qm.Quotas[ns])[memName].Used)
	}
	if (*qm.Quotas[ns])[coreName].Used < 0 {
		t.Errorf("RmUsage: Used core went negative: %d", (*qm.Quotas[ns])[coreName].Used)
	}
}

func TestIsManagedQuota(t *testing.T) {
	initTest()
	if !IsManagedQuota("nvidia.com/gpumem") {
		t.Error("IsManagedQuota should return true for managed memory quota")
	}
	if !IsManagedQuota("nvidia.com/gpucore") {
		t.Error("IsManagedQuota should return true for managed core quota")
	}
	if IsManagedQuota("other-resource") {
		t.Error("IsManagedQuota should return false for unmanaged quota")
	}
}

func TestAddQuotaAndDelQuota(t *testing.T) {
	initTest()
	qm := NewQuotaManager()
	ns := "testns"
	memName := "nvidia.com/gpumem"
	coreName := "nvidia.com/gpucore"

	rq := &corev1.ResourceQuota{}
	rq.Namespace = ns
	rq.Spec.Hard = corev1.ResourceList{
		corev1.ResourceName("limits." + memName):  *resource.NewQuantity(100, resource.DecimalSI),
		corev1.ResourceName("limits." + coreName): *resource.NewQuantity(10, resource.DecimalSI),
	}

	qm.AddQuota(rq)
	if (*qm.Quotas[ns])[memName].Limit != 100 {
		t.Errorf("AddQuota: expected memory limit 100, got %d", (*qm.Quotas[ns])[memName].Limit)
	}
	if (*qm.Quotas[ns])[coreName].Limit != 10 {
		t.Errorf("AddQuota: expected core limit 10, got %d", (*qm.Quotas[ns])[coreName].Limit)
	}

	qm.DelQuota(rq)
	if (*qm.Quotas[ns])[memName].Limit != 0 {
		t.Errorf("DelQuota: expected memory limit 0, got %d", (*qm.Quotas[ns])[memName].Limit)
	}
	if (*qm.Quotas[ns])[coreName].Limit != 0 {
		t.Errorf("DelQuota: expected core limit 0, got %d", (*qm.Quotas[ns])[coreName].Limit)
	}
}
