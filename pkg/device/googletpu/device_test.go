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

package googletpu

import (
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

const tpuResource = "google.com/tpu"

func newBackend() *GoogleTPUDevices {
	return InitGoogleTPUDevice(GoogleTPUConfig{ResourceCountName: tpuResource})
}

func container(limits, requests corev1.ResourceList) *corev1.Container {
	return &corev1.Container{
		Name:      "app",
		Resources: corev1.ResourceRequirements{Limits: limits, Requests: requests},
	}
}

// podRequesting returns a pod with one container per given chip count.
func podRequesting(chipsPerContainer ...int64) *corev1.Pod {
	pod := &corev1.Pod{}
	for _, chips := range chipsPerContainer {
		limits := corev1.ResourceList{tpuResource: *resource.NewQuantity(chips, resource.DecimalSI)}
		pod.Spec.Containers = append(pod.Spec.Containers, *container(limits, nil))
	}
	return pod
}

func tpuNode(name string, capacity, allocatable int64, labels map[string]string) *corev1.Node {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	if capacity > 0 {
		node.Status.Capacity = corev1.ResourceList{tpuResource: *resource.NewQuantity(capacity, resource.DecimalSI)}
	}
	if allocatable > 0 {
		node.Status.Allocatable = corev1.ResourceList{tpuResource: *resource.NewQuantity(allocatable, resource.DecimalSI)}
	}
	return node
}

func freeChips(n int) []*device.DeviceUsage {
	chips := make([]*device.DeviceUsage, 0, n)
	for i := range n {
		chips = append(chips, &device.DeviceUsage{
			ID:     fmt.Sprintf("node-%s-%d", GoogleTPUDevice, i),
			Index:  uint(i),
			Count:  1,
			Type:   GoogleTPUDevice,
			Health: true,
		})
	}
	return chips
}

func TestGenerateResourceRequests(t *testing.T) {
	dev := newBackend()
	whole := device.ContainerDeviceRequest{Nums: 4, Type: GoogleTPUDevice}
	tests := []struct {
		name    string
		ctr     *corev1.Container
		want    device.ContainerDeviceRequest
		invalid bool
	}{
		{
			name: "no TPU resource",
			ctr:  container(corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, nil),
		},
		{
			name: "count in limits",
			ctr:  container(corev1.ResourceList{tpuResource: resource.MustParse("4")}, nil),
			want: whole,
		},
		{
			name: "count in requests only",
			ctr:  container(nil, corev1.ResourceList{tpuResource: resource.MustParse("4")}),
			want: whole,
		},
		{
			name: "zero requests no device",
			ctr:  container(corev1.ResourceList{tpuResource: resource.MustParse("0")}, nil),
		},
		{
			name:    "count too large for int64",
			ctr:     container(corev1.ResourceList{tpuResource: resource.MustParse("10Ei")}, nil),
			invalid: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := dev.GenerateResourceRequests(test.ctr)
			if test.invalid {
				var invalid *device.ErrInvalidDeviceRequest
				assert.Assert(t, errors.As(err, &invalid), "got error %v", err)
			} else {
				assert.NilError(t, err)
			}
			assert.DeepEqual(t, got, test.want)
		})
	}
}

func TestMutateAdmission(t *testing.T) {
	dev := newBackend()
	tests := []struct {
		name    string
		ctr     *corev1.Container
		want    bool
		wantErr bool
	}{
		{
			name: "TPU container is handled",
			ctr:  container(corev1.ResourceList{tpuResource: resource.MustParse("4")}, nil),
			want: true,
		},
		{
			name: "container without TPU is left alone",
			ctr:  container(corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}, nil),
		},
		{
			name: "zero count is left alone",
			ctr:  container(corev1.ResourceList{tpuResource: resource.MustParse("0")}, nil),
		},
		{
			name:    "invalid count is rejected",
			ctr:     container(corev1.ResourceList{tpuResource: resource.MustParse("10Ei")}, nil),
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := dev.MutateAdmission(test.ctr, &corev1.Pod{})
			assert.Equal(t, err != nil, test.wantErr)
			assert.Equal(t, got, test.want)
		})
	}
}

