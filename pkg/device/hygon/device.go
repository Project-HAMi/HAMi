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

package hygon

import (
	"errors"
	"flag"
	"math"
	"slices"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type HCUDevices struct {
}

const (
	HandshakeAnnos     = "hami.io/node-handshake-hcu"
	RegisterAnnos      = "hami.io/node-hcu-register"
	HygonHCUDevice     = "HCU"
	HygonHCUCommonWord = "HCU"
	HCUInUse           = "hygon.com/use-hcutype"
	HCUNoUse           = "hygon.com/nouse-hcutype"
	// HCUUseUUID annotation specifies a comma-separated list of HCU UUIDs to use.
	HCUUseUUID = "hygon.com/use-gpuuuid"
	// HCUNoUseUUID annotation specifies a comma-separated list of HCU UUIDs to exclude.
	HCUNoUseUUID = "hygon.com/nouse-gpuuuid"

	// NodeLockHCU should same with device plugin node lock name
	// there is a bug with nodelock package utils, the key is hard coded as "hami.io/mutex.lock"
	// so we can only use this value now.
	NodeLockHCU = "hami.io/mutex.lock"
)

var (
	HygonResourceCount  string
	HygonResourceMemory string
	HygonResourceCores  string
	MemoryFactor        int32
)

type HygonConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
	MemoryFactor       int32  `yaml:"memoryFactor"`
}

func InitHCUDevice(config HygonConfig) *HCUDevices {
	HygonResourceCount = config.ResourceCountName
	HygonResourceMemory = config.ResourceMemoryName
	HygonResourceCores = config.ResourceCoreName
	MemoryFactor = config.MemoryFactor
	_, ok := device.InRequestDevices[HygonHCUDevice]
	if !ok {
		device.InRequestDevices[HygonHCUDevice] = "hami.io/hcu-devices-to-allocate"
		device.SupportDevices[HygonHCUDevice] = "hami.io/hcu-devices-allocated"
		util.HandshakeAnnos[HygonHCUDevice] = HandshakeAnnos
	}
	return &HCUDevices{}
}

func (dev *HCUDevices) CommonWord() string {
	return HygonHCUCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&HygonResourceCount, "hcu-name", "hygon.com/hcunum", "hcu resource count")
	fs.StringVar(&HygonResourceMemory, "hcu-memory", "hygon.com/hcumem", "hcu memory resource")
	fs.StringVar(&HygonResourceCores, "hcu-cores", "hygon.com/hcucores", "hcu core resource")
}

func (dev *HCUDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(HygonResourceCount)]
	return ok, nil
}

func checkHCUtype(annos map[string]string, cardtype string) bool {
	return device.CheckType(annos, cardtype, HCUInUse, HCUNoUse)
}

func (dev *HCUDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	if !device.PodRequiresDevice(dev, p) {
		return nil
	}
	return nodelock.LockNode(n.Name, NodeLockHCU, p)
}

func (dev *HCUDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	if !device.PodRequiresDevice(dev, p) {
		return nil
	}
	return nodelock.ReleaseNodeLock(n.Name, NodeLockHCU, p, false)
}

func (dev *HCUDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	devEncoded, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*device.DeviceInfo{}, errors.New("annos not found " + RegisterAnnos)
	}
	nodedevices, err := device.DecodeNodeDevices(devEncoded)
	if err != nil {
		klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, err
	}
	for idx := range nodedevices {
		nodedevices[idx].DeviceVendor = HygonHCUCommonWord
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, errors.New("no gpu found on node")
	}
	devDecoded := device.EncodeNodeDevices(nodedevices)
	klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devDecoded)
	return nodedevices, nil
}

func (dev *HCUDevices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(HandshakeAnnos, nn)
}

func (dev *HCUDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return device.CheckHealth(devType, dev.GetResourceNames().ResourceCountName, n)
}

func (dev *HCUDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, HygonHCUDevice) == 0 {
		return true, checkHCUtype(annos, d.Type), false
	}
	return false, false, false
}

