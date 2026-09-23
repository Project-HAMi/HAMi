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
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	nvmlmock "github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/nodepodinformer"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func TestSortedIntSetKeys(t *testing.T) {
	got := sortedIntSetKeys(map[int]struct{}{3: {}, 0: {}, 1: {}})
	want := []int{0, 1, 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortedIntSetKeys = %v, want %v", got, want)
	}
	if got := sortedIntSetKeys(nil); len(got) != 0 {
		t.Errorf("sortedIntSetKeys(nil) = %v, want empty", got)
	}
}

func TestActiveMigGPUUUIDs(t *testing.T) {
	now := metav1.NewTime(time.Now())
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-live","profile":"1g.5gb","placement":{"start":6,"size":1}}]`,
			}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-pending","profile":"2g.10gb","placement":{"start":0,"size":2}}]`,
			}},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-complete","profile":"1g.5gb","placement":{"start":0,"size":1}}]`,
			}},
			Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &now,
				Annotations: map[string]string{
					nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-deleting","profile":"1g.5gb","placement":{"start":0,"size":1}}]`,
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	}

	podPointers := make([]*corev1.Pod, 0, len(pods))
	for i := range pods {
		podPointers = append(podPointers, &pods[i])
	}
	got, err := activeMigGPUUUIDs(podPointers)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{}{"GPU-live": {}, "GPU-pending": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activeMigGPUUUIDs() = %v, want %v", got, want)
	}
}

func TestActiveMigGPUUUIDsFailsClosedOnInvalidAnnotation(t *testing.T) {
	pods := []*corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid",
			Namespace: "default",
			Annotations: map[string]string{
				nvidia.MigAllocationsAnnotation: "not-json",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}}
	if _, err := activeMigGPUUUIDs(pods); err == nil {
		t.Fatal("activeMigGPUUUIDs accepted an invalid active allocation annotation")
	}
}

func mockMigRecoveryDevice(t *testing.T) (*MigInstanceManager, *nvmlmock.Device) {
	t.Helper()
	dev := &nvmlmock.Device{
		GetIndexFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS },
		GetMigModeFunc: func() (int, int, nvml.Return) {
			return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
		},
		GetMaxMigDeviceCountFunc: func() (int, nvml.Return) { return 0, nvml.SUCCESS },
	}
	manager := newMigInstanceManager(&nvmlmock.Interface{
		InitFunc:                   func() nvml.Return { return nvml.SUCCESS },
		ShutdownFunc:               func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc:         func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(int) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
		DeviceGetHandleByUUIDFunc:  func(string) (nvml.Device, nvml.Return) { return dev, nvml.SUCCESS },
	})
	require.NoError(t, manager.Init())
	t.Cleanup(manager.Shutdown)
	return manager, dev
}

func TestMigRecoveryAllowsPendingReservations(t *testing.T) {
	manager, _ := mockMigRecoveryDevice(t)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			nvidia.MigAllocationsAnnotation: `[{"containerIndex":0,"deviceIndex":0,"gpuUUID":"GPU-test","profile":"1g.5gb","placement":{"start":0,"size":1}}]`,
		}},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	plugin := &NvidiaDevicePlugin{
		migMgr: manager, migResetDeviceCount: 1,
		listNodePods: func() ([]*corev1.Pod, error) { return []*corev1.Pod{pod}, nil },
	}
	// No runtime identity exists yet. Recovery must allow Allocate to create it.
	require.NoError(t, plugin.reconcileActiveMigAllocations())
	require.True(t, plugin.migPrimed)
	// The reservation must also protect a newly created, locally tracked instance
	// while the informer is still waiting for the runtime annotation update.
	key := allocationKey(0, "1g.5gb", nvml.GpuInstancePlacement{Start: 0, Size: 1})
	plugin.migMgr.byAllocation[key] = &migInstance{MigUUID: "MIG-new"}
	require.NoError(t, plugin.reconcileActiveMigAllocations())
	require.Contains(t, plugin.migMgr.byAllocation, key)
}

func TestMigRecoveryRejectsInvalidRuntimeIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase corev1.PodPhase
		raw   string
	}{
		{"running without identity", corev1.PodRunning, `[{"gpuUUID":"GPU-test","profile":"1g.5gb","placement":{"size":1}}]`},
		{"pending partial identity", corev1.PodPending, `[{"gpuUUID":"GPU-test","profile":"1g.5gb","placement":{"size":1},"migUUID":"MIG-test"}]`},
		{"malformed annotation", corev1.PodPending, "not-json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager, _ := mockMigRecoveryDevice(t)
			plugin := &NvidiaDevicePlugin{
				migMgr: manager, migResetDeviceCount: 1,
				listNodePods: func() ([]*corev1.Pod, error) {
					return []*corev1.Pod{{
						ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{nvidia.MigAllocationsAnnotation: tc.raw}},
						Status:     corev1.PodStatus{Phase: tc.phase},
					}}, nil
				},
			}
			// The mock has no destructive methods: any reset would panic.
			require.Error(t, plugin.reconcileActiveMigAllocations())
			require.False(t, plugin.migPrimed)
		})
	}
}

