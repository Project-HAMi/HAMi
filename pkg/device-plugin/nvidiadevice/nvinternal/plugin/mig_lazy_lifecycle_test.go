/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package plugin

import (
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	v1 "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func initializedLazyMIGManager(t *testing.T, dev nvml.Device) *MigInstanceManager {
	t.Helper()
	manager := newMigInstanceManager(&nvmlmock.Interface{
		InitFunc:                   func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:               func() nvml.Return { return nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
	})
	require.NoError(t, manager.Init())
	t.Cleanup(manager.Shutdown)
	return manager
}

func TestMIGRecoveryCleansOrphanGPUInstance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		destroyRet nvml.Return
		wantError  bool
	}{
		{name: "destroyed", destroyRet: nvml.SUCCESS},
		{name: "already absent", destroyRet: nvml.ERROR_NOT_FOUND},
		{name: "destroy failure", destroyRet: nvml.ERROR_UNKNOWN, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
			gi := &nvmlmock.GpuInstance{
				GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
					return nvml.GpuInstanceInfo{Id: 1, Placement: placement}, nvml.SUCCESS
				},
				GetComputeInstanceProfileInfoFunc: func(profileID, _ int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
					if profileID != 0 {
						return nvml.ComputeInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
					}
					return nvml.ComputeInstanceProfileInfo{Id: uint32(profileID)}, nvml.SUCCESS
				},
				GetComputeInstancesFunc: func(*nvml.ComputeInstanceProfileInfo) ([]nvml.ComputeInstance, nvml.Return) {
					return nil, nvml.SUCCESS
				},
				DestroyFunc: func() nvml.Return { return tc.destroyRet },
			}
			profileID := profileNameToGIProfileID["1g"]
			dev := &nvmlmock.Device{
				GetMigModeFunc: func() (int, int, nvml.Return) {
					return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
				},
				GetGpuInstanceProfileInfoFunc: func(id int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
					if id != profileID {
						return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
					}
					return nvml.GpuInstanceProfileInfo{Id: uint32(id)}, nvml.SUCCESS
				},
				GetGpuInstancesFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
					return []nvml.GpuInstance{gi}, nvml.SUCCESS
				},
			}
			manager := initializedLazyMIGManager(t, dev)

			err := manager.RestoreAllocations(1, nil, nil, true)
			if tc.wantError {
				require.ErrorContains(t, err, "destroy orphan GI")
			} else {
				require.NoError(t, err)
			}
			require.Len(t, gi.DestroyCalls(), 1)
			require.Empty(t, manager.byAllocation)
		})
	}
}

func TestMIGRecoveryPreservesGPUInstanceWhenComputeInspectionIsUnsupported(t *testing.T) {
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	gi := &nvmlmock.GpuInstance{
		GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
			return nvml.GpuInstanceInfo{Id: 1, Placement: placement}, nvml.SUCCESS
		},
		GetComputeInstanceProfileInfoFunc: func(int, int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			return nvml.ComputeInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		DestroyFunc: func() nvml.Return { return nvml.SUCCESS },
	}
	profileID := profileNameToGIProfileID["1g"]
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(id int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			if id != profileID {
				return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
			}
			return nvml.GpuInstanceProfileInfo{Id: uint32(id)}, nvml.SUCCESS
		},
		GetGpuInstancesFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
			return []nvml.GpuInstance{gi}, nvml.SUCCESS
		},
	}
	manager := initializedLazyMIGManager(t, dev)

	err := manager.RestoreAllocations(1, nil, nil, true)
	require.ErrorContains(t, err, "no supported compute instance profile could be inspected")
	require.Empty(t, gi.DestroyCalls())
	require.Empty(t, manager.byAllocation)
}

