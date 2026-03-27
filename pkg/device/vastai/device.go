/*
Copyright 2026 The HAMi Authors.

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
	"slices"
	"sort"
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
	klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devEncoded)
	for idx := range nodedevices {
		nodedevices[idx].DeviceVendor = VastaiCommonWord
		nodedevices[idx].Devcore = 100 // only for calscore use
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no vastai device found", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, errors.New("no gpu found on node")
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
			mempnum := 100

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
	score := float32(0)
	for _, containerDevices := range podDevices {
		if len(containerDevices) == 0 {
			continue
		}
		cntMap := make(map[string]int)
		for _, device := range containerDevices {
			if device.CustomInfo == nil {
				return 0
			}
			if strategy, ok := device.CustomInfo["DeviceStrategy"]; ok {
				if val, ok := strategy.(string); ok && val != "die" {
					return 0
				}
			}
			if AIC, ok := device.CustomInfo["AIC"]; ok {
				if id, ok := AIC.(string); ok {
					cntMap[id]++
				}
			} else {
				return 0
			}
		}
		maxCnt, totalCnt := 0, 0
		for _, cnt := range cntMap {
			if cnt > maxCnt {
				maxCnt = cnt
			}
			totalCnt += cnt
		}
		if totalCnt == 0 {
			continue
		}
		score += float32(maxCnt) / float32(totalCnt)
	}
	klog.V(4).InfoS("ScoreNode", "node", node.Name, "deviceType", dev.CommonWord(), "score", score)
	return score
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
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	dieMode := isDieMode(devices)
	for i, dev := range slices.Backward(devices) {
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, _ := va.checkType(pod.GetAnnotations(), *dev, k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, VastaiUseUUID, VastaiNoUseUUID, VastaiCommonWord) {
			reason[common.CardUUIDMismatch]++
			klog.V(5).InfoS(common.CardUUIDMismatch, "pod", klog.KObj(pod), "device", dev.ID, "current device info is:", *dev)
			continue
		}

		if dev.Count <= dev.Used {
			reason[common.CardTimeSlicingExhausted]++
			klog.V(5).InfoS(common.CardTimeSlicingExhausted, "pod", klog.KObj(pod), "device", dev.ID, "count", dev.Count, "used", dev.Used)
			continue
		}
		if k.Nums > 0 {
			klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
			if !dieMode {
				k.Nums--
			}
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:        int(dev.Index),
				UUID:       dev.ID,
				Type:       k.Type,
				Usedcores:  k.Coresreq,
				CustomInfo: dev.CustomInfo,
			})
		}
		if k.Nums == 0 && !dieMode {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}

	}

	if dieMode {
		if len(tmpDevs[k.Type]) == int(originReq) {
			klog.V(5).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		} else if len(tmpDevs[k.Type]) > int(originReq) {
			if originReq == 1 {
				tmpDevs[k.Type] = device.ContainerDevices{tmpDevs[k.Type][0]}
			} else {
				// If requesting multiple devices, select the best combination of cards.
				tmpDevs[k.Type] = va.computeBestCombination(int(originReq), tmpDevs[k.Type])
			}
			klog.V(5).InfoS("device allocate success", "pod", klog.KObj(pod), "best device combination", tmpDevs)
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

func (dev *VastaiDevices) computeBestCombination(reqNum int, containerDevices device.ContainerDevices) device.ContainerDevices {
	deviceMap := make(map[string]device.ContainerDevices)
	for _, dev := range containerDevices {
		if dev.CustomInfo != nil {
			if AIC, ok := dev.CustomInfo["AIC"]; ok {
				if id, ok := AIC.(string); ok {
					deviceMap[id] = append(deviceMap[id], dev)
				}
			}
		}
	}

	type DeviceCount struct {
		ID    string
		Count int
	}
	var sortedDevices []DeviceCount
	for id, devices := range deviceMap {
		sortedDevices = append(sortedDevices, DeviceCount{
			ID:    id,
			Count: len(devices),
		})
	}

	sort.SliceStable(sortedDevices, func(i, j int) bool {
		return sortedDevices[i].Count > sortedDevices[j].Count
	})
	result := device.ContainerDevices{}
	for _, item := range sortedDevices {
		devices := deviceMap[item.ID]
		for _, dev := range devices {
			result = append(result, dev)
			if len(result) == reqNum {
				return result
			}
		}
	}
	return result
}

func isDieMode(devices []*device.DeviceUsage) bool {
	if len(devices) == 0 {
		return false
	}
	dev := devices[0]
	if dev.CustomInfo == nil {
		return false
	}
	if strategy, ok := dev.CustomInfo["DeviceStrategy"]; ok {
		if val, ok := strategy.(string); ok && val == "die" {
			return true
		}
	}
	return false
}
