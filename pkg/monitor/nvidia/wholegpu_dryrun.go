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

package nvidia

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/Project-HAMi/HAMi/pkg/device"
	nv "github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

// WholeGPUDryRunOptions restricts the read-only whole-GPU diagnostic to exact
// namespace, pod, or container names. Empty fields do not restrict the scan.
type WholeGPUDryRunOptions struct {
	Namespace string
	Pod       string
	Container string
}

// WholeGPUDeviceReport holds one NVML sample for a confirmed whole-GPU device.
type WholeGPUDeviceReport struct {
	Index       int
	UUID        string
	MemoryUsed  uint64
	MemoryTotal uint64
	SMUtil      uint32
	Error       string
}

// WholeGPUContainerReport holds a confirmed whole-GPU container and its device
// samples. A non-empty Error on an individual device does not invalidate the
// container classification.
type WholeGPUContainerReport struct {
	Namespace string
	Pod       string
	Container string
	Devices   []WholeGPUDeviceReport
}

// WholeGPUDiagnostic records a non-fatal classification or per-pod decoding
// result encountered by the diagnostic.
type WholeGPUDiagnostic struct {
	Namespace string
	Pod       string
	Container string
	Status    string
	Message   string
}

// WholeGPUDryRunReport is the complete result of one read-only whole-GPU scan.
type WholeGPUDryRunReport struct {
	Node                string
	ScannedPods         int
	CandidateContainers int
	ConfirmedContainers int
	Containers          []WholeGPUContainerReport
	Diagnostics         []WholeGPUDiagnostic
}

// RunWholeGPUDryRun lists pods assigned to nodeName, classifies their NVIDIA
// allocations from Kubernetes annotations, and samples NVML for confirmed whole
// GPUs. It performs only Kubernetes GET/LIST requests and NVML reads.
func RunWholeGPUDryRun(ctx context.Context, client kubernetes.Interface, nvmllib nvml.Interface, nodeName string, opts WholeGPUDryRunOptions) (*WholeGPUDryRunReport, error) {
	if client == nil {
		return nil, errors.New("kubernetes client is nil")
	}
	if nvmllib == nil {
		return nil, errors.New("NVML interface is nil")
	}
	if nodeName == "" {
		return nil, errors.New("node name is empty")
	}

	nodeDevs, err := getWholeGPUNodeDevices(ctx, client, nodeName)
	if err != nil {
		return nil, err
	}
	pods, err := client.CoreV1().Pods(opts.Namespace).List(ctx, metav1.ListOptions{FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName)})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	report := &WholeGPUDryRunReport{Node: nodeName}
	candidates := make([]wholeGPUCandidate, 0)
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.NodeName != nodeName || !matchesWholeGPUPodFilter(pod, opts) {
			continue
		}
		report.ScannedPods++
		podDevices, err := device.DecodePodDevices(nvidiaDeviceChecklist, pod.Annotations)
		if err != nil {
			report.Diagnostics = append(report.Diagnostics, WholeGPUDiagnostic{Namespace: pod.Namespace, Pod: pod.Name, Status: "annotation-error", Message: err.Error()})
			continue
		}
		for idx, ctrDevs := range podDevices[nv.NvidiaGPUDevice] {
			if len(ctrDevs) == 0 {
				continue
			}
			containerName, ok := containerNameByIndex(pod, idx)
			if !ok {
				report.Diagnostics = append(report.Diagnostics, WholeGPUDiagnostic{Namespace: pod.Namespace, Pod: pod.Name, Status: "container-index-error", Message: fmt.Sprintf("allocation index %d is outside pod container range", idx)})
				continue
			}
			if opts.Container != "" && containerName != opts.Container {
				continue
			}
			report.CandidateContainers++
			candidates = append(candidates, wholeGPUCandidate{pod: pod, container: containerName, devices: ctrDevs})
		}
	}

	// Sampling needs NVML up; bring it up lazily so candidates ruled out on
	// registry data alone never initialize the library at all.
	nvmlStarted := false
	startNVML := func() error {
		if nvmlStarted {
			return nil
		}
		ret := nvmllib.Init()
		if !errors.Is(ret, nvml.SUCCESS) {
			return fmt.Errorf("NVML initialization failed: %v", ret)
		}
		nvmlStarted = true
		return nil
	}

	for _, candidate := range candidates {
		var initErr error
		verdict, cause := evaluateContainerWholeGPU(candidate.devices, nodeDevs, func(uuid string) (int32, bool) {
			if err := startNVML(); err != nil {
				initErr = err
				return 0, false
			}
			return physTotalMiB(nvmllib, uuid)
		})
		if initErr != nil {
			return nil, initErr
		}
		if verdict != confirmedWholeGPU {
			report.Diagnostics = append(report.Diagnostics, wholeGPUVerdictDiagnostic(candidate, verdict, cause, nodeDevs))
			continue
		}
		report.ConfirmedContainers++
		report.Containers = append(report.Containers, WholeGPUContainerReport{
			Namespace: candidate.pod.Namespace,
			Pod:       candidate.pod.Name,
			Container: candidate.container,
			Devices:   makeWholeGPUDeviceReports(candidate.devices),
		})
	}

	if nvmlStarted {
		// NVML may have been brought up by registry-clean candidates that
		// still failed their physical check, so gate the shutdown on
		// whether the library actually started, not on confirmed verdicts.
		defer nvmllib.Shutdown()
	}
	if report.ConfirmedContainers > 0 {
		// A confirmed verdict means startNVML already succeeded during
		// evaluation, so sampling is safe to run here.
		for containerIdx := range report.Containers {
			for deviceIdx := range report.Containers[containerIdx].Devices {
				sampleWholeGPUDevice(nvmllib, &report.Containers[containerIdx].Devices[deviceIdx])
			}
		}
	}

	sortWholeGPUReport(report)
	return report, nil
}