func TestMIGRecoveryPreservesUnownedInstanceByDefault(t *testing.T) {
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	ci := &nvmlmock.ComputeInstance{GetInfoFunc: func() (nvml.ComputeInstanceInfo, nvml.Return) {
		return nvml.ComputeInstanceInfo{Id: 2}, nvml.SUCCESS
	}}
	gi := &nvmlmock.GpuInstance{
		GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
			return nvml.GpuInstanceInfo{Id: 1, Placement: placement}, nvml.SUCCESS
		},
		GetComputeInstanceProfileInfoFunc: func(profileID, _ int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			if profileID == 0 {
				return nvml.ComputeInstanceProfileInfo{Id: 0}, nvml.SUCCESS
			}
			return nvml.ComputeInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetComputeInstancesFunc: func(*nvml.ComputeInstanceProfileInfo) ([]nvml.ComputeInstance, nvml.Return) {
			return []nvml.ComputeInstance{ci}, nvml.SUCCESS
		},
		DestroyFunc: func() nvml.Return { return nvml.SUCCESS },
	}
	profileID := profileNameToGIProfileID["1g"]
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(id int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			if id == profileID {
				return nvml.GpuInstanceProfileInfo{Id: uint32(id)}, nvml.SUCCESS
			}
			return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetGpuInstancesFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
			return []nvml.GpuInstance{gi}, nvml.SUCCESS
		},
		GetMaxMigDeviceCountFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		GetMigDeviceHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) {
			return &nvmlmock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
				GetUUIDFunc:          func() (string, nvml.Return) { return "MIG-external", nvml.SUCCESS },
			}, nvml.SUCCESS
		},
	}
	manager := initializedLazyMIGManager(t, dev)

	require.NoError(t, manager.RestoreAllocations(1, nil, nil, false))
	require.Empty(t, manager.byAllocation)
	require.Empty(t, gi.DestroyCalls())
	require.Empty(t, ci.DestroyCalls())
}

func TestMIGRecoveryRestoresPersistedHAMiInstanceByDefault(t *testing.T) {
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	ci := &nvmlmock.ComputeInstance{GetInfoFunc: func() (nvml.ComputeInstanceInfo, nvml.Return) {
		return nvml.ComputeInstanceInfo{Id: 2}, nvml.SUCCESS
	}}
	gi := &nvmlmock.GpuInstance{
		GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
			return nvml.GpuInstanceInfo{Id: 1, Placement: placement}, nvml.SUCCESS
		},
		GetComputeInstanceProfileInfoFunc: func(profileID, _ int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			if profileID == 0 {
				return nvml.ComputeInstanceProfileInfo{Id: 0}, nvml.SUCCESS
			}
			return nvml.ComputeInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetComputeInstancesFunc: func(*nvml.ComputeInstanceProfileInfo) ([]nvml.ComputeInstance, nvml.Return) {
			return []nvml.ComputeInstance{ci}, nvml.SUCCESS
		},
	}
	profileID := profileNameToGIProfileID["1g"]
	dev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "GPU-parent", nvml.SUCCESS },
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(id int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			if id == profileID {
				return nvml.GpuInstanceProfileInfo{Id: uint32(id)}, nvml.SUCCESS
			}
			return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetGpuInstancesFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
			return []nvml.GpuInstance{gi}, nvml.SUCCESS
		},
		GetMaxMigDeviceCountFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		GetMigDeviceHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) {
			return &nvmlmock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
				GetUUIDFunc:          func() (string, nvml.Return) { return "MIG-owned", nvml.SUCCESS },
			}, nvml.SUCCESS
		},
	}
	manager := initializedLazyMIGManager(t, dev)
	owned := map[string]migOwnershipRecord{
		"MIG-owned": {
			Version: dynamicMIGOwnershipVersion, MIGUUID: "MIG-owned", ParentGPUUUID: "GPU-parent", Profile: "1g.5gb",
			Placement: migOwnershipPlacement{Start: placement.Start, Size: placement.Size}, GPUInstanceID: 1, ComputeInstanceID: 2,
		},
	}

	require.NoError(t, manager.RestoreAllocations(1, nil, owned, false))
	key := allocationKey(0, "1g.5gb", placement)
	require.Equal(t, migInstanceIdle, manager.byAllocation[key].State)

	result, err := manager.EnsureAllocation(0, "1g.5gb", placement)
	require.NoError(t, err)
	require.Equal(t, "MIG-owned", result.MigUUID)
	require.True(t, result.Reused)
	require.False(t, result.Created)
}