func TestMigRecoveryRetriesResetAfterInformerSync(t *testing.T) {
	manager, dev := mockMigRecoveryDevice(t)
	resets := 0
	destroyedGI, destroyedCI := 0, 0
	ci := &nvmlmock.ComputeInstance{DestroyFunc: func() nvml.Return {
		destroyedCI++
		return nvml.SUCCESS
	}}
	gi := &nvmlmock.GpuInstance{
		GetComputeInstanceProfileInfoFunc: func(profile, engine int) (nvml.ComputeInstanceProfileInfo, nvml.Return) {
			if profile == 0 {
				return nvml.ComputeInstanceProfileInfo{}, nvml.SUCCESS
			}
			return nvml.ComputeInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
		},
		GetComputeInstancesFunc: func(*nvml.ComputeInstanceProfileInfo) ([]nvml.ComputeInstance, nvml.Return) {
			return []nvml.ComputeInstance{ci}, nvml.SUCCESS
		},
		DestroyFunc: func() nvml.Return { destroyedGI++; return nvml.SUCCESS },
	}
	dev.GetGpuInstanceProfileInfoFunc = func(profile int) (nvml.GpuInstanceProfileInfo, nvml.Return) {
		if profile == nvml.GPU_INSTANCE_PROFILE_1_SLICE {
			return nvml.GpuInstanceProfileInfo{}, nvml.SUCCESS
		}
		return nvml.GpuInstanceProfileInfo{}, nvml.ERROR_NOT_SUPPORTED
	}
	dev.GetGpuInstancesFunc = func(*nvml.GpuInstanceProfileInfo) ([]nvml.GpuInstance, nvml.Return) {
		return []nvml.GpuInstance{gi}, nvml.SUCCESS
	}
	// Reset enumerates profiles only after checking/enabling MIG mode.
	dev.GetMigModeFunc = func() (int, int, nvml.Return) {
		resets++
		return nvml.DEVICE_MIG_ENABLE, nvml.DEVICE_MIG_ENABLE, nvml.SUCCESS
	}
	synced := false
	plugin := &NvidiaDevicePlugin{
		migMgr: manager, migResetDeviceCount: 1,
		listNodePods: func() ([]*corev1.Pod, error) {
			if !synced {
				return nil, nodepodinformer.ErrNotSynced
			}
			return nil, nil
		},
	}
	require.ErrorIs(t, plugin.reconcileActiveMigAllocations(), nodepodinformer.ErrNotSynced)
	require.Zero(t, resets)
	synced = true
	// Concurrent callers must perform the initial scan/reset exactly once.
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wg.Go(func() { results <- plugin.reconcileActiveMigAllocations() })
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.True(t, plugin.migPrimed)
	// Once for busy detection, once for reset; subsequent reconciles do neither.
	require.Equal(t, 2, resets)
	require.Equal(t, 1, destroyedGI)
	require.Equal(t, 1, destroyedCI)
}

func TestMigReconciliationConfirmsMissingPodBeforeDestroy(t *testing.T) {
	client := fake.NewSimpleClientset()
	informer, err := nodepodinformer.New(client, "node-a")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	informer.Start(ctx)
	require.Eventually(t, func() bool { _, err := informer.List(); return err == nil }, time.Second, time.Millisecond)
	// Keep the watch connected but suppress new events to reproduce delayed
	// propagation independently from the authoritative fake API tracker.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "new-pod", Namespace: "default", UID: "new-uid", Annotations: map[string]string{
			nvidia.MigAllocationsAnnotation: `[{"gpuUUID":"GPU-test","profile":"1g.5gb","placement":{"start":0,"size":1}}]`,
		}},
		Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	apiErr := error(nil)
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		if apiErr != nil {
			return true, nil, apiErr
		}
		items := []corev1.Pod{}
		if pod != nil {
			items = append(items, *pod)
		}
		return true, &corev1.PodList{Items: items}, nil
	})
	cached, err := informer.List()
	require.NoError(t, err)
	require.Empty(t, cached)
	manager, dev := mockMigRecoveryDevice(t)
	giDestroyed, ciDestroyed := 0, 0
	ci := &nvmlmock.ComputeInstance{DestroyFunc: func() nvml.Return { ciDestroyed++; return nvml.SUCCESS }}
	gi := &nvmlmock.GpuInstance{
		GetComputeInstanceByIdFunc: func(int) (nvml.ComputeInstance, nvml.Return) { return ci, nvml.SUCCESS },
		DestroyFunc:                func() nvml.Return { giDestroyed++; return nvml.SUCCESS },
	}
	dev.GetGpuInstanceByIdFunc = func(int) (nvml.GpuInstance, nvml.Return) { return gi, nvml.SUCCESS }
	key := allocationKey(0, "1g.5gb", nvml.GpuInstancePlacement{Start: 0, Size: 1})
	manager.byAllocation[key] = &migInstance{MigUUID: "MIG-test", GIID: 1, CIID: 2}
	manager.byAllocationMigUUID["MIG-test"] = key
	plugin := &NvidiaDevicePlugin{migMgr: manager, migPrimed: true,
		listNodePods: func() ([]*corev1.Pod, error) { return informer.ListFresh(t.Context()) },
	}
	require.NoError(t, plugin.reconcileActiveMigAllocations())
	require.Contains(t, manager.byAllocation, key)
	require.Zero(t, giDestroyed)
	require.Zero(t, ciDestroyed)
	apiErr = errors.New("API unavailable")
	require.ErrorIs(t, plugin.reconcileActiveMigAllocations(), apiErr)
	require.Contains(t, manager.byAllocation, key)
	require.Zero(t, giDestroyed)
	require.Zero(t, ciDestroyed)
	apiErr, pod = nil, nil
	require.NoError(t, plugin.reconcileActiveMigAllocations())
	require.Empty(t, manager.byAllocation)
	require.Equal(t, 1, giDestroyed)
	require.Equal(t, 1, ciDestroyed)
}
