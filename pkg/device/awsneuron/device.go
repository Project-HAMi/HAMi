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

package awsneuron

import (
	"cmp"
	"fmt"
	"maps"
	"math"
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

type AWSNeuronDevices struct {
	resourceCountName string
	resourceCoreName  string
}

const (
	AWSNeuronDevice          = "AWSNeuron"
	AWSNeuronCommonWord      = "AWSNeuron"
	AWSNeuronDeviceSelection = "aws.amazon.com/neuron-index"
	AWSNeuronUseUUID         = "aws.amazon.com/use-neuron-uuid"
	AWSNeuronNoUseUUID       = "aws.amazon.com/nouse-neuron-uuid"
	AWSNeuronAssignedIndex   = "AWS_NEURON_IDS"
	AWSNeuronAssignedNode    = "aws.amazon.com/predicate-node"
	AWSNeuronPredicateTime   = "NEURON_ALLOC_TIME"
	AWSNeuronResourceType    = "NEURON_RESOURCE_TYPE"
	AWSNeuronAllocated       = "NEURON_ALLOCATED"
	AWSUsageInfo             = "awsusageinfo"
	AWSNodeType              = "AWSNodeType"
	AWSCoresPerNeuronDevice  = "AWSCoresPerNeuronDevice"
	maxAWSNeuronDeviceCount  = int64(math.MaxInt32)
	// maxCoresPerNeuronDevice is the largest AWS Neuron core geometry HAMi
	// supports. Inferentia1 exposes four NeuronCores; Inferentia2 and Trainium
	// expose two.
	maxCoresPerNeuronDevice = int64(4)
	maxAWSNeuronCoreCount   = maxAWSNeuronDeviceCount * maxCoresPerNeuronDevice
)

type AWSNeuronConfig struct {
	ResourceCountName string `yaml:"resourceCountName"`
	ResourceCoreName  string `yaml:"resourceCoreName"`
}

func InitAWSNeuronDevice(config AWSNeuronConfig) *AWSNeuronDevices {
	_, ok := device.SupportDevices[AWSNeuronDevice]
	if !ok {
		device.SupportDevices[AWSNeuronDevice] = "hami.io/aws-neuron-devices-allocated"
	}
	return &AWSNeuronDevices{
		resourceCountName: config.ResourceCountName,
		resourceCoreName:  config.ResourceCoreName,
	}
}

func (dev *AWSNeuronDevices) CommonWord() string {
	return AWSNeuronCommonWord
}

func (dev *AWSNeuronDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, countRequested := resourceQuantity(ctr, corev1.ResourceName(dev.resourceCountName))
	if countRequested {
		_, err := validateResourceRequest(count, dev.resourceCountName, maxAWSNeuronDeviceCount)
		return err == nil, err
	}

	core, coreRequested := resourceQuantity(ctr, corev1.ResourceName(dev.resourceCoreName))
	if !coreRequested {
		return false, nil
	}
	_, err := validateResourceRequest(core, dev.resourceCoreName, maxAWSNeuronCoreCount)
	if err != nil {
		return false, err
	}
	return true, nil
}

func resourceQuantity(ctr *corev1.Container, name corev1.ResourceName) (resource.Quantity, bool) {
	quantity, ok := ctr.Resources.Limits[name]
	if !ok {
		quantity, ok = ctr.Resources.Requests[name]
	}
	return quantity, ok
}

func validateResourceRequest(quantity resource.Quantity, resourceName string, maxValue int64) (int64, error) {
	value, isInteger := quantity.AsInt64()
	if !isInteger {
		return 0, fmt.Errorf("%s must be an integer", resourceName)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", resourceName)
	}
	if value > maxValue {
		return 0, fmt.Errorf("%s must not exceed %d", resourceName, maxValue)
	}
	return value, nil
}

func (dev *AWSNeuronDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	counts, ok := n.Status.Capacity.Name(corev1.ResourceName(dev.resourceCountName), resource.DecimalSI).AsInt64()
	if !ok || counts <= 0 || counts > maxAWSNeuronDeviceCount {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", dev.resourceCountName)
	}
	coresTotal, ok := n.Status.Capacity.Name(corev1.ResourceName(dev.resourceCoreName), resource.DecimalSI).AsInt64()
	if !ok || coresTotal <= 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", dev.resourceCoreName)
	}
	if coresTotal%counts != 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("%s capacity %d is not divisible by %s capacity %d", dev.resourceCoreName, coresTotal, dev.resourceCountName, counts)
	}
	coresPerDevice := coresTotal / counts
	if coresPerDevice > math.MaxInt32 {
		return []*device.DeviceInfo{}, fmt.Errorf("cores per AWS Neuron device %d exceeds the maximum of %d", coresPerDevice, int64(math.MaxInt32))
	}
	// Keep the mask within the supported four-core AWS Neuron geometry.
	coremask := int32(0)
	for i < int(min(coresPerDevice, maxCoresPerNeuronDevice)) {
		coremask *= 2
		coremask++
		i++
	}
	i = 0
	customInfo := map[string]any{
		AWSNodeType:             n.Labels["node.kubernetes.io/instance-type"],
		AWSCoresPerNeuronDevice: int32(coresPerDevice),
	}

	for int64(i) < counts {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:        uint(i),
			ID:           n.Name + "-" + AWSNeuronDevice + "-" + fmt.Sprint(i),
			Count:        int32(coresPerDevice),
			Devmem:       0,
			Devcore:      coremask,
			Type:         AWSNeuronDevice,
			Numa:         0,
			Health:       true,
			CustomInfo:   customInfo,
			DeviceVendor: AWSNeuronCommonWord,
		})
		i++
	}
	i = 0
	for i < len(nodedevices) {
		klog.V(4).Infoln("Registered AWS nodedevices:", nodedevices[i])
		i++
	}
	return nodedevices, nil
}

