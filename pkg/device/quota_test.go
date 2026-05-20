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
