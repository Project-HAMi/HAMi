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

package ascend

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	NodeLockAscend         = "hami.io/mutex.lock"
	Ascend910Prefix        = "Ascend910"
	Ascend910CType         = "Ascend910C"
	Ascend910NetworkWeight = 10
)

type Devices struct {
	config           VNPUConfig
	nodeRegisterAnno string
	useUUIDAnno      string
	noUseUUIDAnno    string
	handshakeAnno    string
}

type RuntimeInfo struct {
	UUID string `json:"UUID,omitempty"`
	Temp string `json:"temp,omitempty"`
}

var (
	enableAscend bool
	configFile   string
)

func (dev *Devices) trimMemory(m int64) (int64, string) {
	for i := range dev.config.Templates {
		if m <= dev.config.Templates[i].Memory {
			return dev.config.Templates[i].Memory, dev.config.Templates[i].Name
		}
	}
	if m <= dev.config.MemoryCapacity {
		return dev.config.MemoryAllocatable, ""
	}
	return 0, ""
}

func InitDevices(config []VNPUConfig) []*Devices {
	var devs []*Devices
	if !enableAscend {
		return devs
	}
	for _, vnpu := range config {
		commonWord := vnpu.CommonWord
		dev := &Devices{
			config:           vnpu,
			nodeRegisterAnno: fmt.Sprintf("hami.io/node-register-%s", commonWord),
			useUUIDAnno:      fmt.Sprintf("hami.io/use-%s-uuid", commonWord),
			noUseUUIDAnno:    fmt.Sprintf("hami.io/no-use-%s-uuid", commonWord),
			handshakeAnno:    fmt.Sprintf("hami.io/node-handshake-%s", commonWord),
		}
		sort.Slice(dev.config.Templates, func(i, j int) bool {
			return dev.config.Templates[i].Memory < dev.config.Templates[j].Memory
		})
		_, ok := device.InRequestDevices[commonWord]
		if !ok {
			device.InRequestDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-to-allocate", commonWord)
			device.SupportDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-allocated", commonWord)
			util.HandshakeAnnos[commonWord] = dev.handshakeAnno
		}
		devs = append(devs, dev)
		klog.Infof("load ascend vnpu config %s: %v", commonWord, dev.config)
	}
	return devs
}

func ParseConfig(fs *flag.FlagSet) {
	fs.BoolVar(&enableAscend, "enable-ascend", false, "enable ascend device")
}

func (dev *Devices) CommonWord() string {
	return dev.config.CommonWord
}

func (dev *Devices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)]
	if !ok {
		return false, nil
	}

	reqNum := count.Value()
	if dev.config.CommonWord == Ascend910CType {
		if reqNum == 1 {
			// Since the minimum allocation unit is one physical module (2 NPUs), round up the limits and requests to 2.
			klog.InfoS("Adjusted Ascend910C device request from 1 to 2 (minimum allocation unit)", "pod", klog.KObj(p))
			reqNum = 2
			ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)] = *resource.NewQuantity(reqNum, resource.DecimalExponent)
			if _, exists := ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceName)]; exists {
				ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceName)] = *resource.NewQuantity(reqNum, resource.DecimalExponent)
			}
		} else if reqNum%2 != 0 {
			// Reject any other odd-numbered request (e.g., 3, 5, 7...)
			errMsg := fmt.Sprintf("Ascend910C device request must be 1 or 2*n, got %d", reqNum)
			klog.ErrorS(nil, errMsg, "pod", klog.KObj(p))
			return false, errors.New(errMsg)
		}
	}

	trimMem := dev.config.MemoryAllocatable
	memory, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
	if ok {
		trimMem, _ = dev.trimMemory(memory.Value())
		if trimMem <= 0 {
			return false, fmt.Errorf("%s %d is invalid", dev.config.ResourceMemoryName, memory.Value())
		}
	}
	if count.Value() > 1 {
		if trimMem != dev.config.MemoryAllocatable {
			return true, errors.New("vNPU nor supported for multiple devices")
		}
	}
	ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)] = resource.MustParse(fmt.Sprint(trimMem))
	ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceMemoryName)] = resource.MustParse(fmt.Sprint(trimMem))
	return true, nil
}

func (dev *Devices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	anno, ok := n.Annotations[dev.nodeRegisterAnno]
	if !ok {
		return []*device.DeviceInfo{}, fmt.Errorf("annos not found %s", dev.nodeRegisterAnno)
	}
	nodeDevices, err := device.UnMarshalNodeDevices(anno)
	for idx := range nodeDevices {
		nodeDevices[idx].DeviceVendor = dev.config.CommonWord
	}
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal node devices", "node", n.Name, "device annotation", anno)
		return []*device.DeviceInfo{}, err
	}
	if len(nodeDevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", anno)
		return []*device.DeviceInfo{}, errors.New("no device found on node")
	}
	return nodeDevices, nil
}

