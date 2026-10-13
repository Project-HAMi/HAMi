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
	"strconv"
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
	resourceCountName            string
	resourceMemoryName           string
	resourceMemoryPercentageName string
	resourceCoreName             string
	runtimeClassName             string
	defaultGPUNum                int64
	defaultMemory                int64
	defaultCores                 int64
}

const (
	AMDDevice          = "AMD"
	AMDCommonWord      = "AMD"
	AMDDeviceSelection = "amd.com/gpu-index"
	AMDInUse           = "amd.com/use-gputype"
	AMDNoUse           = "amd.com/nouse-gputype"
	AMDUseUUID         = "amd.com/use-gpu-uuid"
	AMDNoUseUUID       = "amd.com/nouse-gpu-uuid"
	// AMDNumaBind asks for every GPU of a multi-GPU request to sit on one NUMA node.
	AMDNumaBind = "amd.com/numa-bind"
	// AMDLinkBind asks for every GPU of a multi-GPU request to be linked to all the others over XGMI.
	AMDLinkBind     = "amd.com/link-bind"
	AMDAssignedNode = "amd.com/predicate-node"
	NodeLockAMD     = "hami.io/mutex.lock"
	RegisterAnnos   = "hami.io/node-amd-register"
)

type AMDConfig struct {
	ResourceCountName            string `yaml:"resourceCountName"`
	ResourceMemoryName           string `yaml:"resourceMemoryName"`
	ResourceMemoryPercentageName string `yaml:"resourceMemoryPercentageName"`
	ResourceCoreName             string `yaml:"resourceCoreName"`
	// RuntimeClassName is set on the Pod when the user left spec.runtimeClassName empty.
	RuntimeClassName string `yaml:"runtimeClassName"`
	// DefaultGPUNum is how many GPUs a container gets when it asks for memory or
	// cores but not a count. 0 means 1.
	DefaultGPUNum int64 `yaml:"defaultGPUNum"`
	// DefaultMemory (MiB) is the memory of a container that asks for neither memory nor
	// a percentage; 0 leaves it the whole GPU.
	DefaultMemory int64 `yaml:"defaultMemory"`
	// DefaultCores (percent, 1-100) is the cores of a container that asks for none; 0 leaves it all of them.
	DefaultCores int64 `yaml:"defaultCores"`
}

func InitAMDGPUDevice(config AMDConfig) *AMDDevices {
	_, ok := device.InRequestDevices[AMDDevice]
	if !ok {
		device.InRequestDevices[AMDDevice] = "hami.io/amd-devices-to-allocate"
		device.SupportDevices[AMDDevice] = "hami.io/amd-devices-allocated"
	}
	return &AMDDevices{
		resourceCountName:            config.ResourceCountName,
		resourceMemoryName:           config.ResourceMemoryName,
		resourceMemoryPercentageName: config.ResourceMemoryPercentageName,
		resourceCoreName:             config.ResourceCoreName,
		runtimeClassName:             config.RuntimeClassName,
		defaultGPUNum:                max(config.DefaultGPUNum, 1),
		defaultMemory:                max(config.DefaultMemory, 0),
		defaultCores:                 min(max(config.DefaultCores, 0), 100),
	}
}

func (dev *AMDDevices) CommonWord() string {
	return AMDCommonWord
}