func TestGetNodeDevices(t *testing.T) {
	dev := newBackend()

	t.Run("one device per chip", func(t *testing.T) {
		node := tpuNode("node-a", 4, 4, map[string]string{AcceleratorLabel: "tpu-v5-lite-podslice"})
		got, err := dev.GetNodeDevices(*node)
		assert.NilError(t, err)
		assert.Equal(t, len(got), 4)
		for i, d := range got {
			assert.DeepEqual(t, *d, device.DeviceInfo{
				ID:           fmt.Sprintf("node-a-GoogleTPU-%d", i),
				Index:        uint(i),
				Count:        1,
				Type:         "GoogleTPU-tpu-v5-lite-podslice",
				Health:       true,
				DeviceVendor: GoogleTPUCommonWord,
			})
		}
	})

	t.Run("no accelerator label", func(t *testing.T) {
		got, err := dev.GetNodeDevices(*tpuNode("node-b", 1, 1, nil))
		assert.NilError(t, err)
		assert.Equal(t, len(got), 1)
		assert.Equal(t, got[0].Type, GoogleTPUDevice)
	})

	t.Run("a missing chip makes the whole node unhealthy", func(t *testing.T) {
		got, err := dev.GetNodeDevices(*tpuNode("node-c", 4, 3, nil))
		assert.NilError(t, err)
		assert.Equal(t, len(got), 4)
		for _, d := range got {
			assert.Assert(t, !d.Health, "device %s is reported healthy", d.ID)
		}
	})

	t.Run("node without TPU", func(t *testing.T) {
		got, err := dev.GetNodeDevices(*tpuNode("node-d", 0, 0, nil))
		assert.Assert(t, err != nil)
		assert.Equal(t, len(got), 0)
	})
}

func TestCheckHealth(t *testing.T) {
	dev := newBackend()
	const nodeName = "tpu-check-health"
	t.Cleanup(func() { device.ForgetRegisteredDevices(GoogleTPUDevice, nodeName) })
	check := func(capacity, allocatable int64, labels map[string]string) (bool, bool) {
		return dev.CheckHealth(GoogleTPUDevice, tpuNode(nodeName, capacity, allocatable, labels))
	}
	labels := map[string]string{AcceleratorLabel: "tpu-v6e-slice"}

	healthy, needUpdate := check(4, 4, labels)
	assert.Assert(t, healthy && needUpdate, "a node seen for the first time must be registered")

	healthy, needUpdate = check(4, 4, labels)
	assert.Assert(t, healthy && !needUpdate, "an unchanged node must not be read again")

	healthy, needUpdate = check(4, 3, labels)
	assert.Assert(t, healthy && needUpdate, "a lost chip must be read again")

	healthy, needUpdate = check(4, 3, map[string]string{AcceleratorLabel: "tpu-v5p-slice"})
	assert.Assert(t, healthy && needUpdate, "a changed accelerator label must be read again")

	healthy, needUpdate = check(4, 0, labels)
	assert.Assert(t, !healthy && !needUpdate, "a node with no allocatable chip must leave the cache")

	healthy, needUpdate = check(4, 4, labels)
	assert.Assert(t, healthy && needUpdate, "a recovered node must be registered again")
}

