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

package device

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

const testDevType = "test-gpu"

// sumUsage sums the collapsed per-UUID usage the accounting consumers see
// (countPodDevices / getNodesUsage both sum every ContainerDevice entry).
func sumUsage(pd PodDevices) map[string]struct {
	count int
	mem   int32
	cores int32
} {
	res := make(map[string]struct {
		count int
		mem   int32
		cores int32
	})
	for _, psd := range pd {
		for _, ctrDevs := range psd {
			for _, d := range ctrDevs {
				v := res[d.UUID]
				v.count++
				v.mem += d.Usedmem
				v.cores += d.Usedcores
				res[d.UUID] = v
			}
		}
	}
	return res
}

func ctr(devs ...ContainerDevice) ContainerDevices { return ContainerDevices(devs) }

func dev(uuid string, mem, cores int32) ContainerDevice {
	return ContainerDevice{UUID: uuid, Type: testDevType, Usedmem: mem, Usedcores: cores}
}

func podWithInit(numInit, numApp int) *corev1.Pod {
	p := &corev1.Pod{}
	for range numInit {
		p.Spec.InitContainers = append(p.Spec.InitContainers, corev1.Container{})
	}
	for range numApp {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{})
	}
	return p
}

func TestCollapseInitContainerUsage(t *testing.T) {
	tests := []struct {
		name    string
		pod     *corev1.Pod
		podDev  PodDevices
		appOnly bool
		// expected per-UUID effective usage
		want map[string]struct {
			count int
			mem   int32
			cores int32
		}
	}{
		{
			name:   "no init containers - app sum unchanged",
			pod:    podWithInit(0, 2),
			podDev: PodDevices{testDevType: PodSingleDevice{ctr(dev("gpu-0", 10, 30)), ctr(dev("gpu-0", 4, 20))}},
			want: map[string]struct {
				count int
				mem   int32
				cores int32
			}{"gpu-0": {count: 2, mem: 14, cores: 50}},
		},
		{
			name: "init smaller than app - app wins",
			pod:  podWithInit(1, 1),
			// init 10Gi, app 20Gi on same uuid
			podDev: PodDevices{testDevType: PodSingleDevice{ctr(dev("gpu-0", 10, 0)), ctr(dev("gpu-0", 20, 0))}},
			want: map[string]struct {
				count int
				mem   int32
				cores int32
			}{"gpu-0": {count: 1, mem: 20, cores: 0}},
		},
		{
			name: "init larger than app - init wins (Case 2)",
			pod:  podWithInit(1, 1),
			// init 20Gi, app 10Gi
			podDev: PodDevices{testDevType: PodSingleDevice{ctr(dev("gpu-0", 20, 0)), ctr(dev("gpu-0", 10, 0))}},
			want: map[string]struct {
				count int
				mem   int32
				cores int32
			}{"gpu-0": {count: 1, mem: 20, cores: 0}},
		},
		{
			name: "multi init - max across init containers",
			pod:  podWithInit(2, 1),
			// init1 12Gi, init2 20Gi, app 10Gi -> max(sum(app)=10, max(init)=20)=20
			podDev: PodDevices{testDevType: PodSingleDevice{
				ctr(dev("gpu-0", 12, 0)),
				ctr(dev("gpu-0", 20, 0)),
				ctr(dev("gpu-0", 10, 0)),
			}},
			want: map[string]struct {
				count int
				mem   int32
				cores int32
			}{"gpu-0": {count: 1, mem: 20, cores: 0}},
		},
		{
			name: "multi app sum beats single init",
			pod:  podWithInit(1, 2),
			// init 15Gi, app1 10Gi, app2 10Gi -> max(sum(app)=20, init=15)=20
			podDev: PodDevices{testDevType: PodSingleDevice{
				ctr(dev("gpu-0", 15, 0)),
				ctr(dev("gpu-0", 10, 0)),
				ctr(dev("gpu-0", 10, 0)),
			}},
			want: map[string]struct {
				count int
				mem   int32
				cores int32
			}{"gpu-0": {count: 2, mem: 20, cores: 0}},
		},
		{
			name: "distinct UUIDs never merged",
			pod:  podWithInit(1, 1),
			// init on gpu-0 20Gi, app on gpu-1 10Gi
			podDev: PodDevices{testDevType: PodSingleDevice{
				ctr(dev("gpu-0", 20, 0)),
				ctr(dev("gpu-1", 10, 0)),
			}},
			want: map[string]struct {
				count int
				mem   int32
				cores int32
			}{
				"gpu-0": {count: 1, mem: 20, cores: 0},
				"gpu-1": {count: 1, mem: 10, cores: 0},
			},
		},
		{
			name:    "app only view drops init",
			pod:     podWithInit(1, 1),
			appOnly: true,
			// init 20Gi, app 10Gi -> app-only = 10Gi
			podDev: PodDevices{testDevType: PodSingleDevice{ctr(dev("gpu-0", 20, 0)), ctr(dev("gpu-0", 10, 0))}},
			want: map[string]struct {
				count int
				mem   int32
				cores int32
			}{"gpu-0": {count: 1, mem: 10, cores: 0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got PodDevices
			if tt.appOnly {
				got = AppContainersOnly(tt.pod, tt.podDev)
			} else {
				got = CollapseInitContainerUsage(tt.pod, tt.podDev)
			}
			sums := sumUsage(got)
			if len(sums) != len(tt.want) {
				t.Fatalf("uuid count = %d, want %d (got %+v)", len(sums), len(tt.want), sums)
			}
			for uuid, w := range tt.want {
				g, ok := sums[uuid]
				if !ok {
					t.Fatalf("missing uuid %s in %+v", uuid, sums)
				}
				if g.count != w.count || g.mem != w.mem || g.cores != w.cores {
					t.Errorf("uuid %s = {count:%d mem:%d cores:%d}, want {count:%d mem:%d cores:%d}",
						uuid, g.count, g.mem, g.cores, w.count, w.mem, w.cores)
				}
			}
		})
	}
}

func termStatus(exitCode int32) corev1.ContainerStatus {
	return corev1.ContainerStatus{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode}}}
}

