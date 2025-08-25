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

package metax

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	MetaxSGPUCommonWord = "Metax-SGPU"
	MetaxSGPUDevice     = "Metax-SGPU"

	MetaxNodeLock = "hami.io/mutex.lock"
)

var (
	MetaxResourceNameVCount  string
	MetaxResourceNameVCore   string
	MetaxResourceNameVMemory string
	MetaxTopologyAware       bool
)

type MetaxSDevices struct {
	jqCache *JitteryQosCache
}

func InitMetaxSDevice(config MetaxConfig) *MetaxSDevices {
	MetaxResourceNameVCount = config.ResourceVCountName
	MetaxResourceNameVCore = config.ResourceVCoreName
	MetaxResourceNameVMemory = config.ResourceVMemoryName
	MetaxTopologyAware = config.TopologyAware

	device.InRequestDevices[MetaxSGPUDevice] = "hami.io/metax-sgpu-devices-to-allocate"
	device.SupportDevices[MetaxSGPUDevice] = "hami.io/metax-sgpu-devices-allocated"

	return &MetaxSDevices{
		jqCache: NewJitteryQosCache(),
	}
}

func (sdev *MetaxSDevices) CommonWord() string {
	return MetaxSGPUCommonWord
}

func (sdev *MetaxSDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVCount)]
	if !ok {
		return false, nil
	}

	qos, ok := p.Annotations[MetaxSGPUQosPolicy]
	if !ok {
		if p.Annotations == nil {
			p.Annotations = make(map[string]string)
		}

		p.Annotations[MetaxSGPUQosPolicy] = BestEffort
		return true, nil
	}

	if qos == BestEffort ||
		qos == FixedShare ||
		qos == BurstShare {
		return true, nil
	} else {
		return true, fmt.Errorf("%s must be set one of [%s, %s, %s]",
			MetaxSGPUQosPolicy, BestEffort, FixedShare, BurstShare)
	}
}

func (sdev *MetaxSDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	metaxSDevices, err := sdev.getMetaxSDevices(n)
	if err != nil {
		return []*device.DeviceInfo{}, err
	}

	sdev.jqCache.Sync(metaxSDevices)

	return convertMetaxSDeviceToHAMIDevice(metaxSDevices), nil
}

func (sdev *MetaxSDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[MetaxSGPUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)

		// hami
		(*annoinput)[device.InRequestDevices[MetaxSGPUDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[MetaxSGPUDevice]] = deviceStr
		klog.Infof("pod add annotation key [%s, %s], values is [%s]",
			device.InRequestDevices[MetaxSGPUDevice], device.SupportDevices[MetaxSGPUDevice], deviceStr)

		// metax
		metaxPodDevice := convertHAMIPodDeviceToMetaxPodDevice(devlist)
		klog.Infof("metaxPodDevice allocated info: %s", metaxPodDevice.String())

		byte, err := json.Marshal(metaxPodDevice)
		if err != nil {
			klog.Errorf("metaxPodDevice marshal failed, origin values is [%s]", deviceStr)
		}

		(*annoinput)[MetaxAllocatedSDevices] = string(byte)
		(*annoinput)[MetaxPredicateTime] = strconv.FormatInt(time.Now().UnixNano(), 10)

		sdev.addJitteryQos(pod.Annotations[MetaxSGPUQosPolicy], devlist)
	}

	return *annoinput
}

