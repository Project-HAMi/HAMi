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
	"flag"
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
	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
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

	ns := "default"
	memName := "nvidia.com/gpumem"
	coreName := "nvidia.com/gpucores"
	qm := device.NewQuotaManager()

	qm.Quotas[ns] = &device.DeviceQuota{
		memName:  &device.Quota{Used: 1000, Limit: 2000, LimitSet: true},
		coreName: &device.Quota{Used: 200, Limit: 400, LimitSet: true},
	}

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
			name: "InitContainers run sequentially: max init fits quota, but simple sum would exceed",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-init-fit",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					SchedulerName: "hami-scheduler",
					// Two init containers each asking for 800. Max = 800.
					InitContainers: []corev1.Container{
						{
							Name: "init1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("1"),
									"nvidia.com/gpumem": resource.MustParse("800"),
								},
							},
						},
						{
							Name: "init2",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("1"),
									"nvidia.com/gpumem": resource.MustParse("800"),
								},
							},
						},
					},
					// App container asking for 500. Total effective requirement = max(800, 500) = 800.
					// 800 is less than the available 1000 limit, so it should fit.
					// If the quota manager wrongly sums everything (800+800+500=2100), this test will catch it by failing.
					Containers: []corev1.Container{
						{
							Name: "app1",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("1"),
									"nvidia.com/gpumem": resource.MustParse("500"),
								},
							},
						},
					},
				},
			},
			fit: true,
		},
		{
			name: "InitContainer request exceeds quota directly",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-init-fail",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					SchedulerName: "hami-scheduler",
					InitContainers: []corev1.Container{
						{
							Name: "init-massive",
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu":    resource.MustParse("1"),
									"nvidia.com/gpumem": resource.MustParse("1500"), // 1500 > 1000 available limit
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "app1",
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
			fit: false,
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

// mluPod builds a pod requesting one MLU with the given vmemory and vcore.
func mluPod(ns string, vmemory, vcore string) *corev1.Pod {
	limits := corev1.ResourceList{
		"cambricon.com/mlu":              resource.MustParse("1"),
		"cambricon.com/mlu.smlu.vmemory": resource.MustParse(vmemory),
	}
	if vcore != "" {
		limits["cambricon.com/mlu.smlu.vcore"] = resource.MustParse(vcore)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mlu-pod", Namespace: ns},
		Spec: corev1.PodSpec{
			SchedulerName: "hami-scheduler",
			Containers: []corev1.Container{{
				Name:      "container1",
				Resources: corev1.ResourceRequirements{Limits: limits},
			}},
		},
	}
}

// Quota used to be checked for NVIDIA only, so a namespace limit on any other
// vendor's memory or cores was silently ignored.
func TestFitResourceQuotaNonNvidia(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			DefaultGPUNum:                1,
			MemoryFactor:                 1,
		},
		CambriconConfig: cambricon.CambriconConfig{
			ResourceCountName:  "cambricon.com/mlu",
			ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
			ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
		},
		HygonConfig: hygon.HygonConfig{
			ResourceCountName:  "hygon.com/dcunum",
			ResourceMemoryName: "hygon.com/dcumem",
			ResourceCoreName:   "hygon.com/dcucores",
			// Hygon scales the requested memory by this before recording it.
			MemoryFactor: 2,
		},
	}
	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		t.Fatalf("failed to initialize devices: %v", err)
	}

	qm := device.NewQuotaManager()

	// One MLU vmemory unit is 256 MiB, so a limit of 100 units leaves room for
	// 25600 MiB. Comparing the request against the raw 100 would deny every pod.
	qm.Quotas["mlu-mem"] = &device.DeviceQuota{
		"cambricon.com/mlu.smlu.vmemory": &device.Quota{Used: 0, Limit: 100, LimitSet: true},
	}
	qm.Quotas["mlu-core"] = &device.DeviceQuota{
		"cambricon.com/mlu.smlu.vcore": &device.Quota{Used: 20, Limit: 50, LimitSet: true},
	}
	qm.Quotas["dcu-mem"] = &device.DeviceQuota{
		"hygon.com/dcumem": &device.Quota{Used: 0, Limit: 1000, LimitSet: true},
	}
	t.Cleanup(func() {
		for _, ns := range []string{"mlu-mem", "mlu-core", "dcu-mem"} {
			delete(qm.Quotas, ns)
		}
	})

	dcuPod := func(ns, dcumem string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "dcu-pod", Namespace: ns},
			Spec: corev1.PodSpec{
				SchedulerName: "hami-scheduler",
				Containers: []corev1.Container{{
					Name: "container1",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"hygon.com/dcunum": resource.MustParse("1"),
							"hygon.com/dcumem": resource.MustParse(dcumem),
						},
					},
				}},
			},
		}
	}

	testCases := []struct {
		name string
		pod  *corev1.Pod
		fit  bool
	}{
		{
			name: "mlu memory within quota",
			pod:  mluPod("mlu-mem", "50", ""),
			fit:  true,
		},
		{
			name: "mlu memory over quota",
			pod:  mluPod("mlu-mem", "200", ""),
			fit:  false,
		},
		{
			name: "mlu cores over quota",
			pod:  mluPod("mlu-core", "1", "60"),
			fit:  false,
		},
		{
			name: "mlu in a namespace with no quota",
			pod:  mluPod("unquotaed", "10000", ""),
			fit:  true,
		},
		{
			name: "dcu memory within quota once the factor is applied",
			pod:  dcuPod("dcu-mem", "800"),
			fit:  true,
		},
		{
			name: "dcu memory over quota",
			pod:  dcuPod("dcu-mem", "1200"),
			fit:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fitResourceQuota(tc.pod); got != tc.fit {
				t.Errorf("fitResourceQuota() = %v, want %v", got, tc.fit)
			}
		})
	}
}

