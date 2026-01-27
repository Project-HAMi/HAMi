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

package mthreads

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type MthreadsDevices struct {
}

const (
	MthreadsGPUDevice       = "Mthreads"
	MthreadsGPUCommonWord   = "Mthreads"
	MthreadsDeviceSelection = "mthreads.com/gpu-index"
	// MthreadsUseUUID annotation specifies a comma-separated list of Mthreads UUIDs to use.
	MthreadsUseUUID = "mthreads.ai/use-gpuuuid"
	// MthreadsNoUseUUID annotation specifies a comma-separated list of Mthreads UUIDs to exclude.
	MthreadsNoUseUUID        = "mthreads.ai/nouse-gpuuuid"
	MthreadsAssignedGPUIndex = "mthreads.com/gpu-index"
	MthreadsAssignedNode     = "mthreads.com/predicate-node"
	MthreadsPredicateTime    = "mthreads.com/predicate-time"
	coresPerMthreadsGPU      = 16
	memoryPerMthreadsGPU     = 96
)

var (
	MthreadsResourceCount  string
	MthreadsResourceMemory string
	MthreadsResourceCores  string
	legalMemoryslices      = []int64{2, 4, 8, 16, 32, 64, 96}
)

type MthreadsConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
}

func InitMthreadsDevice(config MthreadsConfig) *MthreadsDevices {
	MthreadsResourceCount = config.ResourceCountName
	MthreadsResourceCores = config.ResourceCoreName
	MthreadsResourceMemory = config.ResourceMemoryName
	_, ok := device.InRequestDevices[MthreadsGPUDevice]
	if !ok {
		device.InRequestDevices[MthreadsGPUDevice] = "hami.io/mthreads-vgpu-devices-to-allocate"
		device.SupportDevices[MthreadsGPUDevice] = "hami.io/mthreads-vgpu-devices-allocated"
	}
	return &MthreadsDevices{}
}

func (dev *MthreadsDevices) CommonWord() string {
	return MthreadsGPUCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&MthreadsResourceCount, "mthreads-name", "mthreads.com/vgpu", "mthreads resource count")
	fs.StringVar(&MthreadsResourceMemory, "mthreads-memory", "mthreads.com/sgpu-memory", "mthreads memory resource")
	fs.StringVar(&MthreadsResourceCores, "mthreads-cores", "mthreads.com/sgpu-core", "mthreads core resource")
}

func (dev *MthreadsDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceCount)]
	if ok {
		if count.Value() > 1 {
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceCores)] = *resource.NewQuantity(count.Value()*int64(coresPerMthreadsGPU), resource.DecimalSI)
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceMemory)] = *resource.NewQuantity(count.Value()*int64(memoryPerMthreadsGPU), resource.DecimalSI)
			p.Annotations["mthreads.com/request-gpu-num"] = fmt.Sprint(count.Value())
			return ok, nil
		}
		mem, memok := ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceMemory)]
		if !memok {
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceCores)] = *resource.NewQuantity(count.Value()*int64(coresPerMthreadsGPU), resource.DecimalSI)
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceMemory)] = *resource.NewQuantity(count.Value()*int64(memoryPerMthreadsGPU), resource.DecimalSI)
		} else {
			memnum, _ := mem.AsInt64()
			found := slices.Contains(legalMemoryslices, memnum)
			if !found {
				return true, errors.New("sGPU memory request value is invalid, valid values are [1, 2, 4, 8, 16, 32, 64, 96]")
			}
		}
	}
	return ok, nil
}