func TestMIGRecoveryRejectsOwnershipThatDoesNotMatchNVML(t *testing.T) {
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	ci := &nvmlmock.ComputeInstance{GetInfoFunc: func() (nvml.ComputeInstanceInfo, nvml.Return) {
		return nvml.ComputeInstanceInfo{Id: 2}, nvml.SUCCESS
	}}
	gi := &nvmlmock.GpuInstance{
		GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
			return nvml.GpuInstanceInfo{Id: 1, Placement: placement}, nvml.SUCCESS
		},
		GetComputeInstanceProfileInfoFunc: func(profileID, _ int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			if profileID == 0 {
				return nvml.ComputeInstanceProfileInfo{Id: 0}, nvml.SUCCESS
			}
			return nvml.ComputeInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetComputeInstancesFunc: func(*nvml.ComputeInstanceProfileInfo) ([]nvml.ComputeInstance, nvml.Return) {
			return []nvml.ComputeInstance{ci}, nvml.SUCCESS
		},
	}
	profileID := profileNameToGIProfileID["1g"]
	dev := &nvmlmock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "GPU-parent", nvml.SUCCESS },
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(id int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			if id == profileID {
				return nvml.GpuInstanceProfileInfo{Id: uint32(id)}, nvml.SUCCESS
			}
			return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetGpuInstancesFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
			return []nvml.GpuInstance{gi}, nvml.SUCCESS
		},
		GetMaxMigDeviceCountFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		GetMigDeviceHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) {
			return &nvmlmock.Device{
				GetGpuInstanceIdFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
				GetUUIDFunc:          func() (string, nvml.Return) { return "MIG-owned", nvml.SUCCESS },
			}, nvml.SUCCESS
		},
	}
	manager := initializedLazyMIGManager(t, dev)
	owned := map[string]migOwnershipRecord{
		"MIG-owned": {
			Version: dynamicMIGOwnershipVersion, MIGUUID: "MIG-owned", ParentGPUUUID: "GPU-different", Profile: "1g.5gb",
			Placement: migOwnershipPlacement{Start: placement.Start, Size: placement.Size}, GPUInstanceID: 1, ComputeInstanceID: 2,
		},
	}

	err := manager.RestoreAllocations(1, nil, owned, false)
	require.ErrorContains(t, err, "does not match the live NVML geometry")
	require.Empty(t, manager.byAllocation)
}

func TestMIGRecoveryPreservesOrphanOnBusyGPU(t *testing.T) {
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	gi := &nvmlmock.GpuInstance{
		GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
			return nvml.GpuInstanceInfo{Id: 1, Placement: placement}, nvml.SUCCESS
		},
		GetComputeInstanceProfileInfoFunc: func(profileID, _ int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			if profileID == 0 {
				return nvml.ComputeInstanceProfileInfo{Id: 0}, nvml.SUCCESS
			}
			return nvml.ComputeInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetComputeInstancesFunc: func(*nvml.ComputeInstanceProfileInfo) ([]nvml.ComputeInstance, nvml.Return) {
			return nil, nvml.SUCCESS
		},
		DestroyFunc: func() nvml.Return { return nvml.SUCCESS },
	}
	profileID := profileNameToGIProfileID["1g"]
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(id int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			if id == profileID {
				return nvml.GpuInstanceProfileInfo{Id: uint32(id)}, nvml.SUCCESS
			}
			return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetGpuInstancesFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
			return []nvml.GpuInstance{gi}, nvml.SUCCESS
		},
	}
	manager := initializedLazyMIGManager(t, dev)

	require.NoError(t, manager.RestoreAllocations(1, map[int]struct{}{0: {}}, nil, true))
	require.Empty(t, manager.byAllocation)
	require.Empty(t, gi.DestroyCalls())
}

func TestMIGReleasedAllocationBecomesIdleAndIsReused(t *testing.T) {
	manager := initializedLazyMIGManager(t, &nvmlmock.Device{})
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: "1g.5gb", Placement: placement, GIID: 1, CIID: 2,
		MigUUID: "MIG-reuse", State: migInstanceActive,
	}
	manager.byAllocationMigUUID["MIG-reuse"] = key

	require.NoError(t, manager.ReconcileActiveAllocations(nil))
	require.Equal(t, migInstanceIdle, manager.byAllocation[key].State)

	result, err := manager.EnsureAllocation(0, "1g.5gb", placement)
	require.NoError(t, err)
	require.Equal(t, "MIG-reuse", result.MigUUID)
	require.False(t, result.Created)
	require.True(t, result.Reused)
	require.Empty(t, result.Reclaimed)
	require.Equal(t, migInstanceActive, manager.byAllocation[key].State)
}

