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

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_Resourcereqs(t *testing.T) {
	nvidia.ResourceName = "hami.io/gpu"
	nvidia.ResourceMem = "hami.io/gpumem"
	nvidia.ResourceMemPercentage = "hami.io/gpumem-percentage"
	nvidia.ResourceCores = "hami.io/gpucores"
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Resourcereqs(test.args)
			assert.DeepEqual(t, test.want, got)
		})
	}
}
