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

package vastai

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type VastaiDevices struct {
}

const (
	HandshakeAnnos   = "hami.io/node-handshake-va"
	RegisterAnnos    = "hami.io/node-va-register"
	VastaiDevice     = "Vastai"
	VastaiCommonWord = "Vastai"
	VastaiInUse      = "vastaitech.com/use-va"
	VastaiNoUse      = "vastaitech.com/nouse-va"
	VastaiUseUUID    = "vastaitech.com/use-gpuuuid"
	VastaiNoUseUUID  = "vastaitech.com/nouse-gpuuuid"
)

var (
	VastaiResourceCount string
)

type VastaiConfig struct {
	ResourceCountName string `yaml:"resourceCountName"`
}

func InitVastaiDevice(config VastaiConfig) *VastaiDevices {
	VastaiResourceCount = config.ResourceCountName
	commonWord := VastaiCommonWord
	_, ok := device.InRequestDevices[commonWord]
	if !ok {
		device.InRequestDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-to-allocate", commonWord)
		device.SupportDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-allocated", commonWord)
		util.HandshakeAnnos[commonWord] = HandshakeAnnos
	}
	return &VastaiDevices{}
}

func (dev *VastaiDevices) CommonWord() string {
	return VastaiCommonWord
}

func (dev *VastaiDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	devEncoded, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*device.DeviceInfo{}, errors.New("annos not found " + RegisterAnnos)
	}
	nodedevices, err := device.UnMarshalNodeDevices(devEncoded)
	if err != nil {
		klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, err
	}
	for idx := range nodedevices {
		nodedevices[idx].DeviceVendor = VastaiCommonWord
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, errors.New("no gpu found on node")
	}
	if klog.V(5).Enabled() {
		devDecoded := device.EncodeNodeDevices(nodedevices)
		klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devDecoded)
	}
	return nodedevices, nil
}

func (dev *VastaiDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(VastaiResourceCount)]
	return ok, nil
}

func (dev *VastaiDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.LockNode(n.Name, nodelock.NodeLockKey, p)
}

func (dev *VastaiDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.ReleaseNodeLock(n.Name, nodelock.NodeLockKey, p, false)
}

func (dev *VastaiDevices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(HandshakeAnnos, nn)
}

func (dev *VastaiDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, VastaiDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *VastaiDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return device.CheckHealth(devType, n)
}

func (dev *VastaiDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.V(5).Info("Start to count vastai devices for container ", ctr.Name)
	vastaiResourceCount := corev1.ResourceName(VastaiResourceCount)
	v, ok := ctr.Resources.Limits[vastaiResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[vastaiResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found vastai devices")
			memnum := 0
			corenum := int32(0)
			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             VastaiDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *VastaiDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[VastaiDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.InRequestDevices[VastaiDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[VastaiDevice]] = deviceStr
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.InRequestDevices[VastaiDevice], deviceStr)
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.SupportDevices[VastaiDevice], deviceStr)
	}
	return *annoinput
}

func (dev *VastaiDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *VastaiDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (va *VastaiDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]device.ContainerDevices
	tmpDevs = make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	for i := 0; i < len(devices); i++ {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, numa := va.checkType(pod.GetAnnotations(), *dev, k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if numa && prevnuma != dev.Numa {
			if k.Nums != originReq {
				reason[common.NumaNotFit] += len(tmpDevs)
				klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", dev.ID, "k.nums", k.Nums, "numa", numa, "prevnuma", prevnuma, "device numa", dev.Numa)
			}
			k.Nums = originReq
			prevnuma = dev.Numa
			tmpDevs = make(map[string]device.ContainerDevices)
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, VastaiUseUUID, VastaiNoUseUUID, VastaiCommonWord) {
			reason[common.CardUUIDMismatch]++
			klog.V(5).InfoS(common.CardUUIDMismatch, "pod", klog.KObj(pod), "device", dev.ID, "current device info is:", *dev)
			continue
		}

		memreq := int32(0)
		if dev.Count <= dev.Used {
			reason[common.CardTimeSlicingExhausted]++
			klog.V(5).InfoS(common.CardTimeSlicingExhausted, "pod", klog.KObj(pod), "device", dev.ID, "count", dev.Count, "used", dev.Used)
			continue
		}
		if k.Coresreq > 100 {
			klog.ErrorS(nil, "core limit can't exceed 100", "pod", klog.KObj(pod), "device", dev.ID)
			k.Coresreq = 100
			//return false, tmpDevs
		}
		if k.Memreq > 0 {
			memreq = k.Memreq
		}
		if k.MemPercentagereq != 101 && k.Memreq == 0 {
			//This incurs an issue
			memreq = dev.Totalmem * k.MemPercentagereq / 100
		}
		if dev.Totalmem-dev.Usedmem < memreq {
			reason[common.CardInsufficientMemory]++
			klog.V(5).InfoS(common.CardInsufficientMemory, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total memory", dev.Totalmem, "device used memory", dev.Usedmem, "request memory", memreq)
			continue
		}
		if dev.Totalcore-dev.Usedcores < k.Coresreq {
			reason[common.CardInsufficientCore]++
			klog.V(5).InfoS(common.CardInsufficientCore, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", k.Coresreq)
			continue
		}
		// Coresreq=100 indicates it want this card exclusively
		if dev.Totalcore == 100 && k.Coresreq == 100 && dev.Used > 0 {
			reason[common.ExclusiveDeviceAllocateConflict]++
			klog.V(5).InfoS(common.ExclusiveDeviceAllocateConflict, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "used", dev.Used)
			continue
		}
		// You can't allocate core=0 job to an already full GPU
		if dev.Totalcore != 0 && dev.Usedcores == dev.Totalcore && k.Coresreq == 0 {
			reason[common.CardComputeUnitsExhausted]++
			klog.V(5).InfoS(common.CardComputeUnitsExhausted, "pod", klog.KObj(pod), "device", dev.ID, "device index", i)
			continue
		}
		if k.Nums > 0 {
			klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:       int(dev.Index),
				UUID:      dev.ID,
				Type:      k.Type,
				Usedmem:   memreq,
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

func (dev *VastaiDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName: VastaiResourceCount,
	}
}
