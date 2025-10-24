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

	"github.com/Project-HAMi/HAMi/pkg/device"

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

		expected device.ContainerDeviceRequest
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

			expected: device.ContainerDeviceRequest{
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

			expected: device.ContainerDeviceRequest{
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

			expected: device.ContainerDeviceRequest{
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

			expected: device.ContainerDeviceRequest{
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

			expected: device.ContainerDeviceRequest{
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

			expected: device.ContainerDeviceRequest{
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

			expected: device.ContainerDeviceRequest{
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
			name: "test annotations nil",
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
		usage   device.DeviceUsage
		request device.ContainerDeviceRequest

		expected bool
	}{
		{
			name:   "check no use device",
			reqQos: BestEffort,
			usage: device.DeviceUsage{
				ID:   "GPU-123",
				Used: 0,
				CustomInfo: map[string]any{
					"QosPolicy": BurstShare,
				},
			},
			request: device.ContainerDeviceRequest{
				Coresreq: 50,
			},

			expected: true,
		},
		{
			name:   "check request exclusive",
			reqQos: BestEffort,
			usage: device.DeviceUsage{
				ID:   "GPU-123",
				Used: 2,
				CustomInfo: map[string]any{
					"QosPolicy": BurstShare,
				},
			},
			request: device.ContainerDeviceRequest{
				Coresreq: 100,
			},

			expected: true,
		},
		{
			name:   "check fail",
			reqQos: BestEffort,
			usage: device.DeviceUsage{
				ID:   "GPU-123",
				Used: 2,
				CustomInfo: map[string]any{
					"QosPolicy": BurstShare,
				},
			},
			request: device.ContainerDeviceRequest{
				Coresreq: 50,
			},

			expected: false,
		},
		{
			name:   "check pass",
			reqQos: BestEffort,
			usage: device.DeviceUsage{
				ID:   "GPU-123",
				Used: 2,
				CustomInfo: map[string]any{
					"QosPolicy": BestEffort,
				},
			},
			request: device.ContainerDeviceRequest{
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
		devs   device.PodSingleDevice

		expectedCache map[string]string
	}{
		{
			name:   "request BestEffort",
			reqQos: BestEffort,
			devs: device.PodSingleDevice{
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

func TestMetaxSDevices_Fit(t *testing.T) {
	config := MetaxConfig{
		ResourceVCountName:  "metax-tech.com/sgpu",
		ResourceVCoreName:   "metax-tech.com/vcore",
		ResourceVMemoryName: "metax-tech.com/vmemory",
	}
	dev := InitMetaxSDevice(config)

	tests := []struct {
		name       string
		devices    []*device.DeviceUsage
		request    device.ContainerDeviceRequest
		annos      map[string]string
		wantFit    bool
		wantLen    int
		wantDevIDs []string
		wantReason string
	}{
		{
			name: "fit success",
			devices: []*device.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      MetaxSGPUDevice,
					Health:    true,
				},
				{
					ID:        "dev-1",
					Index:     0,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      MetaxSGPUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-1"},
			wantReason: "",
		},
		{
			name: "fit fail: memory not enough",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientMemory",
		},
		{
			name: "fit fail: core not enough",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1024,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientCore",
		},
		{
			name: "fit fail: type mismatch",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Health:    true,
				Type:      MetaxSGPUDevice,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "OtherType",
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTypeMismatch",
		},
		{
			name: "fit fail: device unhealthy",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Health:    false,
				Type:      MetaxSGPUDevice,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             MetaxSGPUDevice,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTypeMismatch",
		},
		{
			name: "fit fail: card overused",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      100,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTimeSlicingExhausted",
		},
		{
			name: "fit success: but core limit can't exceed 100",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientCore",
		},
		{
			name: "fit fail:  card exclusively",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         100,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 ExclusiveDeviceAllocateConflict",
		},
		{
			name: "fit fail: user assign use uuid mismatch",
			devices: []*device.DeviceUsage{{
				ID:        "dev-1",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{MetaxUseUUID: "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: user assign no use uuid match",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{MetaxNoUseUUID: "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail:  CardComputeUnitsExhausted",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardComputeUnitsExhausted",
		},
		{
			name: "fit fail:  AllocatedCardsInsufficientRequest",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 10,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 AllocatedCardsInsufficientRequest",
		},
		{
			name: "fit success:  memory percentage",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 10,
				Numa:      0,
				Type:      MetaxSGPUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
				Type:             MetaxSGPUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocated := &device.PodDevices{}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: test.annos,
				},
			}
			fit, result, reason := dev.Fit(test.devices, test.request, pod, &device.NodeInfo{}, allocated)
			if fit != test.wantFit {
				t.Errorf("Fit: got %v, want %v", fit, test.wantFit)
			}
			if test.wantFit {
				if len(result[MetaxSGPUDevice]) != test.wantLen {
					t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[MetaxSGPUDevice]))
				}
				for idx, id := range test.wantDevIDs {
					if id != result[MetaxSGPUDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[MetaxSGPUDevice][idx].UUID)
					}
				}
			}

			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
		})
	}
}

func TestMetaxSDevices_AddResourceUsage(t *testing.T) {
	tests := []struct {
		name        string
		deviceUsage *device.DeviceUsage
		ctr         *device.ContainerDevice
		wantErr     bool
		wantUsage   *device.DeviceUsage
	}{
		{
			name: "test add resource usage",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      0,
				Usedcores: 15,
				Usedmem:   2000,
			},
			ctr: &device.ContainerDevice{
				UUID:      "dev-0",
				Usedcores: 50,
				Usedmem:   1024,
			},
			wantUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      1,
				Usedcores: 65,
				Usedmem:   3024,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &MetaxDevices{}
			if err := dev.AddResourceUsage(&corev1.Pod{}, tt.deviceUsage, tt.ctr); (err != nil) != tt.wantErr {
				t.Errorf("AddResourceUsage() error=%v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if tt.deviceUsage.Usedcores != tt.wantUsage.Usedcores {
					t.Errorf("expected used cores: %d, got used cores %d", tt.wantUsage.Usedcores, tt.deviceUsage.Usedcores)
				}
				if tt.deviceUsage.Usedmem != tt.wantUsage.Usedmem {
					t.Errorf("expected used mem: %d, got used mem %d", tt.wantUsage.Usedmem, tt.deviceUsage.Usedmem)
				}
				if tt.deviceUsage.Used != tt.wantUsage.Used {
					t.Errorf("expected used: %d, got used %d", tt.wantUsage.Used, tt.deviceUsage.Used)
				}
			}
		})
	}
}

func TestPrioritizeExclusiveDevices(t *testing.T) {
	for _, ts := range []struct {
		name             string
		candidateDevices device.ContainerDevices
		require          int

		expectedDevices device.ContainerDevices
	}{
		{
			name: "require one device",
			candidateDevices: device.ContainerDevices{
				{
					UUID:       "GPU-1",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-2",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},
			require: 1,

			expectedDevices: device.ContainerDevices{
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},
		},
		{
			name: "require two device",
			candidateDevices: device.ContainerDevices{
				{
					UUID:       "GPU-1",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-2",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},
			require: 2,

			expectedDevices: device.ContainerDevices{
				{
					UUID:       "GPU-1",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-2",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
			},
		},
		{
			name: "require four device, best result",
			candidateDevices: device.ContainerDevices{
				{
					UUID:       "GPU-1",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-2",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-8",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},
			require: 4,

			expectedDevices: device.ContainerDevices{
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-8",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},
		},
		{
			name: "require four device, general result",
			candidateDevices: device.ContainerDevices{
				{
					UUID:       "GPU-1",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-2",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},
			require: 4,

			expectedDevices: device.ContainerDevices{
				{
					UUID:       "GPU-1",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-2",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},
		},
		{
			name: "no metalink, require two device",
			candidateDevices: device.ContainerDevices{
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
			},
			require: 2,

			expectedDevices: device.ContainerDevices{
				{
					UUID:       "GPU-5",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
			},
		},
		{
			name: "part metalink, require two device, best result",
			candidateDevices: device.ContainerDevices{
				{
					UUID:       "GPU-3",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
				{
					UUID:       "GPU-4",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-8",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
			},
			require: 2,

			expectedDevices: device.ContainerDevices{
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-8",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
			},
		},
		{
			name: "part metalink, require four device, bad result",
			candidateDevices: device.ContainerDevices{
				{
					UUID:       "GPU-3",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
				{
					UUID:       "GPU-4",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-8",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
			},
			require: 4,

			expectedDevices: device.ContainerDevices{
				{
					UUID:       "GPU-6",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-7",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-8",
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					UUID:       "GPU-3",
					CustomInfo: map[string]any{"LinkZone": int32(0)},
				},
			},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := prioritizeExclusiveDevices(ts.candidateDevices, ts.require)

			if !reflect.DeepEqual(result, ts.expectedDevices) {
				t.Errorf("prioritizeExclusiveDevices failed: result %v, expected %v",
					result, ts.expectedDevices)
			}
		})
	}
}

func TestNeedScore(t *testing.T) {
	for _, ts := range []struct {
		name       string
		podDevices device.PodSingleDevice

		expected bool
	}{
		{
			name: "enable, allocate 100core",
			podDevices: device.PodSingleDevice{
				{
					{
						Usedcores: 100,
						CustomInfo: map[string]any{
							"Pod.Annotations": map[string]string{
								MetaxSGPUTopologyAware: "true",
							},
						},
					},
				},
			},

			expected: true,
		},
		{
			name: "disable, allocate 100core",
			podDevices: device.PodSingleDevice{
				{
					{
						Usedcores: 100,
						CustomInfo: map[string]any{
							"Pod.Annotations": map[string]string{
								MetaxSGPUTopologyAware: "false",
							},
						},
					},
				},
			},

			expected: false,
		},
		{
			name: "enable, allocate 99core",
			podDevices: device.PodSingleDevice{
				{
					{
						Usedcores: 99,
						CustomInfo: map[string]any{
							"Pod.Annotations": map[string]string{
								MetaxSGPUTopologyAware: "true",
							},
						},
					},
				},
			},

			expected: false,
		},
		{
			name: "enable, container[0]: 99core, container[1]: 100core",
			podDevices: device.PodSingleDevice{
				{
					{
						Usedcores: 99,
						CustomInfo: map[string]any{
							"Pod.Annotations": map[string]string{
								MetaxSGPUTopologyAware: "true",
							},
						},
					},
				},
				{
					{
						Usedcores: 100,
						CustomInfo: map[string]any{
							"Pod.Annotations": map[string]string{
								MetaxSGPUTopologyAware: "true",
							},
						},
					},
				},
			},

			expected: true,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := needScore(ts.podDevices)

			if result != ts.expected {
				t.Errorf("needScore failed: result %v, expected %v",
					result, ts.expected)
			}
		})
	}
}

func TestScoreExclusiveDevices(t *testing.T) {
	for _, ts := range []struct {
		name       string
		podDevices device.PodSingleDevice
		previous   []*device.DeviceUsage

		expectedScore int
	}{
		{
			name: "allocate one device, rest zero device",
			podDevices: device.PodSingleDevice{
				[]device.ContainerDevice{
					{
						UUID:       "GPU-4",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
				},
			},
			previous: []*device.DeviceUsage{
				{
					ID:         "GPU-1",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-2",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-3",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-4",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
			},

			expectedScore: 0,
		},
		{
			name: "allocate one device, rest three device",
			podDevices: device.PodSingleDevice{
				[]device.ContainerDevice{
					{
						UUID:       "GPU-4",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
				},
			},
			previous: []*device.DeviceUsage{
				{
					ID:         "GPU-1",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-2",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-3",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-4",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
			},

			expectedScore: -30,
		},
		{
			name: "allocate two device, best result",
			podDevices: device.PodSingleDevice{
				[]device.ContainerDevice{
					{
						UUID:       "GPU-3",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-4",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
				},
			},
			previous: []*device.DeviceUsage{
				{
					ID:         "GPU-1",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-2",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-3",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-4",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
			},

			expectedScore: 60,
		},
		{
			name: "allocate two device, bad result",
			podDevices: device.PodSingleDevice{
				[]device.ContainerDevice{
					{
						UUID:       "GPU-4",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-5",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(2)},
					},
				},
			},
			previous: []*device.DeviceUsage{
				{
					ID:         "GPU-1",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-2",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-3",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-4",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-5",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					ID:         "GPU-6",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					ID:         "GPU-7",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					ID:         "GPU-8",
					Used:       1,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},

			expectedScore: 0,
		},
		{
			name: "allocate four device, best result",
			podDevices: device.PodSingleDevice{
				[]device.ContainerDevice{
					{
						UUID:       "GPU-1",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-2",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-3",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-4",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
				},
			},
			previous: []*device.DeviceUsage{
				{
					ID:         "GPU-1",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-2",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-3",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-4",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-5",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},

			expectedScore: 600,
		},
		{
			name: "allocate four device, bad result",
			podDevices: device.PodSingleDevice{
				[]device.ContainerDevice{
					{
						UUID:       "GPU-3",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-4",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-5",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(2)},
					},
					{
						UUID:       "GPU-6",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(2)},
					},
				},
			},
			previous: []*device.DeviceUsage{
				{
					ID:         "GPU-2",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-3",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-4",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-5",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					ID:         "GPU-6",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},

			expectedScore: 180,
		},
		{
			name: "allocate eight device",
			podDevices: device.PodSingleDevice{
				[]device.ContainerDevice{
					{
						UUID:       "GPU-1",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-2",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-3",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-4",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(1)},
					},
					{
						UUID:       "GPU-5",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(2)},
					},
					{
						UUID:       "GPU-6",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(2)},
					},
					{
						UUID:       "GPU-7",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(2)},
					},
					{
						UUID:       "GPU-8",
						Usedcores:  100,
						CustomInfo: map[string]any{"LinkZone": int32(2)},
					},
				},
			},
			previous: []*device.DeviceUsage{
				{
					ID:         "GPU-1",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-2",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-3",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-4",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(1)},
				},
				{
					ID:         "GPU-5",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					ID:         "GPU-6",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					ID:         "GPU-7",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
				{
					ID:         "GPU-8",
					Used:       0,
					CustomInfo: map[string]any{"LinkZone": int32(2)},
				},
			},

			expectedScore: 1200,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			result := scoreExclusiveDevices(ts.podDevices, ts.previous)

			if result != ts.expectedScore {
				t.Errorf("scoreExclusiveDevices failed: result %v, expected %v",
					result, ts.expectedScore)
			}
		})
	}
}
