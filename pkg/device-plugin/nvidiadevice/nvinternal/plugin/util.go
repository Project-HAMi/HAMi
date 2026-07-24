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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"gopkg.in/yaml.v3"
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

var eraseNextDeviceTypeFromAnnotation = func(dtype string, p corev1.Pod) error {
	pdevices, err := device.DecodePodDevices(device.InRequestDevices, p.Annotations)
	if err != nil {
		return err
	}
	res := device.PodSingleDevice{}
	pd, ok := pdevices[dtype]
	if !ok {
		return errors.New("erase device annotation not found")
	}
	found := false
	for _, val := range pd {
		if found {
			res = append(res, val)
		} else {
			if len(val) > 0 {
				found = true
				res = append(res, device.ContainerDevices{})
			} else {
				res = append(res, val)
			}
		}
	}
	klog.Infoln("After erase res=", res)
	newannos := make(map[string]string)
	newannos[device.InRequestDevices[dtype]] = device.EncodePodSingleDevice(res)
	return util.PatchPodAnnotations(&p, newannos)
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

func GetMigUUIDFromSmiOutput(output string, uuid string, idx int) string {
	migmode := false
	for val := range strings.SplitSeq(output, "\n") {
		if !strings.Contains(val, "MIG") && strings.Contains(val, uuid) {
			migmode = true
			continue
		}
		if !strings.Contains(val, "MIG") && !strings.Contains(val, uuid) {
			migmode = false
			continue
		}
		if !migmode {
			continue
		}
		klog.Infoln("inspecting", val)
		num := strings.Split(val, "Device")[1]
		num = strings.Split(num, ":")[0]
		num = strings.TrimSpace(num)
		index, err := strconv.Atoi(num)
		if err != nil {
			klog.Fatal("atoi failed num=", num)
		}
		if index == idx {
			outputStr := strings.Split(val, ":")[2]
			outputStr = strings.TrimSpace(outputStr)
			outputStr = strings.TrimRight(outputStr, ")")
			return outputStr
		}
	}
	return ""
}

func GetMigUUIDFromIndex(uuid string, idx int) string {
	defer nvml.Shutdown()
	if nvret := nvml.Init(); nvret != nvml.SUCCESS {
		klog.Errorln("nvml Init err: ", nvret)
		panic(0)
	}
	originuuid := strings.Split(uuid, "[")[0]
	ndev, ret := nvml.DeviceGetHandleByUUID(originuuid)
	if ret != nvml.SUCCESS {
		klog.Error(`nvml get device uuid error ret=`, ret)
		panic(0)
	}
	migdev, ret := nvml.DeviceGetMigDeviceHandleByIndex(ndev, idx)
	if ret != nvml.SUCCESS {
		klog.Error("nvml get mig dev error ret=", ret, ",idx=", idx, "using nvidia-smi -L for query")
		cmd := exec.Command("nvidia-smi", "-L")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			klog.Fatalf("nvidia-smi -L failed with %s\n", err)
		}
		outStr := stdout.String()
		uuid := GetMigUUIDFromSmiOutput(outStr, originuuid, idx)
		return uuid
	}
	res, ret := migdev.GetUUID()
	if ret != nvml.SUCCESS {
		klog.Error(`nvml get mig uuid error ret=`, ret)
		panic(0)
	}
	return res
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

func (nv *NvidiaDevicePlugin) ApplyMigTemplate() {
	nv.applyMutex.Lock()
	nv.DisableOtherNVMLOperation()
	defer func() {
		nv.EnableOtherNVMLOperation()
		nv.applyMutex.Unlock()
	}()
	data, err := yaml.Marshal(nv.migCurrent)
	if err != nil {
		klog.Error("marshal failed", err.Error())
	}
	klog.Infoln("Applying data=", string(data))
	os.WriteFile("/tmp/migconfig.yaml", data, os.ModePerm)
	cmd := exec.Command("nvidia-mig-parted", "apply", "-f", "/tmp/migconfig.yaml")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		klog.Fatalf("nvidia-mig-parted failed with %s,reason:%s\n", err, stderr.String())
	}
	outStr := stdout.String()
	klog.Infoln("Mig apply", outStr)

	// nvidia-mig-parted can report success while creating zero MIG instances on
	// some newer cards (e.g. RTX PRO 6000 Blackwell Server Edition), where its
	// NVML-based create path is a silent no-op. Verify the instances actually
	// exist and fall back to the nvidia-smi CLI for any GPU that is short.
	nv.ensureMigInstancesViaSmi()
}