func (dev *AMDDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(dev.resourceCountName)]
	if ok {
		core, coreRequested := ctr.Resources.Limits[corev1.ResourceName(dev.resourceCoreName)]
		if coreRequested {
			corePercentage, coreIsInteger := core.AsInt64()
			if !coreIsInteger || corePercentage < 1 || corePercentage > 100 {
				return false, fmt.Errorf("%s must be an integer percentage between 1 and 100", dev.resourceCoreName)
			}
		}
	}
	if _, err := dev.memoryPercentage(ctr); err != nil {
		return false, err
	}
	if !ok {
		// Memory, a percentage or cores without a count still need a card: give the default number.
		asked := false
		for _, name := range []string{dev.resourceMemoryName, dev.resourceMemoryPercentageName, dev.resourceCoreName} {
			if name == "" {
				continue
			}
			_, inLimits := ctr.Resources.Limits[corev1.ResourceName(name)]
			_, inRequests := ctr.Resources.Requests[corev1.ResourceName(name)]
			asked = asked || inLimits || inRequests
		}
		if asked {
			if ctr.Resources.Limits == nil {
				ctr.Resources.Limits = corev1.ResourceList{}
			}
			count := *resource.NewQuantity(dev.defaultGPUNum, resource.DecimalSI)
			if requested, inRequests := ctr.Resources.Requests[corev1.ResourceName(dev.resourceCountName)]; inRequests {
				// A request-only count must match the limit we add, so keep it.
				count = requested
			}
			ctr.Resources.Limits[corev1.ResourceName(dev.resourceCountName)] = count
			ok = true
		}
	}
	if ok && p.Spec.RuntimeClassName == nil && dev.runtimeClassName != "" {
		p.Spec.RuntimeClassName = &dev.runtimeClassName
	}
	klog.Infoln("MutateAdmission result", ok)
	return ok, nil
}

func (dev *AMDDevices) hasResource(ctr *corev1.Container, name string) bool {
	if name == "" {
		return false
	}
	_, inLimits := ctr.Resources.Limits[corev1.ResourceName(name)]
	_, inRequests := ctr.Resources.Requests[corev1.ResourceName(name)]
	return inLimits || inRequests
}

// memoryPercentage returns the requested share of device memory, 0 when unset.
func (dev *AMDDevices) memoryPercentage(ctr *corev1.Container) (int64, error) {
	if dev.resourceMemoryPercentageName == "" {
		return 0, nil
	}
	name := corev1.ResourceName(dev.resourceMemoryPercentageName)
	q, ok := ctr.Resources.Limits[name]
	if !ok {
		q, ok = ctr.Resources.Requests[name]
	}
	if !ok {
		return 0, nil
	}
	pct, isInt := q.AsInt64()
	if !isInt || pct < 0 || pct > 100 {
		return 0, fmt.Errorf("invalid %s value %s in container %s: must be an integer between 0 and 100", dev.resourceMemoryPercentageName, q.String(), ctr.Name)
	}
	return pct, nil
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
		// The scheduler picks a node's devices for a request by a substring of
		// the registered type, so a registration without a type would never
		// match and the node would be rejected as having no device.
		if nodedevices[idx].Type == "" {
			nodedevices[idx].Type = AMDDevice
		}
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
	if !device.PodRequiresDevice(dev, p) {
		return nil
	}
	return nodelock.LockNode(n.Name, NodeLockAMD, p)
}