func runningStatus() corev1.ContainerStatus {
	return corev1.ContainerStatus{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
}

func TestInitContainersAllTerminated(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		wantTerm      bool
		wantSucceeded bool
	}{
		{
			name:     "no init containers",
			pod:      podWithInit(0, 1),
			wantTerm: false,
		},
		{
			name: "init still running",
			pod: func() *corev1.Pod {
				p := podWithInit(1, 1)
				p.Status.InitContainerStatuses = []corev1.ContainerStatus{runningStatus()}
				return p
			}(),
			wantTerm: false,
		},
		{
			name: "status not yet populated",
			pod: func() *corev1.Pod {
				p := podWithInit(2, 1)
				p.Status.InitContainerStatuses = []corev1.ContainerStatus{termStatus(0)}
				return p
			}(),
			wantTerm: false,
		},
		{
			name: "all terminated exit 0",
			pod: func() *corev1.Pod {
				p := podWithInit(2, 1)
				p.Status.InitContainerStatuses = []corev1.ContainerStatus{termStatus(0), termStatus(0)}
				return p
			}(),
			wantTerm:      true,
			wantSucceeded: true,
		},
		{
			name: "terminated non-zero",
			pod: func() *corev1.Pod {
				p := podWithInit(1, 1)
				p.Status.InitContainerStatuses = []corev1.ContainerStatus{termStatus(1)}
				return p
			}(),
			wantTerm:      true,
			wantSucceeded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InitContainersAllTerminated(tt.pod); got != tt.wantTerm {
				t.Errorf("InitContainersAllTerminated = %v, want %v", got, tt.wantTerm)
			}
			if got := InitContainersAllSucceeded(tt.pod); got != tt.wantSucceeded {
				t.Errorf("InitContainersAllSucceeded = %v, want %v", got, tt.wantSucceeded)
			}
		})
	}
}
