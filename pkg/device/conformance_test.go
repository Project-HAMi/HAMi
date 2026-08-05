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

// This file holds a backend-agnostic conformance suite for the device.Devices
// interface (see devices.go). Every hardware backend implements the same
// interface, but each is otherwise tested in isolation, so the same class of
// contract violation has repeatedly been fixed one backend at a time (e.g. nil
// map / nil pointer panics on the admission and Fit paths in PRs #2254 and
// #2294). The suite runs one shared set of contract assertions against every
// constructible backend so a regression in any of them fails here immediately.
//
// It lives in the external device_test package on purpose: the backend
// sub-packages import github.com/Project-HAMi/HAMi/pkg/device, so an internal
// (package device) test that imported them back would create an import cycle.
//
// The invariants asserted here are intentionally the ones that should hold for
// every backend today. The int32-overflow invariant for GenerateResourceRequests
// is deliberately left out of this first pass because the underlying fixes are
// still open for several backends (#2278, #2284); it is tracked as follow-up
// work so this suite stays green while those land.
package device_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/amd"
	"github.com/Project-HAMi/HAMi/pkg/device/awsneuron"
	"github.com/Project-HAMi/HAMi/pkg/device/biren"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/enflame"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/kunlun"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/device/vastai"
)

// conformanceCase pairs a constructed backend with a human-readable name used as
// the subtest label.
type conformanceCase struct {
	name string
	dev  device.Devices
}

// conformanceCases constructs every backend that can be built from a plain
// in-memory config, using representative resource names that mirror the
// production defaults wired up in pkg/scheduler/config.InitDevicesWithConfig.
//
// The ascend and iluvatar backends are intentionally omitted for now: their
// constructors return a slice gated behind an enable flag and need per-template
// VNPU / vGPU configuration, so they are left for a follow-up that extends this
// registry rather than making the first pass more fragile.
func conformanceCases() []conformanceCase {
	return []conformanceCase{
		{"nvidia", nvidia.InitNvidiaDevice(nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
		})},
		{"cambricon", cambricon.InitMLUDevice(cambricon.CambriconConfig{
			ResourceCountName:  "cambricon.com/vmlu",
			ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
			ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
		})},
		{"hygon", hygon.InitDCUDevice(hygon.HygonConfig{
			ResourceCountName:  "hygon.com/dcunum",
			ResourceMemoryName: "hygon.com/dcumem",
			ResourceCoreName:   "hygon.com/dcucores",
		})},
		{"enflame-gcu", enflame.InitGCUDevice(enflame.EnflameConfig{
			ResourceNameGCU: "enflame.com/gcu",
		})},
		{"enflame-vgcu", enflame.InitEnflameDevice(enflame.EnflameConfig{
			ResourceNameDRSGCU: "enflame.com/drs-gcu",
			ResourceNameMemory: "enflame.com/gcu-memory",
			ResourceNameCore:   "enflame.com/gcu-core",
		})},
		{"mthreads", mthreads.InitMthreadsDevice(mthreads.MthreadsConfig{
			ResourceCountName:  "mthreads.com/vgpu",
			ResourceMemoryName: "mthreads.com/sgpu-memory",
			ResourceCoreName:   "mthreads.com/sgpu-core",
		})},
		{"metax-gpu", metax.InitMetaxDevice(metax.MetaxConfig{
			ResourceCountName: "metax-tech.com/gpu",
		})},
		{"metax-sgpu", metax.InitMetaxSDevice(metax.MetaxConfig{
			ResourceVCountName:  "metax-tech.com/sgpu",
			ResourceVMemoryName: "metax-tech.com/vmemory",
			ResourceVCoreName:   "metax-tech.com/vcore",
		})},
		{"kunlun", kunlun.InitKunlunDevice(kunlun.KunlunConfig{
			ResourceCountName: "kunlunxin.com/xpu",
		})},
		{"kunlun-vxpu", kunlun.InitKunlunVDevice(kunlun.KunlunConfig{
			ResourceVCountName:  "kunlunxin.com/vxpu",
			ResourceVMemoryName: "kunlunxin.com/vxpu-memory",
		})},
		{"awsneuron", awsneuron.InitAWSNeuronDevice(awsneuron.AWSNeuronConfig{
			ResourceCountName: "aws.amazon.com/neuron",
			ResourceCoreName:  "aws.amazon.com/neuroncore",
		})},
		{"amd", amd.InitAMDGPUDevice(amd.AMDConfig{
			ResourceCountName:  "amd.com/gpu",
			ResourceMemoryName: "amd.com/gpumem",
			ResourceCoreName:   "amd.com/gpucores",
		})},
		{"vastai", vastai.InitVastaiDevice(vastai.VastaiConfig{
			ResourceCountName: "vastaitech.com/vastai",
		})},
		{"biren", biren.InitBirenDevice(biren.BirenConfig{
			ResourceCountName: "biren.com/gpu",
		})},
	}
}

