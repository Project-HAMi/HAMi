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
	wh, err := NewWebHook()
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
	wh, err := NewWebHook()
	if err != nil {
		t.Fatalf("Error creating WebHook: %v", err)
	}

	// call the Handle method
	resp := wh.Handle(context.Background(), req)
	if resp.Allowed {
		t.Errorf("Expected denied response, but got: %v", resp)
	}

}

func TestEnsureNvidiaExclusiveCoreDefault(t *testing.T) {
	config.NvidiaResourceCountName = "nvidia.com/gpu"
	config.NvidiaResourceMemoryName = "nvidia.com/gpumem"
	config.NvidiaResourceMemoryPercentageName = "nvidia.com/gpumem-percentage"
	config.NvidiaResourceCoreName = "nvidia.com/gpucores"

	tests := []struct {
		name        string
		limits      corev1.ResourceList
		requests    corev1.ResourceList
		wantCores   bool
		expectValue int64
	}{
		{
			name: "exclusive via percentage",
			limits: corev1.ResourceList{
				"nvidia.com/gpu":               resource.MustParse("1"),
				"nvidia.com/gpumem-percentage": resource.MustParse("100"),
			},
			wantCores:   true,
			expectValue: 100,
		},
		{
			name: "non-exclusive percentage",
			limits: corev1.ResourceList{
				"nvidia.com/gpu":               resource.MustParse("1"),
				"nvidia.com/gpumem-percentage": resource.MustParse("50"),
			},
			wantCores: false,
		},
		{
			name: "no memory fields defaults to exclusive",
			limits: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			wantCores:   true,
			expectValue: 100,
		},
		{
			name: "explicit cores remains unchanged",
			limits: corev1.ResourceList{
				"nvidia.com/gpu":      resource.MustParse("1"),
				"nvidia.com/gpucores": resource.MustParse("70"),
			},
			wantCores:   true,
			expectValue: 70,
		},
		{
			name: "memory size present treated as shareable",
			limits: corev1.ResourceList{
				"nvidia.com/gpu":    resource.MustParse("1"),
				"nvidia.com/gpumem": resource.MustParse("8192"),
			},
			wantCores: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctr := &corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   tt.limits,
					Requests: tt.requests,
				},
			}

			ensureNvidiaExclusiveCoreDefault(ctr)

			qty, exists := ctr.Resources.Limits[corev1.ResourceName(config.NvidiaResourceCoreName)]
			if tt.wantCores != exists {
				t.Fatalf("expected cores presence %v, got %v", tt.wantCores, exists)
			}
			if tt.wantCores && qty.Value() != tt.expectValue {
				t.Fatalf("expected cores value %d, got %d", tt.expectValue, qty.Value())
			}
		})
	}

	t.Run("custom resource names", func(t *testing.T) {
		config.NvidiaResourceCountName = "hami.io/gpu"
		config.NvidiaResourceMemoryPercentageName = "hami.io/gpumem-percentage"
		config.NvidiaResourceMemoryName = "hami.io/gpumem"
		config.NvidiaResourceCoreName = "hami.io/gpucores"

		ctr := &corev1.Container{
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"hami.io/gpu":               resource.MustParse("1"),
					"hami.io/gpumem-percentage": resource.MustParse("100"),
				},
			},
		}

		ensureNvidiaExclusiveCoreDefault(ctr)

		qty, exists := ctr.Resources.Limits[corev1.ResourceName(config.NvidiaResourceCoreName)]
		if !exists {
			t.Fatalf("expected cores presence true, got false")
		}
		if qty.Value() != 100 {
			t.Fatalf("expected cores value 100, got %d", qty.Value())
		}
	})

	config.NvidiaResourceCountName = "nvidia.com/gpu"
	config.NvidiaResourceMemoryName = "nvidia.com/gpumem"
	config.NvidiaResourceMemoryPercentageName = "nvidia.com/gpumem-percentage"
	config.NvidiaResourceCoreName = "nvidia.com/gpucores"
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
	wh, err := NewWebHook()
	if err != nil {
		t.Fatalf("Error creating WebHook: %v", err)
	}

	resp := wh.Handle(context.Background(), req)

	if !resp.Allowed {
		t.Errorf("Expected allowed response for pod with different scheduler, but got: %v", resp)
	}
}