// ensureMigInstancesViaSmi verifies that the MIG instances requested in
// nv.migCurrent actually exist and, for any GPU that is short, recreates the
// geometry with the nvidia-smi CLI. It is a no-op on hardware where
// nvidia-mig-parted already carved the instances correctly, so it is safe for
// all MIG-capable cards.
func (nv *NvidiaDevicePlugin) ensureMigInstancesViaSmi() {
	current, ok := nv.migCurrent.MigConfigs["current"]
	if !ok {
		return
	}

	out, err := exec.Command("nvidia-smi", "-L").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to list GPUs with nvidia-smi -L, skipping MIG fallback: %v, output: %s", err, string(out))
		return
	}
	counts := migInstanceCountsFromSmi(string(out))

	for _, migSpec := range current {
		if !migSpec.MigEnabled {
			continue
		}
		expected := 0
		for _, c := range migSpec.MigDevices {
			expected += int(c)
		}
		if expected == 0 {
			continue
		}
		for _, dev := range migSpec.Devices {
			gpuIndex := int(dev)
			if counts[gpuIndex] >= expected {
				continue
			}
			klog.Warningf("GPU %d has %d MIG instance(s) but %d were requested; nvidia-mig-parted did not create them, falling back to nvidia-smi", gpuIndex, counts[gpuIndex], expected)
			if err := createMigDevicesViaSmi(gpuIndex, migSpec.MigDevices); err != nil {
				klog.Errorf("nvidia-smi MIG fallback failed for GPU %d: %v", gpuIndex, err)
				continue
			}
			klog.Infof("nvidia-smi MIG fallback created geometry on GPU %d: %v", gpuIndex, migSpec.MigDevices)
		}
	}
}

// migInstanceCountsFromSmi parses `nvidia-smi -L` output and returns the number
// of already-created MIG devices per physical GPU index. GPUs with no MIG
// devices are still recorded with a count of zero.
func migInstanceCountsFromSmi(output string) map[int]int {
	counts := make(map[int]int)
	currentGPU := -1
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "GPU ") {
			// e.g. "GPU 0: NVIDIA RTX PRO 6000 Blackwell Server Edition (UUID: GPU-...)"
			idxStr := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(trimmed, "GPU "), ":", 2)[0])
			if idx, err := strconv.Atoi(idxStr); err == nil {
				currentGPU = idx
				if _, seen := counts[currentGPU]; !seen {
					counts[currentGPU] = 0
				}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "MIG ") && currentGPU >= 0 {
			counts[currentGPU]++
		}
	}
	return counts
}

// createMigDevicesViaSmi resets and recreates the desired MIG geometry on a
// single physical GPU using the nvidia-smi CLI. It is the fallback for GPUs
// where nvidia-mig-parted reports success but the NVML create path produces no
// MIG instances.
func createMigDevicesViaSmi(gpuIndex int, migDevices map[string]int32) error {
	gpu := strconv.Itoa(gpuIndex)

	cgi := buildCreateGpuInstancesArg(migDevices)
	if cgi == "" {
		return fmt.Errorf("no MIG devices requested for GPU %d", gpuIndex)
	}

	// Best-effort: ensure MIG mode is enabled (a no-op if already enabled).
	runNvidiaSmi("-i", gpu, "-mig", "1")

	// Destroy any pre-existing compute/GPU instances so the geometry is clean.
	// Compute instances must be destroyed before their parent GPU instances.
	// These fail harmlessly when nothing exists yet, so errors are only logged.
	runNvidiaSmi("mig", "-i", gpu, "-dci")
	runNvidiaSmi("mig", "-i", gpu, "-dgi")

	// -C also creates the matching compute instance for each GPU instance.
	if out, err := runNvidiaSmi("mig", "-i", gpu, "-cgi", cgi, "-C"); err != nil {
		return fmt.Errorf("nvidia-smi mig create failed on GPU %d (cgi=%s): %v, output: %s", gpuIndex, cgi, err, out)
	}
	return nil
}

// buildCreateGpuInstancesArg expands a MigDevices map (profile name -> count)
// into a comma-separated argument for `nvidia-smi mig -cgi`, ordering larger
// GPU-instance slices first so placement succeeds for mixed geometries.
func buildCreateGpuInstancesArg(migDevices map[string]int32) string {
	type profile struct {
		name   string
		slices int
	}
	var profiles []profile
	for name, count := range migDevices {
		for i := int32(0); i < count; i++ {
			profiles = append(profiles, profile{name: name, slices: migProfileSlices(name)})
		}
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].slices != profiles[j].slices {
			return profiles[i].slices > profiles[j].slices
		}
		return profiles[i].name < profiles[j].name
	})
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.name)
	}
	return strings.Join(names, ",")
}

// migProfileSlices returns the leading GPU-instance slice count of a MIG
// profile name (e.g. "2g.10gb" -> 2). It returns 1 when the name cannot be
// parsed so the profile is still created, just placed last.
func migProfileSlices(name string) int {
	idx := strings.Index(name, "g")
	if idx <= 0 {
		return 1
	}
	if n, err := strconv.Atoi(name[:idx]); err == nil && n > 0 {
		return n
	}
	return 1
}

