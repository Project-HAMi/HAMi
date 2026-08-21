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

package device

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makePod(name string, numInit, numApp int) *corev1.Pod {
	initContainers := make([]corev1.Container, numInit)
	for i := range initContainers {
		initContainers[i] = corev1.Container{Name: "init"}
	}
	appContainers := make([]corev1.Container, numApp)
	for i := range appContainers {
		appContainers[i] = corev1.Container{Name: "app"}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: corev1.PodSpec{
			InitContainers: initContainers,
			Containers:     appContainers,
		},
	}
}

func TestCollapseInitContainerUsage_NilInput(t *testing.T) {
	result := CollapseInitContainerUsage(nil, nil)
	assert.Assert(t, result == nil)

	pod := makePod("test", 1, 1)
	result = CollapseInitContainerUsage(pod, nil)
	assert.Assert(t, result == nil)
}

func TestCollapseInitContainerUsage_NoInitContainers(t *testing.T) {
	pod := makePod("test", 0, 2)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30, Slots: 2},
			},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_OnlyInitContainers(t *testing.T) {
	pod := makePod("test", 2, 0)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 30}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 30, Slots: 1},
			},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_MixedInitAndApp(t *testing.T) {
	pod := makePod("test", 1, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50, Slots: 1},
			},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_InitLargerThanApp(t *testing.T) {
	pod := makePod("test", 1, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 1000, Usedcores: 80}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 1000, Usedcores: 80, Slots: 1}},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_AppLargerThanInit(t *testing.T) {
	pod := makePod("test", 1, 2)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}}, // init
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}}, // app0
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30}}, // app1
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50, Slots: 2}},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_MultipleInitContainersPeak(t *testing.T) {
	pod := makePod("test", 3, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 50, Usedcores: 5}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30, Slots: 1}},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_MultipleDeviceTypes(t *testing.T) {
	pod := makePod("test", 1, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
		},
		"kunlun": PodSingleDevice{
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 200, Usedcores: 20}},
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 300, Usedcores: 30}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50, Slots: 1}},
		},
		"kunlun": PodSingleDevice{
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 300, Usedcores: 30, Slots: 1}},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_MultipleDevicesSameContainer(t *testing.T) {
	pod := makePod("test", 1, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 300, Usedcores: 30},
			},
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 150, Usedcores: 15},
			},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20, Slots: 1},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 300, Usedcores: 30, Slots: 1},
			},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestCollapseInitContainerUsage_EmptyContainerDeviceList(t *testing.T) {
	pod := makePod("test", 2, 2)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{}, // init0 empty
			{}, // init1 empty
			{}, // app0 empty
			{}, // app1 empty
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			nil, // no devices => nil slice
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

// TestCollapseInitContainerUsage_MultiAppSameGPUSlots guards the regression: three
// app containers sharing one GPU must collapse to three slots, not one.
func TestCollapseInitContainerUsage_MultiAppSameGPUSlots(t *testing.T) {
	pod := makePod("test", 0, 3)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
		},
	}
	result := CollapseInitContainerUsage(pod, raw)
	assert.Equal(t, result["NVIDIA"][0][0].Slots, int32(3))
}

func TestSteadyStateDeviceUsage_NilInput(t *testing.T) {
	result := SteadyStateDeviceUsage(nil, nil)
	assert.Assert(t, result == nil)

	pod := makePod("test", 1, 1)
	result = SteadyStateDeviceUsage(pod, nil)
	assert.Assert(t, result == nil)
}

func TestSteadyStateDeviceUsage_OnlyAppContainers(t *testing.T) {
	pod := makePod("test", 0, 2)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30, Slots: 2}},
		},
	}
	result := SteadyStateDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestSteadyStateDeviceUsage_IgnoresInitContainers(t *testing.T) {
	pod := makePod("test", 1, 2)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 999, Usedcores: 99}}, // init
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}}, // app0
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}}, // app1
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30, Slots: 2}},
		},
	}
	result := SteadyStateDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestSteadyStateDeviceUsage_MultipleDeviceTypes(t *testing.T) {
	pod := makePod("test", 1, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}},
		},
		"kunlun": PodSingleDevice{
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 300, Usedcores: 30}},
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 400, Usedcores: 40}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20, Slots: 1}},
		},
		"kunlun": PodSingleDevice{
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 400, Usedcores: 40, Slots: 1}},
		},
	}
	result := SteadyStateDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestSteadyStateDeviceUsage_MultipleDevicesPerContainer(t *testing.T) {
	pod := makePod("test", 1, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ // init (ignored)
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 1000, Usedcores: 100},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 2000, Usedcores: 200},
			},
			{ // app
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 200, Usedcores: 20},
			},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10, Slots: 1},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 200, Usedcores: 20, Slots: 1},
			},
		},
	}
	result := SteadyStateDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

