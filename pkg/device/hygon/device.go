<<<<<<< HEAD
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
	"slices"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
=======
package hygon

import (
	"flag"
	"strings"

	"4pd.io/k8s-vgpu/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
>>>>>>> 21785f7 (update to v2.3.2)
)

type DCUDevices struct {
}

const (
<<<<<<< HEAD
	HandshakeAnnos     = "hami.io/node-handshake-dcu"
	RegisterAnnos      = "hami.io/node-dcu-register"
=======
	HandshakeAnnos     = "4pd.io/node-handshake-dcu"
	RegisterAnnos      = "4pd.io/node-dcu-register"
>>>>>>> 21785f7 (update to v2.3.2)
	HygonDCUDevice     = "DCU"
	HygonDCUCommonWord = "DCU"
	DCUInUse           = "hygon.com/use-dcutype"
	DCUNoUse           = "hygon.com/nouse-dcutype"
<<<<<<< HEAD
	// DCUUseUUID annotation specifies a comma-separated list of DCU UUIDs to use.
	DCUUseUUID = "hygon.com/use-gpuuuid"
	// DCUNoUseUUID annotation specifies a comma-separated list of DCU UUIDs to exclude.
	DCUNoUseUUID = "hygon.com/nouse-gpuuuid"

	// NodeLockDCU should same with device plugin node lock name
	// there is a bug with nodelock package utils, the key is hard coded as "hami.io/mutex.lock"
	// so we can only use this value now.
	NodeLockDCU = "hami.io/mutex.lock"
=======
>>>>>>> 21785f7 (update to v2.3.2)
)

var (
	HygonResourceCount  string
	HygonResourceMemory string
	HygonResourceCores  string
<<<<<<< HEAD
	MemoryFactor        int32
)

type HygonConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
	MemoryFactor       int32  `yaml:"memoryFactor"`
}

func InitDCUDevice(config HygonConfig) *DCUDevices {
	HygonResourceCount = config.ResourceCountName
	HygonResourceMemory = config.ResourceMemoryName
	HygonResourceCores = config.ResourceCoreName
	MemoryFactor = config.MemoryFactor
	_, ok := device.InRequestDevices[HygonDCUDevice]
	if !ok {
		device.InRequestDevices[HygonDCUDevice] = "hami.io/dcu-devices-to-allocate"
		device.SupportDevices[HygonDCUDevice] = "hami.io/dcu-devices-allocated"
		util.HandshakeAnnos[HygonDCUDevice] = HandshakeAnnos
	}
	return &DCUDevices{}
}

func (dev *DCUDevices) CommonWord() string {
	return HygonDCUCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
=======
)

func InitDCUDevice() *DCUDevices {
	return &DCUDevices{}
}

func (dev *DCUDevices) ParseConfig(fs *flag.FlagSet) {
>>>>>>> 21785f7 (update to v2.3.2)
	fs.StringVar(&HygonResourceCount, "dcu-name", "hygon.com/dcunum", "dcu resource count")
	fs.StringVar(&HygonResourceMemory, "dcu-memory", "hygon.com/dcumem", "dcu memory resource")
	fs.StringVar(&HygonResourceCores, "dcu-cores", "hygon.com/dcucores", "dcu core resource")
}

<<<<<<< HEAD
func (dev *DCUDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(HygonResourceCount)]
	return ok, nil
}

func checkDCUtype(annos map[string]string, cardtype string) bool {
	if inuse, ok := annos[DCUInUse]; ok {
=======
func (dev *DCUDevices) MutateAdmission(ctr *corev1.Container) bool {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(HygonResourceCount)]
	return ok
}

func checkDCUtype(annos map[string]string, cardtype string) bool {
	inuse, ok := annos[DCUInUse]
	if ok {
>>>>>>> 21785f7 (update to v2.3.2)
		if !strings.Contains(inuse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(inuse)) {
				return true
			}
		} else {
<<<<<<< HEAD
			for val := range strings.SplitSeq(inuse, ",") {
=======
			for _, val := range strings.Split(inuse, ",") {
>>>>>>> 21785f7 (update to v2.3.2)
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return true
				}
			}
		}
		return false
	}
<<<<<<< HEAD
	if nouse, ok := annos[DCUNoUse]; ok {
=======
	nouse, ok := annos[DCUNoUse]
	if ok {
>>>>>>> 21785f7 (update to v2.3.2)
		if !strings.Contains(nouse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(nouse)) {
				return false
			}
		} else {
<<<<<<< HEAD
			for val := range strings.SplitSeq(nouse, ",") {
=======
			for _, val := range strings.Split(nouse, ",") {
>>>>>>> 21785f7 (update to v2.3.2)
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return false
				}
			}
		}
		return true
	}
	return true
}

