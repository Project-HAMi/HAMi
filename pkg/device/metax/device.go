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

package metax

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type MetaxDevices struct {
}

const (
	MetaxGPUDevice       = "Metax-GPU"
	MetaxGPUCommonWord   = "Metax-GPU"
	MetaxAnnotationLoss  = "metax-tech.com/gpu.topology.losses"
	MetaxAnnotationScore = "metax-tech.com/gpu.topology.scores"
)

var (
	MetaxResourceCount string
)

func InitMetaxDevice(config MetaxConfig) *MetaxDevices {
	MetaxResourceCount = config.ResourceCountName
	device.InRequestDevices[MetaxGPUDevice] = "hami.io/metax-gpu-devices-to-allocate"
	device.SupportDevices[MetaxGPUDevice] = "hami.io/metax-gpu-devices-allocated"
	return &MetaxDevices{}
}

func (dev *MetaxDevices) CommonWord() string {
	return MetaxGPUCommonWord
}

func (dev *MetaxDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceCount)]
	return ok, nil
}

func (dev *MetaxDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	count, ok := n.Status.Capacity.Name(corev1.ResourceName(MetaxResourceCount), resource.DecimalSI).AsInt64()
	if !ok || count == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", MetaxResourceCount)
	}
	for int64(i) < count {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:   uint(i),
			ID:      n.Name + "-metax-" + fmt.Sprint(i),
			Count:   1,
			Devmem:  65536,
			Devcore: 100,
			Type:    MetaxGPUDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *MetaxDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[MetaxGPUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)

		(*annoinput)[device.InRequestDevices[MetaxGPUDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[MetaxGPUDevice]] = deviceStr
		klog.Infof("pod add annotation key [%s, %s], values is [%s]",
			device.InRequestDevices[MetaxGPUDevice], device.SupportDevices[MetaxGPUDevice], deviceStr)
	}

	return *annoinput
}

func (dev *MetaxDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MetaxDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MetaxDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *MetaxDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, MetaxGPUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *MetaxDevices) checkUUID(annos map[string]string, d device.DeviceUsage) bool {
	return true
}

func (dev *MetaxDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	count, _ := n.Status.Capacity.Name(corev1.ResourceName(MetaxResourceCount), resource.DecimalSI).AsInt64()

	return count > 0, true
}

func (dev *MetaxDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count metax devices for container ", ctr.Name)
	metaxResourceCount := corev1.ResourceName(MetaxResourceCount)
	v, ok := ctr.Resources.Limits[metaxResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[metaxResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found metax devices")
			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             MetaxGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         100,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *MetaxDevices) customFilterRule(allocated *device.PodDevices, request device.ContainerDeviceRequest, toAllocate device.ContainerDevices, device *device.DeviceUsage) bool {
	for _, ctrs := range (*allocated)[device.Type] {
		for _, ctrdev := range ctrs {
			if strings.Compare(ctrdev.UUID, device.ID) != 0 {
				klog.InfoS("Metax needs all devices on a device", "used", ctrdev.UUID, "allocating", device.ID)
				return false
			}
		}
	}
	return true
}

func parseMetaxAnnos(annos string, index int) float32 {
	scoreMap := map[int]int{}
	err := json.Unmarshal([]byte(annos), &scoreMap)
	if err != nil {
		klog.Warningf("annos[%s] Unmarshal failed, %v", annos, err)
		return 0
	}

	res, ok := scoreMap[index]
	if !ok {
		klog.Warningf("scoreMap[%v] not contains [%d]", scoreMap, index)
		return 0
	}

	return float32(res)
}

func (dev *MetaxDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	sum := 0
	for _, dev := range podDevices {
		sum += len(dev)
	}

	res := float32(0)
	if policy == string(util.NodeSchedulerPolicyBinpack) {
		lossAnno, ok := node.Annotations[MetaxAnnotationLoss]
		if ok {
			// it's preferred to select the node with lower loss
			loss := parseMetaxAnnos(lossAnno, sum)
			res = 2000 - loss

			klog.InfoS("Detected annotations", "policy", policy, "key", MetaxAnnotationLoss, "value", lossAnno, "requesting", sum, "extract", loss)
		}
	} else if policy == string(util.NodeSchedulerPolicySpread) {
		scoreAnno, ok := node.Annotations[MetaxAnnotationScore]
		if ok {
			// it's preferred to select the node with higher score
			// But we have to give it a smaller value because of Spread policy
			score := parseMetaxAnnos(scoreAnno, sum)
			res = 2000 - score

			klog.InfoS("Detected annotations", "policy", policy, "key", MetaxAnnotationScore, "value", scoreAnno, "requesting", sum, "extract", score)
		}
	}

	return res
}

func (dev *MetaxDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (mat *MetaxDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]device.ContainerDevices
	tmpDevs = make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, numa := mat.checkType(pod.GetAnnotations(), *dev, k)
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
		if !mat.checkUUID(pod.GetAnnotations(), *dev) {
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

		if !mat.customFilterRule(allocated, request, tmpDevs[k.Type], dev) {
			reason[common.CardNotFoundCustomFilterRule]++
			klog.V(5).InfoS(common.CardNotFoundCustomFilterRule, "pod", klog.KObj(pod), "device", dev.ID, "device index", i)
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