func (sdev *MetaxSDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	found := false

	for _, val := range p.Spec.Containers {
		if (sdev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	return nodelock.LockNode(n.Name, MetaxNodeLock, p)
}

func (sdev *MetaxSDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	found := false

	for _, val := range p.Spec.Containers {
		if (sdev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	return nodelock.ReleaseNodeLock(n.Name, MetaxNodeLock, p, false)
}

func (sdev *MetaxSDevices) NodeCleanUp(nn string) error {
	return nil
}

func (sdev *MetaxSDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) bool {
	if strings.Compare(n.Type, MetaxSGPUDevice) == 0 {
		if sdev.checkDeviceQos(annos[MetaxSGPUQosPolicy], d, n) {
			return true
		} else {
			return false
		}
	}

	return false
}

func (sdev *MetaxSDevices) checkUUID(annos map[string]string, d device.DeviceUsage) bool {
	useUUIDAnno, ok := annos[MetaxUseUUID]
	if ok {
		klog.V(5).Infof("check UUID for metax, useUUID[%s], deviceID[%s]", useUUIDAnno, d.ID)

		useUUIDs := strings.Split(useUUIDAnno, ",")
		if slices.Contains(useUUIDs, d.ID) {
			klog.V(5).Infof("check UUID pass, the deviceID[%s]", d.ID)
			return true
		}
		return false
	}

	noUseUUIDAnno, ok := annos[MetaxNoUseUUID]
	if ok {
		klog.V(5).Infof("check UUID for metax, nouseUUID[%s], deviceID[%s]", noUseUUIDAnno, d.ID)

		noUseUUIDs := strings.Split(noUseUUIDAnno, ",")
		if slices.Contains(noUseUUIDs, d.ID) {
			klog.V(5).Infof("check UUID failed to pass, the deviceID[%s]", d.ID)
			return false
		}
		return true
	}

	return true
}

func (sdev *MetaxSDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	devices, _ := sdev.getMetaxSDevices(*n)

	return len(devices) > 0, true
}

func (sdev *MetaxSDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	value, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVCount)]
	if !ok {
		return device.ContainerDeviceRequest{}
	}

	count, ok := value.AsInt64()
	if !ok {
		klog.Errorf("container<%s> resource<%s> cannot decode to int64",
			ctr.Name, MetaxResourceNameVCount)
		return device.ContainerDeviceRequest{}
	}

	core := int64(100)
	coreQuantity, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVCore)]
	if ok {
		if v, ok := coreQuantity.AsInt64(); ok {
			core = v
		}
	}

	mem := int64(0)
	memQuantity, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVMemory)]
	if ok {
		if v, ok := memQuantity.AsInt64(); ok {
			hasUnit := strings.IndexFunc(memQuantity.String(), func(c rune) bool {
				return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			}) >= 0

			// if user not set unit, default unit is Gi
			if hasUnit {
				mem = v / 1024 / 1024
			} else {
				mem = v * 1024
			}
		}
	}

	memPercent := 0
	if mem == 0 {
		memPercent = 100
	}

	klog.Infof("container<%s> request<count:%d, mem:%d, memPercent:%d, core:%d>",
		ctr.Name, count, mem, memPercent, core)
	return device.ContainerDeviceRequest{
		Nums:             int32(count),
		Type:             MetaxSGPUDevice,
		Memreq:           int32(mem),
		MemPercentagereq: int32(memPercent),
		Coresreq:         int32(core),
	}
}

func (sdev *MetaxSDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	if !needScore(podDevices) {
		return 0
	}

	// TODO: score should not depend on policy
	// we have to give it a smaller value because of Spread policy
	weight := 10000
	if policy == string(util.NodeSchedulerPolicySpread) {
		weight = -10000
	}

	return float32(weight * scoreExclusiveDevices(podDevices, previous))
}

func (sdev *MetaxSDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem

	if value, ok := n.CustomInfo["QosPolicy"]; ok {
		if qos, ok := value.(string); ok {
			expectedQos := pod.Annotations[MetaxSGPUQosPolicy]
			if ctr.Usedcores == 100 {
				expectedQos = ""
			}

			n.CustomInfo["QosPolicy"] = expectedQos
			klog.Infof("device[%s] temp changed qos [%s] to [%s]", n.ID, qos, expectedQos)
		}
	}

	return nil
}

