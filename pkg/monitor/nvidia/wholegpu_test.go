/*
Copyright 2025 The HAMi Authors.

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

package nvidia

import (
	"errors"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	mock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// wholeGPUAnnotation encodes ctrDevs the same way the device plugin does,
// one entry per container in order, and returns it under the annotation key
// reconcileWholeGPU reads.
func wholeGPUAnnotation(ctrDevs ...device.ContainerDevices) map[string]string {
	return map[string]string{nv.AllocatedDevicesAnnotation: device.EncodePodSingleDevice(device.PodSingleDevice(ctrDevs))}
}

func Test_containerNameByIndex(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init-a"}, {Name: "init-b"}},
			Containers:     []corev1.Container{{Name: "main-a"}, {Name: "main-b"}},
		},
	}

	tests := []struct {
		name     string
		idx      int
		wantName string
		wantOK   bool
	}{
		{"negative index", -1, "", false},
		{"first init container", 0, "init-a", true},
		{"second init container", 1, "init-b", true},
		{"first regular container", 2, "main-a", true},
		{"second regular container", 3, "main-b", true},
		{"one past the last container", 4, "", false},
		{"far out of range", 99, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, ok := containerNameByIndex(pod, tc.idx)
			assert.Equal(t, ok, tc.wantOK)
			assert.Equal(t, name, tc.wantName)
		})
	}

	t.Run("no init containers", func(t *testing.T) {
		p := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "only"}}}}
		name, ok := containerNameByIndex(p, 0)
		assert.Equal(t, ok, true)
		assert.Equal(t, name, "only")
	})
}

func Test_evaluateContainerWholeGPU(t *testing.T) {
	t.Run("no devices is not a whole GPU", func(t *testing.T) {
		got := evaluateContainerWholeGPU(device.ContainerDevices{}, map[string]*device.DeviceInfo{})
		assert.Equal(t, got, notWholeGPU)
	})

	t.Run("full memory allocation on a non-MIG device is confirmed", func(t *testing.T) {
		ctrDevs := device.ContainerDevices{{UUID: "GPU-1", Usedmem: 8000}}
		nodeDevs := map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}
		assert.Equal(t, evaluateContainerWholeGPU(ctrDevs, nodeDevs), confirmedWholeGPU)
	})

	t.Run("over-allocation still counts as whole", func(t *testing.T) {
		ctrDevs := device.ContainerDevices{{UUID: "GPU-1", Usedmem: 9000}}
		nodeDevs := map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}
		assert.Equal(t, evaluateContainerWholeGPU(ctrDevs, nodeDevs), confirmedWholeGPU)
	})

	t.Run("MIG device is confirmed whole at instance level", func(t *testing.T) {
		ctrDevs := device.ContainerDevices{{UUID: "GPU-1", Usedmem: 8000}}
		nodeDevs := map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: nv.MigMode}}
		assert.Equal(t, evaluateContainerWholeGPU(ctrDevs, nodeDevs), confirmedWholeGPU)
	})

	t.Run("fractional memory allocation is not a whole GPU", func(t *testing.T) {
		ctrDevs := device.ContainerDevices{{UUID: "GPU-1", Usedmem: 2000}}
		nodeDevs := map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}
		assert.Equal(t, evaluateContainerWholeGPU(ctrDevs, nodeDevs), notWholeGPU)
	})

	t.Run("unknown UUID with no other verdict is indeterminate", func(t *testing.T) {
		ctrDevs := device.ContainerDevices{{UUID: "GPU-unregistered", Usedmem: 8000}}
		got := evaluateContainerWholeGPU(ctrDevs, map[string]*device.DeviceInfo{})
		assert.Equal(t, got, indeterminate)
	})

	t.Run("a confirmed not-whole verdict takes priority over an unknown UUID elsewhere", func(t *testing.T) {
		ctrDevs := device.ContainerDevices{
			{UUID: "GPU-unregistered", Usedmem: 8000},
			{UUID: "GPU-fractional", Usedmem: 1000},
		}
		nodeDevs := map[string]*device.DeviceInfo{"GPU-fractional": {ID: "GPU-fractional", Devmem: 8000, Mode: ""}}
		assert.Equal(t, evaluateContainerWholeGPU(ctrDevs, nodeDevs), notWholeGPU)
	})

	t.Run("multi-device container confirmed only when every device is whole", func(t *testing.T) {
		ctrDevs := device.ContainerDevices{
			{UUID: "GPU-1", Usedmem: 8000},
			{UUID: "GPU-2", Usedmem: 8000},
		}
		nodeDevs := map[string]*device.DeviceInfo{
			"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""},
			"GPU-2": {ID: "GPU-2", Devmem: 8000, Mode: ""},
		}
		assert.Equal(t, evaluateContainerWholeGPU(ctrDevs, nodeDevs), confirmedWholeGPU)
	})
}

func Test_wholeGPUUsage_deviceMethods(t *testing.T) {
	newHandle := func(mem nvml.Memory, memRet nvml.Return, utilization nvml.Utilization, utilRet nvml.Return) *mock.Device {
		return &mock.Device{
			GetMemoryInfoFunc:       func() (nvml.Memory, nvml.Return) { return mem, memRet },
			GetUtilizationRatesFunc: func() (nvml.Utilization, nvml.Return) { return utilization, utilRet },
		}
	}

	t.Run("DeviceMax and DeviceNum report the UUID count", func(t *testing.T) {
		u := &wholeGPUUsage{uuids: []string{"a", "b", "c"}}
		assert.Equal(t, u.DeviceMax(), 3)
		assert.Equal(t, u.DeviceNum(), 3)
	})

	t.Run("unsupported metrics are always zero", func(t *testing.T) {
		u := &wholeGPUUsage{uuids: []string{"GPU-aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}}
		assert.Equal(t, u.DeviceMemoryContextSize(0), uint64(0))
		assert.Equal(t, u.DeviceMemoryModuleSize(0), uint64(0))
		assert.Equal(t, u.DeviceMemoryBufferSize(0), uint64(0))
		assert.Equal(t, u.DeviceMemoryOffset(0), uint64(0))
		assert.Equal(t, u.LastKernelTime(), int64(0))
		assert.Equal(t, u.GetPriority(), 0)
		assert.Equal(t, u.GetRecentKernel(), int32(0))
		assert.Equal(t, u.GetUtilizationSwitch(), int32(0))
	})

	t.Run("setters are no-ops and never panic", func(t *testing.T) {
		u := &wholeGPUUsage{}
		u.SetDeviceSmLimit(123)
		u.SetDeviceMemoryLimit(456)
		u.SetRecentKernel(1)
		u.SetUtilizationSwitch(1)
	})

	t.Run("IsValidUUID checks length and bounds", func(t *testing.T) {
		u := &wholeGPUUsage{uuids: []string{"GPU-aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "short"}}
		assert.Assert(t, u.IsValidUUID(0))
		assert.Assert(t, !u.IsValidUUID(1))
		assert.Assert(t, !u.IsValidUUID(-1))
		assert.Assert(t, !u.IsValidUUID(2))
	})

	t.Run("DeviceUUID returns the uuid or empty string out of range", func(t *testing.T) {
		u := &wholeGPUUsage{uuids: []string{"GPU-1"}}
		assert.Equal(t, u.DeviceUUID(0), "GPU-1")
		assert.Equal(t, u.DeviceUUID(-1), "")
		assert.Equal(t, u.DeviceUUID(1), "")
	})

	t.Run("DeviceMemoryTotal reports bytes used, DeviceMemoryLimit reports capacity", func(t *testing.T) {
		u := &wholeGPUUsage{
			uuids: []string{"GPU-1"},
			nvmllib: &mock.Interface{
				DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) {
					return newHandle(nvml.Memory{Used: 4096, Total: 8192}, nvml.SUCCESS, nvml.Utilization{}, nvml.SUCCESS), nvml.SUCCESS
				},
			},
		}
		assert.Equal(t, u.DeviceMemoryTotal(0), uint64(4096))
		assert.Equal(t, u.DeviceMemoryLimit(0), uint64(8192))
	})

	t.Run("DeviceSmUtil reports GPU utilization percentage", func(t *testing.T) {
		u := &wholeGPUUsage{
			uuids: []string{"GPU-1"},
			nvmllib: &mock.Interface{
				DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) {
					return newHandle(nvml.Memory{}, nvml.SUCCESS, nvml.Utilization{Gpu: 42}, nvml.SUCCESS), nvml.SUCCESS
				},
			},
		}
		assert.Equal(t, u.DeviceSmUtil(0), uint64(42))
	})

	t.Run("handle lookup failure yields zero values, not a panic", func(t *testing.T) {
		u := &wholeGPUUsage{
			uuids: []string{"GPU-1"},
			nvmllib: &mock.Interface{
				DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) {
					return nil, nvml.ERROR_NOT_SUPPORTED
				},
			},
		}
		assert.Equal(t, u.DeviceMemoryTotal(0), uint64(0))
		assert.Equal(t, u.DeviceMemoryLimit(0), uint64(0))
		assert.Equal(t, u.DeviceSmUtil(0), uint64(0))
	})

	t.Run("memory and utilization query failure yields zero", func(t *testing.T) {
		u := &wholeGPUUsage{
			uuids: []string{"GPU-1"},
			nvmllib: &mock.Interface{
				DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) {
					return newHandle(nvml.Memory{Used: 999, Total: 999}, nvml.ERROR_NOT_SUPPORTED, nvml.Utilization{Gpu: 99}, nvml.ERROR_NOT_SUPPORTED), nvml.SUCCESS
				},
			},
		}
		assert.Equal(t, u.DeviceMemoryTotal(0), uint64(0))
		assert.Equal(t, u.DeviceMemoryLimit(0), uint64(0))
		assert.Equal(t, u.DeviceSmUtil(0), uint64(0))
	})

	t.Run("out of range index never touches NVML", func(t *testing.T) {
		u := &wholeGPUUsage{uuids: []string{"GPU-1"}}
		assert.Equal(t, u.DeviceMemoryTotal(5), uint64(0))
		assert.Equal(t, u.DeviceMemoryLimit(-1), uint64(0))
		assert.Equal(t, u.DeviceSmUtil(5), uint64(0))
	})
}

func Test_reconcileWholeGPU_addsSynthesizedEntry(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default", UID: "uid1",
			Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-1", Type: nv.NvidiaGPUDevice, Usedmem: 8000}}),
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ctr"}}},
	}
	l := &ContainerLister{
		containers: map[string]*ContainerUsage{},
		wholeGPU: &wholeGPUState{
			nvmllib:         &mock.Interface{},
			nvmlInitialized: true,
			verdicts:        make(map[string]wholeGPUVerdict),
			getNodeDevices: func() (map[string]*device.DeviceInfo, error) {
				return map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}, nil
			},
		},
	}

	l.reconcileWholeGPU([]*corev1.Pod{pod})

	got, ok := l.containers["uid1_ctr"]
	assert.Equal(t, ok, true)
	assert.Equal(t, got.synthesized, true)
	assert.Equal(t, got.PodUID, "uid1")
	assert.Equal(t, got.ContainerName, "ctr")
	assert.Assert(t, got.data == nil)
	usage, ok := got.Info.(*wholeGPUUsage)
	assert.Assert(t, ok)
	assert.Equal(t, len(usage.uuids), 1)
	assert.Equal(t, usage.uuids[0], "GPU-1")
	assert.Equal(t, l.wholeGPU.verdicts["uid1_ctr"], confirmedWholeGPU)
}

func Test_reconcileWholeGPU_replacesRealShmEntry(t *testing.T) {
	dir := t.TempDir()
	writeCacheFile(t, dir, "x.cache", headerBytes(v1CacheFileSize, SharedRegionMagicFlag, 1, 0))
	existing, err := loadCache(dir)
	assert.NilError(t, err)
	assert.Assert(t, existing.data != nil)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default", UID: "uid2",
			Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-1", Type: nv.NvidiaGPUDevice, Usedmem: 8000}}),
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ctr"}}},
	}
	key := "uid2_ctr"
	l := &ContainerLister{
		containers: map[string]*ContainerUsage{key: existing},
		wholeGPU: &wholeGPUState{
			nvmllib:         &mock.Interface{},
			nvmlInitialized: true,
			verdicts:        make(map[string]wholeGPUVerdict),
			getNodeDevices: func() (map[string]*device.DeviceInfo, error) {
				return map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}, nil
			},
		},
	}

	l.reconcileWholeGPU([]*corev1.Pod{pod})

	got, ok := l.containers[key]
	assert.Equal(t, ok, true)
	assert.Assert(t, got != existing)
	assert.Equal(t, got.synthesized, true)
	assert.Assert(t, got.data == nil)
}

func Test_reconcileWholeGPU_prunesOnPodDeletion(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default", UID: "uid3",
			Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-1", Type: nv.NvidiaGPUDevice, Usedmem: 8000}}),
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ctr"}}},
	}
	l := &ContainerLister{
		containers: map[string]*ContainerUsage{},
		wholeGPU: &wholeGPUState{
			nvmllib:         &mock.Interface{},
			nvmlInitialized: true,
			verdicts:        make(map[string]wholeGPUVerdict),
			getNodeDevices: func() (map[string]*device.DeviceInfo, error) {
				return map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}, nil
			},
		},
	}
	l.reconcileWholeGPU([]*corev1.Pod{pod})
	_, ok := l.containers["uid3_ctr"]
	assert.Equal(t, ok, true)

	l.reconcileWholeGPU([]*corev1.Pod{})

	_, ok = l.containers["uid3_ctr"]
	assert.Equal(t, ok, false)
	_, cached := l.wholeGPU.verdicts["uid3_ctr"]
	assert.Equal(t, cached, false)
}

func Test_reconcileWholeGPU_verdictCached_noNodeGet(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p", Namespace: "default", UID: "uid4",
			// Fractional allocation: evaluateContainerWholeGPU returns a
			// terminal notWholeGPU verdict, which must also be cached.
			Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-1", Type: nv.NvidiaGPUDevice, Usedmem: 2000}}),
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ctr"}}},
	}
	calls := 0
	l := &ContainerLister{
		containers: map[string]*ContainerUsage{},
		wholeGPU: &wholeGPUState{
			nvmllib:         &mock.Interface{},
			nvmlInitialized: true,
			verdicts:        make(map[string]wholeGPUVerdict),
			getNodeDevices: func() (map[string]*device.DeviceInfo, error) {
				calls++
				return map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}, nil
			},
		},
	}

	l.reconcileWholeGPU([]*corev1.Pod{pod})
	assert.Equal(t, calls, 1)
	assert.Equal(t, l.wholeGPU.verdicts["uid4_ctr"], notWholeGPU)

	l.reconcileWholeGPU([]*corev1.Pod{pod})
	l.reconcileWholeGPU([]*corev1.Pod{pod})
	assert.Equal(t, calls, 1)
}

func Test_reconcileWholeGPU_noFlapOnNodeTableFailure(t *testing.T) {
	newLister := func(fail *bool) (*ContainerLister, *corev1.Pod) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "p", Namespace: "default", UID: "uid5",
				Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-1", Type: nv.NvidiaGPUDevice, Usedmem: 8000}}),
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ctr"}}},
		}
		l := &ContainerLister{
			containers: map[string]*ContainerUsage{},
			wholeGPU: &wholeGPUState{
				nvmllib:         &mock.Interface{},
				nvmlInitialized: true,
				verdicts:        make(map[string]wholeGPUVerdict),
				getNodeDevices: func() (map[string]*device.DeviceInfo, error) {
					if *fail {
						return nil, errors.New("apiserver unavailable")
					}
					return map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}, nil
				},
			},
		}
		return l, pod
	}

	t.Run("a brand new candidate is skipped, not misclassified, during an outage", func(t *testing.T) {
		fail := true
		l, pod := newLister(&fail)
		l.reconcileWholeGPU([]*corev1.Pod{pod})

		_, ok := l.containers["uid5_ctr"]
		assert.Equal(t, ok, false)
		_, cached := l.wholeGPU.verdicts["uid5_ctr"]
		assert.Equal(t, cached, false)
	})

	t.Run("an already-confirmed entry survives an outage untouched", func(t *testing.T) {
		fail := false
		l, pod := newLister(&fail)
		l.reconcileWholeGPU([]*corev1.Pod{pod})
		before := l.containers["uid5_ctr"]
		assert.Assert(t, before != nil)

		fail = true
		l.reconcileWholeGPU([]*corev1.Pod{pod})

		after, ok := l.containers["uid5_ctr"]
		assert.Equal(t, ok, true)
		assert.Assert(t, after == before)
	})
}

func Test_ContainerLister_Update_WholeGPU(t *testing.T) {
	originalEnabled := wholeGPUMonitoringEnabled
	t.Cleanup(func() { wholeGPUMonitoringEnabled = originalEnabled })

	t.Run("disabled: annotated pod is left alone, matching current behavior", func(t *testing.T) {
		wholeGPUMonitoringEnabled = false
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "p", Namespace: "default", UID: "uid7",
				Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-1", Type: nv.NvidiaGPUDevice, Usedmem: 8000}}),
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ctr"}}},
		}
		l := &ContainerLister{
			containerPath: t.TempDir(),
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(pod),
		}
		assert.NilError(t, l.Update())
		assert.Equal(t, len(l.containers), 0)
		assert.Assert(t, l.wholeGPU == nil)
	})

	t.Run("enabled: annotated pod gets a synthesized entry through Update", func(t *testing.T) {
		wholeGPUMonitoringEnabled = true
		defer func() { wholeGPUMonitoringEnabled = false }()

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "p", Namespace: "default", UID: "uid8",
				Annotations: wholeGPUAnnotation(device.ContainerDevices{{UUID: "GPU-1", Type: nv.NvidiaGPUDevice, Usedmem: 8000}}),
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ctr"}}},
		}
		l := &ContainerLister{
			containerPath: t.TempDir(),
			containers:    map[string]*ContainerUsage{},
			podLister:     newTestPodLister(pod),
			wholeGPU: &wholeGPUState{
				nvmllib:         &mock.Interface{},
				nvmlInitialized: true,
				verdicts:        make(map[string]wholeGPUVerdict),
				getNodeDevices: func() (map[string]*device.DeviceInfo, error) {
					return map[string]*device.DeviceInfo{"GPU-1": {ID: "GPU-1", Devmem: 8000, Mode: ""}}, nil
				},
			},
		}
		assert.NilError(t, l.Update())

		got, ok := l.containers["uid8_ctr"]
		assert.Equal(t, ok, true)
		assert.Equal(t, got.synthesized, true)
	})
}