func coresPerNeuronDevice(customInfo map[string]any) int32 {
	if value, ok := customInfo[AWSCoresPerNeuronDevice].(int32); ok && value > 0 {
		return value
	}
	// Allocations created before geometry was stored per device used the
	// historic two-core layout. Preserve that behavior when decoding them.
	return 2
}

func (dev *AWSNeuronDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[AWSNeuronDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[device.SupportDevices[AWSNeuronDevice]] = device.EncodePodSingleDevice(devlist)
		value := ""
		for ctridx, dp := range devlist {
			if len(dp) > 0 {
				ctr := getContainerByIndex(pod, ctridx)
				if ctr == nil {
					continue
				}
				for _, val := range dp {
					devValue, ok := ctr.Resources.Limits[corev1.ResourceName(dev.resourceCountName)]
					if ok {
						c, _ := devValue.AsInt64()
						if c > 0 {
							value = value + fmt.Sprint(val.Idx) + ","
						}
					} else {
						coresPerDevice := coresPerNeuronDevice(val.CustomInfo)
						coreOffset := int64(val.Idx) * int64(coresPerDevice)
						for core := range min(coresPerDevice, int32(maxCoresPerNeuronDevice)) {
							if (val.Usedcores & (1 << core)) != 0 {
								value = value + fmt.Sprint(coreOffset+int64(core)) + ","
								(*annoinput)[AWSNeuronResourceType] = dev.resourceCoreName
							}
						}
					}
				}
				if len(value) > 0 {
					// This needs to modify, it has to be core indexes?
					(*annoinput)[AWSNeuronAssignedIndex] = strings.TrimRight(value, ",")

					tmp := strconv.FormatInt(time.Now().UnixNano(), 10)
					(*annoinput)[AWSNeuronPredicateTime] = tmp
					(*annoinput)[AWSNeuronAllocated] = "false"
					(*annoinput)[AWSNeuronAssignedNode] = (*annoinput)[util.AssignedNodeAnnotations]
				}
			}
		}
	}
	klog.V(4).InfoS("annos", "input", (*annoinput))
	return *annoinput
}