func (dev *Devices) PatchAnnotations(pod *corev1.Pod, annoInput *map[string]string, pd device.PodDevices) map[string]string {
	commonWord := dev.CommonWord()
	devList, ok := pd[commonWord]
	if ok && len(devList) > 0 {
		(*annoInput)[device.InRequestDevices[commonWord]] = device.EncodePodSingleDevice(devList)
		(*annoInput)[device.SupportDevices[commonWord]] = device.EncodePodSingleDevice(devList)
		(*annoInput)["predicate-time"] = strconv.FormatInt(time.Now().Unix(), 10)
		allocateStr := fmt.Sprintf("huawei.com/%s", dev.CommonWord())
		var rtInfo []RuntimeInfo
		for _, dp := range devList {
			for _, val := range dp {
				_, temp := dev.trimMemory(int64(val.Usedmem))
				rtInfo = append(rtInfo, RuntimeInfo{
					UUID: val.UUID,
					Temp: temp,
				})
			}
		}
		s, err := json.Marshal(rtInfo)
		if err != nil {
			klog.ErrorS(err, "failed to marshal runtime info", "runtime info", rtInfo)
		}
		(*annoInput)[allocateStr] = string(s)
	}
	return *annoInput
}

func (dev *Devices) LockNode(n *corev1.Node, p *corev1.Pod) error {
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

	return nodelock.LockNode(n.Name, NodeLockAscend, p)
}

func (dev *Devices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
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

	return nodelock.ReleaseNodeLock(n.Name, NodeLockAscend, p, false)
}

func (dev *Devices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(dev.handshakeAnno, nn)
}

func (dev *Devices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, dev.CommonWord()) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *Devices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return device.CheckHealth(devType, n)
}