// runNvidiaSmi runs nvidia-smi with the given args and returns its combined
// output. Failures are logged at a high verbosity because some calls (instance
// teardown) are expected to fail when there is nothing to tear down.
func runNvidiaSmi(args ...string) (string, error) {
	out, err := exec.Command("nvidia-smi", args...).CombinedOutput()
	if err != nil {
		klog.V(4).Infof("nvidia-smi %s: %v, output: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out), err
}

func (nv *NvidiaDevicePlugin) GenerateMigTemplate(devtype string, devindex int, val device.ContainerDevice) (int, bool) {
	needsreset := false
	position := -1 // Initialize to an invalid position

	for _, migTemplate := range nv.schedulerConfig.MigGeometriesList {
		if containsModel(devtype, migTemplate.Models) {
			klog.InfoS("type found", "Type", devtype, "Models", strings.Join(migTemplate.Models, ", "))

			templateIdx, pos, err := device.ExtractMigTemplatesFromUUID(val.UUID)
			if err != nil {
				klog.ErrorS(err, "failed to extract template index from UUID", "UUID", val.UUID)
				return -1, false
			}
			position = pos

			if templateIdx < 0 || templateIdx >= len(migTemplate.Geometries) {
				klog.ErrorS(nil, "invalid template index extracted from UUID", "UUID", val.UUID, "Index", templateIdx)
				return -1, false
			}

			v := migTemplate.Geometries[templateIdx]

			for migidx, migpartedDev := range nv.migCurrent.MigConfigs["current"] {
				if containsDevice(devindex, migpartedDev.Devices) {
					for _, migTemplateEntry := range v {
						currentCount, ok := migpartedDev.MigDevices[migTemplateEntry.Name]
						expectedCount := migTemplateEntry.Count

						if !ok || currentCount != expectedCount {
							needsreset = true
							klog.InfoS("updated mig device count", "Template", v)
						} else {
							klog.InfoS("incremented mig device count", "TemplateName", migTemplateEntry.Name, "Count", currentCount+1)
						}
					}

					if needsreset {
						for k := range nv.migCurrent.MigConfigs["current"][migidx].MigDevices {
							delete(nv.migCurrent.MigConfigs["current"][migidx].MigDevices, k)
						}

						for _, migTemplateEntry := range v {
							nv.migCurrent.MigConfigs["current"][migidx].MigDevices[migTemplateEntry.Name] = migTemplateEntry.Count
							nv.migCurrent.MigConfigs["current"][migidx].MigEnabled = true
						}
					}
					break
				}
			}
			break
		}
	}

	return position, needsreset
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

// Helper function to check if a device index is in the list of devices.
func containsDevice(target int, devices []int32) bool {
	for _, device := range devices {
		if int(device) == target {
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

func (nv *NvidiaDevicePlugin) GetContainerDeviceStrArray(c device.ContainerDevices) []string {
	tmp := []string{}
	needsreset := false
	position := 0
	for _, val := range c {
		if !strings.Contains(val.UUID, "[") {
			tmp = append(tmp, val.UUID)
		} else {
			devtype, devindex := GetIndexAndTypeFromUUID(val.UUID)
			position, needsreset = nv.GenerateMigTemplate(devtype, devindex, val)
			if needsreset {
				nv.ApplyMigTemplate()
				if nv.deviceListStrategies.Includes(spec.DeviceListStrategyVolumeMounts) ||
					nv.deviceListStrategies.Includes(spec.DeviceListStrategyCDIAnnotations) ||
					nv.deviceListStrategies.Includes(spec.DeviceListStrategyCDICRI) {
					klog.V(3).Infoln("generate CDI spec file")
					const (
						maxTryTimes      = 5
						waitTimeInterval = 5 * time.Second
						specFilePath     = "/var/run/cdi/k8s.device-plugin.nvidia.com-gpu.json"
						kind             = "k8s.device-plugin.nvidia.com/gpu"
					)
					for i := 0; i < maxTryTimes; i++ {
						if err := createSpecFile(specFilePath); err != nil {
							klog.Warningf("failed to create CDI spec file: %v", err)
						} else {
							klog.Infof("createSpecFile ok. file path %s", specFilePath)
						}
						if err := checkCDISpecFile(specFilePath, kind); err != nil {
							klog.Warningf("check CDI spec file failed. %v", err)
							if i == maxTryTimes-1 {
								klog.Fatalf("exceed the max trytime %d", maxTryTimes)
							} else {
								time.Sleep(waitTimeInterval)
								klog.Warningf("try to create CDI spec file again. try times: %d", i)
								continue
							}
						}
						klog.Infof("check CDI spec file ok")
						break
					}
				}
			}
			tmp = append(tmp, GetMigUUIDFromIndex(val.UUID, position))
		}
	}
	klog.V(3).Infoln("mig current=", nv.migCurrent, ":", needsreset, "position=", position, "uuid lists", tmp)
	return tmp
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
		return
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
