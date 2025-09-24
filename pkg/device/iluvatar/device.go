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

package iluvatar

import (
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type IluvatarDevices struct {
}

const (
	IluvatarGPUDevice       = "Iluvatar"
	IluvatarGPUCommonWord   = "Iluvatar"
	IluvatarDeviceSelection = "iluvatar.ai/predicate-gpu-idx-"
	// IluvatarUseUUID is user can use specify Iluvatar device for set Iluvatar UUID.
	IluvatarUseUUID = "iluvatar.ai/use-gpuuuid"
	// IluvatarNoUseUUID is user can not use specify Iluvatar device for set Iluvatar UUID.
	IluvatarNoUseUUID = "iluvatar.ai/nouse-gpuuuid"
)

var (
	IluvatarResourceCount  string
	IluvatarResourceMemory string
	IluvatarResourceCores  string
)

type IluvatarConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
}

func InitIluvatarDevice(config IluvatarConfig) *IluvatarDevices {
	IluvatarResourceCount = config.ResourceCountName
	IluvatarResourceMemory = config.ResourceMemoryName
	IluvatarResourceCores = config.ResourceCoreName
	device.InRequestDevices[IluvatarGPUDevice] = "hami.io/iluvatar-vgpu-devices-to-allocate"
	device.SupportDevices[IluvatarGPUDevice] = "hami.io/iluvatar-vgpu-devices-allocated"
	return &IluvatarDevices{}
}

func (dev *IluvatarDevices) CommonWord() string {
	return IluvatarGPUCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&IluvatarResourceCount, "iluvatar-name", "iluvatar.ai/vgpu", "iluvatar resource count")
	fs.StringVar(&IluvatarResourceMemory, "iluvatar-memory", "iluvatar.ai/vcuda-memory", "iluvatar memory resource")
	fs.StringVar(&IluvatarResourceCores, "iluvatar-cores", "iluvatar.ai/vcuda-core", "iluvatar core resource")
}

func (dev *IluvatarDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(IluvatarResourceCount)]
	if ok {
		if count.Value() > 1 {
			ctr.Resources.Limits[corev1.ResourceName(IluvatarResourceCores)] = *resource.NewQuantity(count.Value()*int64(100), resource.DecimalSI)
		}
	}
	return ok, nil
}

func (dev *IluvatarDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	cards, ok := n.Status.Capacity.Name(corev1.ResourceName(IluvatarResourceCores), resource.DecimalSI).AsInt64()
	if !ok || cards == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", IluvatarResourceCores)
	}
	memoryTotal, _ := n.Status.Capacity.Name(corev1.ResourceName(IluvatarResourceMemory), resource.DecimalSI).AsInt64()
	for int64(i)*100 < cards {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:   uint(i),
			ID:      n.Name + "-iluvatar-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  int32(memoryTotal * 256 * 100 / cards),
			Devcore: 100,
			Type:    IluvatarGPUDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *IluvatarDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[IluvatarGPUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[device.InRequestDevices[IluvatarGPUDevice]] = device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.SupportDevices[IluvatarGPUDevice]] = device.EncodePodSingleDevice(devlist)
		(*annoinput)["iluvatar.ai/gpu-assigned"] = "false"
		(*annoinput)["iluvatar.ai/predicate-time"] = strconv.FormatInt(time.Now().UnixNano(), 10)
		for idx, dp := range devlist {
			annoKey := IluvatarDeviceSelection + fmt.Sprint(idx)
			value := ""
			for _, val := range dp {
				value = value + fmt.Sprint(val.Idx) + ","
			}
			if len(value) > 0 {
				(*annoinput)[annoKey] = strings.TrimRight(value, ",")
			}
		}
	}
	return *annoinput
}

func (dev *IluvatarDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *IluvatarDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *IluvatarDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *IluvatarDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, IluvatarGPUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *IluvatarDevices) checkUUID(annos map[string]string, d device.DeviceUsage) bool {
	userUUID, ok := annos[IluvatarUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for Iluvatar user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		return slices.Contains(userUUIDs, d.ID)
	}

	noUserUUID, ok := annos[IluvatarNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for Iluvatar not user uuid [%s], device id is %s", noUserUUID, d.ID)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		return !slices.Contains(noUserUUIDs, d.ID)
	}
	return true
}

func (dev *IluvatarDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *IluvatarDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count iluvatar devices for container ", ctr.Name)
	iluvatarResourceCount := corev1.ResourceName(IluvatarResourceCount)
	iluvatarResourceMem := corev1.ResourceName(IluvatarResourceMemory)
	iluvatarResourceCores := corev1.ResourceName(IluvatarResourceCores)
	v, ok := ctr.Resources.Limits[iluvatarResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[iluvatarResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found iluvatar devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[iluvatarResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[iluvatarResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums) * 256
				}
			}
			corenum := int32(0)
			core, ok := ctr.Resources.Limits[iluvatarResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[iluvatarResourceCores]
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
				Type:             IluvatarGPUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *IluvatarDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *IluvatarDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (ilu *IluvatarDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
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

		_, found, numa := ilu.checkType(pod.GetAnnotations(), *dev, k)
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
		if !ilu.checkUUID(pod.GetAnnotations(), *dev) {
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

func (dev *IluvatarDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  IluvatarResourceCount,
		ResourceMemoryName: IluvatarResourceMemory,
		ResourceCoreName:   IluvatarResourceCores,
	}
}
