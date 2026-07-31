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
	"strings"
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

func TestFitResourceQuota(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
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
	coreName := "nvidia.com/gpucores"

	qm.Quotas[ns] = &device.DeviceQuota{
		memName:  &device.Quota{Used: 1000, Limit: 2000},
		coreName: &device.Quota{Used: 200, Limit: 400},
	}
	t.Cleanup(func() { delete(qm.Quotas, ns) })

	testCases := []struct {
		name string
		pod  *corev1.Pod
		fit  bool
	}{
		{
			name: "quota passed",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					SchedulerName: "hami-scheduler",
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
			fit: true,
		},
		{
			name: "quota exceeded",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					SchedulerName: "hami-scheduler",
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
			fit: false,
		},
		{
			name: "request multiple gpus",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					SchedulerName: "hami-scheduler",
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
			fit: false,
		},
		{
			name: "request multiple gpus within quota",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					SchedulerName: "hami-scheduler",
					Containers: []corev1.Container{
						{
							Name: "container1",
							SecurityContext: &corev1.SecurityContext{
								Privileged: nil,
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("2"),
									"nvidia.com/gpumem": resource.MustParse("400"),
								},
							},
						},
					},
				},
			},
			fit: true,
		},
		{
			name: "request ascend",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					SchedulerName: "hami-scheduler",
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
			fit: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := fitResourceQuota(tc.pod)
			if tc.fit != result {
				t.Errorf("Expected %v, but got %v", tc.fit, result)
			}
		})
	}
}

type quotaWebhookDevice struct {
	names device.ResourceNames
	word  string
}

func (d *quotaWebhookDevice) CommonWord() string  { return d.word }
func (d *quotaWebhookDevice) MemoryFactor() int32 { return 1 }
func (d *quotaWebhookDevice) MutateAdmission(*corev1.Container, *corev1.Pod) (bool, error) {
	return false, nil
}
func (d *quotaWebhookDevice) CheckHealth(string, *corev1.Node) (bool, bool) { return true, true }
func (d *quotaWebhookDevice) NodeCleanUp(string) error                      { return nil }
func (d *quotaWebhookDevice) GetResourceNames() device.ResourceNames        { return d.names }
func (d *quotaWebhookDevice) GetNodeDevices(corev1.Node) ([]*device.DeviceInfo, error) {
	return nil, nil
}
func (d *quotaWebhookDevice) LockNode(*corev1.Node, *corev1.Pod) error        { return nil }
func (d *quotaWebhookDevice) ReleaseNodeLock(*corev1.Node, *corev1.Pod) error { return nil }
func (d *quotaWebhookDevice) GenerateResourceRequests(*corev1.Container) device.ContainerDeviceRequest {
	return device.ContainerDeviceRequest{}
}
func (d *quotaWebhookDevice) PatchAnnotations(*corev1.Pod, *map[string]string, device.PodDevices) map[string]string {
	return nil
}
func (d *quotaWebhookDevice) ScoreNode(*corev1.Node, device.PodSingleDevice, []*device.DeviceUsage, string) float32 {
	return 0
}
func (d *quotaWebhookDevice) AddResourceUsage(*corev1.Pod, *device.DeviceUsage, *device.ContainerDevice) error {
	return nil
}
func (d *quotaWebhookDevice) Fit([]*device.DeviceUsage, device.ContainerDeviceRequest, *corev1.Pod, *device.NodeInfo, *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	return false, nil, ""
}

func TestFitResourceQuotaNonNvidiaExceeded(t *testing.T) {
	oldMap := device.DevicesMap
	t.Cleanup(func() { device.DevicesMap = oldMap })
	t.Cleanup(func() { delete(device.GetLocalCache().Quotas, "default") })

	deviceName := "Ascend910B"
	memName := "huawei.com/Ascend910B-memory"
	device.DevicesMap = map[string]device.Devices{
		deviceName: &quotaWebhookDevice{
			word: deviceName,
			names: device.ResourceNames{
				ResourceCountName:  "huawei.com/Ascend910B",
				ResourceMemoryName: memName,
				ResourceCoreName:   "huawei.com/Ascend910B-core",
			},
		},
	}
	qm := device.NewQuotaManager()
	qm.Quotas["default"] = &device.DeviceQuota{
		memName: &device.Quota{Used: 9000, Limit: 10000},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"huawei.com/Ascend910B":        resource.MustParse("1"),
						"huawei.com/Ascend910B-memory": resource.MustParse("2000"),
					},
				},
			}},
		},
	}
	if fitResourceQuota(pod) {
		t.Fatal("fitResourceQuota should deny non-NVIDIA pod exceeding memory quota")
	}
}