func TestFit(t *testing.T) {
	dev := newBackend()
	taken, unhealthy := freeChips(4), freeChips(4)
	for i := range 4 {
		taken[i].Used = 1
		unhealthy[i].Health = false
	}
	ironwoodChips := func(used int) []*device.DeviceUsage {
		chips := freeChips(4)
		for i, chip := range chips {
			chip.Type = GoogleTPUDevice + "-tpu7x"
			if i < used {
				chip.Used = 1
			}
		}
		return chips
	}
	always := corev1.ContainerRestartPolicyAlways
	withSidecar := podRequesting(2)
	withSidecar.Spec.InitContainers = podRequesting(2).Spec.Containers
	withSidecar.Spec.InitContainers[0].RestartPolicy = &always
	otherChips := freeChips(4)
	for _, chip := range otherChips {
		chip.Type = GoogleTPUDevice + "-tpu-v6e-slice"
	}

	tests := []struct {
		name    string
		devices []*device.DeviceUsage
		// A nil pod stands for a caller that only knows the container request.
		pod        *corev1.Pod
		request    int64
		wantChips  []int
		wantReason map[string]int
	}{
		{
			name:      "whole free node",
			devices:   freeChips(4),
			request:   4,
			wantChips: []int{0, 1, 2, 3},
		},
		{
			name:       "fewer chips than the node has",
			devices:    freeChips(4),
			request:    2,
			wantReason: map[string]int{common.NodeRequiresAllDevices: 2},
		},
		{
			name:       "more chips than the node has",
			devices:    freeChips(4),
			request:    8,
			wantReason: map[string]int{common.NodeRequiresAllDevices: 8},
		},
		{
			name:       "node already taken",
			devices:    taken,
			request:    4,
			wantReason: map[string]int{common.CardTimeSlicingExhausted: 4},
		},
		{
			name:       "unhealthy node",
			devices:    unhealthy,
			request:    4,
			wantReason: map[string]int{common.CardNotHealth: 4},
		},
		{
			name:      "one container takes the node",
			devices:   freeChips(4),
			pod:       podRequesting(4),
			request:   4,
			wantChips: []int{0, 1, 2, 3},
		},
		{
			name:      "first of two Ironwood containers that divide the node",
			devices:   ironwoodChips(0),
			pod:       podRequesting(2, 2),
			request:   2,
			wantChips: []int{0, 1},
		},
		{
			name:      "second of two Ironwood containers that divide the node",
			devices:   ironwoodChips(2),
			pod:       podRequesting(2, 2),
			request:   2,
			wantChips: []int{2, 3},
		},
		{
			name:      "Ironwood sidecar counts toward the node",
			devices:   ironwoodChips(0),
			pod:       withSidecar,
			request:   2,
			wantChips: []int{0, 1},
		},
		{
			name:       "other generations cannot divide the node",
			devices:    otherChips,
			pod:        podRequesting(2, 2),
			request:    2,
			wantReason: map[string]int{common.NodeRequiresAllDevices: 2},
		},
		{
			name:       "Ironwood containers ask for less than the node has",
			devices:    ironwoodChips(0),
			pod:        podRequesting(2, 1),
			request:    2,
			wantReason: map[string]int{common.NodeRequiresAllDevices: 3},
		},
		{
			name:       "Ironwood containers ask for more than the node has",
			devices:    ironwoodChips(0),
			pod:        podRequesting(4, 4),
			request:    4,
			wantReason: map[string]int{common.NodeRequiresAllDevices: 8},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := device.ContainerDeviceRequest{Nums: test.request, Type: GoogleTPUDevice}
			fit, got, reason := dev.Fit(test.devices, request, test.pod, nil, nil)
			assert.Equal(t, fit, test.wantChips != nil)
			if !fit {
				assert.Equal(t, len(got), 0)
				assert.DeepEqual(t, common.ParseReason(reason), test.wantReason)
				return
			}
			assert.Equal(t, reason, "")
			want := make(device.ContainerDevices, 0, len(test.wantChips))
			for _, i := range test.wantChips {
				want = append(want, device.ContainerDevice{
					Idx:  i,
					UUID: fmt.Sprintf("node-%s-%d", GoogleTPUDevice, i),
					Type: GoogleTPUDevice,
				})
			}
			assert.DeepEqual(t, got[GoogleTPUDevice], want)
		})
	}
}

func TestAddResourceUsage(t *testing.T) {
	dev := newBackend()
	chip := freeChips(1)[0]
	assert.NilError(t, dev.AddResourceUsage(nil, chip, &device.ContainerDevice{UUID: chip.ID, Type: GoogleTPUDevice}))
	assert.Equal(t, chip.Used, int64(1))
}

// The scheduler rebuilds node usage from the annotation PatchAnnotations
// writes, so it has to decode back to the devices Fit allocated.
func TestPatchAnnotationsRoundTrip(t *testing.T) {
	dev := newBackend()
	fit, allocated, _ := dev.Fit(freeChips(4), device.ContainerDeviceRequest{Nums: 4, Type: GoogleTPUDevice}, nil, nil, nil)
	assert.Assert(t, fit)

	annotations := map[string]string{}
	dev.PatchAnnotations(&corev1.Pod{}, &annotations, device.PodDevices{
		GoogleTPUDevice: device.PodSingleDevice{allocated[GoogleTPUDevice]},
	})
	assert.Equal(t, len(annotations), 1)

	decoded, err := device.DecodePodDevices(device.SupportDevices, annotations)
	assert.NilError(t, err)
	// Idx is not part of the annotation format.
	want := make(device.ContainerDevices, 0, 4)
	for _, chip := range allocated[GoogleTPUDevice] {
		want = append(want, device.ContainerDevice{UUID: chip.UUID, Type: chip.Type})
	}
	assert.DeepEqual(t, decoded[GoogleTPUDevice][0], want)

	empty := map[string]string{}
	dev.PatchAnnotations(&corev1.Pod{}, &empty, device.PodDevices{})
	assert.Equal(t, len(empty), 0)
}