func (mats *MetaxSDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	klog.Infof("pod[%v] container request[%v] devices fit", klog.KObj(pod), request)

	reason := make(map[string]int)
	candidateDevices := device.ContainerDevices{}

	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]

		if !mats.checkType(annos, *dev, request) {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, request.Type)
			continue
		}

		if !mats.checkUUID(annos, *dev) {
			reason[common.CardUUIDMismatch]++
			klog.V(5).InfoS(common.CardUUIDMismatch, "pod", klog.KObj(pod), "device", dev.ID, "current device info is:", *dev)
			continue
		}

		if dev.Count <= dev.Used {
			reason[common.CardTimeSlicingExhausted]++
			klog.V(5).InfoS(common.CardTimeSlicingExhausted, "pod", klog.KObj(pod), "device", dev.ID, "count", dev.Count, "used", dev.Used)
			continue
		}

		memreq := int32(0)
		if request.Memreq > 0 {
			memreq = request.Memreq
		} else {
			memreq = dev.Totalmem * request.MemPercentagereq / 100
		}

		if dev.Totalmem-dev.Usedmem < memreq {
			reason[common.CardInsufficientMemory]++
			klog.V(5).InfoS(common.CardInsufficientMemory, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total memory", dev.Totalmem, "device used memory", dev.Usedmem, "request memory", memreq)
			continue
		}

		if dev.Totalcore-dev.Usedcores < request.Coresreq {
			reason[common.CardInsufficientCore]++
			klog.V(5).InfoS(common.CardInsufficientCore, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", request.Coresreq)
			continue
		}

		// Coresreq=100 indicates it want this card exclusively
		if dev.Totalcore == 100 && request.Coresreq == 100 && dev.Used > 0 {
			reason[common.ExclusiveDeviceAllocateConflict]++
			klog.V(5).InfoS(common.ExclusiveDeviceAllocateConflict, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "used", dev.Used)
			continue
		}

		// You can't allocate core=0 job to an already full GPU
		if dev.Totalcore != 0 && dev.Usedcores == dev.Totalcore && request.Coresreq == 0 {
			reason[common.CardComputeUnitsExhausted]++
			klog.V(5).InfoS(common.CardComputeUnitsExhausted, "pod", klog.KObj(pod), "device", dev.ID, "device index", i)
			continue
		}

		ctrDevice := device.ContainerDevice{
			Idx:        int(dev.Index),
			UUID:       dev.ID,
			Type:       dev.Type,
			Usedmem:    memreq,
			Usedcores:  request.Coresreq,
			CustomInfo: maps.Clone(dev.CustomInfo),
		}

		// WorkAround: add Pod annotations into ContainerDevice to pass annotations in `ScoreNode`
		if ctrDevice.CustomInfo == nil {
			ctrDevice.CustomInfo = make(map[string]any)
		}
		ctrDevice.CustomInfo["Pod.Annotations"] = pod.Annotations

		candidateDevices = append(candidateDevices, ctrDevice)
	}

	klog.V(5).Infof("pod[%v] candidate devices: %v", klog.KObj(pod), candidateDevices)

	if len(candidateDevices) < int(request.Nums) {
		if len(candidateDevices) > 0 {
			reason[common.AllocatedCardsInsufficientRequest] = len(candidateDevices)
		}

		klog.V(5).Infof("pod[%v] fit devices Insufficient: request[%d], fit[%d]", klog.KObj(pod), request.Nums, len(candidateDevices))
		return false, map[string]device.ContainerDevices{request.Type: candidateDevices}, common.GenReason(reason, len(devices))
	}

	bestDevices := candidateDevices[0:request.Nums]
	if request.Coresreq == 100 {
		bestDevices = prioritizeExclusiveDevices(candidateDevices, int(request.Nums))
	}

	klog.Infof("pod[%v] devices fit success, fit devices[%v]", klog.KObj(pod), bestDevices)
	return true, map[string]device.ContainerDevices{request.Type: bestDevices}, ""
}

func (sdev *MetaxSDevices) getMetaxSDevices(n corev1.Node) ([]*MetaxSDeviceInfo, error) {
	anno, ok := n.Annotations[MetaxSDeviceAnno]
	if !ok {
		return []*MetaxSDeviceInfo{}, errors.New("annos not found " + MetaxSDeviceAnno)
	}

	metaxSDevices := []*MetaxSDeviceInfo{}
	err := json.Unmarshal([]byte(anno), &metaxSDevices)
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal metax sdevices", "node", n.Name, "sdevice annotation", anno)
		return []*MetaxSDeviceInfo{}, err
	}

	if len(metaxSDevices) == 0 {
		klog.InfoS("no metax sgpu device found", "node", n.Name, "sdevice annotation", anno)
		return []*MetaxSDeviceInfo{}, errors.New("no sdevice found on node")
	}

	klog.V(5).Infof("node[%s] metax sdevice information: %s", n.Name, NodeMetaxSDeviceInfo(metaxSDevices).String())
	return metaxSDevices, nil
}

func (sdev *MetaxSDevices) checkDeviceQos(reqQos string, usage device.DeviceUsage, request device.ContainerDeviceRequest) bool {
	if usage.Used == 0 {
		klog.Infof("device[%s] not use, it can switch to any qos", usage.ID)
		return true
	}

	if request.Coresreq == 100 {
		klog.Infoln("request exclusive device, no need verify qos")
		return true
	}

	devQos := ""
	if qos, ok := sdev.jqCache.Get(usage.ID); ok {
		devQos = qos
	} else {
		if value, ok := usage.CustomInfo["QosPolicy"]; ok {
			if qos, ok := value.(string); ok {
				devQos = qos
			}
		}
	}

	klog.Infof("device[%s]: devQos [%s], reqQos [%s]", usage.ID, devQos, reqQos)
	if devQos == "" || reqQos == devQos {
		return true
	} else {
		return false
	}
}

func (sdev *MetaxSDevices) addJitteryQos(reqQos string, devs device.PodSingleDevice) {
	for _, ctrdev := range devs {
		for _, dev := range ctrdev {
			if value, ok := dev.CustomInfo["QosPolicy"]; ok {
				if currentQos, ok := value.(string); ok {
					expectedQos := reqQos
					if dev.Usedcores == 100 {
						expectedQos = ""
					}

					if currentQos != expectedQos {
						sdev.jqCache.Add(dev.UUID, expectedQos)
						klog.Infof("device[%s] add to cache, expectedQos[%s] not equal to currentQos[%s]",
							dev.UUID, expectedQos, currentQos)
					}
				}
			}
		}
	}
}