type wholeGPUCandidate struct {
	pod       *corev1.Pod
	container string
	devices   device.ContainerDevices
}

func getWholeGPUNodeDevices(ctx context.Context, client kubernetes.Interface, nodeName string) (map[string]*device.DeviceInfo, error) {
	node, err := client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}
	annotation := node.Annotations[nv.RegisterAnnos]
	if annotation == "" {
		return map[string]*device.DeviceInfo{}, nil
	}
	devices, err := device.UnMarshalNodeDevices(annotation)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal node %s register annotation: %w", nodeName, err)
	}
	result := make(map[string]*device.DeviceInfo, len(devices))
	for _, dev := range devices {
		if dev != nil {
			result[dev.ID] = dev
		}
	}
	return result, nil
}

func matchesWholeGPUPodFilter(pod *corev1.Pod, opts WholeGPUDryRunOptions) bool {
	return (opts.Namespace == "" || pod.Namespace == opts.Namespace) && (opts.Pod == "" || pod.Name == opts.Pod)
}

func wholeGPUVerdictDiagnostic(candidate wholeGPUCandidate, verdict wholeGPUVerdict, cause indeterminateCause, nodeDevs map[string]*device.DeviceInfo) WholeGPUDiagnostic {
	diagnostic := WholeGPUDiagnostic{Namespace: candidate.pod.Namespace, Pod: candidate.pod.Name, Container: candidate.container}
	switch verdict {
	case indeterminate:
		if cause == causePhysUnreadable {
			diagnostic.Status = "nvml-unreadable"
			diagnostic.Message = "physical memory of one or more allocated devices could not be read through NVML"
		} else {
			diagnostic.Status = "unregistered-device"
			diagnostic.Message = "one or more allocated UUIDs are absent from the node device registry"
		}
	default:
		diagnostic.Status = "not-whole-gpu"
		for _, allocated := range candidate.devices {
			registered := nodeDevs[allocated.UUID]
			if registered != nil && registered.Mode == nv.MigMode {
				diagnostic.Message = fmt.Sprintf("device %s is registered as MIG", allocated.UUID)
				return diagnostic
			}
			if registered != nil && allocated.Usedmem < registered.Devmem {
				diagnostic.Message = fmt.Sprintf("device %s has allocated memory %d below registered memory %d", allocated.UUID, allocated.Usedmem, registered.Devmem)
				return diagnostic
			}
		}
		diagnostic.Message = "allocation does not represent one or more whole GPUs"
	}
	return diagnostic
}

func makeWholeGPUDeviceReports(devices device.ContainerDevices) []WholeGPUDeviceReport {
	reports := make([]WholeGPUDeviceReport, len(devices))
	for i, device := range devices {
		reports[i] = WholeGPUDeviceReport{Index: i, UUID: device.UUID}
	}
	return reports
}

func sampleWholeGPUDevice(nvmllib nvml.Interface, report *WholeGPUDeviceReport) {
	handle, ret := nvmllib.DeviceGetHandleByUUID(report.UUID)
	if !errors.Is(ret, nvml.SUCCESS) {
		report.Error = fmt.Sprintf("DeviceGetHandleByUUID failed: %v", ret)
		return
	}
	memory, ret := handle.GetMemoryInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		report.Error = fmt.Sprintf("GetMemoryInfo failed: %v", ret)
		return
	}
	report.MemoryUsed = memory.Used
	report.MemoryTotal = memory.Total
	utilization, ret := handle.GetUtilizationRates()
	if !errors.Is(ret, nvml.SUCCESS) {
		report.Error = fmt.Sprintf("GetUtilizationRates failed: %v", ret)
		return
	}
	report.SMUtil = utilization.Gpu
}

func sortWholeGPUReport(report *WholeGPUDryRunReport) {
	sort.Slice(report.Containers, func(i, j int) bool {
		left, right := report.Containers[i], report.Containers[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Pod != right.Pod {
			return left.Pod < right.Pod
		}
		return left.Container < right.Container
	})
	// Devices need no sort: makeWholeGPUDeviceReports already assigns
	// Index in slice order.
	sort.Slice(report.Diagnostics, func(i, j int) bool {
		left, right := report.Diagnostics[i], report.Diagnostics[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Pod != right.Pod {
			return left.Pod < right.Pod
		}
		if left.Container != right.Container {
			return left.Container < right.Container
		}
		return left.Status < right.Status
	})
}
