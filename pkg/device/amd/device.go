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

package amd

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type AMDDevices struct {
	resourceCountName  string
	resourceMemoryName string
	resourceCoreName   string
}

const (
	AMDDevice          = "AMD"
	AMDCommonWord      = "AMD"
	AMDDeviceSelection = "amd.com/gpu-index"
	AMDInUse           = "amd.com/use-gputype"
	AMDNoUse           = "amd.com/nouse-gputype"
	AMDUseUUID         = "amd.com/use-gpu-uuid"
	AMDNoUseUUID       = "amd.com/nouse-gpu-uuid"
	AMDAssignedNode    = "amd.com/predicate-node"
	NodeLockAMD        = "hami.io/mutex.lock"
	RegisterAnnos      = "hami.io/node-amd-register"
)

type AMDConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
}

func InitAMDGPUDevice(config AMDConfig) *AMDDevices {
	_, ok := device.InRequestDevices[AMDDevice]
	if !ok {
		device.InRequestDevices[AMDDevice] = "hami.io/amd-devices-to-allocate"
		device.SupportDevices[AMDDevice] = "hami.io/amd-devices-allocated"
	}
	return &AMDDevices{
		resourceCountName:  config.ResourceCountName,
		resourceMemoryName: config.ResourceMemoryName,
		resourceCoreName:   config.ResourceCoreName,
	}
}

func (dev *AMDDevices) CommonWord() string {
	return AMDCommonWord
}

func (dev *AMDDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(dev.resourceCountName)]
	if ok {
		countValue, countIsInteger := count.AsInt64()
		if !countIsInteger || countValue < 1 {
			return false, fmt.Errorf("%s must be a positive integer", dev.resourceCountName)
		}

		core, coreRequested := ctr.Resources.Limits[corev1.ResourceName(dev.resourceCoreName)]
		if coreRequested {
			corePercentage, coreIsInteger := core.AsInt64()
			if !coreIsInteger || corePercentage < 1 || corePercentage > 100 {
				return false, fmt.Errorf("%s must be an integer percentage between 1 and 100", dev.resourceCoreName)
			}
		}

	}
	if !ok && dev.resourceMemoryName != "" {
		_, ok = ctr.Resources.Limits[corev1.ResourceName(dev.resourceMemoryName)]
	}
	if !ok && dev.resourceCoreName != "" {
		_, ok = ctr.Resources.Limits[corev1.ResourceName(dev.resourceCoreName)]
	}
	klog.Infoln("MutateAdmission result", ok)
	return ok, nil
}

func (dev *AMDDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	devEncoded, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*device.DeviceInfo{}, errors.New("annos not found " + RegisterAnnos)
	}
	nodedevices, err := device.UnMarshalNodeDevices(devEncoded)
	if err != nil {
		klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, err
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no amd gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, errors.New("no gpu found on node")
	}
	for idx := range nodedevices {
		nodedevices[idx].DeviceVendor = AMDCommonWord
	}
	return nodedevices, nil
}

func (dev *AMDDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[AMDDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.InRequestDevices[AMDDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[AMDDevice]] = deviceStr
		klog.V(5).Infof("pod add annotation key [%s], values is [%s]", device.InRequestDevices[AMDDevice], deviceStr)
		klog.V(5).Infof("pod add annotation key [%s], values is [%s]", device.SupportDevices[AMDDevice], deviceStr)
	}
	klog.V(4).InfoS("annos", "input", (*annoinput))
	return *annoinput
}

func (dev *AMDDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.LockNode(n.Name, NodeLockAMD, p)
}

func (dev *AMDDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.ReleaseNodeLock(n.Name, NodeLockAMD, p, false)
}

func (dev *AMDDevices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(RegisterAnnos, nn)
}

func checkAMDType(annos map[string]string, cardType string) bool {
	cardType = strings.ToUpper(cardType)
	if inuse, ok := annos[AMDInUse]; ok && strings.TrimSpace(inuse) != "" {
		useTypes := strings.Split(inuse, ",")
		if !slices.ContainsFunc(useTypes, func(useType string) bool {
			useType = strings.TrimSpace(useType)
			return useType != "" && strings.Contains(cardType, strings.ToUpper(useType))
		}) {
			return false
		}
	}
	if noUse, ok := annos[AMDNoUse]; ok && strings.TrimSpace(noUse) != "" {
		noUseTypes := strings.Split(noUse, ",")
		if slices.ContainsFunc(noUseTypes, func(noUseType string) bool {
			noUseType = strings.TrimSpace(noUseType)
			return noUseType != "" && strings.Contains(cardType, strings.ToUpper(noUseType))
		}) {
			return false
		}
	}
	return true
}

func (dev *AMDDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.EqualFold(n.Type, AMDDevice) {
		return true, checkAMDType(annos, d.Type), false
	}
	return false, false, false
}