func (dev *MthreadsDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	cores, ok := n.Status.Capacity.Name(corev1.ResourceName(MthreadsResourceCores), resource.DecimalSI).AsInt64()
	if !ok || cores == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", MthreadsResourceCores)
	}
	memoryTotal, _ := n.Status.Capacity.Name(corev1.ResourceName(MthreadsResourceMemory), resource.DecimalSI).AsInt64()
	for int64(i)*coresPerMthreadsGPU < cores {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:        uint(i),
			ID:           n.Name + "-mthreads-" + fmt.Sprint(i),
			Count:        100,
			Devmem:       int32(memoryTotal * 512 * coresPerMthreadsGPU / cores),
			Devcore:      coresPerMthreadsGPU,
			Type:         MthreadsGPUDevice,
			Numa:         0,
			Health:       true,
			DeviceVendor: MthreadsGPUCommonWord,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *MthreadsDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[MthreadsGPUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[device.SupportDevices[MthreadsGPUDevice]] = device.EncodePodSingleDevice(devlist)
		for _, dp := range devlist {
			if len(dp) > 0 {
				value := ""
				for _, val := range dp {
					value = value + fmt.Sprint(val.Idx) + ","
				}
				if len(value) > 0 {
					(*annoinput)[MthreadsAssignedGPUIndex] = strings.TrimRight(value, ",")
					//(*annoinput)[MthreadsAssignedNode]=
					tmp := strconv.FormatInt(time.Now().UnixNano(), 10)
					(*annoinput)[MthreadsPredicateTime] = tmp
					(*annoinput)[MthreadsAssignedNode] = (*annoinput)[util.AssignedNodeAnnotations]
				}
			}
		}
	}
	klog.Infoln("annoinput", (*annoinput))
	return *annoinput
}

func (dev *MthreadsDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MthreadsDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MthreadsDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *MthreadsDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, MthreadsGPUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *MthreadsDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *MthreadsDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count mthreads devices for container ", ctr.Name)
	mthreadsResourceCount := corev1.ResourceName(MthreadsResourceCount)
	mthreadsResourceMem := corev1.ResourceName(MthreadsResourceMemory)
	mthreadsResourceCores := corev1.ResourceName(MthreadsResourceCores)
	v, ok := ctr.Resources.Limits[mthreadsResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[mthreadsResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.InfoS("Detected mthreads device request",
				"container", ctr.Name,
				"deviceCount", n)
			memnum := 0
			mem, ok := ctr.Resources.Limits[mthreadsResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[mthreadsResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums) * 512
					klog.InfoS("Memory allocation calculated",
						"container", ctr.Name,
						"requestedMem", memnums,
						"allocatedMem", memnum)
				}
			}
			corenum := int32(0)
			core, ok := ctr.Resources.Limits[mthreadsResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[mthreadsResourceCores]
			}
			if ok {
				corenums, ok := core.AsInt64()
				if ok {
					corenum = int32(corenums)
				}
			}

			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             MthreadsGPUDevice,
				Memreq:           int32(memnum) / int32(n),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum / int32(n),
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *MthreadsDevices) customFilterRule(allocated *device.PodDevices, request device.ContainerDeviceRequest, toAllocate device.ContainerDevices, device *device.DeviceUsage) bool {
	for _, ctrs := range (*allocated)[device.Type] {
		for _, ctrdev := range ctrs {
			if strings.Compare(ctrdev.UUID, device.ID) != 0 {
				klog.InfoS("Mthreads needs all devices on a device", "used", ctrdev.UUID, "allocating", device.ID)
				return false
			}
		}
	}
	return true
}

func (dev *MthreadsDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *MthreadsDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (mth *MthreadsDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
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

		klog.V(3).InfoS("Type check", "device", dev.Type, "req", k.Type)
		if !strings.Contains(dev.Type, k.Type) {
			reason[common.CardTypeMismatch]++
			continue
		}

		_, found, numa := mth.checkType(pod.GetAnnotations(), *dev, k)
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
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, MthreadsUseUUID, MthreadsNoUseUUID, mth.CommonWord()) {
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

		if !mth.customFilterRule(allocated, request, tmpDevs[k.Type], dev) {
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

func (dev *MthreadsDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  MthreadsResourceCount,
		ResourceMemoryName: MthreadsResourceMemory,
		ResourceCoreName:   MthreadsResourceCores,
	}
}
