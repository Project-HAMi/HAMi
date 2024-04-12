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
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
