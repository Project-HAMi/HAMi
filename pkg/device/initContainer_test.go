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
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30},
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
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 30},
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
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50},
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
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 1000, Usedcores: 80}},
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
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50}},
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
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30}},
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
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 500, Usedcores: 50}},
		},
		"kunlun": PodSingleDevice{
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 300, Usedcores: 30}},
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
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 300, Usedcores: 30},
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

func TestAppContainersOnlyDeviceUsage_NilInput(t *testing.T) {
	result := AppContainersOnlyDeviceUsage(nil, nil)
	assert.Assert(t, result == nil)

	pod := makePod("test", 1, 1)
	result = AppContainersOnlyDeviceUsage(pod, nil)
	assert.Assert(t, result == nil)
}

func TestAppContainersOnlyDeviceUsage_OnlyAppContainers(t *testing.T) {
	pod := makePod("test", 0, 2)
	raw := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10}},
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}},
		},
	}
	expected := PodDevices{
		"NVIDIA": PodSingleDevice{
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30}},
		},
	}
	result := AppContainersOnlyDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestAppContainersOnlyDeviceUsage_IgnoresInitContainers(t *testing.T) {
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
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 300, Usedcores: 30}},
		},
	}
	result := AppContainersOnlyDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestAppContainersOnlyDeviceUsage_MultipleDeviceTypes(t *testing.T) {
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
			{ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 200, Usedcores: 20}},
		},
		"kunlun": PodSingleDevice{
			{ContainerDevice{UUID: "xpu0", Type: "kunlun", Usedmem: 400, Usedcores: 40}},
		},
	}
	result := AppContainersOnlyDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}

func TestAppContainersOnlyDeviceUsage_MultipleDevicesPerContainer(t *testing.T) {
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
				ContainerDevice{UUID: "gpu0", Type: "NVIDIA", Usedmem: 100, Usedcores: 10},
				ContainerDevice{UUID: "gpu1", Type: "NVIDIA", Usedmem: 200, Usedcores: 20},
			},
		},
	}
	result := AppContainersOnlyDeviceUsage(pod, raw)
	assert.DeepEqual(t, expected, result)
}