func (dev *AMDDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	if !device.PodRequiresDevice(dev, p) {
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

func assertLinkBind(annos map[string]string) bool {
	enforce, err := strconv.ParseBool(annos[AMDLinkBind])
	return err == nil && enforce
}

// xgmiLinked reports whether two GPUs list each other as XGMI peers in the
// registration (custominfo.xgmiPeers, published by amd-device-plugin).
func xgmiLinked(a, b *device.DeviceUsage) bool {
	return hasPeer(a, b) && hasPeer(b, a)
}

func hasPeer(from, to *device.DeviceUsage) bool {
	bdf, _ := to.CustomInfo["pciBDF"].(string)
	if bdf == "" {
		return false
	}
	switch peers := from.CustomInfo["xgmiPeers"].(type) {
	case []string:
		return slices.Contains(peers, bdf)
	case []any:
		for _, p := range peers {
			if s, ok := p.(string); ok && s == bdf {
				return true
			}
		}
	}
	return false
}

func assertNuma(annos map[string]string) bool {
	enforce, err := strconv.ParseBool(annos[AMDNumaBind])
	return err == nil && enforce
}

func (dev *AMDDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.EqualFold(n.Type, AMDDevice) {
		return true, checkAMDType(annos, d.Type), assertNuma(annos)
	}
	return false, false, false
}

func (dev *AMDDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	if dev.resourceCountName == "" {
		return true, true
	}
	// Allocatable, not capacity: kubelet keeps an unhealthy GPU in capacity and
	// drops it from allocatable, so only allocatable says whether a pod bound
	// here can still be admitted.
	gpuCount, ok := n.Status.Allocatable.Name(corev1.ResourceName(dev.resourceCountName), resource.DecimalSI).AsInt64()
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

func (dev *AMDDevices) GenerateResourceRequests(ctr *corev1.Container) (device.ContainerDeviceRequest, error) {
	klog.Info("Start to count AMD devices for container ", ctr.Name)
	amdResourceCount := corev1.ResourceName(dev.resourceCountName)
	amdResourceMemory := corev1.ResourceName(dev.resourceMemoryName)
	amdResourceCore := corev1.ResourceName(dev.resourceCoreName)
	count, ok := ctr.Resources.Limits[amdResourceCount]
	if ok {
		if n, ok := count.AsInt64(); ok {
			if n == 0 {
				// An explicit zero count means no device is requested,
				// not an invalid request. See the nvidia backend.
				return device.ContainerDeviceRequest{}, nil
			}
			if n < 0 {
				klog.ErrorS(nil, "amd device count request is out of range", "container", ctr.Name, "request", n)
				return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "amd", Reason: fmt.Sprintf("device count %d is out of range", n)}
			}
			memnum := int64(0)
			mem, memOK := ctr.Resources.Limits[amdResourceMemory]
			if !memOK {
				mem, memOK = ctr.Resources.Requests[amdResourceMemory]
			}
			if memOK {
				memnums, ok := mem.AsInt64()
				if !ok || memnums < 0 {
					klog.ErrorS(nil, "amd device memory request is out of range", "container", ctr.Name, "request", mem.String())
					return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "amd", Reason: fmt.Sprintf("memory request %s is out of range", mem.String())}
				}
				memnum = memnums
			}

			memPercentage, err := dev.memoryPercentage(ctr)
			if err != nil {
				klog.ErrorS(err, "amd device memory percentage request is out of range", "container", ctr.Name)
				return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "amd", Reason: err.Error()}
			}

			// the default memory applies only when the container names no memory at all, so an explicit 0 stays a whole GPU
			if !memOK && !dev.hasResource(ctr, dev.resourceMemoryPercentageName) && dev.defaultMemory > 0 {
				memnum = dev.defaultMemory
			}

			// An omitted core limit means the container receives all CUs on each
			// allocated GPU (or the configured default). This also keeps memory-only
			// AMD requests valid.
			corePercentageNum := int64(100)
			if dev.defaultCores > 0 {
				corePercentageNum = dev.defaultCores
			}
			corePercentage, corePercentageOK := ctr.Resources.Limits[amdResourceCore]
			if !corePercentageOK {
				corePercentage, corePercentageOK = ctr.Resources.Requests[amdResourceCore]
			}
			if corePercentageOK {
				corePercentageNums, ok := corePercentage.AsInt64()
				if !ok || corePercentageNums < 1 || corePercentageNums > 100 {
					klog.ErrorS(nil, "amd device core percentage request is out of range", "container", ctr.Name, "request", corePercentage.String())
					return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "amd", Reason: fmt.Sprintf("core percentage request %s is out of range (must be an integer between 1 and 100)", corePercentage.String())}
				}
				corePercentageNum = corePercentageNums
			}

			klog.InfoS("Detected AMD device request",
				"container", ctr.Name,
				"deviceCount", n)
			return device.ContainerDeviceRequest{
				Nums:             n,
				Type:             AMDDevice,
				Memreq:           memnum,
				MemPercentagereq: memPercentage,
				Coresreq:         corePercentageNum,
			}, nil
		}
		// A quantity the apiserver accepts as an integer can still be too
		// large for int64 (1Ei, 1e19). Falling through would report the
		// container as device-less, which is the fail-open this change
		// exists to remove.
		klog.ErrorS(nil, "amd device count request is not a plain integer", "container", ctr.Name, "request", count.String())
		return device.ContainerDeviceRequest{}, &device.ErrInvalidDeviceRequest{Container: ctr.Name, Device: "amd", Reason: fmt.Sprintf("device count %s is not a plain integer", count.String())}
	}
	return device.ContainerDeviceRequest{}, nil
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

