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

package scheduler

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

func TestHandle(t *testing.T) {
	// create a Pod object
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "container1",
					SecurityContext: &corev1.SecurityContext{
						Privileged: nil,
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	// encode the Pod object
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)
	podBytes, err := runtime.Encode(codec, pod)
	if err != nil {
		t.Fatalf("Error encoding pod: %v", err)
	}

	// create an AdmissionRequest object
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Namespace: "default",
			Name:      "test-pod",
			Object: runtime.RawExtension{
				Raw: podBytes,
			},
		},
	}

	// create a WebHook object
	wh, err := NewWebHook(MutatingWebhookType)
	if err != nil {
		t.Fatalf("Error creating WebHook: %v", err)
	}

	// call the Handle method
	resp := wh.Handle(context.Background(), req)
	if !resp.Allowed {
		t.Errorf("Expected allowed response, but got: %v", resp)
	}

}

func TestPodHasNodeName(t *testing.T) {
	config.SchedulerName = "hami-scheduler"
	config.ForceOverwriteDefaultScheduler = true
	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
		},
	}

	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		klog.Fatalf("Failed to initialize devices with config: %v", err)
	}
	// create a Pod object
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "container1",
					SecurityContext: &corev1.SecurityContext{
						Privileged: nil,
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hami.io/gpu": resource.MustParse("1"),
						},
					},
				},
			},
			NodeName: "test-node",
		},
	}

	// encode the Pod object
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)
	podBytes, err := runtime.Encode(codec, pod)
	if err != nil {
		t.Fatalf("Error encoding pod: %v", err)
	}

	// create an AdmissionRequest object
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Namespace: "default",
			Name:      "test-pod",
			Object: runtime.RawExtension{
				Raw: podBytes,
			},
		},
	}

	// create a WebHook object
	wh, err := NewWebHook(MutatingWebhookType)
	if err != nil {
		t.Fatalf("Error creating WebHook: %v", err)
	}

	// call the Handle method
	resp := wh.Handle(context.Background(), req)
	if resp.Allowed {
		t.Errorf("Expected denied response, but got: %v", resp)
	}

}

func TestPodHasDifferentScheduler(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
		},
	}

	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		klog.Fatalf("Failed to initialize devices with config: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			SchedulerName: "different-scheduler",
			Containers: []corev1.Container{
				{
					Name: "container1",
					SecurityContext: &corev1.SecurityContext{
						Privileged: nil,
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hami.io/gpu": resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)
	podBytes, err := runtime.Encode(codec, pod)
	if err != nil {
		t.Fatalf("Error encoding pod: %v", err)
	}

	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Namespace: "default",
			Name:      "test-pod",
			Object: runtime.RawExtension{
				Raw: podBytes,
			},
		},
	}
	wh, err := NewWebHook(MutatingWebhookType)
	if err != nil {
		t.Fatalf("Error creating WebHook: %v", err)
	}

	resp := wh.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Errorf("Expected allowed response for pod with different scheduler, but got: %v", resp)
	}
}

func TestValidatingHandle(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "hami.io/gpu",
			ResourceMemoryName:           "hami.io/gpumem",
			ResourceMemoryPercentageName: "hami.io/gpumem-percentage",
			ResourceCoreName:             "hami.io/gpucores",
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
			MemoryFactor:                 1,
		},
	}

	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		klog.Fatalf("Failed to initialize devices with config: %v", err)
	}

	qm := device.NewQuotaManager()
	ns := "default"
	memName := "nvidia.com/gpumem"
	coreName := "nvidia.com/gpucore"

	qm.Quotas[ns] = &device.DeviceQuota{
		memName:  &device.Quota{Used: 1000, Limit: 2000},
		coreName: &device.Quota{Used: 200, Limit: 400},
	}

	testCases := []struct {
		name           string
		pod            *corev1.Pod
		expectedDenied bool
	}{
		{
			name: "quota passed",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							SecurityContext: &corev1.SecurityContext{
								Privileged: nil,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("1"),
									"nvidia.com/gpumem": resource.MustParse("100"),
								},
							},
						},
					},
				},
			},
			expectedDenied: false,
		},
		{
			name: "quota exceeded",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							SecurityContext: &corev1.SecurityContext{
								Privileged: nil,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("1"),
									"nvidia.com/gpumem": resource.MustParse("1024"),
								},
							},
						},
					},
				},
			},
			expectedDenied: true,
		},
		{
			name: "request multiple gpus",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							SecurityContext: &corev1.SecurityContext{
								Privileged: nil,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("2"),
									"nvidia.com/gpumem": resource.MustParse("1024"),
								},
							},
						},
					},
				},
			},
			expectedDenied: false,
		},
		{
			name: "request ascend",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "container1",
							SecurityContext: &corev1.SecurityContext{
								Privileged: nil,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"huawei.com/Ascend910B":        resource.MustParse("1"),
									"huawei.com/Ascend910B-memory": resource.MustParse("1024"),
								},
							},
						},
					},
				},
			},
			expectedDenied: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			corev1.AddToScheme(scheme)
			codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)
			podBytes, err := runtime.Encode(codec, tc.pod)
			if err != nil {
				t.Fatalf("Error encoding pod: %v", err)
			}

			// create an AdmissionRequest object
			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Namespace: "default",
					Name:      "test-pod",
					Object: runtime.RawExtension{
						Raw: podBytes,
					},
				},
			}

			// create a WebHook object
			wh, err := NewWebHook(ValidatingWebhookType)
			if err != nil {
				t.Fatalf("Error creating WebHook: %v", err)
			}

			resp := wh.Handle(context.Background(), req)
			if tc.expectedDenied != !resp.Allowed {
				t.Errorf("Expected: %v, but got response: %v", tc.expectedDenied, resp)
			}
		})
	}
}