func TestMIGReusedAllocationRollsBackToIdleOnPreparationFailure(t *testing.T) {
	dev := &nvmlmock.Device{GetIndexFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS }}
	manager := newMigInstanceManager(&nvmlmock.Interface{
		InitFunc:                  func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:              func() nvml.Return { return nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc: func(string) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
	})
	require.NoError(t, manager.Init())
	t.Cleanup(manager.Shutdown)
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: placement, GIID: 1, CIID: 2,
		MigUUID: "MIG-reuse", State: migInstanceIdle,
	}
	manager.byAllocationMigUUID["MIG-reuse"] = key
	strategies, err := v1.NewDeviceListStrategies([]string{"cdi-cri"})
	require.NoError(t, err)
	plugin := &NvidiaDevicePlugin{
		operatingMode: nvidia.MigMode, migMgr: manager, deviceListStrategies: strategies,
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-test","profile":"1g.5gb","placement":{"start":0,"size":1}}]`,
		}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "gpu"}}},
	}

	_, err = plugin.GetContainerDeviceStrArray(device.ContainerDevices{cd("GPU-test", "NVIDIA", 0, 0)}, pod, "gpu")
	require.ErrorContains(t, err, "dynamic MIG CDI handler is unavailable")
	require.Equal(t, migInstanceIdle, manager.byAllocation[key].State)
}

func TestMIGRecoveredSliceProfileIsReusedByFullProfile(t *testing.T) {
	manager := initializedLazyMIGManager(t, &nvmlmock.Device{})
	placement := nvml.GpuInstancePlacement{Start: 2, Size: 1}
	recoveredKey := allocationKey(0, "1g", placement)
	manager.byAllocation[recoveredKey] = &migInstance{
		Profile: "1g", Placement: placement, GIID: 3, CIID: 4,
		MigUUID: "MIG-recovered", State: migInstanceIdle,
	}
	manager.byAllocationMigUUID["MIG-recovered"] = recoveredKey

	result, err := manager.EnsureAllocation(0, "1g.5gb", placement)
	require.NoError(t, err)
	require.Equal(t, "MIG-recovered", result.MigUUID)
	require.NotContains(t, manager.byAllocation, recoveredKey)
	fullKey := allocationKey(0, "1g.5gb", placement)
	require.Equal(t, migInstanceActive, manager.byAllocation[fullKey].State)
	require.Equal(t, fullKey, manager.byAllocationMigUUID["MIG-recovered"])
}