func (dev *Devices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	ascendResourceCount := corev1.ResourceName(dev.config.ResourceName)
	ascendResourceMem := corev1.ResourceName(dev.config.ResourceMemoryName)
	v, ok := ctr.Resources.Limits[ascendResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[ascendResourceCount]
	}
	if ok {
		klog.V(3).Infof("Counting %s devices", dev.config.CommonWord)
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found AscendDevices devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[ascendResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[ascendResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					if dev.config.MemoryFactor > 1 {
						rawMemnums := memnums
						memnums = memnums * int64(dev.config.MemoryFactor)
						klog.V(4).Infof("Update Ascend memory request. before %d, after %d, factor %d", rawMemnums, memnums, dev.config.MemoryFactor)
					}
					m, _ := dev.trimMemory(memnums)
					memnum = int(m)
				}
			}
			corenum := int32(0)

			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             dev.CommonWord(),
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *Devices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	if !strings.HasPrefix(dev.CommonWord(), Ascend910Prefix) {
		return 0
	}
	score := float32(0)
	for _, containerDevices := range podDevices {
		if len(containerDevices) == 0 {
			continue
		}
		cntMap := make(map[int]int)
		for _, device := range containerDevices {
			if device.CustomInfo == nil {
				return 0
			}
			if networkID, ok := device.CustomInfo["NetworkID"]; ok {
				if id, ok := networkID.(float64); ok {
					cntMap[int(id)]++
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
	klog.V(4).InfoS("ScoreNode", "node", node.Name, "deviceType", dev.CommonWord(), "topology score", score, "weight", Ascend910NetworkWeight)
	return score * Ascend910NetworkWeight
}

func (dev *Devices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (dev *Devices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  dev.config.ResourceName,
		ResourceMemoryName: dev.config.ResourceMemoryName,
		ResourceCoreName:   "",
	}
}

func (npu *Devices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]device.ContainerDevices
	tmpDevs = make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	needTopology := false
	if strings.HasPrefix(npu.CommonWord(), Ascend910Prefix) && hasNetworkID(devices) {
		klog.V(4).Infof("all devices have NetworkID. device CommonWord %s", npu.CommonWord())
		needTopology = true
	}
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, numa := npu.checkType(pod.GetAnnotations(), *dev, k)
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
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, npu.useUUIDAnno, npu.noUseUUIDAnno, npu.CommonWord()) {
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
			if !needTopology {
				k.Nums--
			}
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:        int(dev.Index),
				UUID:       dev.ID,
				Type:       k.Type,
				Usedmem:    memreq,
				Usedcores:  k.Coresreq,
				CustomInfo: dev.CustomInfo,
			})
		}
		if k.Nums == 0 && !needTopology {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}
	}

	if needTopology {
		if len(tmpDevs[k.Type]) == int(originReq) {
			klog.V(5).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		} else if len(tmpDevs[k.Type]) > int(originReq) {
			if originReq == 1 {
				tmpDevs[k.Type] = device.ContainerDevices{tmpDevs[k.Type][0]}
			} else {
				// If requesting multiple devices, select the best combination of cards.
				var combination device.ContainerDevices
				if k.Type == Ascend910CType {
					// Use topology-aware allocation for Ascend910C: only select full modules (2 NPUs per card).
					combination = npu.computeBestCombination910C(nodeInfo, int(originReq), tmpDevs[k.Type])
				} else {
					combination = npu.computeBestCombination(nodeInfo, int(originReq), tmpDevs[k.Type])
				}
				tmpDevs[k.Type] = combination
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

func hasNetworkID(devices []*device.DeviceUsage) bool {
	for _, dev := range devices {
		if dev.CustomInfo == nil {
			return false
		}
		if _, ok := dev.CustomInfo["NetworkID"]; !ok {
			return false
		}
	}
	return true
}

func (npudev *Devices) computeBestCombination(nodeInfo *device.NodeInfo, reqNum int, containerDevices device.ContainerDevices) device.ContainerDevices {
	deviceMap := make(map[string]*device.DeviceInfo)
	for _, dev := range nodeInfo.Devices[npudev.config.CommonWord] {
		deviceMap[dev.ID] = &dev
	}
	networkDeviceMap := make(map[int]device.ContainerDevices)
	for _, containerDevice := range containerDevices {
		if dev, ok := deviceMap[containerDevice.UUID]; ok {
			if dev.CustomInfo != nil {
				if networkID, ok := dev.CustomInfo["NetworkID"]; ok {
					if id, ok := networkID.(float64); ok {
						networkDeviceMap[int(id)] = append(networkDeviceMap[int(id)], containerDevice)
					}
				}
			}
		}
	}

	type NetworkDeviceCount struct {
		NetworkID int
		Count     int
	}
	var sortedNetworks []NetworkDeviceCount
	for networkID, devices := range networkDeviceMap {
		sortedNetworks = append(sortedNetworks, NetworkDeviceCount{
			NetworkID: networkID,
			Count:     len(devices),
		})
	}

	sort.Slice(sortedNetworks, func(i, j int) bool {
		return sortedNetworks[i].Count > sortedNetworks[j].Count
	})
	result := device.ContainerDevices{}
	for _, item := range sortedNetworks {
		devices := networkDeviceMap[item.NetworkID]
		for _, dev := range devices {
			result = append(result, dev)
			if len(result) == reqNum {
				return result
			}
		}
	}
	return result
}

func (npudev *Devices) computeBestCombination910C(nodeInfo *device.NodeInfo, reqNum int, containerDevices device.ContainerDevices) device.ContainerDevices {
	// Build a mapping from NPU index to device object for quick lookup.
	indexToDevice := make(map[int]device.ContainerDevice)
	var npuIndices []int
	for _, dev := range containerDevices {
		idx := int(dev.Idx)
		indexToDevice[idx] = dev
		npuIndices = append(npuIndices, idx)
	}

	// Each physical card hosts exactly 2 NPUs (Ascend 910C module design).
	const MaxCardNPUNum = 2

	// Group NPU indices by the module and Sort
	cardTopology := make(map[int][]int)
	for _, idx := range npuIndices {
		cardId := idx / MaxCardNPUNum
		cardTopology[cardId] = append(cardTopology[cardId], idx)
	}

	// Convert the card topology map into a slice for sorting.
	cardTopSlice := make([][]int, 0, len(cardTopology))
	for _, card := range cardTopology {
		cardTopSlice = append(cardTopSlice, card)
	}

	// Sort cards by the number of available NPUs in ascending order.
	sort.Slice(cardTopSlice, func(i, j int) bool {
		return len(cardTopSlice[i]) < len(cardTopSlice[j])
	})

	// Select NPUs card by card, preferring full cards.
	var selectedIndices []int
	taskNPUNum := reqNum

	for _, card := range cardTopSlice {
		if taskNPUNum <= 0 {
			break
		}

		// Only consider cards that have both NPUs available (full card).
		if len(card) == MaxCardNPUNum {
			selectedIndices = append(selectedIndices, card...)
			taskNPUNum -= MaxCardNPUNum
		}
	}

	result := make(device.ContainerDevices, 0, len(selectedIndices))
	for _, idx := range selectedIndices {
		if dev, ok := indexToDevice[idx]; ok {
			result = append(result, dev)
		}
	}

	klog.V(4).InfoS("910C selected devices by card module topology",
		"requested", reqNum,
		"selected", len(result),
		"indices", selectedIndices)

	return result
}
