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
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type AWSNeuronDevices struct {
	resourceCountName string
	resourceCoreName  string
	coresPerAWSNeuron uint
	coremask          uint
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
)

type AWSNeuronConfig struct {
	ResourceCountName string `yaml:"resourceCountName"`
	ResourceCoreName  string `yaml:"resourceCoreName"`
}

func InitAWSNeuronDevice(config AWSNeuronConfig) *AWSNeuronDevices {
	util.SupportDevices[AWSNeuronDevice] = "hami.io/aws-neuron-devices-allocated"
	return &AWSNeuronDevices{
		resourceCountName: config.ResourceCountName,
		resourceCoreName:  config.ResourceCoreName,
		coresPerAWSNeuron: 0,
		coremask:          0,
	}
}

func (dev *AWSNeuronDevices) CommonWord() string {
	return AWSNeuronCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
}

func (dev *AWSNeuronDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(dev.resourceCountName)]
	if !ok {
		_, ok = ctr.Resources.Limits[corev1.ResourceName(dev.resourceCoreName)]
	}
	return ok, nil
}

func (dev *AWSNeuronDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	nodedevices := []*util.DeviceInfo{}
	i := 0
	counts, ok := n.Status.Capacity.Name(corev1.ResourceName(dev.resourceCountName), resource.DecimalSI).AsInt64()
	if !ok || counts == 0 {
		return []*util.DeviceInfo{}, fmt.Errorf("device not found %s", dev.resourceCountName)
	}
	coresTotal, _ := n.Status.Capacity.Name(corev1.ResourceName(dev.resourceCoreName), resource.DecimalSI).AsInt64()
	if dev.coresPerAWSNeuron == 0 {
		dev.coresPerAWSNeuron = uint(coresTotal) / uint(counts)
	}
	dev.coremask = 0
	for i < int(dev.coresPerAWSNeuron) {
		dev.coremask *= 2
		dev.coremask++
		i++
	}
	i = 0
	customInfo := map[string]any{}
	customInfo[AWSNodeType] = n.Labels["node.kubernetes.io/instance-type"]

	for int64(i) < counts {
		nodedevices = append(nodedevices, &util.DeviceInfo{
			Index:      uint(i),
			ID:         n.Name + "-" + AWSNeuronDevice + "-" + fmt.Sprint(i),
			Count:      int32(dev.coresPerAWSNeuron),
			Devmem:     0,
			Devcore:    int32(dev.coremask),
			Type:       AWSNeuronDevice,
			Numa:       0,
			Health:     true,
			CustomInfo: customInfo,
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

func (dev *AWSNeuronDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[AWSNeuronDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[util.SupportDevices[AWSNeuronDevice]] = util.EncodePodSingleDevice(devlist)
		value := ""
		for ctridx, dp := range devlist {
			if len(dp) > 0 {
				for _, val := range dp {
					devValue, ok := pod.Spec.Containers[ctridx].Resources.Limits[corev1.ResourceName(dev.resourceCountName)]
					if ok {
						c, _ := devValue.AsInt64()
						if c > 0 {
							value = value + fmt.Sprint(val.Idx) + ","
						}
					} else {
						if (val.Usedcores & 1) != 0 {
							value = value + fmt.Sprint(dev.coresPerAWSNeuron*uint(val.Idx)) + ","
							(*annoinput)[AWSNeuronResourceType] = dev.resourceCoreName
						}
						if (val.Usedcores & 2) != 0 {
							value = value + fmt.Sprint(dev.coresPerAWSNeuron*uint(val.Idx)+1) + ","
							(*annoinput)[AWSNeuronResourceType] = dev.resourceCoreName
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

func (dev *AWSNeuronDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AWSNeuronDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AWSNeuronDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *AWSNeuronDevices) checkType(n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, AWSNeuronDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *AWSNeuronDevices) checkUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[AWSNeuronUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for AWSNeuron user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		return slices.Contains(userUUIDs, d.ID)
	}

	noUserUUID, ok := annos[AWSNeuronNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for AWSNeuron no-use uuid [%s], device id is %s", noUserUUID, d.ID)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		return !slices.Contains(noUserUUIDs, d.ID)
	}
	return true
}

func (dev *AWSNeuronDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *AWSNeuronDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Start to count awsNeuron devices for container ", ctr.Name)
	awsResourceCount := corev1.ResourceName(dev.resourceCountName)
	awsResourceCores := corev1.ResourceName(dev.resourceCoreName)
	v, ok := ctr.Resources.Limits[awsResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[awsResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.InfoS("Detected awsNeuron device request",
				"container", ctr.Name,
				"deviceCount", n)
			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             AWSNeuronDevice,
				Memreq:           0,
				MemPercentagereq: 0,
				Coresreq:         int32(dev.coresPerAWSNeuron),
			}
		}
	} else {
		core, ok := ctr.Resources.Limits[awsResourceCores]
		if !ok {
			core, ok = ctr.Resources.Requests[awsResourceCores]
		}
		if ok {
			if n, ok := core.AsInt64(); ok {
				klog.InfoS("Detected awsNeuron device request",
					"container", ctr.Name,
					"deviceCores", n)
				num := 1
				if n >= 2 {
					num = int(n / 2)
				}
				corenum := 1
				if n >= 2 {
					corenum = 2
				}
				return util.ContainerDeviceRequest{
					Nums:             int32(num),
					Type:             AWSNeuronDevice,
					Memreq:           0,
					MemPercentagereq: 0,
					Coresreq:         int32(corenum),
				}
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *AWSNeuronDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, previous []*util.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *AWSNeuronDevices) AddResourceUsage(pod *corev1.Pod, n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem

	num, ok := n.CustomInfo[AWSUsageInfo]
	if !ok || num == nil {
		n.CustomInfo[AWSUsageInfo] = 0
	}
	if nValue, ok := n.CustomInfo[AWSUsageInfo].(int); ok {
		if ctrValue, ok2 := ctr.CustomInfo[AWSUsageInfo].(int); ok2 {
			n.CustomInfo[AWSUsageInfo] = nValue + ctrValue
		}
	}
	return nil
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

func addCoreUsage(prev map[string]any, require int) map[string]any {
	res := map[string]any{}
	count, ok := prev[AWSUsageInfo]
	if !ok {
		count = 0
	}
	if count == 0 {
		if require == 2 {
			res[AWSUsageInfo] = 3
			return res
		}
		if require == 1 {
			res[AWSUsageInfo] = 1
			return res
		}
	}
	if countValue, ok := count.(int); ok {
		res[AWSUsageInfo] = 3 - countValue
	} else {
		res[AWSUsageInfo] = 3
	}
	return res
}
func continuousDeviceAvailable(devices []*util.DeviceUsage, start int, count int) []int {
	if len(devices) < start+count {
		return []int{}
	}
	res := []int{}
	iterator := start
	for iterator < start+count {
		if devices[iterator].Used > 0 {
			return []int{}
		}
		res = append(res, iterator)
		iterator++
	}
	return res
}

func graphSelect(devices []*util.DeviceUsage, count int) []int {
	if len(devices) == 0 || devices[0].CustomInfo == nil || devices[0].CustomInfo[AWSNodeType] == nil {
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

func (neuron *AWSNeuronDevices) Fit(devices []*util.DeviceUsage, request util.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod, nodeinfo *util.NodeInfo, allocated *util.PodDevices) (bool, map[string]util.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]util.ContainerDevices)
	reason := make(map[string]int)
	if k.Nums > 1 {
		alloc := graphSelect(devices, int(request.Nums))
		if len(alloc) == 0 {
			reason[common.NumaNotFit]++
			klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", devices, "request nums", request.Nums, "numa")
			return false, tmpDevs, common.GenReason(reason, len(reason))
		}
		for _, dev := range alloc {
			for _, val := range devices {
				if val.Index == uint(dev) {
					customInfo := addCoreUsage(val.CustomInfo, int(k.Coresreq))
					tmpDevs[request.Type] = append(tmpDevs[request.Type], util.ContainerDevice{
						Idx:        int(val.Index),
						UUID:       val.ID,
						Type:       request.Type,
						Usedmem:    val.Totalmem,
						Usedcores:  val.Totalcore,
						CustomInfo: customInfo,
					})
					break
				}
			}
		}
		return true, tmpDevs, ""
	}
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		_, ok := dev.CustomInfo[AWSUsageInfo]
		if !ok {
			dev.CustomInfo[AWSUsageInfo] = int(dev.Usedcores)
		}
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

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
		if !neuron.checkUUID(annos, *dev) {
			reason[common.CardUUIDMismatch]++
			klog.V(5).InfoS(common.CardUUIDMismatch, "pod", klog.KObj(pod), "device", dev.ID, "current device info is:", *dev)
			continue
		}

		if dev.Count <= dev.Used {
			reason[common.CardTimeSlicingExhausted]++
			klog.V(5).InfoS(common.CardTimeSlicingExhausted, "pod", klog.KObj(pod), "device", dev.ID, "count", dev.Count, "used", dev.Used)
			continue
		}

		if countMaskAvailable(dev.Totalcore)-countMaskAvailable(dev.Usedcores) < k.Coresreq {
			reason[common.CardInsufficientCore]++
			klog.V(5).InfoS(common.CardInsufficientCore, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", k.Coresreq)
			continue
		}

		klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
		customInfo := addCoreUsage(dev.CustomInfo, int(k.Coresreq))
		usedcores := 0
		if countValue, ok := customInfo[AWSUsageInfo].(int); ok {
			usedcores = countValue
		}
		tmpDevs[k.Type] = append(tmpDevs[k.Type], util.ContainerDevice{
			Idx:        int(dev.Index),
			UUID:       dev.ID,
			Type:       k.Type,
			Usedmem:    0,
			Usedcores:  int32(usedcores),
			CustomInfo: customInfo,
		})
		klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
		return true, tmpDevs, ""
	}
	klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs))
	return false, tmpDevs, common.GenReason(reason, len(devices))
}
