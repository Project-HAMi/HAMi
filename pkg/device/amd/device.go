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

package amd

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type AMDDevices struct {
	resourceCountName  string
	resourceMemoryName string
}

const (
	AMDDevice          = "AMDGPU"
	AMDCommonWord      = "AMDGPU"
	AMDDeviceSelection = "amd.com/gpu-index"
	AMDUseUUID         = "amd.com/use-gpu-uuid"
	AMDNoUseUUID       = "amd.com/nouse-gpu-uuid"
	AMDAssignedNode    = "amd.com/predicate-node"
	Mi300xMemory       = 192000
)

type AMDConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
}

func InitAMDGPUDevice(config AMDConfig) *AMDDevices {
	_, ok := device.SupportDevices[AMDDevice]
	if !ok {
		device.SupportDevices[AMDDevice] = "hami.io/amd-devices-allocated"
	}
	return &AMDDevices{
		resourceCountName:  config.ResourceCountName,
		resourceMemoryName: config.ResourceMemoryName,
	}
}

func (dev *AMDDevices) CommonWord() string {
	return AMDCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
}

func (dev *AMDDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(dev.resourceCountName)]
	if !ok {
		_, ok = ctr.Resources.Limits[corev1.ResourceName(dev.resourceMemoryName)]
	}
	klog.Infoln("MutateAdmsssion result", ok)
	return ok, nil
}

func (dev *AMDDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	counts, ok := n.Status.Capacity.Name(corev1.ResourceName(dev.resourceCountName), resource.DecimalSI).AsInt64()
	if !ok || counts == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", dev.resourceCountName)
	}
	for int64(i) < counts {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:        uint(i),
			ID:           n.Name + "-" + AMDDevice + "-" + fmt.Sprint(i),
			Count:        1,
			Devmem:       Mi300xMemory,
			Devcore:      100,
			Type:         AMDDevice,
			Numa:         0,
			Health:       true,
			CustomInfo:   make(map[string]any),
			DeviceVendor: AMDCommonWord,
		})
		i++
	}
	i = 0
	for i < len(nodedevices) {
		klog.V(4).Infoln("Registered AMD nodedevices:", nodedevices[i])
		i++
	}
	return nodedevices, nil
}

func (dev *AMDDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[AMDDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[device.SupportDevices[AMDDevice]] = device.EncodePodSingleDevice(devlist)
	}
	klog.V(4).InfoS("annos", "input", (*annoinput))
	return *annoinput
}

func (dev *AMDDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AMDDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AMDDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *AMDDevices) checkType(n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, AMDDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *AMDDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *AMDDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  dev.resourceCountName,
		ResourceMemoryName: dev.resourceMemoryName,
		ResourceCoreName:   "",
	}
}

func (dev *AMDDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count AMD devices for container ", ctr.Name)
	amdResourceCount := corev1.ResourceName(dev.resourceCountName)
	//amdResourceMemory := corev1.ResourceName(dev.resourceMemoryName)
	v, ok := ctr.Resources.Limits[amdResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[amdResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.InfoS("Detected AMD device request",
				"container", ctr.Name,
				"deviceCount", n)
			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             AMDDevice,
				Memreq:           Mi300xMemory,
				MemPercentagereq: 0,
				Coresreq:         0,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *AMDDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *AMDDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (amddevice *AMDDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeinfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		klog.V(3).InfoS("Type check", "device", dev.Type, "req", k.Type, "dev=", dev)
		if !strings.Contains(dev.Type, k.Type) {
			reason[common.CardTypeMismatch]++
			continue
		}

		_, found, _ := amddevice.checkType(k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, AMDUseUUID, AMDNoUseUUID, amddevice.CommonWord()) {
			reason[common.CardUUIDMismatch]++
			klog.V(5).InfoS(common.CardUUIDMismatch, "pod", klog.KObj(pod), "device", dev.ID, "current device info is:", *dev)
			continue
		}

		if dev.Count <= dev.Used {
			reason[common.CardTimeSlicingExhausted]++
			klog.V(5).InfoS(common.CardTimeSlicingExhausted, "pod", klog.KObj(pod), "device", dev.ID, "count", dev.Count, "used", dev.Used)
			continue
		}

		klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)

		if k.Nums > 0 {
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:        int(dev.Index),
				UUID:       dev.ID,
				Type:       k.Type,
				Usedmem:    Mi300xMemory,
				Usedcores:  0,
				CustomInfo: map[string]any{},
			})
		}
		if k.Nums == 0 {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}
	}
	if len(tmpDevs) > 0 {
		reason[common.AllocatedCardsInsufficientRequest] = len(tmpDevs)
		klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs))
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}