<<<<<<< HEAD
func (dev *DCUDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.LockNode(n.Name, NodeLockDCU, p)
}

func (dev *DCUDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.ReleaseNodeLock(n.Name, NodeLockDCU, p, false)
}

func (dev *DCUDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
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
		nodedevices[idx].DeviceVendor = HygonDCUCommonWord
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, errors.New("no gpu found on node")
	}
	devDecoded := device.EncodeNodeDevices(nodedevices)
	klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devDecoded)
	return nodedevices, nil
}

func (dev *DCUDevices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(HandshakeAnnos, nn)
}

func (dev *DCUDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return device.CheckHealth(devType, n)
}

func (dev *DCUDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, HygonDCUDevice) == 0 {
		return true, checkDCUtype(annos, d.Type), false
	}
	return false, false, false
}

func (dev *DCUDevices) checkUUID(annos map[string]string, d device.DeviceUsage) bool {
	userUUID, ok := annos[DCUUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for dcu user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		return slices.Contains(userUUIDs, d.ID)
	}

	noUserUUID, ok := annos[DCUNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for dcu not user uuid [%s], device id is %s", noUserUUID, d.ID)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		return !slices.Contains(noUserUUIDs, d.ID)
	}
	return true
}

func (dev *DCUDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count dcu devices for container ", ctr.Name)
=======
func (dev *DCUDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool) {
	if strings.Compare(n.Type, HygonDCUDevice) == 0 {
		return true, checkDCUtype(annos, d.Type)
	}
	return false, false
}

func (dev *DCUDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Infof("Counting dcu devices")
>>>>>>> 21785f7 (update to v2.3.2)
	dcuResourceCount := corev1.ResourceName(HygonResourceCount)
	dcuResourceMem := corev1.ResourceName(HygonResourceMemory)
	dcuResourceCores := corev1.ResourceName(HygonResourceCores)
	v, ok := ctr.Resources.Limits[dcuResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[dcuResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found dcu devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[dcuResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[dcuResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
<<<<<<< HEAD
					if MemoryFactor > 1 {
						rawMemnums := memnums
						memnums = memnums * int64(MemoryFactor)
						klog.V(4).Infof("Update memory request. before %d, after %d, factor %d", rawMemnums, memnums, MemoryFactor)
					}
					memnum = int(memnums)
				}
			}
			corenum := int32(100)
=======
					memnum = int(memnums)
				}
			}
			corenum := int32(0)
>>>>>>> 21785f7 (update to v2.3.2)
			core, ok := ctr.Resources.Limits[dcuResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[dcuResourceCores]
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

<<<<<<< HEAD
			return device.ContainerDeviceRequest{
=======
			return util.ContainerDeviceRequest{
>>>>>>> 21785f7 (update to v2.3.2)
				Nums:             int32(n),
				Type:             HygonDCUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
<<<<<<< HEAD
	return device.ContainerDeviceRequest{}
}

func (dev *DCUDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[HygonDCUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.InRequestDevices[HygonDCUDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[HygonDCUDevice]] = deviceStr
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.InRequestDevices[HygonDCUDevice], deviceStr)
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.SupportDevices[HygonDCUDevice], deviceStr)
	}
	return *annoinput
}

func (dev *DCUDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *DCUDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (dcu *DCUDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
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

		_, found, numa := dcu.checkType(pod.GetAnnotations(), *dev, k)
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
		if !dcu.checkUUID(pod.GetAnnotations(), *dev) {
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

func (dev *DCUDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  HygonResourceCount,
		ResourceMemoryName: HygonResourceMemory,
		ResourceCoreName:   HygonResourceCores,
	}
=======
	return util.ContainerDeviceRequest{}
>>>>>>> 21785f7 (update to v2.3.2)
}
