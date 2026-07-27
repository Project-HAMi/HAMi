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

package enflame

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type EnflameDevices struct{}

const (
	EnflameVGCUDevice     = "Enflame"
	EnflameVGCUCommonWord = "Enflame"
	// EnflameUseUUID annotation specifies a comma-separated list of Enflame UUIDs to use.
	EnflameUseUUID = "enflame.com/use-gpuuuid"
	// EnflameNoUseUUID annotation specifies a comma-separated list of Enflame UUIDs to exclude.
	EnflameNoUseUUID   = "enflame.com/nouse-gpuuuid"
	PodRequestGCUSize  = "enflame.com/gcu-request-size"
	PodAssignedGCUID   = "enflame.com/gcu-assigned-id"
	PodHasAssignedGCU  = "enflame.com/gcu-assigned"
	PodAssignedGCUIdx  = "enflame.com/gcu-assigned-index"
	PodAssignedGCUMin  = "enflame.com/gcu-assigned-minor"
	PodAssignedGCUTime = "enflame.com/gcu-assigned-time"
	AssignedContainers = "assigned-containers"
	GCUDrsCapacity     = "enflame.com/gcu-drs-capacity"

	SharedResourceName = "enflame.com/shared-gcu"
	CountNoSharedName  = "enflame.com/gcu-count"

	enflameRequestModeDirect  int32 = 0
	enflameRequestModeBySpec  int32 = -1
	enflameUnknownCoreRequest int32 = 0
)

type drsCapacitySpec struct {
	Devices  []drsDeviceSpec   `json:"devices"`
	Profiles map[string]string `json:"profiles"`
}

type drsDeviceSpec struct {
	Index    string `json:"index"`
	Minor    string `json:"minor"`
	Capacity any    `json:"capacity"`
}

type assignedContainerInfo struct {
	Allocated    bool   `json:"allocated"`
	Request      int32  `json:"request"`
	ProfileID    string `json:"profileID,omitempty"`
	ProfileName  string `json:"profileName,omitempty"`
	InstanceID   string `json:"instanceID,omitempty"`
	InstanceUUID string `json:"instanceUUID,omitempty"`
}

func InitEnflameDevice(config EnflameConfig) *EnflameDevices {
	EnflameResourceNameDRSGCU = config.ResourceNameDRSGCU
	if EnflameResourceNameDRSGCU == "" {
		EnflameResourceNameDRSGCU = config.ResourceNameVGCU
	}
	if EnflameResourceNameDRSGCU == "" {
		EnflameResourceNameDRSGCU = "enflame.com/drs-gcu"
	}
	EnflameResourceNameGCUMemory = config.ResourceNameMemory
	if EnflameResourceNameGCUMemory == "" {
		EnflameResourceNameGCUMemory = "enflame.com/gcu-memory"
	}
	EnflameResourceNameGCUCore = config.ResourceNameCore
	if EnflameResourceNameGCUCore == "" {
		EnflameResourceNameGCUCore = "enflame.com/gcu-core"
	}
	EnflameResourceNameVGCU = config.ResourceNameVGCU
	EnflameResourceNameVGCUPercentage = config.ResourceNameVGCUPercentage
	_, ok := device.SupportDevices[EnflameVGCUDevice]
	if !ok {
		device.SupportDevices[EnflameVGCUDevice] = "hami.io/enflame-vgpu-devices-allocated"
	}
	return &EnflameDevices{}
}

func (dev *EnflameDevices) CommonWord() string {
	return EnflameVGCUCommonWord
}