func TestNonGPUPodAllowedAdmission(t *testing.T) {
	prevSchedulerName := config.SchedulerName
	prevForceOverwrite := config.ForceOverwriteDefaultScheduler
	t.Cleanup(func() {
		config.SchedulerName = prevSchedulerName
		config.ForceOverwriteDefaultScheduler = prevForceOverwrite
	})

	config.SchedulerName = "hami-scheduler"
	config.ForceOverwriteDefaultScheduler = false

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

	// Pod with NO GPU resources at all - should be allowed through
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-gpu-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "cpu-only",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
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
			UID:       "non-gpu-uid",
			Namespace: "default",
			Name:      "non-gpu-pod",
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
		t.Errorf("Expected non-GPU pod to be allowed, but got denied: %v", resp.Result)
	}
}

func TestEmptyContainersDenied(t *testing.T) {
	// Pod with no containers should be denied
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{},
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
			UID:       "empty-uid",
			Namespace: "default",
			Name:      "empty-pod",
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
	if resp.Allowed {
		t.Errorf("Expected pod with no containers to be denied, but got allowed")
	}
}

func TestSchedulerNameEmptyNoOverwrite(t *testing.T) {
	prevSchedulerName := config.SchedulerName
	prevForceOverwrite := config.ForceOverwriteDefaultScheduler
	t.Cleanup(func() {
		config.SchedulerName = prevSchedulerName
		config.ForceOverwriteDefaultScheduler = prevForceOverwrite
	})

	config.SchedulerName = "hami-scheduler"
	config.ForceOverwriteDefaultScheduler = false

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
		t.Fatalf("Expected allowed response, but got: %v", resp)
	}
	if len(resp.Patches) == 0 {
		t.Fatalf("Expected patches for schedulerName injection, got none")
	}
	found := false
	for _, patch := range resp.Patches {
		if patch.Path != "/spec/schedulerName" {
			continue
		}
		if patch.Operation != "add" && patch.Operation != "replace" {
			continue
		}
		if value, ok := patch.Value.(string); ok && value == config.SchedulerName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Expected schedulerName patch to %q, got patches: %+v", config.SchedulerName, resp.Patches)
	}
}

func TestPrivilegedContainerDenied(t *testing.T) {
	prevSchedulerName := config.SchedulerName
	prevDevicesMap := device.DevicesMap
	prevDevicesToHandle := device.DevicesToHandle
	t.Cleanup(func() {
		config.SchedulerName = prevSchedulerName
		device.DevicesMap = prevDevicesMap
		device.DevicesToHandle = prevDevicesToHandle
	})

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
		t.Fatalf("Failed to initialize devices with config: %v", err)
	}

	privileged := true
	testCases := []struct {
		name    string
		pod     *corev1.Pod
		allowed bool
	}{
		{
			name: "privileged container only without gpu",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "privileged-pod", Namespace: "default"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "privileged",
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
						},
					},
				},
			},
			allowed: true,
		},
		{
			name: "privileged sidecar with gpu workload",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "mixed-pod", Namespace: "default"},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "privileged-sidecar",
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
						},
						{
							Name: "gpu-workload",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			allowed: false,
		},
		{
			name: "privileged init container with gpu workload",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "init-privileged-pod", Namespace: "default"},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "privileged-init",
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "gpu-workload",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"hami.io/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			allowed: false,
		},
		{
			name: "privileged pod with different scheduler",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "other-scheduler-pod", Namespace: "default"},
				Spec: corev1.PodSpec{
					SchedulerName: "other-scheduler",
					Containers: []corev1.Container{
						{
							Name: "privileged",
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
						},
					},
				},
			},
			allowed: true,
		},
	}

	wh, err := NewWebHook()
	if err != nil {
		t.Fatalf("Error creating WebHook: %v", err)
	}

	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			podBytes, err := runtime.Encode(codec, tc.pod)
			if err != nil {
				t.Fatalf("Error encoding pod: %v", err)
			}

			req := admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Namespace: tc.pod.Namespace,
					Name:      tc.pod.Name,
					Object: runtime.RawExtension{
						Raw: podBytes,
					},
				},
			}

			resp := wh.Handle(context.Background(), req)
			if tc.allowed {
				if !resp.Allowed {
					t.Fatalf("Expected allowed response, but got denied: %+v", resp.Result)
				}
				return
			}
			if resp.Allowed {
				t.Fatalf("Expected denied response for privileged pod, but got allowed with %d patches", len(resp.Patches))
			}
			if len(resp.Patches) != 0 {
				t.Fatalf("Expected no patches for privileged pod, got %d", len(resp.Patches))
			}
			if resp.Result == nil || !strings.Contains(resp.Result.Message, "is privileged") {
				t.Fatalf("Expected privilege denial message, got: %+v", resp.Result)
			}
		})
	}
}