func TestMIGPlacementPressureReclaimsOnlyBlockingIdleInstances(t *testing.T) {
	target := nvml.GpuInstancePlacement{Start: 0, Size: 2}
	oldCI := &nvmlmock.ComputeInstance{DestroyFunc: func() nvml.Return { return nvml.SUCCESS }}
	oldGI := &nvmlmock.GpuInstance{
		GetComputeInstanceByIdFunc: func(int) (nvml.ComputeInstance, nvml.Return) { return oldCI, nvml.SUCCESS },
		DestroyFunc:                func() nvml.Return { return nvml.SUCCESS },
	}
	newCI := &nvmlmock.ComputeInstance{
		GetInfoFunc: func() (nvml.ComputeInstanceInfo, nvml.Return) {
			return nvml.ComputeInstanceInfo{Id: 20}, nvml.SUCCESS
		},
	}
	newGI := &nvmlmock.GpuInstance{
		GetInfoFunc: func() (nvml.GpuInstanceInfo, nvml.Return) {
			return nvml.GpuInstanceInfo{Id: 10, Placement: target}, nvml.SUCCESS
		},
		GetComputeInstanceProfileInfoFunc: func(int, int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			return nvml.ComputeInstanceProfileInfo{}, nvml.SUCCESS
		},
		CreateComputeInstanceFunc: func(*nvml.ComputeInstanceProfileInfo) (nvml.ComputeInstance, nvml.Return) {
			return newCI, nvml.SUCCESS
		},
	}
	migDev := &nvmlmock.Device{
		GetGpuInstanceIdFunc: func() (int, nvml.Return) { return 10, nvml.SUCCESS },
		GetUUIDFunc:          func() (string, nvml.Return) { return "MIG-new", nvml.SUCCESS },
	}
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceByIdFunc: func(int) (nvml.GpuInstance, nvml.Return) { return oldGI, nvml.SUCCESS },
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.SUCCESS
		},
		GetGpuInstancePossiblePlacementsFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstancePlacement, nvml.Return) {
			return []nvml.GpuInstancePlacement{target}, nvml.SUCCESS
		},
		CreateGpuInstanceWithPlacementFunc: func(*nvml.GpuInstanceProfileInfo, *nvml.GpuInstancePlacement) (nvml.GpuInstance, nvml.Return) {
			return newGI, nvml.SUCCESS
		},
		GetMaxMigDeviceCountFunc:      func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		GetMigDeviceHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return migDev, nvml.SUCCESS },
	}
	manager := initializedLazyMIGManager(t, dev)
	oldTime := time.Unix(1, 0)
	blockingKey := allocationKey(0, "1g.5gb", nvml.GpuInstancePlacement{Start: 0, Size: 1})
	nonBlockingKey := allocationKey(0, "1g.5gb", nvml.GpuInstancePlacement{Start: 6, Size: 1})
	manager.byAllocation[blockingKey] = &migInstance{Profile: blockingKey.Profile, Placement: nvml.GpuInstancePlacement{Start: 0, Size: 1}, GIID: 1, CIID: 2, MigUUID: "MIG-old", State: migInstanceIdle, LastUsed: oldTime}
	manager.byAllocation[nonBlockingKey] = &migInstance{Profile: nonBlockingKey.Profile, Placement: nvml.GpuInstancePlacement{Start: 6, Size: 1}, GIID: 3, CIID: 4, MigUUID: "MIG-kept", State: migInstanceIdle, LastUsed: oldTime}
	manager.byAllocationMigUUID["MIG-old"] = blockingKey
	manager.byAllocationMigUUID["MIG-kept"] = nonBlockingKey

	result, err := manager.EnsureAllocation(0, "2g.10gb", target)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, "MIG-new", result.MigUUID)
	require.Equal(t, []string{"MIG-old"}, result.Reclaimed)
	require.NotContains(t, manager.byAllocationMigUUID, "MIG-old")
	require.Contains(t, manager.byAllocationMigUUID, "MIG-kept")
	require.Len(t, oldGI.DestroyCalls(), 1)
	require.Len(t, oldCI.DestroyCalls(), 1)
}

func TestMIGReclaimFailurePreservesTrackedInstance(t *testing.T) {
	requested := nvml.GpuInstancePlacement{Start: 0, Size: 2}
	ci := &nvmlmock.ComputeInstance{DestroyFunc: func() nvml.Return { return nvml.ERROR_UNKNOWN }}
	gi := &nvmlmock.GpuInstance{
		GetComputeInstanceByIdFunc: func(int) (nvml.ComputeInstance, nvml.Return) { return ci, nvml.SUCCESS },
	}
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.SUCCESS
		},
		GetGpuInstancePossiblePlacementsFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstancePlacement, nvml.Return) {
			return []nvml.GpuInstancePlacement{requested}, nvml.SUCCESS
		},
		GetGpuInstanceByIdFunc: func(int) (nvml.GpuInstance, nvml.Return) { return gi, nvml.SUCCESS },
	}
	manager := initializedLazyMIGManager(t, dev)
	idlePlacement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", idlePlacement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: idlePlacement, GIID: 1, CIID: 2, MigUUID: "MIG-error", State: migInstanceIdle,
	}
	manager.byAllocationMigUUID["MIG-error"] = key

	result, err := manager.EnsureAllocation(0, "2g.10gb", requested)
	require.ErrorContains(t, err, "destroy CI")
	require.Empty(t, result.Reclaimed)
	require.Contains(t, manager.byAllocationMigUUID, "MIG-error")
	require.Equal(t, migInstanceError, manager.byAllocation[key].State)
}

