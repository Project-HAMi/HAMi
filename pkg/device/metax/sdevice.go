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
	"strconv"
	"strings"
	"time"

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
)

type MetaxSDevices struct {
	jqCache *JitteryQosCache
}

func InitMetaxSDevice(config MetaxConfig) *MetaxSDevices {
	MetaxResourceNameVCount = config.ResourceVCountName
	MetaxResourceNameVCore = config.ResourceVCoreName
	MetaxResourceNameVMemory = config.ResourceVMemoryName

	util.InRequestDevices[MetaxSGPUDevice] = "hami.io/metax-sgpu-devices-to-allocate"
	util.SupportDevices[MetaxSGPUDevice] = "hami.io/metax-sgpu-devices-allocated"

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

func (sdev *MetaxSDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	metaxSDevices, err := sdev.getMetaxSDevices(n)
	if err != nil {
		return []*util.DeviceInfo{}, err
	}

	sdev.jqCache.Sync(metaxSDevices)

	return convertMetaxSDeviceToHAMIDevice(metaxSDevices), nil
}

func (sdev *MetaxSDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[MetaxSGPUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := util.EncodePodSingleDevice(devlist)

		// hami
		(*annoinput)[util.InRequestDevices[MetaxSGPUDevice]] = deviceStr
		(*annoinput)[util.SupportDevices[MetaxSGPUDevice]] = deviceStr
		klog.Infof("pod add annotation key [%s, %s], values is [%s]",
			util.InRequestDevices[MetaxSGPUDevice], util.SupportDevices[MetaxSGPUDevice], deviceStr)

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

func (sdev *MetaxSDevices) checkType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, MetaxSGPUDevice) == 0 {
		return true, sdev.checkDeviceQos(annos[MetaxSGPUQosPolicy], d, n), false
	}

	return false, false, false
}

func (sdev *MetaxSDevices) checkUUID(annos map[string]string, d util.DeviceUsage) bool {
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

func (sdev *MetaxSDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	value, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVCount)]
	if !ok {
		return util.ContainerDeviceRequest{}
	}

	count, ok := value.AsInt64()
	if !ok {
		klog.Errorf("container<%s> resource<%s> cannot decode to int64",
			ctr.Name, MetaxResourceNameVCount)
		return util.ContainerDeviceRequest{}
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
	return util.ContainerDeviceRequest{
		Nums:             int32(count),
		Type:             MetaxSGPUDevice,
		Memreq:           int32(mem),
		MemPercentagereq: int32(memPercent),
		Coresreq:         int32(core),
	}
}

func (sdev *MetaxSDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, previous []*util.DeviceUsage, policy string) float32 {
	return 0
}

func (sdev *MetaxSDevices) AddResourceUsage(pod *corev1.Pod, n *util.DeviceUsage, ctr *util.ContainerDevice) error {
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

func (mats *MetaxSDevices) Fit(devices []*util.DeviceUsage, request util.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod, nodeInfo *util.NodeInfo, allocated *util.PodDevices) (bool, map[string]util.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]util.ContainerDevices
	tmpDevs = make(map[string]util.ContainerDevices)
	reason := make(map[string]int)
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, numa := mats.checkType(annos, *dev, k)
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
			tmpDevs = make(map[string]util.ContainerDevices)
		}
		if !mats.checkUUID(annos, *dev) {
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
			tmpDevs[k.Type] = append(tmpDevs[k.Type], util.ContainerDevice{
				Idx:        int(dev.Index),
				UUID:       dev.ID,
				Type:       k.Type,
				Usedmem:    memreq,
				Usedcores:  k.Coresreq,
				CustomInfo: maps.Clone(dev.CustomInfo),
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

func (sdev *MetaxSDevices) checkDeviceQos(reqQos string, usage util.DeviceUsage, request util.ContainerDeviceRequest) bool {
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

func (sdev *MetaxSDevices) addJitteryQos(reqQos string, devs util.PodSingleDevice) {
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
