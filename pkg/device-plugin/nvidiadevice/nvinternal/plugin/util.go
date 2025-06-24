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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/info"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// GetLibPath returns the path to the vGPU library.
func GetLibPath() string {
	libPath := hostHookPath + "/vgpu/libvgpu.so." + info.GetVersion()
	if _, err := os.Stat(libPath); os.IsNotExist(err) {
		libPath = hostHookPath + "/vgpu/libvgpu.so"
	}
	return libPath
}

func GetNextDeviceRequest(dtype string, p corev1.Pod) (corev1.Container, util.ContainerDevices, error) {
	pdevices, err := util.DecodePodDevices(util.InRequestDevices, p.Annotations)
	if err != nil {
		return corev1.Container{}, util.ContainerDevices{}, err
	}
	klog.Infof("pod annotation decode vaule is %+v", pdevices)
	res := util.ContainerDevices{}

	pd, ok := pdevices[dtype]
	if !ok {
		return corev1.Container{}, res, errors.New("device request not found")
	}
	for ctridx, ctrDevice := range pd {
		if len(ctrDevice) > 0 {
			return p.Spec.Containers[ctridx], ctrDevice, nil
		}
	}
	return corev1.Container{}, res, errors.New("device request not found")
}

func EraseNextDeviceTypeFromAnnotation(dtype string, p corev1.Pod) error {
	pdevices, err := util.DecodePodDevices(util.InRequestDevices, p.Annotations)
	if err != nil {
		return err
	}
	res := util.PodSingleDevice{}
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
				res = append(res, util.ContainerDevices{})
			} else {
				res = append(res, val)
			}
		}
	}
	klog.Infoln("After erase res=", res)
	newannos := make(map[string]string)
	newannos[util.InRequestDevices[dtype]] = util.EncodePodSingleDevice(res)
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
	for _, val := range strings.Split(output, "\n") {
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
}

func (nv *NvidiaDevicePlugin) GenerateMigTemplate(devtype string, devindex int, val util.ContainerDevice) (int, bool) {
	needsreset := false
	position := -1 // Initialize to an invalid position

	for _, migTemplate := range nv.schedulerConfig.MigGeometriesList {
		if containsModel(devtype, migTemplate.Models) {
			klog.InfoS("type found", "Type", devtype, "Models", strings.Join(migTemplate.Models, ", "))

			templateIdx, pos, err := util.ExtractMigTemplatesFromUUID(val.UUID)
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

func (nv *NvidiaDevicePlugin) GetContainerDeviceStrArray(c util.ContainerDevices) []string {
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
			}
			tmp = append(tmp, GetMigUUIDFromIndex(val.UUID, position))
		}
	}
	klog.V(3).Infoln("mig current=", nv.migCurrent, ":", needsreset, "position=", position, "uuid lists", tmp)
	return tmp
}