func TestMIGErrorAtExactKeyRetriesReclamation(t *testing.T) {
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	ci := &nvmlmock.ComputeInstance{DestroyFunc: func() nvml.Return { return nvml.SUCCESS }}
	gi := &nvmlmock.GpuInstance{
		GetComputeInstanceByIdFunc: func(int) (nvml.ComputeInstance, nvml.Return) { return ci, nvml.SUCCESS },
		DestroyFunc:                func() nvml.Return { return nvml.SUCCESS },
	}
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.SUCCESS
		},
		GetGpuInstancePossiblePlacementsFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstancePlacement, nvml.Return) {
			return []nvml.GpuInstancePlacement{placement}, nvml.SUCCESS
		},
		GetGpuInstanceByIdFunc: func(int) (nvml.GpuInstance, nvml.Return) { return gi, nvml.SUCCESS },
		CreateGpuInstanceWithPlacementFunc: func(*nvml.GpuInstanceProfileInfo, *nvml.GpuInstancePlacement) (nvml.GpuInstance, nvml.Return) {
			return nil, nvml.ERROR_UNKNOWN
		},
	}
	manager := initializedLazyMIGManager(t, dev)
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: placement, GIID: 1, CIID: 2, MigUUID: "MIG-error", State: migInstanceError,
	}
	manager.byAllocationMigUUID["MIG-error"] = key

	result, err := manager.EnsureAllocation(0, "1g.5gb", placement)
	require.ErrorContains(t, err, "create GI")
	require.Equal(t, []string{"MIG-error"}, result.Reclaimed)
	require.NotContains(t, manager.byAllocationMigUUID, "MIG-error")
	require.Len(t, gi.DestroyCalls(), 1)
	require.Len(t, ci.DestroyCalls(), 1)
}

func TestMIGInvalidPlacementDoesNotReclaimIdleInstance(t *testing.T) {
	valid := nvml.GpuInstancePlacement{Start: 4, Size: 2}
	requested := nvml.GpuInstancePlacement{Start: 0, Size: 2}
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.SUCCESS
		},
		GetGpuInstancePossiblePlacementsFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstancePlacement, nvml.Return) {
			return []nvml.GpuInstancePlacement{valid}, nvml.SUCCESS
		},
	}
	manager := initializedLazyMIGManager(t, dev)
	idlePlacement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", idlePlacement)
	manager.byAllocation[key] = &migInstance{Profile: key.Profile, Placement: idlePlacement, MigUUID: "MIG-idle", State: migInstanceIdle}
	manager.byAllocationMigUUID["MIG-idle"] = key

	_, err := manager.EnsureAllocation(0, "2g.10gb", requested)
	require.ErrorContains(t, err, "invalid placement")
	require.Contains(t, manager.byAllocationMigUUID, "MIG-idle")
	require.Equal(t, migInstanceIdle, manager.byAllocation[key].State)
}

func TestMIGPlacementPressureNeverReclaimsActiveInstance(t *testing.T) {
	requested := nvml.GpuInstancePlacement{Start: 0, Size: 2}
	dev := &nvmlmock.Device{
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetGpuInstanceProfileInfoFunc: func(int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
			return nvml.GpuInstanceProfileInfo{}, nvml.SUCCESS
		},
		GetGpuInstancePossiblePlacementsFunc: func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstancePlacement, nvml.Return) {
			return []nvml.GpuInstancePlacement{requested}, nvml.SUCCESS
		},
	}
	manager := initializedLazyMIGManager(t, dev)
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{Profile: key.Profile, Placement: placement, MigUUID: "MIG-active", State: migInstanceActive}
	manager.byAllocationMigUUID["MIG-active"] = key

	_, err := manager.EnsureAllocation(0, "2g.10gb", requested)
	require.ErrorContains(t, err, "overlaps Active MIG allocation")
	require.Contains(t, manager.byAllocationMigUUID, "MIG-active")
}

