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
	"reflect"
	"testing"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResetIdleMigGPUs(t *testing.T) {
	tests := []struct {
		name       string
		input      nvidia.MigConfigSpecSlice
		inUse      map[int]struct{}
		wantReset  []int
		wantLayout nvidia.MigConfigSpecSlice
	}{
		{
			name: "all idle gets reset to MIG-on empty partitions",
			input: nvidia.MigConfigSpecSlice{
				{MigEnabled: true, Devices: []int32{0}, MigDevices: map[string]int32{"1g.5gb": 4}},
				{MigEnabled: false, Devices: []int32{1}, MigDevices: map[string]int32{}},
			},
			inUse:     map[int]struct{}{},
			wantReset: []int{0, 1},
			wantLayout: nvidia.MigConfigSpecSlice{
				{MigEnabled: true, Devices: []int32{0}, MigDevices: map[string]int32{}},
				{MigEnabled: true, Devices: []int32{1}, MigDevices: map[string]int32{}},
			},
		},
		{
			name: "busy gpu is preserved, idle is reset",
			input: nvidia.MigConfigSpecSlice{
				{MigEnabled: true, Devices: []int32{0}, MigDevices: map[string]int32{"1g.5gb": 4}},
				{MigEnabled: true, Devices: []int32{1}, MigDevices: map[string]int32{"3g.20gb": 2}},
			},
			inUse:     map[int]struct{}{0: {}},
			wantReset: []int{1},
			wantLayout: nvidia.MigConfigSpecSlice{
				{MigEnabled: true, Devices: []int32{0}, MigDevices: map[string]int32{"1g.5gb": 4}},
				{MigEnabled: true, Devices: []int32{1}, MigDevices: map[string]int32{}},
			},
		},
		{
			name: "spec entry without devices is left alone",
			input: nvidia.MigConfigSpecSlice{
				{MigEnabled: true, Devices: nil, MigDevices: map[string]int32{"1g.5gb": 4}},
			},
			inUse:     map[int]struct{}{},
			wantReset: []int{},
			wantLayout: nvidia.MigConfigSpecSlice{
				{MigEnabled: true, Devices: nil, MigDevices: map[string]int32{"1g.5gb": 4}},
			},
		},
		{
			name:       "empty input",
			input:      nvidia.MigConfigSpecSlice{},
			inUse:      map[int]struct{}{},
			wantReset:  []int{},
			wantLayout: nvidia.MigConfigSpecSlice{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resetIdleMigGPUs(tc.input, tc.inUse)
			if !reflect.DeepEqual(got, tc.wantReset) {
				t.Errorf("resetIdleMigGPUs reset set = %v, want %v", got, tc.wantReset)
			}
			if !reflect.DeepEqual(tc.input, tc.wantLayout) {
				t.Errorf("resetIdleMigGPUs mutated layout = %+v, want %+v", tc.input, tc.wantLayout)
			}
		})
	}
}

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

	got := activeMigGPUUUIDs(pods)
	want := map[string]struct{}{"GPU-live": {}, "GPU-pending": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activeMigGPUUUIDs() = %v, want %v", got, want)
	}
}
