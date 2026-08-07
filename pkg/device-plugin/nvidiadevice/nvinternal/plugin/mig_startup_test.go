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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	got := activeMigGPUUUIDs(pods)
	want := map[string]struct{}{"GPU-live": {}, "GPU-pending": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activeMigGPUUUIDs() = %v, want %v", got, want)
	}
}