func TestMIGReconcilerRetriesErroredCleanup(t *testing.T) {
	destroyAttempts := 0
	ci := &nvmlmock.ComputeInstance{DestroyFunc: func() nvml.Return {
		destroyAttempts++
		if destroyAttempts == 1 {
			return nvml.ERROR_UNKNOWN
		}
		return nvml.SUCCESS
	}}
	gi := &nvmlmock.GpuInstance{
		GetComputeInstanceByIdFunc: func(int) (nvml.ComputeInstance, nvml.Return) { return ci, nvml.SUCCESS },
		DestroyFunc:                func() nvml.Return { return nvml.SUCCESS },
	}
	dev := &nvmlmock.Device{
		GetGpuInstanceByIdFunc: func(int) (nvml.GpuInstance, nvml.Return) { return gi, nvml.SUCCESS },
	}
	manager := initializedLazyMIGManager(t, dev)
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: placement, GIID: 1, CIID: 2,
		MigUUID: "MIG-error", State: migInstanceError,
	}
	manager.byAllocationMigUUID["MIG-error"] = key
	strategies, err := v1.NewDeviceListStrategies([]string{"cdi-cri"})
	require.NoError(t, err)
	removed := []string{}
	handler := &recordingDynamicMIGCDI{remove: func(uuid string) error {
		removed = append(removed, uuid)
		return nil
	}}
	plugin := &NvidiaDevicePlugin{
		migMgr: manager, migPrimed: true, cdiHandler: handler, deviceListStrategies: strategies,
		listNodePods: func() ([]*corev1.Pod, error) { return nil, nil },
	}

	require.ErrorContains(t, plugin.reconcileActiveMigAllocations(), "retry cleanup")
	require.Contains(t, manager.byAllocation, key)
	require.Contains(t, manager.byAllocationMigUUID, "MIG-error")
	require.Empty(t, removed)

	require.NoError(t, plugin.reconcileActiveMigAllocations())
	require.NotContains(t, manager.byAllocation, key)
	require.NotContains(t, manager.byAllocationMigUUID, "MIG-error")
	require.Equal(t, []string{"MIG-error"}, removed)
	require.Equal(t, 2, destroyAttempts)
}

func TestMIGCleanupRetryPreservesActiveReservation(t *testing.T) {
	manager := initializedLazyMIGManager(t, &nvmlmock.Device{})
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: placement, MigUUID: "MIG-error", State: migInstanceError,
	}
	manager.byAllocationMigUUID["MIG-error"] = key

	reclaimed, err := manager.RetryErrorAllocations(map[migAllocationKey]struct{}{key: {}})
	require.NoError(t, err)
	require.Empty(t, reclaimed)
	require.Contains(t, manager.byAllocation, key)
}

func TestIdleMIGKeepsCDIEntry(t *testing.T) {
	manager := initializedLazyMIGManager(t, &nvmlmock.Device{})
	placement := nvml.GpuInstancePlacement{Start: 0, Size: 1}
	key := allocationKey(0, "1g.5gb", placement)
	manager.byAllocation[key] = &migInstance{
		Profile: key.Profile, Placement: placement, MigUUID: "MIG-idle-CDI", State: migInstanceActive,
	}
	manager.byAllocationMigUUID["MIG-idle-CDI"] = key
	strategies, err := v1.NewDeviceListStrategies([]string{"cdi-cri"})
	require.NoError(t, err)
	removeCalls := 0
	handler := &recordingDynamicMIGCDI{remove: func(string) error { removeCalls++; return nil }}
	plugin := &NvidiaDevicePlugin{
		migMgr: manager, migPrimed: true, cdiHandler: handler, deviceListStrategies: strategies,
		listNodePods:     func() ([]*corev1.Pod, error) { return nil, nil },
		listLiveNodePods: func() ([]*corev1.Pod, error) { return nil, nil },
	}

	require.NoError(t, plugin.reconcileActiveMigAllocations())
	require.Equal(t, migInstanceIdle, manager.byAllocation[key].State)
	require.Zero(t, removeCalls)
}

func TestReclaimedMIGCDIRemovalRetriesWithoutBlockingAllocation(t *testing.T) {
	strategies, err := v1.NewDeviceListStrategies([]string{"cdi-cri"})
	require.NoError(t, err)
	removeErr := true
	handler := &recordingDynamicMIGCDI{remove: func(string) error {
		if removeErr {
			return errTestCDIRemove
		}
		return nil
	}}
	plugin := &NvidiaDevicePlugin{deviceListStrategies: strategies, cdiHandler: handler}

	plugin.removeReclaimedMIGArtifacts([]string{"MIG-old"})
	require.Contains(t, plugin.pendingCDIRemovals, "MIG-old")
	removeErr = false
	plugin.removeReclaimedMIGArtifacts([]string{"MIG-old"})
	require.NotContains(t, plugin.pendingCDIRemovals, "MIG-old")
}

var errTestCDIRemove = &testLifecycleError{"remove CDI"}

type testLifecycleError struct{ message string }

func (e *testLifecycleError) Error() string { return e.message }