func (dev *EnflameDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	resourceCount := corev1.ResourceName(EnflameResourceNameDRSGCU)
	count, ok := ctr.Resources.Limits[resourceCount]
	if !ok {
		count, ok = ctr.Resources.Requests[resourceCount]
	}
	if ctr.Resources.Limits == nil {
		ctr.Resources.Limits = corev1.ResourceList{}
	}
	if ctr.Resources.Requests == nil {
		ctr.Resources.Requests = corev1.ResourceList{}
	}

	// Direct DRS API: enflame.com/drs-gcu
	if ok {
		if count.Value() <= 0 {
			return false, fmt.Errorf("%s must be greater than 0", EnflameResourceNameDRSGCU)
		}
		if _, exists := ctr.Resources.Limits[resourceCount]; !exists {
			ctr.Resources.Limits[resourceCount] = count
		}
		if _, exists := ctr.Resources.Requests[resourceCount]; !exists {
			ctr.Resources.Requests[resourceCount] = count
		}
		return true, nil
	}

	// Unified API: request by memory/core, then convert profile in Fit().
	memReq, hasMem := getContainerResourceRequest(ctr, corev1.ResourceName(EnflameResourceNameGCUMemory))
	coreReq, hasCore := getContainerResourceRequest(ctr, corev1.ResourceName(EnflameResourceNameGCUCore))
	if !hasMem && !hasCore {
		return false, nil
	}
	if hasMem && memReq <= 0 {
		return false, fmt.Errorf("%s must be greater than 0", EnflameResourceNameGCUMemory)
	}
	if hasCore && (coreReq <= 0 || coreReq > 100) {
		return false, fmt.Errorf("%s must be in range (0,100]", EnflameResourceNameGCUCore)
	}
	if hasMem {
		memQty := ctr.Resources.Limits[corev1.ResourceName(EnflameResourceNameGCUMemory)]
		if memQty.IsZero() {
			memQty = ctr.Resources.Requests[corev1.ResourceName(EnflameResourceNameGCUMemory)]
		}
		if _, exists := ctr.Resources.Limits[corev1.ResourceName(EnflameResourceNameGCUMemory)]; !exists {
			ctr.Resources.Limits[corev1.ResourceName(EnflameResourceNameGCUMemory)] = memQty
		}
		if _, exists := ctr.Resources.Requests[corev1.ResourceName(EnflameResourceNameGCUMemory)]; !exists {
			ctr.Resources.Requests[corev1.ResourceName(EnflameResourceNameGCUMemory)] = memQty
		}
	}
	if hasCore {
		coreQty := ctr.Resources.Limits[corev1.ResourceName(EnflameResourceNameGCUCore)]
		if coreQty.IsZero() {
			coreQty = ctr.Resources.Requests[corev1.ResourceName(EnflameResourceNameGCUCore)]
		}
		if _, exists := ctr.Resources.Limits[corev1.ResourceName(EnflameResourceNameGCUCore)]; !exists {
			ctr.Resources.Limits[corev1.ResourceName(EnflameResourceNameGCUCore)] = coreQty
		}
		if _, exists := ctr.Resources.Requests[corev1.ResourceName(EnflameResourceNameGCUCore)]; !exists {
			ctr.Resources.Requests[corev1.ResourceName(EnflameResourceNameGCUCore)] = coreQty
		}
	}
	return true, nil
}

func (dev *EnflameDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	capacityRaw, ok := n.Annotations[GCUDrsCapacity]
	if !ok {
		return []*device.DeviceInfo{}, fmt.Errorf("annotation not found %s", GCUDrsCapacity)
	}
	spec := drsCapacitySpec{}
	if err := json.Unmarshal([]byte(capacityRaw), &spec); err != nil {
		return []*device.DeviceInfo{}, fmt.Errorf("failed to parse %s: %w", GCUDrsCapacity, err)
	}
	maxSlice := 0
	maxMemGB := 0
	for profileName := range spec.Profiles {
		slice, memGB := parseProfile(profileName)
		if slice > maxSlice {
			maxSlice = slice
		}
		if memGB > maxMemGB {
			maxMemGB = memGB
		}
	}
	if maxSlice <= 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("no valid drs profiles found on node %s", n.Name)
	}
	if maxMemGB <= 0 {
		maxMemGB = maxSlice
	}
	nodedevices := make([]*device.DeviceInfo, 0, len(spec.Devices))
	for idx, d := range spec.Devices {
		devIndex, err := strconv.Atoi(d.Index)
		if err != nil {
			devIndex = idx
		}
		capacity, err := parseDRSCapacity(d.Capacity)
		if err != nil || capacity <= 0 {
			return []*device.DeviceInfo{}, fmt.Errorf("invalid drs capacity on node %s", n.Name)
		}
		minor := strings.TrimSpace(d.Minor)
		if minor == "" {
			minor = strconv.Itoa(devIndex)
		}
		profiles := map[string]string{}
		maps.Copy(profiles, spec.Profiles)
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:        uint(devIndex),
			ID:           fmt.Sprintf("%s-enflame-drs-%d", n.Name, devIndex),
			Count:        capacity,
			Devmem:       int32(maxMemGB * 1024),
			Devcore:      100,
			Type:         EnflameVGCUDevice,
			Numa:         0,
			Health:       true,
			DeviceVendor: EnflameVGCUCommonWord,
			CustomInfo: map[string]any{
				"minor":    minor,
				"index":    strconv.Itoa(devIndex),
				"profiles": profiles,
				"maxSlice": maxSlice,
			},
		})
	}
	if len(nodedevices) == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("no drs devices found on node %s", n.Name)
	}
	return nodedevices, nil
}