func getContainerByIndex(pod *corev1.Pod, ctridx int) *corev1.Container {
	if ctridx < len(pod.Spec.InitContainers) {
		return &pod.Spec.InitContainers[ctridx]
	}
	ctridx -= len(pod.Spec.InitContainers)
	if ctridx < len(pod.Spec.Containers) {
		return &pod.Spec.Containers[ctridx]
	}
	return nil
}

func (dev *AWSNeuronDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AWSNeuronDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AWSNeuronDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *AWSNeuronDevices) checkType(n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, AWSNeuronDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *AWSNeuronDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *AWSNeuronDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  dev.resourceCountName,
		ResourceMemoryName: "",
		ResourceCoreName:   dev.resourceCoreName,
	}
}

func (dev *AWSNeuronDevices) GenerateResourceRequests(ctr *corev1.Container) (device.ContainerDeviceRequest, error) {
	klog.Info("Start to count awsNeuron devices for container ", ctr.Name)
	awsResourceCount := corev1.ResourceName(dev.resourceCountName)
	awsResourceCores := corev1.ResourceName(dev.resourceCoreName)
	v, ok := resourceQuantity(ctr, awsResourceCount)
	if ok {
		// An explicit zero count means no device is requested, not an
		// invalid request. See the nvidia backend. MutateAdmission keeps
		// using the shared validator, which still rejects zero there.
		if zero, isInt := v.AsInt64(); isInt && zero == 0 {
			return device.ContainerDeviceRequest{}, nil
		}
		n, err := validateResourceRequest(v, dev.resourceCountName, maxAWSNeuronDeviceCount)
		if err != nil {
			klog.ErrorS(err, "Invalid awsNeuron device request", "container", ctr.Name)
			return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "awsneuron", Reason: err.Error()}
		}
		klog.InfoS("Detected awsNeuron device request",
			"container", ctr.Name,
			"deviceCount", n)
		return device.ContainerDeviceRequest{
			Nums:             int32(n),
			Type:             AWSNeuronDevice,
			Memreq:           0,
			MemPercentagereq: 0,
			// A zero core request denotes a whole AWS Neuron device. Fit converts
			// it to the selected device's actual addressable core count.
			Coresreq: 0,
		}, nil
	} else {
		core, ok := resourceQuantity(ctr, awsResourceCores)
		if ok {
			n, err := validateResourceRequest(core, dev.resourceCoreName, maxAWSNeuronCoreCount)
			if err != nil {
				klog.ErrorS(err, "Invalid awsNeuron core request", "container", ctr.Name)
				return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "awsneuron", Reason: err.Error()}
			}
			klog.InfoS("Detected awsNeuron device request",
				"container", ctr.Name,
				"deviceCores", n)
			return device.ContainerDeviceRequest{
				Nums:             1,
				Type:             AWSNeuronDevice,
				Memreq:           0,
				MemPercentagereq: 0,
				TotalCoresreq:    n,
			}, nil
		}
	}
	return device.ContainerDeviceRequest{}, nil
}

func (dev *AWSNeuronDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *AWSNeuronDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores = dev.AccumulateCores(n.Usedcores, ctr.Usedcores)
	n.Usedmem += ctr.Usedmem
	if n.CustomInfo == nil {
		n.CustomInfo = map[string]any{}
	}
	n.CustomInfo[AWSUsageInfo] = int(n.Usedcores)
	return nil
}

func (dev *AWSNeuronDevices) AccumulateCores(used, allocated int32) int32 {
	return used | allocated
}

func (dev *AWSNeuronDevices) CountAllocatedCores(allocated int32) int64 {
	return int64(countMaskAvailable(allocated))
}

func countMaskAvailable(mask int32) int32 {
	tmp := mask
	ret := int32(0)
	for tmp > 0 {
		ret = ret + tmp%2
		tmp /= 2
	}
	return ret
}