// fitQuota checks the pod's total AMD memory, this candidate card included,
// against the namespace ResourceQuota. Only memory is checked: the core quota
// counts the CUs the plugin masks while a request is a percentage.
func fitQuota(pod *corev1.Pod, tmpDevs map[string]device.ContainerDevices, allocated *device.PodDevices, ns, devUUID string, memreq int64) bool {
	hypo := device.PodDevices{}
	if allocated != nil {
		for devType, podSingle := range *allocated {
			hypo[devType] = append(device.PodSingleDevice{}, podSingle...)
		}
	}
	cur := append(device.ContainerDevices{}, tmpDevs[AMDDevice]...)
	cur = append(cur, device.ContainerDevice{UUID: devUUID, Type: AMDDevice, Usedmem: memreq})
	hypo[AMDDevice] = append(hypo[AMDDevice], cur)

	var mem int64
	for _, ctrDevs := range device.CollapseInitContainerUsage(pod, hypo)[AMDDevice] {
		for _, val := range ctrDevs {
			mem += val.Usedmem
		}
	}
	return device.GetLocalCache().FitQuota(ns, mem, 1, 0, AMDDevice)
}

func (amddevice *AMDDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeinfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	if !assertLinkBind(pod.GetAnnotations()) || request.Nums < 2 {
		return amddevice.fit(devices, request, pod, nodeinfo, allocated, false)
	}
	// link-bind: the group has to be a clique of XGMI links. A greedy pass can
	// be led astray by its first pick, so try each GPU as the starting point.
	// ponytail: greedy per start, fine for 4 or 8 GPU hives; exact clique search if hives grow.
	var firstReason string
	var firstDevs map[string]device.ContainerDevices
	for start := range devices {
		ordered := append(slices.Clone(devices[start:]), devices[:start]...)
		ok, devs, reason := amddevice.fit(ordered, request, pod, nodeinfo, allocated, true)
		if ok {
			return true, devs, ""
		}
		if start == 0 {
			firstDevs, firstReason = devs, reason
		}
	}
	return false, firstDevs, firstReason
}