func (dev *EnflameDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[EnflameVGCUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[device.SupportDevices[EnflameVGCUDevice]] = device.EncodePodSingleDevice(devlist)
		(*annoinput)[PodHasAssignedGCU] = "false"
		(*annoinput)[PodAssignedGCUTime] = strconv.FormatInt(time.Now().UnixNano(), 10)

		assigned := map[string]assignedContainerInfo{}
		for ctridx, ctrDevices := range devlist {
			if len(ctrDevices) == 0 {
				continue
			}
			chosen := ctrDevices[0]
			slice := int32(readCustomInfoInt(chosen.CustomInfo, "drsSlice"))
			if slice <= 0 {
				slice = 1
			}
			ctrName := containerNameByIndex(pod, ctridx)
			profileName := readCustomInfoString(chosen.CustomInfo, "profileName")
			profileID := readCustomInfoString(chosen.CustomInfo, "profileID")
			assigned[ctrName] = assignedContainerInfo{
				Allocated:   false,
				Request:     slice,
				ProfileID:   profileID,
				ProfileName: profileName,
			}

			if _, exists := (*annoinput)[PodAssignedGCUIdx]; !exists {
				if index := readCustomInfoString(chosen.CustomInfo, "index"); index != "" {
					(*annoinput)[PodAssignedGCUIdx] = index
					(*annoinput)[PodAssignedGCUID] = index
				}
			}
			if _, exists := (*annoinput)[PodAssignedGCUMin]; !exists {
				if minor := readCustomInfoString(chosen.CustomInfo, "minor"); minor != "" {
					(*annoinput)[PodAssignedGCUMin] = minor
				}
			}
			if _, exists := (*annoinput)[PodRequestGCUSize]; !exists && slice > 0 {
				(*annoinput)[PodRequestGCUSize] = strconv.FormatInt(int64(slice), 10)
			}
		}
		if len(assigned) > 0 {
			if payload, err := json.Marshal(assigned); err != nil {
				klog.ErrorS(err, "failed to marshal assigned containers", "pod", klog.KObj(pod))
			} else {
				(*annoinput)[AssignedContainers] = string(payload)
			}
		}
	}
	return *annoinput
}