func prioritizeExclusiveDevices(candidateDevices device.ContainerDevices, require int) device.ContainerDevices {
	if len(candidateDevices) <= require {
		return candidateDevices
	}

	linkDevicesMap := map[int32]device.ContainerDevices{}
	for _, device := range candidateDevices {
		linkZone := int32(0)
		if v, ok := device.CustomInfo["LinkZone"]; ok {
			if v, ok := v.(int32); ok {
				linkZone = v
			}
		}

		linkDevicesMap[linkZone] = append(linkDevicesMap[linkZone], device)
	}

	linkDevices := []device.ContainerDevices{}
	otherDevices := device.ContainerDevices{}
	for link, devices := range linkDevicesMap {
		if link <= 0 {
			otherDevices = append(otherDevices, devices...)
		} else {
			linkDevices = append(linkDevices, devices)
		}
	}

	sort.Slice(linkDevices, func(i int, j int) bool {
		return len(linkDevices[i]) < len(linkDevices[j])
	})

	klog.V(5).Infof("linkDevices: %v, otherDevices: %v", linkDevices, otherDevices)

	// 1. pickup devices within MetaLink
	for _, devices := range linkDevices {
		if len(devices) >= require {
			klog.V(5).Infof("prioritize exclusive devices: best result, within metalink")
			return devices[0:require]
		}
	}

	prioritizeDevices := device.ContainerDevices{}

	// 2. pickup devices cross MetaLink
	for _, devices := range linkDevices {
		for _, device := range devices {
			prioritizeDevices = append(prioritizeDevices, device)

			if len(prioritizeDevices) >= require {
				klog.V(5).Infof("prioritize exclusive devices: general result, cross metalink")
				return prioritizeDevices
			}
		}
	}

	// 3. if not satisfied, pick up devices no MetaLink
	for _, device := range otherDevices {
		prioritizeDevices = append(prioritizeDevices, device)

		if len(prioritizeDevices) >= require {
			klog.V(5).Infof("prioritize exclusive devices: bad result, some devices no metalink")
			return prioritizeDevices
		}
	}

	return candidateDevices[0:require]
}

func needScore(podDevices device.PodSingleDevice) bool {
	enableTopoAware := MetaxTopologyAware

	for _, ctrDevices := range podDevices {
		for _, device := range ctrDevices {
			if device.Usedcores == 100 {
				if annotations, ok := device.CustomInfo["Pod.Annotations"].(map[string]string); ok {
					if value, ok := annotations[MetaxSGPUTopologyAware]; ok {
						if enable, err := strconv.ParseBool(value); err == nil {
							enableTopoAware = enable
						}
					}
				}

				return enableTopoAware
			}
		}
	}

	return false
}

func scoreExclusiveDevices(podDevices device.PodSingleDevice, previous []*device.DeviceUsage) int {
	availableDevices := LinkDevices{}
	allocatedDevices := LinkDevices{}
	restDevices := LinkDevices{}

	for _, ctrDevices := range podDevices {
		for _, device := range ctrDevices {
			if device.Usedcores == 100 {
				linkZone := int32(0)
				if v, ok := device.CustomInfo["LinkZone"]; ok {
					if v, ok := v.(int32); ok {
						linkZone = v
					}
				}

				allocatedDevices = append(allocatedDevices, &LinkDevice{
					uuid:     device.UUID,
					linkZone: linkZone,
				})
			}
		}
	}

	for _, dev := range previous {
		if dev.Used == 0 {
			linkZone := int32(0)
			if v, ok := dev.CustomInfo["LinkZone"]; ok {
				if v, ok := v.(int32); ok {
					linkZone = v
				}
			}

			availableDevices = append(availableDevices, &LinkDevice{
				uuid:     dev.ID,
				linkZone: linkZone,
			})

			find := false
			for _, allocatedDev := range allocatedDevices {
				if allocatedDev.uuid == dev.ID {
					find = true
					break
				}
			}

			if !find {
				restDevices = append(restDevices, &LinkDevice{
					uuid:     dev.ID,
					linkZone: linkZone,
				})
			}
		}
	}

	klog.V(5).Infof("calcScore devices >>> available: %s, allocated: %s, rest: %s",
		availableDevices, allocatedDevices, restDevices)

	availableScore := availableDevices.Score()
	allocatedScore := allocatedDevices.Score()
	restScore := restDevices.Score()
	lossScore := availableScore - allocatedScore - restScore

	result := 10*allocatedScore - lossScore
	klog.V(5).Infof("calcScore result[%d] >>> availableScore[%d], allocatedScore[%d], restScore[%d], lossScore[%d]",
		result, availableScore, allocatedScore, restScore, lossScore)

	return result
}
