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
	"fmt"
	"reflect"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMutateAdmission(t *testing.T) {
	for _, ts := range []struct {
		name      string
		container *corev1.Container
		pod       *corev1.Pod

		expectedFound bool
		expectedError string
		expectedPod   *corev1.Pod
	}{
		{
			name: "no sgpu resource",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: BestEffort,
					},
				},
			},

			expectedFound: false,
			expectedError: "",
			expectedPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: BestEffort,
					},
				},
			},
		},
		{
			name: "qos policy error",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu": resource.MustParse("1"),
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: "best-effortx",
					},
				},
			},

			expectedFound: true,
			expectedError: fmt.Sprintf("%s must be set one of [%s, %s, %s]",
				MetaxSGPUQosPolicy, BestEffort, FixedShare, BurstShare),
			expectedPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: "best-effortx",
					},
				},
			},
		},
		{
			name: "no qos policy",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu": resource.MustParse("1"),
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},

			expectedFound: true,
			expectedError: "",
			expectedPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: BestEffort,
					},
				},
			},
		},
		{
			name: "pod annotation nil",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu": resource.MustParse("1"),
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},

			expectedFound: true,
			expectedError: "",
			expectedPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: BestEffort,
					},
				},
			},
		},
		{
			name: "qos policy fit",
			container: &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"metax-tech.com/sgpu": resource.MustParse("1"),
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: BurstShare,
					},
				},
			},

			expectedFound: true,
			expectedError: "",
			expectedPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						MetaxSGPUQosPolicy: BurstShare,
					},
				},
			},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			metaxSDevice := &MetaxSDevices{}
			fs := flag.FlagSet{}
			ParseConfig(&fs)

			resFound, resErr := metaxSDevice.MutateAdmission(ts.container, ts.pod)

			if resFound != ts.expectedFound {
				t.Errorf("MutateAdmission failed: resFound %v, expectedFound %v",
					resFound, ts.expectedFound)
			}

			resErrString := ""
			if resErr != nil {
				resErrString = resErr.Error()
			}

			if resErrString != ts.expectedError {
				t.Errorf("MutateAdmission failed: resErr %v, expectedError %v",
					resErr, ts.expectedError)
			}

			if !reflect.DeepEqual(ts.expectedPod, ts.pod) {
				t.Errorf("MutateAdmission failed: result %v, expected %v",
					ts.pod, ts.expectedPod)
			}
		})
	}
}

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

func TestGetMetaxSDevices(t *testing.T) {
	for _, ts := range []struct {
		name string
		node corev1.Node

		expected []*MetaxSDeviceInfo
	}{
		{
			name: "test normal node",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "normal",
					Annotations: map[string]string{
						MetaxSDeviceAnno: "[{\"uuid\": \"GPU-123\", \"model\": \"sgpu\", \"totalDevCount\": 16, \"totalCompute\": 100, \"bdf\": \"0000:44:00.0\", \"totalVRam\" : 32768, \"numa\": 1, \"healthy\": true, \"qosPolicy\": \"fixed-share\"}]",
					},
				},
			},

			expected: []*MetaxSDeviceInfo{
				{
					UUID:              "GPU-123",
					BDF:               "0000:44:00.0",
					Model:             "sgpu",
					TotalDevCount:     16,
					TotalCompute:      100,
					TotalVRam:         32768,
					AvailableDevCount: 0,
					AvailableCompute:  0,
					AvailableVRam:     0,
					Numa:              1,
					Healthy:           true,
					QosPolicy:         FixedShare,
				},
			},
		},
		{
			name: "test annotaions nil",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "normal",
					Annotations: map[string]string{},
				},
			},

			expected: []*MetaxSDeviceInfo{},
		},
		{
			name: "test Unmarshal fail",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "normal",
					Annotations: map[string]string{
						MetaxSDeviceAnno: "",
					},
				},
			},

			expected: []*MetaxSDeviceInfo{},
		},
		{
			name: "test devices len 0",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "normal",
					Annotations: map[string]string{
						MetaxSDeviceAnno: "[]",
					},
				},
			},

			expected: []*MetaxSDeviceInfo{},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			dev := &MetaxSDevices{}
			got, _ := dev.getMetaxSDevices(ts.node)

			if !reflect.DeepEqual(got, ts.expected) {
				t.Errorf("getMetaxSDevices failed: result %v, expected %v",
					got, ts.expected)
			}
		})
	}
}