func (dev *EnflameDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *EnflameDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *EnflameDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *EnflameDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, EnflameVGCUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *EnflameDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *EnflameDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count enflame devices for container ", ctr.Name)
	resourceCount := corev1.ResourceName(EnflameResourceNameDRSGCU)
	v, ok := ctr.Resources.Limits[resourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[resourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok && n > 0 {
			klog.Info("Found enflame devices")
			if n > math.MaxInt32 {
				klog.ErrorS(nil, "drs request is too large", "container", ctr.Name, "request", n)
				return device.ContainerDeviceRequest{}
			}
			return device.ContainerDeviceRequest{
				Nums:             1,
				Type:             EnflameVGCUDevice,
				Memreq:           int32(n),
				MemPercentagereq: enflameRequestModeDirect,
				Coresreq:         enflameUnknownCoreRequest,
			}
		}
	}
	memReq, hasMem := getContainerResourceRequest(ctr, corev1.ResourceName(EnflameResourceNameGCUMemory))
	coreReq, hasCore := getContainerResourceRequest(ctr, corev1.ResourceName(EnflameResourceNameGCUCore))
	if !hasMem && !hasCore {
		return device.ContainerDeviceRequest{}
	}
	if hasMem && memReq > math.MaxInt32 {
		klog.ErrorS(nil, "gcu memory request is too large", "container", ctr.Name, "request", memReq)
		return device.ContainerDeviceRequest{}
	}
	if hasCore && coreReq > math.MaxInt32 {
		klog.ErrorS(nil, "gcu core request is too large", "container", ctr.Name, "request", coreReq)
		return device.ContainerDeviceRequest{}
	}
	klog.Info("Found enflame memory/core based request")
	return device.ContainerDeviceRequest{
		Nums:             1,
		Type:             EnflameVGCUDevice,
		Memreq:           int32(memReq),
		MemPercentagereq: enflameRequestModeBySpec,
		Coresreq:         int32(coreReq),
	}
}

func (dev *EnflameDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *EnflameDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	slice := int32(readCustomInfoInt(ctr.CustomInfo, "drsSlice"))
	if slice <= 0 {
		slice = 1
	}
	n.Used += slice
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (enf *EnflameDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	isMutex := util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, pod) == util.GPUSchedulerPolicyMutex.String()
	profile, profileMatch := enf.selectProfileByRequest(devices, k)
	if !profileMatch {
		reason[common.ModeNotFit]++
		return false, tmpDevs, common.GenReason(reason, len(devices))
	}
	requiredSlice := int32(profile.Size)
	if requiredSlice <= 0 {
		reason[common.ModeNotFit]++
		return false, tmpDevs, common.GenReason(reason, len(devices))
	}
	profileMemoryMiB := int32(profile.MemoryGB * 1024)
	if profileMemoryMiB <= 0 {
		reason[common.ModeNotFit]++
		return false, tmpDevs, common.GenReason(reason, len(devices))
	}
	profileCorePercent := int32(profile.CorePercent)
	if profileCorePercent <= 0 {
		profileCorePercent = 1
	}
	for i, v := range slices.Backward(devices) {
		dev := v
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, _ := enf.checkType(pod.GetAnnotations(), *dev, k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, EnflameUseUUID, EnflameNoUseUUID, enf.CommonWord()) {
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
		if dev.Totalmem-dev.Usedmem < profileMemoryMiB {
			reason[common.CardInsufficientMemory]++
			klog.V(5).InfoS(common.CardInsufficientMemory, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total memory", dev.Totalmem, "device used memory", dev.Usedmem, "request memory", profileMemoryMiB)
			continue
		}
		if dev.Totalcore > 0 && dev.Totalcore-dev.Usedcores < profileCorePercent {
			reason[common.CardInsufficientCore]++
			klog.V(5).InfoS(common.CardInsufficientCore, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "device total core", dev.Totalcore, "device used core", dev.Usedcores, "request cores", profileCorePercent)
			continue
		}
		if k.Nums > 0 {
			klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:       int(dev.Index),
				UUID:      dev.ID,
				Type:      k.Type,
				Usedmem:   profileMemoryMiB,
				Usedcores: profileCorePercent,
				CustomInfo: map[string]any{
					"profileName": profile.Name,
					"profileID":   profile.ID,
					"minor":       readCustomInfoString(dev.CustomInfo, "minor"),
					"index":       readCustomInfoString(dev.CustomInfo, "index"),
					"drsSlice":    profile.Size,
					"requestMem":  profile.RequestMemoryGB,
					"requestCore": profile.RequestCorePercent,
				},
			})
		}
		if k.Nums == 0 {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}

	}
	if len(tmpDevs[k.Type]) > 0 {
		reason[common.AllocatedCardsInsufficientRequest] = len(tmpDevs[k.Type])
		klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs))
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}

func (dev *EnflameDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  EnflameResourceNameDRSGCU,
		ResourceMemoryName: EnflameResourceNameGCUMemory,
		ResourceCoreName:   EnflameResourceNameGCUCore,
	}
}

type drsProfileCandidate struct {
	Name               string
	ID                 string
	Size               int
	MemoryGB           int
	CorePercent        int
	RequestMemoryGB    int
	RequestCorePercent int
}

func (dev *EnflameDevices) selectProfileByRequest(devices []*device.DeviceUsage, request device.ContainerDeviceRequest) (drsProfileCandidate, bool) {
	candidates := collectDRSProfiles(devices)
	if len(candidates) == 0 {
		return drsProfileCandidate{}, false
	}
	if request.MemPercentagereq == enflameRequestModeDirect {
		for _, c := range candidates {
			if c.Size == int(request.Memreq) {
				return c, true
			}
		}
		return drsProfileCandidate{}, false
	}

	maxMemGB := candidates[len(candidates)-1].MemoryGB
	requestMemGB := normalizeMemoryRequestToGB(request.Memreq, maxMemGB)
	requestCorePercent := int(request.Coresreq)

	for _, c := range candidates {
		if requestMemGB > 0 && c.MemoryGB < requestMemGB {
			continue
		}
		if requestCorePercent > 0 && c.CorePercent < requestCorePercent {
			continue
		}
		c.RequestMemoryGB = requestMemGB
		c.RequestCorePercent = requestCorePercent
		return c, true
	}
	return drsProfileCandidate{}, false
}

func parseProfilesFromCustomInfo(customInfo map[string]any) map[string]string {
	if customInfo == nil {
		return map[string]string{}
	}
	rawProfiles, ok := customInfo["profiles"]
	if !ok {
		return map[string]string{}
	}
	switch typed := rawProfiles.(type) {
	case map[string]string:
		return typed
	case map[string]any:
		res := map[string]string{}
		for name, profileID := range typed {
			profileIDStr, ok := profileID.(string)
			if !ok {
				continue
			}
			res[name] = profileIDStr
		}
		return res
	default:
		return map[string]string{}
	}
}