func (amddevice *AMDDevices) fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeinfo *device.NodeInfo, allocated *device.PodDevices, linkBind bool) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	if k.Coresreq > 100 || k.Coresreq < 0 {
		klog.ErrorS(nil, "core limit out of range (must be 0-100)", "pod", klog.KObj(pod), "coresreq", k.Coresreq)
		return false, tmpDevs, "core limit out of range"
	}
	gpuPolicy := util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, pod)
	isMutex := util.PolicyContains(gpuPolicy, util.GPUSchedulerPolicyMutex)
	// topology-aware: collect every GPU that fits and take the best linked combination,
	// instead of the first ones found. A hard link-bind already demands a clique.
	topology := util.PolicyContains(gpuPolicy, util.GPUSchedulerPolicyTopology) && originReq > 1 && !linkBind
	var cordoned map[string]struct{}
	if nodeinfo != nil && nodeinfo.Node != nil {
		cordoned = common.CordonedDevices(nodeinfo.Node.Annotations)
	}
	for i, v := range slices.Backward(devices) {
		dev := v
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)
		if !dev.Health {
			reason[common.CardNotHealth]++
			klog.V(5).InfoS(common.CardNotHealth, "pod", klog.KObj(pod), "device", dev.ID, "health", dev.Health)
			continue
		}
		if _, isCordoned := cordoned[dev.ID]; isCordoned {
			reason[common.CardCordoned]++
			klog.V(5).InfoS(common.CardCordoned, "pod", klog.KObj(pod), "device", dev.ID)
			continue
		}
		klog.V(3).InfoS("Type check", "device", dev.Type, "req", k.Type, "dev=", dev)
		_, found, numa := amddevice.checkType(pod.GetAnnotations(), *dev, k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		// numa-bind: a run of GPUs must share one NUMA node, so a new node starts the run over.
		if numa && !topology && prevnuma != dev.Numa {
			if k.Nums != originReq {
				reason[common.NumaNotFit] += len(tmpDevs[k.Type])
				klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", dev.ID, "k.nums", k.Nums, "prevnuma", prevnuma, "device numa", dev.Numa)
			}
			k.Nums = originReq
			prevnuma = dev.Numa
			tmpDevs = make(map[string]device.ContainerDevices)
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
		if memReq <= 0 && k.MemPercentagereq > 0 && dev.Totalmem > 0 {
			memReq = max(dev.Totalmem*k.MemPercentagereq/100, 1)
		}
		if memReq <= 0 && dev.Totalmem > 0 {
			memReq = dev.Totalmem
		}
		// topology mode collects more candidates than it allocates, so a candidate is
		// checked alone here and the chosen combination is checked as a whole below
		quotaDevs := tmpDevs
		if topology {
			quotaDevs = nil
		}
		if !fitQuota(pod, quotaDevs, allocated, pod.Namespace, dev.ID, memReq) {
			reason[common.ResourceQuotaNotFit]++
			klog.V(3).InfoS(common.ResourceQuotaNotFit, "pod", klog.KObj(pod), "memreq", memReq)
			continue
		}
		if dev.Totalmem-dev.Usedmem < memReq {
			reason[common.CardInsufficientMemory]++
			klog.V(5).InfoS(common.CardInsufficientMemory, "pod", klog.KObj(pod), "device", dev.ID, "device total memory", dev.Totalmem, "device used memory", dev.Usedmem, "request memory", memReq)
			continue
		}

		coreReq := int64(0)
		if k.Coresreq > 0 {
			if dev.Totalcore <= 0 {
				reason[common.CardInsufficientCore]++
				continue
			}
			coreReq = dev.Totalcore * k.Coresreq / 100
			coreReq = max(coreReq, 1)
			// RDNA applies the CU mask per WGP, so the device plugin hands out
			// whole WGPs; account for the same count.
			if unit := cuPerWGP(dev.CustomInfo); unit > 1 {
				coreReq = min((coreReq+unit-1)/unit*unit, dev.Totalcore)
			}
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

		if linkBind {
			linked := true
			for _, c := range tmpDevs[k.Type] {
				if i := slices.IndexFunc(devices, func(d *device.DeviceUsage) bool { return d.ID == c.UUID }); i < 0 || !xgmiLinked(devices[i], dev) {
					linked = false
					break
				}
			}
			if !linked {
				reason[common.GPULinkNotFit]++
				klog.V(5).InfoS(common.GPULinkNotFit, "pod", klog.KObj(pod), "device", dev.ID)
				continue
			}
		}

		klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)

		if k.Nums > 0 {
			if !topology {
				k.Nums--
			}
			// Keep the map keyed by the logical AMD device type, but retain the
			// registered product type in the allocation annotation. Consumers of
			// the annotation (for example workload GPU reporting) need the latter
			// to identify the actual AMD model. The registered type comes from an
			// external node annotation and can be empty; DecodeContainerDevices
			// rejects an empty type, so fall back to the logical type to keep
			// the allocation annotation readable.
			allocType := dev.Type
			if allocType == "" {
				allocType = k.Type
			}
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:       int(dev.Index),
				UUID:      dev.ID,
				Type:      allocType,
				Usedmem:   memReq,
				Usedcores: coreReq,
			})
		}
		if k.Nums == 0 && !topology {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}
	}
	if topology && len(tmpDevs[k.Type]) >= int(originReq) {
		if best, ok := bestLinkedCombination(devices, tmpDevs[k.Type], int(originReq), assertNuma(pod.GetAnnotations())); ok {
			for i, c := range best {
				if !fitQuota(pod, map[string]device.ContainerDevices{AMDDevice: best[:i]}, allocated, pod.Namespace, c.UUID, c.Usedmem) {
					reason[common.ResourceQuotaNotFit]++
					return false, tmpDevs, common.GenReason(reason, len(devices))
				}
			}
			tmpDevs[k.Type] = best
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "best device combination", tmpDevs)
			return true, tmpDevs, ""
		}
		reason[common.NumaNotFit] += len(tmpDevs[k.Type])
	}
	if len(tmpDevs[k.Type]) > 0 {
		reason[common.AllocatedCardsInsufficientRequest] = len(tmpDevs[k.Type])
		klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs[k.Type]))
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}