// mustNotPanic runs f and fails the test (instead of crashing the whole run) if
// it panics. The contract is that these interface methods degrade gracefully on
// empty / unrelated input rather than panicking.
func mustNotPanic(t *testing.T, op string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked, want graceful handling: %v", op, r)
		}
	}()
	f()
}

// TestConformanceRegistry is a guard on the suite itself: every case must have a
// name and a non-nil backend, so a constructor that silently returns nil cannot
// hide from the other conformance tests.
func TestConformanceRegistry(t *testing.T) {
	cases := conformanceCases()
	if len(cases) == 0 {
		t.Fatal("conformanceCases() returned no backends")
	}
	for _, c := range cases {
		if c.name == "" {
			t.Errorf("conformance case with empty name for %T", c.dev)
		}
		if c.dev == nil {
			t.Errorf("backend %q constructed as nil", c.name)
		}
	}
}

// TestConformanceResourceNamesNonEmpty asserts every backend advertises at least
// one resource name. The scheduler and quota code route a request to a backend
// by these names, so a backend that returns none is effectively unreachable.
func TestConformanceResourceNamesNonEmpty(t *testing.T) {
	for _, c := range conformanceCases() {
		t.Run(c.name, func(t *testing.T) {
			rn := c.dev.GetResourceNames()
			if rn.ResourceCountName == "" && rn.ResourceMemoryName == "" && rn.ResourceCoreName == "" {
				t.Errorf("GetResourceNames() returned no resource names; the backend is unreachable by the scheduler")
			}
		})
	}
}

// TestConformanceUnrelatedContainerYieldsNoRequest asserts that a container
// requesting none of a backend's resources produces a zero-device request. The
// scheduler and ResourceQuota checks rely on Nums == 0 to skip such containers;
// a non-zero result would make an unrelated container look like a GPU consumer.
func TestConformanceUnrelatedContainerYieldsNoRequest(t *testing.T) {
	for _, c := range conformanceCases() {
		t.Run(c.name, func(t *testing.T) {
			ctr := &corev1.Container{
				Name: "no-accelerator",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			}
			var req device.ContainerDeviceRequest
			mustNotPanic(t, "GenerateResourceRequests", func() {
				req = c.dev.GenerateResourceRequests(ctr)
			})
			if req.Nums != 0 {
				t.Errorf("GenerateResourceRequests() = Nums %d for a container with no accelerator request; want 0", req.Nums)
			}
		})
	}
}

// TestConformanceFitEmptyDevicesNoPanic asserts that fitting a real request
// against an empty candidate list returns false without panicking. This is the
// nil / empty-input path behind fixes such as #2294; on a node with no devices
// of a backend's type the scheduler still calls Fit for that backend.
func TestConformanceFitEmptyDevicesNoPanic(t *testing.T) {
	pod := &corev1.Pod{}
	nodeInfo := &device.NodeInfo{Node: &corev1.Node{}}
	deviceLists := []struct {
		name    string
		devices []*device.DeviceUsage
	}{
		{"nil", nil},
		{"empty", []*device.DeviceUsage{}},
	}
	for _, c := range conformanceCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, dl := range deviceLists {
				t.Run(dl.name, func(t *testing.T) {
					allocated := device.PodDevices{}
					req := device.ContainerDeviceRequest{Nums: 1, Type: c.dev.CommonWord()}
					var fit bool
					mustNotPanic(t, "Fit", func() {
						fit, _, _ = c.dev.Fit(dl.devices, req, pod, nodeInfo, &allocated)
					})
					if fit {
						t.Errorf("Fit() = true for a 1-device request against a %s candidate list; want false", dl.name)
					}
				})
			}
		})
	}
}

// TestConformanceMutateAdmissionNoPanic asserts that admission mutation of a pod
// that requests none of a backend's resources does not panic. This is the nil /
// empty-map path behind fixes such as #2254.
func TestConformanceMutateAdmissionNoPanic(t *testing.T) {
	for _, c := range conformanceCases() {
		t.Run(c.name, func(t *testing.T) {
			ctr := corev1.Container{
				Name: "no-accelerator",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
				},
			}
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{ctr}}}
			mustNotPanic(t, "MutateAdmission", func() {
				_, _ = c.dev.MutateAdmission(&pod.Spec.Containers[0], pod)
			})
		})
	}
}