// A pod asking for two MLUs consumes twice the per-device memory.
func TestFitResourceQuotaCountsEveryDevice(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			DefaultGPUNum:                1,
			MemoryFactor:                 1,
		},
		CambriconConfig: cambricon.CambriconConfig{
			ResourceCountName:  "cambricon.com/mlu",
			ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
			ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
		},
	}
	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		t.Fatalf("failed to initialize devices: %v", err)
	}

	qm := device.NewQuotaManager()
	// 60 units is 15360 MiB of headroom.
	qm.Quotas["mlu-multi"] = &device.DeviceQuota{
		"cambricon.com/mlu.smlu.vmemory": &device.Quota{Used: 0, Limit: 60, LimitSet: true},
	}
	t.Cleanup(func() { delete(qm.Quotas, "mlu-multi") })

	pod := mluPod("mlu-multi", "40", "")
	pod.Spec.Containers[0].Resources.Limits["cambricon.com/mlu"] = resource.MustParse("2")

	// 2 x 40 units is 80, past the 60 unit limit, even though one device fits.
	if fitResourceQuota(pod) {
		t.Error("fitResourceQuota() = true, want false: two devices should each count against the quota")
	}
}

// Ascend applies a configurable factor to the memory it records, so the limit
// has to be raised by the same factor or pods that are within quota get denied.
func TestFitResourceQuotaAscendMemoryFactor(t *testing.T) {
	config.SchedulerName = "hami-scheduler"

	fs := flag.NewFlagSet("ascend-quota-test", flag.ContinueOnError)
	ascend.ParseConfig(fs)
	if err := fs.Parse([]string{"--enable-ascend=true"}); err != nil {
		t.Fatalf("failed to enable the ascend backend: %v", err)
	}
	t.Cleanup(func() {
		restore := flag.NewFlagSet("ascend-quota-restore", flag.ContinueOnError)
		ascend.ParseConfig(restore)
		_ = restore.Parse([]string{"--enable-ascend=false"})
	})

	sConfig := &config.Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			DefaultGPUNum:                1,
			MemoryFactor:                 1,
		},
		VNPUs: ascend.VNPUs{
			Configs: []ascend.VNPUConfig{{
				CommonWord:         "Ascend910B",
				ResourceName:       "huawei.com/Ascend910B",
				ResourceMemoryName: "huawei.com/Ascend910B-memory",
				ResourceCoreName:   "huawei.com/Ascend910B-core",
				MemoryCapacity:     65536,
				MemoryAllocatable:  65536,
				MemoryFactor:       4,
			}},
		},
	}
	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		t.Fatalf("failed to initialize devices: %v", err)
	}
	if _, ok := device.GetDevices()["Ascend910B"]; !ok {
		t.Fatal("the ascend backend was not registered, the test would pass vacuously")
	}

	qm := device.NewQuotaManager()
	qm.Quotas["ascend"] = &device.DeviceQuota{
		"huawei.com/Ascend910B-memory": &device.Quota{Used: 0, Limit: 8192, LimitSet: true},
	}
	t.Cleanup(func() { delete(qm.Quotas, "ascend") })

	// Requesting -core keeps the raw scaled value instead of rounding to a
	// template, which makes the arithmetic here easy to follow.
	ascendPod := func(mem string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "ascend-pod", Namespace: "ascend"},
			Spec: corev1.PodSpec{
				SchedulerName: "hami-scheduler",
				Containers: []corev1.Container{{
					Name: "container1",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"huawei.com/Ascend910B":        resource.MustParse("1"),
							"huawei.com/Ascend910B-memory": resource.MustParse(mem),
							"huawei.com/Ascend910B-core":   resource.MustParse("1"),
						},
					},
				}},
			},
		}
	}

	// 4096 x factor 4 is 16384, against a limit of 8192 x 4 = 32768.
	if !fitResourceQuota(ascendPod("4096")) {
		t.Error("fitResourceQuota() = false, want true: the limit must be scaled by the same factor as the request")
	}
	// 8192 x 4 is 32768 used against 32768 available, and 8193 pushes it over.
	if fitResourceQuota(ascendPod("8193")) {
		t.Error("fitResourceQuota() = true, want false: the request exceeds the scaled limit")
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

func TestFitResourceQuota_InitContainerPeakSequence(t *testing.T) {
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
		t.Fatalf("Failed to initialize devices with config: %v", err)
	}

	ns := "default"
	qm := device.NewQuotaManager()

	// initial quota: 30000 limit, 0 used
	qm.Quotas[ns] = &device.DeviceQuota{
		"nvidia.com/gpumem":   &device.Quota{Used: 0, Limit: 30000, LimitSet: true},
		"nvidia.com/gpucores": &device.Quota{Used: 0, Limit: 0},
	}
	t.Cleanup(func() { delete(qm.Quotas, ns) })

	makePod := func(name string, initMem, appMem int64) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
			Spec: corev1.PodSpec{
				SchedulerName: "hami-scheduler",
				InitContainers: []corev1.Container{
					{
						Name:  "init",
						Image: "busybox",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu":    resource.MustParse("1"),
								"nvidia.com/gpumem": *resource.NewQuantity(initMem, resource.DecimalSI),
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "app",
						Image: "busybox",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"nvidia.com/gpu":    resource.MustParse("1"),
								"nvidia.com/gpumem": *resource.NewQuantity(appMem, resource.DecimalSI),
							},
						},
					},
				},
			},
		}
	}

	// Step 1: Pod1 (init 20000, app 10000) should be allowed
	pod1 := makePod("pod1", 20000, 10000)
	if !fitResourceQuota(pod1) {
		t.Fatal("Step 1 failed: pod1 should be allowed (peak 20000 ≤ 30000)")
	}

	// Simulate pod1 scheduled → record its peak usage (20000)
	if dq, ok := qm.Quotas[ns]; ok {
		if q, ok := (*dq)["nvidia.com/gpumem"]; ok {
			q.Used = 20000
		} else {
			(*dq)["nvidia.com/gpumem"] = &device.Quota{Used: 20000, Limit: 30000, LimitSet: true}
		}
	}

	// Step 2: Pod2 (same) must be DENIED
	pod2 := makePod("pod2", 20000, 10000)
	if fitResourceQuota(pod2) {
		t.Fatal("Step 2 failed: pod2 should be denied (total used 20000 + request 20000 > 30000)")
	}

	// Step 3: Pod1 init finished → usage drops to app only (10000)
	if dq, ok := qm.Quotas[ns]; ok {
		if q, ok := (*dq)["nvidia.com/gpumem"]; ok {
			q.Used = 10000
		}
	}

	// Now pod2 should be allowed
	if !fitResourceQuota(pod2) {
		t.Fatal("Step 3 failed: pod2 should be allowed after pod1 init finished (total 10000+20000=30000)")
	}
}
