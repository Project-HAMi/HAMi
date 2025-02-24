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

package metax

import (
	"flag"
	"reflect"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGenerateResourceRequests(t *testing.T) {
	for _, ts := range []struct {
		name      string
		container *corev1.Container

		expected util.ContainerDeviceRequest
	}{
		{
			name: "one full sgpu test",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu": resource.MustParse("1"),
					},
				},
			},

			expected: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             MetaxSGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "two full sgpu test",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu": resource.MustParse("2"),
					},
				},
			},

			expected: util.ContainerDeviceRequest{
				Nums:             2,
				Type:             MetaxSGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "one sgpu test set vcore",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu":  resource.MustParse("1"),
						"metax-tech.com/vcore": resource.MustParse("30"),
					},
				},
			},

			expected: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             MetaxSGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         30,
			},
		},
		{
			name: "one sgpu test set vmemory",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu":    resource.MustParse("1"),
						"metax-tech.com/vmemory": resource.MustParse("16"),
					},
				},
			},

			expected: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             MetaxSGPUDevice,
				Memreq:           16 * 1024,
				MemPercentagereq: 0,
				Coresreq:         100,
			},
		},
		{
			name: "one sgpu test set vcore&vmemory",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu":    resource.MustParse("1"),
						"metax-tech.com/vcore":   resource.MustParse("60"),
						"metax-tech.com/vmemory": resource.MustParse("16"),
					},
				},
			},

			expected: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             MetaxSGPUDevice,
				Memreq:           16 * 1024,
				MemPercentagereq: 0,
				Coresreq:         60,
			},
		},
		{
			name: "one sgpu test set vcore&vmemory, mem unit Mi",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu":    resource.MustParse("1"),
						"metax-tech.com/vcore":   resource.MustParse("60"),
						"metax-tech.com/vmemory": resource.MustParse("1024Mi"),
					},
				},
			},

			expected: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             MetaxSGPUDevice,
				Memreq:           1024,
				MemPercentagereq: 0,
				Coresreq:         60,
			},
		},
		{
			name: "one sgpu test set vcore&vmemory, mem unit Gi",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu":    resource.MustParse("1"),
						"metax-tech.com/vcore":   resource.MustParse("60"),
						"metax-tech.com/vmemory": resource.MustParse("16Gi"),
					},
				},
			},

			expected: util.ContainerDeviceRequest{
				Nums:             1,
				Type:             MetaxSGPUDevice,
				Memreq:           16 * 1024,
				MemPercentagereq: 0,
				Coresreq:         60,
			},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			metaxSDevice := &MetaxSDevices{}
			fs := flag.FlagSet{}
			ParseConfig(&fs)

			result := metaxSDevice.GenerateResourceRequests(ts.container)

			if !reflect.DeepEqual(ts.expected, result) {
				t.Errorf("GenerateResourceRequests failed: result %v, expected %v",
					result, ts.expected)
			}
		})
	}
}
