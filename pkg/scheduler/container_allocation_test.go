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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestAllocationPreservesContainerIndices(t *testing.T) {
	for _, model := range []string{"NVIDIA", "NVIDIA-Tesla T4"} {
		t.Run(model, func(t *testing.T) {
			pod := &corev1.Pod{Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{Name: "storage-initializer"}},
				Containers: []corev1.Container{{Name: "sidecar-before"}, {Name: "kserve-container", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{
					"hami.io/gpu": resource.MustParse("1"), "hami.io/gpumem": resource.MustParse("4096"), "hami.io/gpucores": resource.MustParse("20"),
				}}}, {Name: "sidecar-after"}},
			}}
			node := &device.NodeInfo{ID: "node", Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}, Devices: map[string][]device.DeviceInfo{
				nvidia.NvidiaGPUDevice: {{ID: "GPU-test", Count: 10, Devmem: 16000, Devcore: 100, Type: model, DeviceVendor: nvidia.NvidiaGPUDevice, Health: true}},
			}}
			requests, err := device.Resourcereqs(pod)
			if err != nil {
				t.Fatal(err)
			}
			result := (&Scheduler{}).scoreNode("node", buildNodeUsage(node, pod), requests, pod, util.NodeSchedulerPolicyBinpack.String(), util.DefaultDeviceScoringWeights())
			if result.err != nil || result.score == nil {
				t.Fatalf("score failed: %v %s", result.err, result.reason)
			}
			allocations := result.score.Devices[nvidia.NvidiaGPUDevice]
			if len(allocations) != 4 || len(result.score.Devices) != 1 {
				t.Fatalf("unexpected allocation layout: %+v", result.score.Devices)
			}
			for i, name := range []string{"storage-initializer", "sidecar-before", "kserve-container", "sidecar-after"} {
				if i == 2 {
					if len(allocations[i]) != 1 || allocations[i][0].UUID != "GPU-test" || allocations[i][0].Usedmem != 4096 || allocations[i][0].Usedcores != 20 {
						t.Errorf("incorrect allocation for %s: %+v", name, allocations[i])
					}
				} else if len(allocations[i]) != 0 {
					t.Errorf("device-less container %s received allocation: %+v", name, allocations[i])
				}
			}
			annotations := map[string]string{}
			nvidia.InitNvidiaDevice(nvidia.NvidiaConfig{}).PatchAnnotations(pod, &annotations, result.score.Devices)
			if got, want := annotations[device.SupportDevices[nvidia.NvidiaGPUDevice]], ";;GPU-test,NVIDIA,4096,20:;;"; got != want {
				t.Errorf("allocation annotation = %q, want %q", got, want)
			}
		})
	}
}