func (dev *AMDDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	if dev.resourceCountName == "" {
		return true, true
	}
	gpuCount, ok := n.Status.Capacity.Name(corev1.ResourceName(dev.resourceCountName), resource.DecimalSI).AsInt64()
	if !ok {
		return false, false
	}
	if gpuCount == 0 {
		return false, false
	}
	return true, true
}

func (dev *AMDDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  dev.resourceCountName,
		ResourceMemoryName: dev.resourceMemoryName,
		ResourceCoreName:   dev.resourceCoreName,
	}
}

func (dev *AMDDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count AMD devices for container ", ctr.Name)
	amdResourceCount := corev1.ResourceName(dev.resourceCountName)
	amdResourceMemory := corev1.ResourceName(dev.resourceMemoryName)
	amdResourceCore := corev1.ResourceName(dev.resourceCoreName)
	count, ok := ctr.Resources.Limits[amdResourceCount]
	if ok {
		if n, ok := count.AsInt64(); ok {
			if n <= 0 || n > math.MaxInt32 {
				klog.ErrorS(nil, "amd device count request is out of range", "container", ctr.Name, "request", n)
				return device.ContainerDeviceRequest{}
			}
			memnum := int32(0)
			mem, memOK := ctr.Resources.Limits[amdResourceMemory]
			if memOK {
				memnums, ok := mem.AsInt64()
				if !ok || memnums < 0 || memnums > math.MaxInt32 {
					klog.ErrorS(nil, "amd device memory request is out of range", "container", ctr.Name, "request", mem.String())
					return device.ContainerDeviceRequest{}
				}
				memnum = int32(memnums)
			}

			// An omitted core limit means the container receives all CUs on each
			// allocated GPU. This also keeps memory-only AMD requests valid.
			corePercentageNum := int32(100)
			corePercentage, corePercentageOK := ctr.Resources.Limits[amdResourceCore]
			if corePercentageOK {
				corePercentageNums, ok := corePercentage.AsInt64()
				if !ok || corePercentageNums < 1 || corePercentageNums > 100 {
					klog.ErrorS(nil, "amd device core percentage request is out of range", "container", ctr.Name, "request", corePercentage.String())
					return device.ContainerDeviceRequest{}
				}
				corePercentageNum = int32(corePercentageNums)
			}

			klog.InfoS("Detected AMD device request",
				"container", ctr.Name,
				"deviceCount", n)
			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             AMDDevice,
				Memreq:           memnum,
				MemPercentagereq: 0,
				Coresreq:         corePercentageNum,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *AMDDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *AMDDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (amddevice *AMDDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeinfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	isMutex := util.PolicyContains(util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, pod), util.GPUSchedulerPolicyMutex)
	for i, v := range slices.Backward(devices) {
		dev := v
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)
		if !dev.Health {
			reason[common.CardNotHealth]++
			klog.V(5).InfoS(common.CardNotHealth, "pod", klog.KObj(pod), "device", dev.ID, "health", dev.Health)
			continue
		}
		klog.V(3).InfoS("Type check", "device", dev.Type, "req", k.Type, "dev=", dev)
		_, found, _ := amddevice.checkType(pod.GetAnnotations(), *dev, k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, AMDUseUUID, AMDNoUseUUID, amddevice.CommonWord()) {
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
		memReq := k.Memreq
		if memReq <= 0 && dev.Totalmem > 0 {
			memReq = dev.Totalmem
		}
		if dev.Totalmem-dev.Usedmem < memReq {
			reason[common.CardInsufficientMemory]++
			klog.V(5).InfoS(common.CardInsufficientMemory, "pod", klog.KObj(pod), "device", dev.ID, "device total memory", dev.Totalmem, "device used memory", dev.Usedmem, "request memory", memReq)
			continue
		}

		coreReq := int32(0)
		if k.Coresreq > 0 {
			if dev.Totalcore <= 0 {
				reason[common.CardInsufficientCore]++
				continue
			}
			coreReq = dev.Totalcore * k.Coresreq / 100
			coreReq = max(coreReq, 1)
			coreReq = min(coreReq, dev.Totalcore)
		} else if dev.Totalmem > 0 && memReq >= dev.Totalmem {
			// Memreq omitted or zero means whole-card memory; treat core request as whole-card as well.
			coreReq = dev.Totalcore
		}
		if dev.Totalcore-dev.Usedcores < coreReq {
			reason[common.CardInsufficientCore]++
			klog.V(5).InfoS(common.CardInsufficientCore, "pod", klog.KObj(pod), "device", dev.ID, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", coreReq)
			continue
		}

		klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)

		if k.Nums > 0 {
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:  int(dev.Index),
				UUID: dev.ID,
				// Keep the map keyed by the logical AMD device type, but retain the
				// registered product type in the allocation annotation. Consumers of
				// the annotation (for example workload GPU reporting) need the latter
				// to identify the actual AMD model.
				Type:      dev.Type,
				Usedmem:   memReq,
				Usedcores: coreReq,
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
