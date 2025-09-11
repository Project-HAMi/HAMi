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

package enflame

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
)

const (
	EnflameGCUDevice     = "GCU"
	EnflameGCUCommonWord = "GCU"
)

type GCUDevices struct {
}

func InitGCUDevice(config EnflameConfig) *GCUDevices {
	EnflameResourceNameGCU = config.ResourceNameGCU
	device.InRequestDevices[EnflameGCUDevice] = "hami.io/enflame-gcu-devices-to-allocate"
	device.SupportDevices[EnflameGCUDevice] = "hami.io/enflame-gcu-devices-allocated"
	return &GCUDevices{}
}

func (dev *GCUDevices) CommonWord() string {
	return EnflameGCUCommonWord
}

func (dev *GCUDevices) MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(EnflameResourceNameGCU)]
	return ok, nil
}

func (dev *GCUDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	count, _ := n.Status.Capacity.Name(corev1.ResourceName(EnflameResourceNameGCU), resource.DecimalSI).AsInt64()
	return count > 0, true
}

func (dev *GCUDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *GCUDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	count, ok := n.Status.Capacity.Name(corev1.ResourceName(EnflameResourceNameGCU), resource.DecimalSI).AsInt64()
	if !ok || count == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", EnflameResourceNameGCU)
	}
	for int64(i) < count {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:   uint(i),
			ID:      n.Name + "-" + EnflameGCUDevice + "-" + fmt.Sprint(i),
			Count:   1,
			Devmem:  100,
			Devcore: 100,
			Type:    EnflameGCUDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *GCUDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count enflame devices for container ", ctr.Name)
	enflameResourceCount := corev1.ResourceName(EnflameResourceNameGCU)
	v, ok := ctr.Resources.Limits[enflameResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[enflameResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found enflame devices")
			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             EnflameGCUDevice,
				Memreq:           100,
				MemPercentagereq: 100,
				Coresreq:         100,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *GCUDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[EnflameGCUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.InRequestDevices[EnflameGCUDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[EnflameGCUDevice]] = deviceStr
		klog.Infof("pod add annotation key [%s, %s], values is [%s]",
			device.InRequestDevices[EnflameGCUDevice], device.SupportDevices[EnflameGCUDevice], deviceStr)
	}
	return *annoinput
}

func (dev *GCUDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *GCUDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *GCUDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *GCUDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (gcuDev *GCUDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		if !gcuDev.checkType(k) {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}

		if dev.Used > 0 {
			reason[common.ExclusiveDeviceAllocateConflict]++
			klog.V(5).InfoS(common.ExclusiveDeviceAllocateConflict, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "used", dev.Used)
			continue
		}

		if k.Nums > 0 {
			klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:       int(dev.Index),
				UUID:      dev.ID,
				Type:      k.Type,
				Usedmem:   k.Memreq,
				Usedcores: k.Coresreq,
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

func (dev *GCUDevices) checkType(n device.ContainerDeviceRequest) bool {
	return n.Type == EnflameGCUDevice
}