func collectDRSProfiles(devices []*device.DeviceUsage) []drsProfileCandidate {
	maxSlice := 0
	for _, devUsage := range devices {
		if candidateMaxSlice := readCustomInfoInt(devUsage.CustomInfo, "maxSlice"); candidateMaxSlice > maxSlice {
			maxSlice = candidateMaxSlice
		}
	}
	if maxSlice <= 0 {
		for _, devUsage := range devices {
			for profileName := range parseProfilesFromCustomInfo(devUsage.CustomInfo) {
				size, _ := parseProfile(profileName)
				if size > maxSlice {
					maxSlice = size
				}
			}
		}
	}
	if maxSlice <= 0 {
		return []drsProfileCandidate{}
	}

	seen := map[string]drsProfileCandidate{}
	for _, devUsage := range devices {
		for profileName, profileID := range parseProfilesFromCustomInfo(devUsage.CustomInfo) {
			size, memGB := parseProfile(profileName)
			if size <= 0 || memGB <= 0 {
				continue
			}
			corePercent := int(math.Ceil(float64(size) * 100 / float64(maxSlice)))
			seen[profileName] = drsProfileCandidate{
				Name:        profileName,
				ID:          profileID,
				Size:        size,
				MemoryGB:    memGB,
				CorePercent: corePercent,
			}
		}
	}
	candidates := make([]drsProfileCandidate, 0, len(seen))
	for _, c := range seen {
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Size == candidates[j].Size {
			return candidates[i].MemoryGB < candidates[j].MemoryGB
		}
		return candidates[i].Size < candidates[j].Size
	})
	return candidates
}

func parseProfile(profileName string) (int, int) {
	normalized := strings.ToLower(strings.TrimSpace(profileName))
	parts := strings.Split(normalized, ".")
	if len(parts) < 2 {
		return 0, 0
	}
	sizePart := strings.TrimSpace(parts[0])
	sizePart = strings.TrimSuffix(sizePart, "g")
	size, err := strconv.Atoi(sizePart)
	if err != nil {
		return 0, 0
	}
	memPart := strings.TrimSpace(parts[1])
	memPart = strings.TrimSuffix(memPart, "gb")
	memGB, err := strconv.Atoi(memPart)
	if err != nil {
		return size, 0
	}
	return size, memGB
}

func normalizeMemoryRequestToGB(rawMemory int32, maxProfileMemoryGB int) int {
	if rawMemory <= 0 {
		return 0
	}
	if maxProfileMemoryGB <= 0 {
		return int(rawMemory)
	}
	// If the request value is much larger than profile-GB units, treat it as MiB.
	if int(rawMemory) > maxProfileMemoryGB*2 {
		return int(math.Ceil(float64(rawMemory) / 1024.0))
	}
	return int(rawMemory)
}

func parseDRSCapacity(raw any) (int32, error) {
	switch typed := raw.(type) {
	case float64:
		return int32(typed), nil
	case int:
		return int32(typed), nil
	case int32:
		return typed, nil
	case int64:
		return int32(typed), nil
	case string:
		capacity, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, err
		}
		return int32(capacity), nil
	default:
		return 0, fmt.Errorf("unknown capacity type: %T", raw)
	}
}

func containerNameByIndex(pod *corev1.Pod, index int) string {
	if pod == nil {
		return fmt.Sprintf("container-%d", index)
	}
	initCount := len(pod.Spec.InitContainers)
	if index < initCount {
		return pod.Spec.InitContainers[index].Name
	}
	containerIdx := index - initCount
	if containerIdx >= 0 && containerIdx < len(pod.Spec.Containers) {
		return pod.Spec.Containers[containerIdx].Name
	}
	return fmt.Sprintf("container-%d", index)
}

func readCustomInfoString(customInfo map[string]any, key string) string {
	if customInfo == nil {
		return ""
	}
	raw, ok := customInfo[key]
	if !ok {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.Itoa(int(typed))
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func readCustomInfoInt(customInfo map[string]any, key string) int {
	if customInfo == nil {
		return 0
	}
	raw, ok := customInfo[key]
	if !ok {
		return 0
	}
	switch typed := raw.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		v, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0
		}
		return v
	default:
		return 0
	}
}

func getContainerResourceRequest(ctr *corev1.Container, resourceName corev1.ResourceName) (int64, bool) {
	if ctr == nil || resourceName == "" {
		return 0, false
	}
	if qty, ok := ctr.Resources.Limits[resourceName]; ok {
		return qty.Value(), true
	}
	if qty, ok := ctr.Resources.Requests[resourceName]; ok {
		return qty.Value(), true
	}
	return 0, false
}
