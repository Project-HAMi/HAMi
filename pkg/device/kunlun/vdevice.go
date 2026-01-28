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

package kunlun

import (
	"errors"
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	XPUDevice      = "XPU"
	XPUCommonWord  = "XPU"
	NodeLock       = "hami.io/mutex.lock"
	RegisterAnnos  = "hami.io/node-register-xpu"
	HandshakeAnnos = "hami.io/node-handshake-xpu"
	UseUUIDAnno    = "hami.io/use-xpu-uuid"
	NoUseUUIDAnno  = "hami.io/no-use-xpu-uuid"
)

const (
	KunlunMaxMemory = 98304
)

type KunlunVDevices struct {
}

func InitKunlunVDevice(config KunlunConfig) *KunlunVDevices {
	KunlunResourceVCount = config.ResourceVCountName
	KunlunResourceVMemory = config.ResourceVMemoryName
	_, ok := device.InRequestDevices[XPUDevice]
	if !ok {
		device.InRequestDevices[XPUDevice] = "hami.io/xpu-devices-to-allocate"
		device.SupportDevices[XPUDevice] = "hami.io/xpu-devices-allocated"
		util.HandshakeAnnos[XPUDevice] = HandshakeAnnos
	}
	return &KunlunVDevices{}
}

func (dev *KunlunVDevices) trimMemory(m int64) int64 {
	temps := []int64{24576, 49152}
	for _, temp := range temps {
		if m <= temp {
			return temp
		}
	}
	return KunlunMaxMemory
}

func (dev *KunlunVDevices) CommonWord() string {
	return XPUDevice
}

func (dev *KunlunVDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(KunlunResourceVCount)]
	if !ok {
		return false, nil
	}
	memory, ok := ctr.Resources.Limits[corev1.ResourceName(KunlunResourceVMemory)]
	if ok {
		trimMem := dev.trimMemory(memory.Value())
		ctr.Resources.Limits[corev1.ResourceName(KunlunResourceVMemory)] = resource.MustParse(fmt.Sprint(trimMem))
		ctr.Resources.Requests[corev1.ResourceName(KunlunResourceVMemory)] = resource.MustParse(fmt.Sprint(trimMem))
		return true, nil
	}
	return true, nil
}

func (dev *KunlunVDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return device.CheckHealth(devType, n)
}

func (dev *KunlunVDevices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(HandshakeAnnos, nn)
}

func (dev *KunlunVDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	anno, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*device.DeviceInfo{}, fmt.Errorf("annos not found %s", RegisterAnnos)
	}
	nodeDevices, err := device.UnMarshalNodeDevices(anno)
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal node devices", "node", n.Name, "device annotation", anno)
		return []*device.DeviceInfo{}, err
	}
	for idx := range nodeDevices {
		nodeDevices[idx].DeviceVendor = dev.CommonWord()
	}
	if len(nodeDevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", anno)
		return []*device.DeviceInfo{}, errors.New("no device found on node")
	}
	return nodeDevices, nil
}

func (dev *KunlunVDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	commonWord := dev.CommonWord()
	devList, ok := pd[commonWord]
	if ok && len(devList) > 0 {
		deviceStr := device.EncodePodSingleDevice(devList)
		(*annoinput)[device.InRequestDevices[commonWord]] = deviceStr
		(*annoinput)[device.SupportDevices[commonWord]] = deviceStr
		klog.V(4).Infof("pod add notation key [%s], values is [%s]", device.InRequestDevices[commonWord], deviceStr)
		klog.V(4).Infof("pod add notation key [%s], values is [%s]", device.SupportDevices[commonWord], deviceStr)
	}
	return *annoinput
}

func (dev *KunlunVDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	found := false
	for _, val := range p.Spec.Containers {
		if (dev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	return nodelock.LockNode(n.Name, NodeLock, p)
}

func (dev *KunlunVDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	found := false
	for _, val := range p.Spec.Containers {
		if (dev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	return nodelock.ReleaseNodeLock(n.Name, NodeLock, p, false)
}

func (dev *KunlunVDevices) CheckType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool) {
	if n.Type == dev.CommonWord() {
		return true, false
	}
	return false, false
}

func (dev *KunlunVDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	xpuResourceCount := corev1.ResourceName(KunlunResourceVCount)
	xpuResourceMem := corev1.ResourceName(KunlunResourceVMemory)
	v, ok := ctr.Resources.Limits[xpuResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[xpuResourceCount]
	}
	if ok {
		klog.V(3).Infof("Counting %s devices", dev.CommonWord())
		if n, ok := v.AsInt64(); ok {
			memnum := 0
			mem, ok := ctr.Resources.Limits[xpuResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[xpuResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					m := dev.trimMemory(memnums)
					memnum = int(m)
				}
			}
			cores := memnum * 100 / KunlunMaxMemory
			mempnum := 0
			if memnum == 0 {
				mempnum = 100
				cores = 100
				memnum = KunlunMaxMemory
			}
			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             dev.CommonWord(),
				Memreq:           int32(memnum), //int32(dev.config.MemoryMax),
				MemPercentagereq: int32(mempnum),
				Coresreq:         int32(cores),
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *KunlunVDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *KunlunVDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (dev *KunlunVDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  KunlunResourceVCount,
		ResourceMemoryName: KunlunResourceVMemory,
		ResourceCoreName:   "",
	}
}

func (dev *KunlunVDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	klog.V(4).InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", request)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)

	alloc := graghSelect(devices, request, FitVXPU)
	if len(alloc) == 0 {
		reason[common.NumaNotFit]++
		klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", devices, "request nums", request.Nums, "numa")
		return false, tmpDevs, common.GenReason(reason, len(reason))
	}
	for _, dev := range alloc {
		for _, val := range devices {
			if val.Index == uint(dev) {
				tmpDevs[request.Type] = append(tmpDevs[request.Type], device.ContainerDevice{
					Idx:       int(val.Index),
					UUID:      val.ID,
					Type:      request.Type,
					Usedmem:   request.Memreq,
					Usedcores: request.Coresreq,
				})
				break
			}
		}
	}
	return true, tmpDevs, ""
}

func FitVXPU(device *device.DeviceUsage, request device.ContainerDeviceRequest) bool {
	if request.Memreq+device.Usedmem > device.Totalmem {
		return false
	}
	if device.Used == 0 {
		return true
	}
	avgMem := device.Usedmem / device.Used
	return avgMem == request.Memreq
}