// TestSteadyStateDeviceUsage_MultiAppSameGPUSlots guards that the steady-state
// shrink path also preserves the per-container slot count.
func TestSteadyStateDeviceUsage_MultiAppSameGPUSlots(t *testing.T) {
	pod := makePod("test", 1, 2)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 999, Usedcores: 99}}, // init
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}}, // app0
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}}, // app1
		},
	}
	result := SteadyStateDeviceUsage(pod, raw)
	assert.Equal(t, result["NVIDIA"][0][0].Slots, int32(2))
}

// --- Sidecar container accounting (sidecarsContainer-design.md) ---

func makePodWithSidecars(name string, initPolicies []*corev1.ContainerRestartPolicy, numApp int) *corev1.Pod {
	initContainers := make([]corev1.Container, len(initPolicies))
	for i, p := range initPolicies {
		initContainers[i] = corev1.Container{Name: fmt.Sprintf("init-%d", i), RestartPolicy: p}
	}
	appContainers := make([]corev1.Container, numApp)
	for i := range appContainers {
		appContainers[i] = corev1.Container{Name: fmt.Sprintf("app-%d", i)}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: corev1.PodSpec{
			InitContainers: initContainers,
			Containers:     appContainers,
		},
	}
}

// Design case "oversubscription prevented": a 4000 sidecar plus a 4000 app
// container on one card must account as 8000, not max(4000,4000)=4000.
func TestCollapseInitContainerUsage_SidecarAddsToAppSum(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := makePodWithSidecars("test", []*corev1.ContainerRestartPolicy{&always}, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 4000, Usedcores: 40}}, // sidecar
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 4000, Usedcores: 40}}, // app
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 8000, Usedcores: 80, Slots: 2}},
		},
	}
	assert.DeepEqual(t, expected, CollapseInitContainerUsage(pod, raw))
}

// Design case "shrink restored": init 20000 + sidecar 2000 + app 10000 →
// collapse = 2000 + max(20000, 10000) = 22000; steady state = 12000, and the
// steady state must still contain the sidecar's share (the pin that a fixed
// gate plus an unfixed target would violate).
func TestCollapseAndSteadyState_SidecarWithInitAndApp(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := makePodWithSidecars("test", []*corev1.ContainerRestartPolicy{nil, &always}, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 20000, Usedcores: 60}}, // init (regular)
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 2000, Usedcores: 10}},  // sidecar
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 10000, Usedcores: 30}}, // app
		},
	}
	expectedCollapsed := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 22000, Usedcores: 70, Slots: 2}},
		},
	}
	assert.DeepEqual(t, expectedCollapsed, CollapseInitContainerUsage(pod, raw))

	expectedSteady := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 12000, Usedcores: 40, Slots: 2}},
		},
	}
	assert.DeepEqual(t, expectedSteady, SteadyStateDeviceUsage(pod, raw))
}

// A nil restartPolicy is a regular init container: results must be identical
// to the pre-sidecar behavior (the "safe everywhere" property).
func TestCollapseInitContainerUsage_NilRestartPolicyUnchanged(t *testing.T) {
	pod := makePodWithSidecars("test", []*corev1.ContainerRestartPolicy{nil}, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 4000, Usedcores: 40}}, // init
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 4000, Usedcores: 40}}, // app
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 4000, Usedcores: 40, Slots: 1}},
		},
	}
	assert.DeepEqual(t, expected, CollapseInitContainerUsage(pod, raw))
}

// All init containers are sidecars: init_peak = 0, so
// effective = sidecar_sum + max(0, app_sum), and the steady state equals it.
func TestCollapseInitContainerUsage_AllSidecars(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := makePodWithSidecars("test", []*corev1.ContainerRestartPolicy{&always, &always}, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 3000, Usedcores: 10}}, // sidecar
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 5000, Usedcores: 20}}, // sidecar
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 1000, Usedcores: 5}},  // app
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 9000, Usedcores: 35, Slots: 3}},
		},
	}
	assert.DeepEqual(t, expected, CollapseInitContainerUsage(pod, raw))
	assert.DeepEqual(t, expected, SteadyStateDeviceUsage(pod, raw))
}

// Per-UUID independence: sidecar on gpu0, regular init on gpu1, app on gpu0.
// Missing per-UUID entries count as 0 before the max() and the addition.
func TestCollapseInitContainerUsage_SidecarMultiUUID(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := makePodWithSidecars("test", []*corev1.ContainerRestartPolicy{&always, nil}, 1)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 2000, Usedcores: 10}}, // sidecar on gpu0
			{ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 7000, Usedcores: 70}}, // init on gpu1
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 3000, Usedcores: 30}}, // app on gpu0
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 5000, Usedcores: 40, Slots: 2},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 7000, Usedcores: 70, Slots: 1},
			},
		},
	}
	assert.DeepEqual(t, expected, CollapseInitContainerUsage(pod, raw))
}