func TestCheckDeviceQos(t *testing.T) {
	for _, ts := range []struct {
		name    string
		reqQos  string
		usage   util.DeviceUsage
		request util.ContainerDeviceRequest

		expected bool
	}{
		{
			name:   "check no use device",
			reqQos: BestEffort,
			usage: util.DeviceUsage{
				ID:   "GPU-123",
				Used: 0,
				CustomInfo: map[string]any{
					"QosPolicy": BurstShare,
				},
			},
			request: util.ContainerDeviceRequest{
				Coresreq: 50,
			},

			expected: true,
		},
		{
			name:   "check request exclusive",
			reqQos: BestEffort,
			usage: util.DeviceUsage{
				ID:   "GPU-123",
				Used: 2,
				CustomInfo: map[string]any{
					"QosPolicy": BurstShare,
				},
			},
			request: util.ContainerDeviceRequest{
				Coresreq: 100,
			},

			expected: true,
		},
		{
			name:   "check fail",
			reqQos: BestEffort,
			usage: util.DeviceUsage{
				ID:   "GPU-123",
				Used: 2,
				CustomInfo: map[string]any{
					"QosPolicy": BurstShare,
				},
			},
			request: util.ContainerDeviceRequest{
				Coresreq: 50,
			},

			expected: false,
		},
		{
			name:   "check pass",
			reqQos: BestEffort,
			usage: util.DeviceUsage{
				ID:   "GPU-123",
				Used: 2,
				CustomInfo: map[string]any{
					"QosPolicy": BestEffort,
				},
			},
			request: util.ContainerDeviceRequest{
				Coresreq: 50,
			},

			expected: true,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			metaxSDevice := &MetaxSDevices{
				jqCache: NewJitteryQosCache(),
			}

			res := metaxSDevice.checkDeviceQos(ts.reqQos, ts.usage, ts.request)
			if res != ts.expected {
				t.Errorf("checkDeviceQos failed: result %v, expected %v",
					res, ts.expected)
			}
		})
	}
}

func TestAddJitteryQos(t *testing.T) {
	for _, ts := range []struct {
		name   string
		reqQos string
		devs   util.PodSingleDevice

		expectedCache map[string]string
	}{
		{
			name:   "request BestEffort",
			reqQos: BestEffort,
			devs: util.PodSingleDevice{
				{
					{
						UUID:      "GPU-123",
						Usedcores: 50,
						CustomInfo: map[string]any{
							"QosPolicy": BestEffort,
						},
					},
					{
						UUID:      "GPU-456",
						Usedcores: 50,
						CustomInfo: map[string]any{
							"QosPolicy": BurstShare,
						},
					},
				},
				{
					{
						UUID:      "GPU-789",
						Usedcores: 100,
						CustomInfo: map[string]any{
							"QosPolicy": BestEffort,
						},
					},
				},
			},

			expectedCache: map[string]string{
				"GPU-456": BestEffort,
				"GPU-789": "",
			},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			metaxSDevice := &MetaxSDevices{
				jqCache: NewJitteryQosCache(),
			}
			metaxSDevice.addJitteryQos(ts.reqQos, ts.devs)

			if !reflect.DeepEqual(metaxSDevice.jqCache.cache, ts.expectedCache) {
				t.Errorf("addJitteryQos failed: result %v, expected %v",
					metaxSDevice.jqCache.cache, ts.expectedCache)
			}
		})
	}
}
