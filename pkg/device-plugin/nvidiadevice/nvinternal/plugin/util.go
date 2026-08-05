/*
 * Copyright (c) 2024, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"tags.cncf.io/container-device-interface/specs-go"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/info"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

// GetLibPath returns the path to the vGPU library.
func GetLibPath() string {
	libPath := hostHookPath + "/vgpu/libvgpu.so." + info.GetVersion()
	if _, err := os.Stat(libPath); os.IsNotExist(err) {
		libPath = hostHookPath + "/vgpu/libvgpu.so"
	}
	return libPath
}

func GetNextDeviceRequest(dtype string, p corev1.Pod) (corev1.Container, device.ContainerDevices, error) {
	pdevices, err := device.DecodePodDevices(device.InRequestDevices, p.Annotations)
	if err != nil {
		return corev1.Container{}, device.ContainerDevices{}, err
	}
	klog.Infof("pod annotation decode value is %+v", pdevices)
	res := device.ContainerDevices{}

	pd, ok := pdevices[dtype]
	if !ok {
		return corev1.Container{}, res, errors.New("device request not found")
	}

	// The annotation format follows the order: init containers first, then regular containers
	// Index mapping:
	//   0 to len(InitContainers)-1: init containers
	//   len(InitContainers) to len(InitContainers)+len(Containers)-1: regular containers
	initContainerCount := len(p.Spec.InitContainers)

	for ctridx, ctrDevice := range pd {
		if len(ctrDevice) > 0 {
			if ctridx < initContainerCount {
				// This is an init container
				klog.Infof("Found device request in init container at index %d, name: %s", ctridx, p.Spec.InitContainers[ctridx].Name)
				return p.Spec.InitContainers[ctridx], ctrDevice, nil
			} else {
				// This is a regular container
				regularContainerIdx := ctridx - initContainerCount
				if regularContainerIdx < len(p.Spec.Containers) {
					klog.Infof("Found device request in container at index %d (original idx: %d), name: %s", regularContainerIdx, ctridx, p.Spec.Containers[regularContainerIdx].Name)
					return p.Spec.Containers[regularContainerIdx], ctrDevice, nil
				}
			}
		}
	}
	return corev1.Container{}, res, errors.New("device request not found")
}

// decodePodSingleDevice decodes the pod's device allocation annotation for the
// given device type into a PodSingleDevice slice. Each element corresponds to
// one container (init containers first, then regular containers).
func decodePodSingleDevice(dtype string, p *corev1.Pod) (device.PodSingleDevice, error) {
	pdevices, err := device.DecodePodDevices(device.InRequestDevices, p.Annotations)
	if err != nil {
		return nil, err
	}
	pd, ok := pdevices[dtype]
	if !ok {
		return nil, errors.New("device request not found")
	}
	return pd, nil
}

// popNextContainerDevices finds and erases the first non-empty ContainerDevices
// from podSingleDev in place, returning the corresponding pod container and the
// popped devices. The container resolution uses the same init-then-regular
// ordering convention as the annotation encoder. It does NOT touch the API
// server — the caller is responsible for patching the annotation once after the
// loop completes.
func popNextContainerDevices(pod *corev1.Pod, podSingleDev device.PodSingleDevice) (corev1.Container, device.ContainerDevices, error) {
	initContainerCount := len(pod.Spec.InitContainers)
	for i, ctrDevs := range podSingleDev {
		if len(ctrDevs) > 0 {
			podSingleDev[i] = device.ContainerDevices{}
			if i < initContainerCount {
				return pod.Spec.InitContainers[i], ctrDevs, nil
			}
			regularIdx := i - initContainerCount
			if regularIdx >= len(pod.Spec.Containers) {
				return corev1.Container{}, nil, fmt.Errorf("container index %d out of range (init=%d, regular=%d)", i, initContainerCount, len(pod.Spec.Containers))
			}
			return pod.Spec.Containers[regularIdx], ctrDevs, nil
		}
	}
	return corev1.Container{}, nil, errors.New("no pending device allocation found")
}

// patchErasedAnnotation patches the pod's device annotation with the given
// podSingleDev (which has had some containers popped). It also updates
// pod.Annotations in place so that subsequent Allocate calls within the same
// kubelet invocation see the erased state.
func patchErasedAnnotation(pod *corev1.Pod, dtype string, podSingleDev device.PodSingleDevice) error {
	klog.V(5).Infof("After erase annotation, remaining devices: %v", podSingleDev)
	encoded := device.EncodePodSingleDevice(podSingleDev)
	annoKey := device.InRequestDevices[dtype]
	newAnnos := map[string]string{annoKey: encoded}
	if err := util.PatchPodAnnotations(pod, newAnnos); err != nil {
		return err
	}
	pod.Annotations[annoKey] = encoded
	return nil
}

func GetIndexAndTypeFromUUID(uuid string) (string, int) {
	defer nvml.Shutdown()
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.Errorln("nvml Init err: ", nvret)
		panic(0)
	}
	originuuid := strings.Split(uuid, "[")[0]
	ndev, ret := nvml.DeviceGetHandleByUUID(originuuid)
	if ret != nvml.SUCCESS {
		klog.Error("nvml get handlebyuuid error ret=", ret)
		panic(0)
	}
	Model, ret := ndev.GetName()
	if ret != nvml.SUCCESS {
		klog.Error("nvml get name error ret=", ret)
		panic(0)
	}
	index, ret := ndev.GetIndex()
	if ret != nvml.SUCCESS {
		klog.Error("nvml get index error ret=", ret)
		panic(0)
	}
	return Model, index
}

func GetMigGpuInstanceIdFromIndex(uuid string, idx int) (int, error) {
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.Errorln("nvml Init err: ", nvret)
		return 0, fmt.Errorf("nvml Init err: %s", nvml.ErrorString(nvret))
	}
	originuuid := strings.Split(uuid, "[")[0]
	ndev, ret := nvml.DeviceGetHandleByUUID(originuuid)
	if ret != nvml.SUCCESS {
		klog.Error(`nvml get device uuid error ret=`, ret)
		return 0, fmt.Errorf("nvml get device uuid error ret=%d", ret)
	}
	migdev, ret := nvml.DeviceGetMigDeviceHandleByIndex(ndev, idx)
	if ret != nvml.SUCCESS {
		klog.Error(`nvml get mig device handle error ret=`, ret)
		return 0, fmt.Errorf("nvml get mig device handle error ret=%d", ret)
	}
	res, ret := migdev.GetGpuInstanceId()
	if ret != nvml.SUCCESS {
		klog.Error(`nvml get gpu instance id error ret=`, ret)
		return 0, fmt.Errorf("nvml get gpu instance id error ret=%d", ret)
	}
	return res, nil
}

func GetDeviceNums() (int, error) {
	defer nvml.Shutdown()
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.Errorln("nvml Init err: ", nvret)
		return 0, fmt.Errorf("nvml Init err: %s", nvml.ErrorString(nvret))
	}
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		klog.Error(`nvml get count error ret=`, ret)
		return 0, fmt.Errorf("nvml get count error ret: %s", nvml.ErrorString(ret))
	}
	return count, nil
}

func GetDeviceNames() ([]string, error) {
	names := []string{}
	defer nvml.Shutdown()
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.Errorln("nvml Init err: ", nvret)
		return names, fmt.Errorf("nvml Init err: %s", nvml.ErrorString(nvret))
	}
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		klog.Error(`nvml get count error ret=`, ret)
		return names, fmt.Errorf("nvml get count error ret: %s", nvml.ErrorString(ret))
	}
	for i := 0; i < count; i++ {
		dev, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			klog.Error(`nvml get device error ret=`, ret)
			return names, fmt.Errorf("nvml get device error ret: %s", nvml.ErrorString(ret))
		}
		name, ret := dev.GetName()
		if ret != nvml.SUCCESS {
			klog.Error(`nvml get name error ret=`, ret)
			return names, fmt.Errorf("nvml get name error ret: %s", nvml.ErrorString(ret))
		}
		names = append(names, name)
	}
	return names, nil
}

func (nv *NvidiaDevicePlugin) DisableOtherNVMLOperation() {
	// Create MIG apply lock file
	if err := CreateMigApplyLock(); err != nil {
		// If the lock file creation fails, it is highly likely that the mig apply will be failed, so the plugin should terminate.
		klog.Fatalf("Failed to create MIG apply lock: %v", err)
		return
	}

	nv.disableHealthChecks <- true
	nv.disableWatchAndRegister <- true
	//wait for disableHealthChecks to be closed,signal must be true or wait forever
	var ackHealthCheck bool
	var ackWatchAndRegister bool
	for {
		select {
		case ackDisableHealthChecksSignal := <-nv.ackDisableHealthChecks:
			if ackDisableHealthChecksSignal {
				ackHealthCheck = true
			} else {
				continue
			}
		case ackWatchAndRegisterSignal := <-nv.ackDisableWatchAndRegister:
			if ackWatchAndRegisterSignal {
				ackWatchAndRegister = true
			} else {
				continue
			}
		}
		if ackHealthCheck && ackWatchAndRegister {
			break
		}
	}
}

func (nv *NvidiaDevicePlugin) EnableOtherNVMLOperation() {
	// Remove MIG apply lock file
	if err := RemoveMigApplyLock(); err != nil {
		klog.Errorf("Failed to remove MIG apply lock: %v", err)
	}

	nv.disableHealthChecks <- false
	nv.disableWatchAndRegister <- false
}

// Helper function to check if a model is in the list of models.
func containsModel(target string, models []string) bool {
	for _, model := range models {
		if strings.Contains(target, model) {
			return true
		}
	}
	return false
}

// Helper function to deepcopy new mig spec
func deepCopyMigConfig(src nvidia.MigConfigSpec) nvidia.MigConfigSpec {
	dst := src
	if src.Devices != nil {
		dst.Devices = make([]int32, len(src.Devices))
		copy(dst.Devices, src.Devices)
	}
	if src.MigDevices != nil {
		dst.MigDevices = make(map[string]int32)
		for k, v := range src.MigDevices {
			dst.MigDevices[k] = v
		}
	}
	return dst
}

func (nv *NvidiaDevicePlugin) GetContainerDeviceStrArray(c device.ContainerDevices, pod *corev1.Pod, containerName string) ([]string, error) {
	if nv.operatingMode != "mig" {
		out := make([]string, 0, len(c))
		for _, value := range c {
			out = append(out, value.UUID)
		}
		return out, nil
	}
	if nv.migMgr == nil || pod == nil {
		return nil, fmt.Errorf("MIG allocation requires an initialized manager and Pod")
	}
	containerIndex := -1
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Name == containerName {
			containerIndex = i
			break
		}
	}
	if containerIndex < 0 {
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == containerName {
				containerIndex = len(pod.Spec.InitContainers) + i
				break
			}
		}
	}
	if containerIndex < 0 {
		return nil, fmt.Errorf("container %q not found in Pod", containerName)
	}
	allocations, err := nvidia.DecodeMigAllocations(pod.Annotations[nvidia.MigAllocationsAnnotation])
	if err != nil {
		return nil, fmt.Errorf("decode scheduler MIG allocations: %w", err)
	}
	containerAllocations := make([]nvidia.MigAllocation, 0, len(c))
	for _, allocation := range allocations {
		if allocation.ContainerIndex == containerIndex {
			containerAllocations = append(containerAllocations, allocation)
		}
	}
	sort.Slice(containerAllocations, func(i, j int) bool { return containerAllocations[i].DeviceIndex < containerAllocations[j].DeviceIndex })
	if len(containerAllocations) != len(c) {
		return nil, fmt.Errorf("container %s has %d MIG reservations, requested %d devices", containerName, len(containerAllocations), len(c))
	}
	if err := nv.reconcileActiveMigAllocations(); err != nil {
		return nil, fmt.Errorf("reconcile MIG allocations before allocation: %w", err)
	}
	out := make([]string, 0, len(c))
	for i, reservation := range containerAllocations {
		if reservation.GPUUUID != c[i].UUID {
			return nil, fmt.Errorf("MIG reservation GPU %s does not match allocated GPU %s", reservation.GPUUUID, c[i].UUID)
		}
		gpuIndex, ok := gpuUUIDToIndex(reservation.GPUUUID)
		if !ok {
			return nil, fmt.Errorf("resolve parent GPU %s", reservation.GPUUUID)
		}
		migUUID, err := nv.migMgr.EnsureAllocation(gpuIndex, reservation.Profile, nvml.GpuInstancePlacement{Start: reservation.Placement.Start, Size: reservation.Placement.Size})
		if err != nil {
			return nil, err
		}
		out = append(out, migUUID)
	}
	return out, nil
}

var podAllocationTrySuccess = func(nodeName string, devName string, lockName string, pod *corev1.Pod) {
	refreshed, err := client.GetClient().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Error getting pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}
	annos := refreshed.Annotations[device.InRequestDevices[devName]]
	klog.Infof("Trying allocation success: %s", annos)
	for _, val := range device.DevicesToHandle {
		if strings.Contains(annos, val) {
			return
		}
	}
	klog.Infof("All devices allocate success, releasing lock")
	PodAllocationSuccess(nodeName, pod, lockName)
}

func PodAllocationTrySuccess(nodeName string, devName string, lockName string, pod *corev1.Pod) {
	podAllocationTrySuccess(nodeName, devName, lockName, pod)
}

func PodAllocationSuccess(nodeName string, pod *corev1.Pod, lockName string) {
	klog.Infof("Pod allocation successful for pod %s/%s on node %s", pod.Namespace, pod.Name, nodeName)
	updatePodAnnotationsAndReleaseLock(nodeName, pod, lockName, util.DeviceBindSuccess)
}

func updatePodAnnotationsAndReleaseLock(nodeName string, pod *corev1.Pod, lockName string, deviceBindPhase string) {
	newAnnos := map[string]string{util.DeviceBindPhase: deviceBindPhase}
	if err := util.PatchPodAnnotations(pod, newAnnos); err != nil {
		klog.Errorf("Failed to patch pod annotations for pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
	if err := nodelock.ReleaseNodeLock(nodeName, lockName, pod, false); err != nil {
		klog.Errorf("Failed to release node lock for node %s and lock %s: %v", nodeName, lockName, err)
	}
}

var podAllocationFailed = func(nodeName string, pod *corev1.Pod, lockName string) {
	klog.Infof("Pod allocation failed for pod %s/%s on node %s", pod.Namespace, pod.Name, nodeName)
	updatePodAnnotationsAndReleaseLock(nodeName, pod, lockName, util.DeviceBindFailed)
}

func PodAllocationFailed(nodeName string, pod *corev1.Pod, lockName string) {
	podAllocationFailed(nodeName, pod, lockName)
}

func checkCDISpecFile(filePath, kind string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("fail to read file: %v", err)
	}
	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("fail to parse json: %v", err)
	}
	return checkCDISpec(spec, kind)
}

func checkCDISpec(spec specs.Spec, kind string) error {
	if spec.Kind != kind {
		return fmt.Errorf("kind mismatch. current: %s, expect: %s", spec.Kind, kind)
	}
	for _, device := range spec.Devices {
		if strings.HasPrefix(device.Name, "MIG") {
			if len(device.ContainerEdits.DeviceNodes) == 0 {
				return fmt.Errorf("MIG device %s has no deviceNodes", device.Name)
			}
			containCap := false
			for _, node := range device.ContainerEdits.DeviceNodes {
				if strings.Contains(node.Path, "nvidia-cap") {
					containCap = true
					break
				}
			}
			if !containCap {
				return fmt.Errorf("MIG device %s does not have a corresponding nvidia-cap device", device.Name)
			}
		}
	}
	return nil
}

func createSpecFile(outputPath string) error {
	nvidiaCtkPath := "/usrbin/nvidia-ctk"
	if outputPath == "" {
		outputPath = "/var/run/cdi/k8s.device-plugin.nvidia.com-gpu.json"
	}
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %v", outputDir, err)
	}

	args := []string{
		"cdi",
		"generate",
		"--vendor", "k8s.device-plugin.nvidia.com",
		"--output", outputPath,
	}

	cmd := exec.Command(nvidiaCtkPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to generate CDI spec file: %v\ncommand: nvidia-ctk %v\noutput: %s",
			err, args, string(output))
	}

	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("spec file was not created at %s: %v", outputPath, err)
	}
	return nil
}