func (dev *HCUDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count hcu devices for container ", ctr.Name)
	hcuResourceCount := corev1.ResourceName(HygonResourceCount)
	hcuResourceMem := corev1.ResourceName(HygonResourceMemory)
	hcuResourceCores := corev1.ResourceName(HygonResourceCores)
	v, ok := ctr.Resources.Limits[hcuResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[hcuResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			if n <= 0 || n > math.MaxInt32 {
				klog.ErrorS(nil, "hcu device count request is out of range", "container", ctr.Name, "request", n)
				return device.ContainerDeviceRequest{}
			}
			klog.Info("Found hcu devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[hcuResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[hcuResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					if memnums < 0 || memnums > math.MaxInt32 {
						klog.ErrorS(nil, "hcu device memory request is out of range", "container", ctr.Name, "request", mem.String())
						return device.ContainerDeviceRequest{}
					}
					if MemoryFactor > 1 {
						rawMemnums := memnums
						memnums = memnums * int64(MemoryFactor)
						if memnums > math.MaxInt32 {
							klog.ErrorS(nil, "hcu device memory request overflows int32 after applying memory factor", "container", ctr.Name, "raw", rawMemnums, "scaled", memnums, "factor", MemoryFactor)
							return device.ContainerDeviceRequest{}
						}
						klog.V(4).Infof("Update memory request. before %d, after %d, factor %d", rawMemnums, memnums, MemoryFactor)
					}
					memnum = int(memnums)
				}
			}
			corenum := int32(100)
			core, ok := ctr.Resources.Limits[hcuResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[hcuResourceCores]
			}
			if ok {
				corenums, valid := core.AsInt64()
				if !valid || corenums < 0 || corenums > 100 {
					klog.ErrorS(nil, "hcu device core request is out of range", "container", ctr.Name, "request", core.String())
					return device.ContainerDeviceRequest{}
				}
				corenum = int32(corenums)
			}

			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             HygonHCUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *HCUDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[HygonHCUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.InRequestDevices[HygonHCUDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[HygonHCUDevice]] = deviceStr
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.InRequestDevices[HygonHCUDevice], deviceStr)
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.SupportDevices[HygonHCUDevice], deviceStr)
	}
	return *annoinput
}

func (hcu *HCUDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (hcu *HCUDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (hcu *HCUDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]device.ContainerDevices
	tmpDevs = make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	if k.Coresreq > 100 || k.Coresreq < 0 {
		klog.ErrorS(nil, "core limit out of range (must be 0-100)", "pod", klog.KObj(pod), "coresreq", k.Coresreq)
		return false, tmpDevs, "core limit out of range"
	}
	isMutex := util.PolicyContains(util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, pod), util.GPUSchedulerPolicyMutex)
	for i, v := range slices.Backward(devices) {
		dev := v
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)
		if !dev.Health {
			reason[common.CardNotHealth]++
			klog.V(5).InfoS(common.CardNotHealth, "pod", klog.KObj(pod), "device", dev.ID, "health", dev.Health)
			continue
		}
		_, found, numa := hcu.checkType(pod.GetAnnotations(), *dev, k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if numa && prevnuma != dev.Numa {
			if k.Nums != originReq {
				reason[common.NumaNotFit] += len(tmpDevs[k.Type])
				klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", dev.ID, "k.nums", k.Nums, "numa", numa, "prevnuma", prevnuma, "device numa", dev.Numa)
			}
			k.Nums = originReq
			prevnuma = dev.Numa
			tmpDevs = make(map[string]device.ContainerDevices)
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, HCUUseUUID, HCUNoUseUUID, hcu.CommonWord()) {
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
		if isMutex && dev.Used > 0 {
			reason[common.ExclusiveDeviceAllocateConflict]++
			klog.V(5).InfoS(common.ExclusiveDeviceAllocateConflict, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "used", dev.Used)
			continue
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
	if len(tmpDevs[k.Type]) > 0 {
		reason[common.AllocatedCardsInsufficientRequest] = len(tmpDevs[k.Type])
		klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs[k.Type]))
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}

func (dev *HCUDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  HygonResourceCount,
		ResourceMemoryName: HygonResourceMemory,
		ResourceCoreName:   HygonResourceCores,
		MemoryFactor:       MemoryFactor,
	}
}