// allocateCoreMask returns one contiguous run of free NeuronCores. The Neuron
// runtime requires a contiguous core range, so separate free bits cannot be
// combined into one allocation.
func allocateCoreMask(used, available int32, required int32) (int32, bool) {
	if required <= 0 || required > int32(maxCoresPerNeuronDevice) {
		return 0, false
	}
	for start := int32(0); start+required <= int32(maxCoresPerNeuronDevice); start++ {
		mask := ((int32(1) << required) - 1) << start
		if available&mask == mask && used&mask == 0 {
			return mask, true
		}
	}
	return 0, false
}

func coreAllocationInfo(info map[string]any, selected int32) map[string]any {
	result := maps.Clone(info)
	if result == nil {
		result = map[string]any{}
	}
	result[AWSUsageInfo] = int(selected)
	return result
}

// continuousDeviceAvailable reports the device indexes of a free run of count
// devices starting at slice position start. It returns indexes rather than
// positions because Fit matches its result against DeviceUsage.Index.
func continuousDeviceAvailable(devices []*device.DeviceUsage, start int, count int) []int {
	if len(devices) < start+count {
		return []int{}
	}
	res := []int{}
	iterator := start
	for iterator < start+count {
		if devices[iterator].Used > 0 || !devices[iterator].Health {
			return []int{}
		}
		res = append(res, int(devices[iterator].Index))
		iterator++
	}
	return res
}

func graphSelect(devices []*device.DeviceUsage, count int) []int {
	if len(devices) == 0 {
		return []int{}
	}
	// The ring and power of two groupings below read adjacency from slice
	// positions, but the scheduler sorts devices by score before calling Fit.
	// Restore index order on a copy so position math matches the physical
	// layout, the same way kunlun's topology selector does. Ordering first
	// also puts the device carrying the node type back at position zero.
	sorted := make([]*device.DeviceUsage, len(devices))
	copy(sorted, devices)
	slices.SortFunc(sorted, func(a, b *device.DeviceUsage) int {
		return cmp.Compare(a.Index, b.Index)
	})
	devices = sorted

	if devices[0].CustomInfo == nil || devices[0].CustomInfo[AWSNodeType] == nil {
		return []int{}
	}
	AWSNodetype := ""
	if nodeType, ok := devices[0].CustomInfo[AWSNodeType].(string); ok {
		AWSNodetype = nodeType
	}
	if strings.Contains(AWSNodetype, "inf") || strings.Contains(AWSNodetype, "Inf") {
		//Deal with ring
		start := 0
		for start < len(devices) {
			res := continuousDeviceAvailable(devices, start, count)
			if len(res) > 0 {
				return res
			}
			start += 1
		}
		return []int{}
	}
	switch count {
	case 1, 4, 8, 16:
		{
			start := 0
			for start < len(devices) {
				res := continuousDeviceAvailable(devices, start, count)
				if len(res) > 0 {
					return res
				}
				start += count
			}
			return []int{}
		}
	}
	return []int{}
}