const (
	xgmiPairScore = 4 // an XGMI link outweighs sharing a NUMA node
	numaPairScore = 1
	// Past this many candidates the number of combinations is no longer small; take the first ones.
	maxCombinationCandidates = 16
)

// bestLinkedCombination picks n of the candidate GPUs that are most tightly
// connected: each XGMI-linked pair counts xgmiPairScore and each pair on one
// known NUMA node numaPairScore. With numaBind every pick has to share a NUMA
// node, so each NUMA node is searched on its own. A group of more than
// maxCombinationCandidates takes its first n. Ties keep the earliest candidates.
func bestLinkedCombination(devices []*device.DeviceUsage, candidates device.ContainerDevices, n int, numaBind bool) (device.ContainerDevices, bool) {
	byID := make(map[string]*device.DeviceUsage, len(devices))
	for _, d := range devices {
		byID[d.ID] = d
	}
	groups := []device.ContainerDevices{candidates}
	if numaBind {
		groups = groups[:0]
		index := map[int]int{}
		for _, c := range candidates {
			numa := -1
			if d := byID[c.UUID]; d != nil {
				numa = d.Numa
			}
			g, ok := index[numa]
			if !ok {
				g = len(groups)
				index[numa] = g
				groups = append(groups, nil)
			}
			groups[g] = append(groups[g], c)
		}
	}
	var out device.ContainerDevices
	bestScore := -1
	for _, group := range groups {
		if len(group) < n {
			continue
		}
		picked, score := bestInGroup(byID, group, n)
		if score > bestScore {
			bestScore, out = score, picked
		}
	}
	return out, out != nil
}

func pairScore(a, b *device.DeviceUsage) int {
	score := 0
	if a != nil && b != nil && xgmiLinked(a, b) {
		score += xgmiPairScore
	}
	if a != nil && b != nil && a.Numa >= 0 && a.Numa == b.Numa {
		score += numaPairScore
	}
	return score
}

func groupScore(byID map[string]*device.DeviceUsage, group device.ContainerDevices) int {
	score := 0
	for i := range group {
		for j := i + 1; j < len(group); j++ {
			score += pairScore(byID[group[i].UUID], byID[group[j].UUID])
		}
	}
	return score
}

// bestInGroup returns the n devices of group with the best score.
func bestInGroup(byID map[string]*device.DeviceUsage, group device.ContainerDevices, n int) (device.ContainerDevices, int) {
	if len(group) > maxCombinationCandidates {
		first := slices.Clone(group[:n])
		return first, groupScore(byID, first)
	}
	var best []int
	bestScore := -1
	pick := make([]int, 0, n)
	var walk func(start int)
	walk = func(start int) {
		if len(pick) == n {
			chosen := make(device.ContainerDevices, 0, n)
			for _, i := range pick {
				chosen = append(chosen, group[i])
			}
			if score := groupScore(byID, chosen); score > bestScore {
				bestScore, best = score, slices.Clone(pick)
			}
			return
		}
		for i := start; i <= len(group)-(n-len(pick)); i++ {
			pick = append(pick, i)
			walk(i + 1)
			pick = pick[:len(pick)-1]
		}
	}
	walk(0)
	out := make(device.ContainerDevices, 0, n)
	for _, i := range best {
		out = append(out, group[i])
	}
	return out, bestScore
}

// cuPerWGP returns how many CUs the device plugin allocates together, as
// published in the registration custominfo; 1 when absent.
func cuPerWGP(info map[string]any) int64 {
	if v, ok := info["cuPerWGP"].(float64); ok && v > 1 && v < math.MaxInt64 && v == math.Trunc(v) {
		return int64(v)
	}
	return 1
}
