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

package k8sutil

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_Resourcereqs(t *testing.T) {
	device.InitDevicesWithConfig(&device.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
		},
	})

	tests := []struct {
		name string
		args *corev1.Pod
		want util.PodDeviceRequests
	}{
		{
			name: "don't resource",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"cpu": *resource.NewQuantity(1, resource.BinarySI),
								},
							},
						},
					},
				},
			},
			want: []util.ContainerDeviceRequests{{}},
		},
		{
			name: "one container use gpu",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
									"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
									"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
								},
							},
						},
					},
				},
			},
			want: []util.ContainerDeviceRequests{
				{
					nvidia.NvidiaGPUDevice: util.ContainerDeviceRequest{
						Nums:             1,
						Type:             nvidia.NvidiaGPUDevice,
						Memreq:           1000,
						MemPercentagereq: 101,
						Coresreq:         30,
					},
				},
			},
		},
		{
			name: "two container only one container use gpu",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
									"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
									"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"cpu": *resource.NewQuantity(1, resource.BinarySI),
								},
							},
						},
					},
				},
			},
			want: []util.ContainerDeviceRequests{
				{
					nvidia.NvidiaGPUDevice: util.ContainerDeviceRequest{
						Nums:             1,
						Type:             nvidia.NvidiaGPUDevice,
						Memreq:           1000,
						MemPercentagereq: 101,
						Coresreq:         30,
					},
				},
				{},
			},
		},
		{
			name: "three containers gpu container first",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
									"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
									"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"cpu": *resource.NewQuantity(1, resource.BinarySI),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"memory": *resource.NewQuantity(2000, resource.BinarySI),
								},
							},
						},
					},
				},
			},
			want: []util.ContainerDeviceRequests{
				{
					nvidia.NvidiaGPUDevice: util.ContainerDeviceRequest{
						Nums:             1,
						Type:             nvidia.NvidiaGPUDevice,
						Memreq:           1000,
						MemPercentagereq: 101,
						Coresreq:         30,
					},
				},
				{},
				{},
			},
		},
		{
			name: "three containers gpu container in the middle",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"cpu": *resource.NewQuantity(1, resource.BinarySI),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
									"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
									"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"memory": *resource.NewQuantity(2000, resource.BinarySI),
								},
							},
						},
					},
				},
			},
			want: []util.ContainerDeviceRequests{
				{},
				{
					nvidia.NvidiaGPUDevice: util.ContainerDeviceRequest{
						Nums:             1,
						Type:             nvidia.NvidiaGPUDevice,
						Memreq:           1000,
						MemPercentagereq: 101,
						Coresreq:         30,
					},
				},
				{},
			},
		},
		{
			name: "three containers gpu container last",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"cpu": *resource.NewQuantity(1, resource.BinarySI),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"memory": *resource.NewQuantity(2000, resource.BinarySI),
								},
							},
						},
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
									"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
									"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
								},
							},
						},
					},
				},
			},
			want: []util.ContainerDeviceRequests{
				{},
				{},
				{
					nvidia.NvidiaGPUDevice: util.ContainerDeviceRequest{
						Nums:             1,
						Type:             nvidia.NvidiaGPUDevice,
						Memreq:           1000,
						MemPercentagereq: 101,
						Coresreq:         30,
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Resourcereqs(test.args)
			assert.DeepEqual(t, test.want, got)
		})
	}
}
func Test_IsPodInTerminatedState(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Pod
		want bool
	}{
		{
			name: "pod in failed state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			want: true,
		},
		{
			name: "pod in succeeded state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			},
			want: true,
		},
		{
			name: "pod in running state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			want: false,
		},
		{
			name: "pod in pending state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			want: false,
		},
		{
			name: "pod in unknown state",
			args: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodUnknown,
				},
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := IsPodInTerminatedState(test.args)
			assert.Equal(t, test.want, got)
		})
	}
}
func Test_AllContainersCreated(t *testing.T) {
	tests := []struct {
		name string
		args *corev1.Pod
		want bool
	}{
		{
			name: "all containers created",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{},
						{},
					},
				},
			},
			want: true,
		},
		{
			name: "not all containers created",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{},
					},
				},
			},
			want: false,
		},
		{
			name: "no containers created",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{},
				},
			},
			want: false,
		},
		{
			name: "more container statuses than containers",
			args: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{},
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{},
						{},
					},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := AllContainersCreated(test.args)
			assert.Equal(t, test.want, got)
		})
	}
}