func (neuron *AWSNeuronDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeinfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	isMutex := util.PolicyContains(util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, pod), util.GPUSchedulerPolicyMutex)
	remainingCores := k.TotalCoresreq
	if remainingCores > 0 && len(devices) > 0 {
		coresPerDevice := int64(countMaskAvailable(devices[0].Totalcore))
		if coresPerDevice == 0 {
			return false, tmpDevs, common.GenReason(map[string]int{common.CardInsufficientCore: 1}, len(devices))
		}
		deviceCount := (remainingCores-1)/coresPerDevice + 1
		if deviceCount > int64(len(devices)) {
			return false, tmpDevs, common.GenReason(map[string]int{common.NodeInsufficientDevice: len(devices)}, int(deviceCount))
		}
		k.Nums = int32(deviceCount)
	}
	if k.Nums > 1 {
		candidates := make([]*device.DeviceUsage, len(devices))
		for i, candidate := range devices {
			copy := candidate.DeepCopy()
			requiredCores := k.Coresreq
			if remainingCores > 0 {
				requiredCores = int32(min(remainingCores, int64(countMaskAvailable(copy.Totalcore))))
			} else if requiredCores == 0 {
				requiredCores = countMaskAvailable(copy.Totalcore)
			}
			if _, found := allocateCoreMask(copy.Usedcores, copy.Totalcore, requiredCores); !found {
				copy.Health = false
			}
			candidates[i] = copy
		}
		alloc := graphSelect(candidates, int(k.Nums))
		if len(alloc) == 0 {
			reason[common.NumaNotFit]++
			klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", devices, "request nums", k.Nums, "numa")
			return false, tmpDevs, common.GenReason(reason, len(devices))
		}
		for _, dev := range alloc {
			for _, val := range devices {
				if val.Index == uint(dev) {
					requiredCores := k.Coresreq
					if remainingCores > 0 {
						requiredCores = int32(min(remainingCores, int64(countMaskAvailable(val.Totalcore))))
						remainingCores -= int64(requiredCores)
					} else if requiredCores == 0 {
						requiredCores = countMaskAvailable(val.Totalcore)
					}
					selected, found := allocateCoreMask(val.Usedcores, val.Totalcore, requiredCores)
					if !found {
						reason[common.CardInsufficientCore]++
						return false, tmpDevs, common.GenReason(reason, len(devices))
					}
					tmpDevs[request.Type] = append(tmpDevs[request.Type], device.ContainerDevice{
						Idx:        int(val.Index),
						UUID:       val.ID,
						Type:       request.Type,
						Usedmem:    val.Totalmem,
						Usedcores:  selected,
						CustomInfo: coreAllocationInfo(val.CustomInfo, selected),
					})
					break
				}
			}
		}
		return true, tmpDevs, ""
	}
	for i, v := range slices.Backward(devices) {
		dev := v
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)
		if !dev.Health {
			reason[common.CardNotHealth]++
			klog.V(5).InfoS(common.CardNotHealth, "pod", klog.KObj(pod), "device", dev.ID, "health", dev.Health)
			continue
		}
		klog.V(3).InfoS("Type check", "device", dev.Type, "req", k.Type, "dev=", dev)
		if !strings.Contains(dev.Type, k.Type) {
			reason[common.CardTypeMismatch]++
			continue
		}

		_, found, _ := neuron.checkType(k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, AWSNeuronUseUUID, AWSNeuronNoUseUUID, neuron.CommonWord()) {
			reason[common.CardUUIDMismatch]++
			klog.V(5).InfoS(common.CardUUIDMismatch, "pod", klog.KObj(pod), "device", dev.ID, "current device info is:", *dev)
			continue
		}

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

		requiredCores := k.Coresreq
		if remainingCores > 0 {
			requiredCores = int32(remainingCores)
		} else if requiredCores == 0 {
			requiredCores = countMaskAvailable(dev.Totalcore)
		}
		if countMaskAvailable(dev.Totalcore)-countMaskAvailable(dev.Usedcores) < requiredCores {
			reason[common.CardInsufficientCore]++
			klog.V(5).InfoS(common.CardInsufficientCore, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", k.Coresreq)
			continue
		}

		klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
		selected, found := allocateCoreMask(dev.Usedcores, dev.Totalcore, requiredCores)
		if !found {
			reason[common.CardInsufficientCore]++
			continue
		}
		tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
			Idx:        int(dev.Index),
			UUID:       dev.ID,
			Type:       k.Type,
			Usedmem:    0,
			Usedcores:  selected,
			CustomInfo: coreAllocationInfo(dev.CustomInfo, selected),
		})
		klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
		return true, tmpDevs, ""
	}
	klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs))
	return false, tmpDevs, common.GenReason(reason, len(devices))
}
