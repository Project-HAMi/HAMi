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

package nvidia

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

const (
	HandshakeAnnos       = "hami.io/node-handshake"
	RegisterAnnos        = "hami.io/node-nvidia-register"
	RegisterGPUPairScore = "hami.io/node-nvidia-score"
	NvidiaGPUDevice      = "NVIDIA"
	GPUInUse             = "nvidia.com/use-gputype"
	GPUNoUse             = "nvidia.com/nouse-gputype"
	NumaBind             = "nvidia.com/numa-bind"
	NodeLockNvidia       = "hami.io/mutex.lock"
	// GPUUseUUID annotation specifies a comma-separated list of GPU UUIDs to use.
	GPUUseUUID = "nvidia.com/use-gpuuuid"
	// GPUNoUseUUID annotation specifies a comma-separated list of GPU UUIDs to exclude.
	GPUNoUseUUID = "nvidia.com/nouse-gpuuuid"
	AllocateMode = "nvidia.com/vgpu-mode"

	MigMode      = "mig"
	HamiCoreMode = "hami-core"
	MpsMode      = "mps"
)

var (
	NodeName          string
	RuntimeSocketFlag string
	DisableCoreLimit  *bool

	// DevicePluginFilterDevice need device-plugin filter this device, don't register this device.
	DevicePluginFilterDevice *FilterDevice
	MemoryFactor             int32 = 1
)

// GPUCoreUtilizationPolicy is set nvidia gpu core isolation policy.
type GPUCoreUtilizationPolicy string

const (
	DefaultCorePolicy GPUCoreUtilizationPolicy = "default"
	ForceCorePolicy   GPUCoreUtilizationPolicy = "force"
	DisableCorePolicy GPUCoreUtilizationPolicy = "disable"
)

type LibCudaLogLevel string

const (
	Error    LibCudaLogLevel = "0"
	Warnings LibCudaLogLevel = "1"
	Infos    LibCudaLogLevel = "3"
	Debugs   LibCudaLogLevel = "4"
)

type NvidiaConfig struct {
	// These configs are shared and can be overwritten by Nodeconfig.
	NodeDefaultConfig            `yaml:",inline"`
	ResourceCountName            string `yaml:"resourceCountName"`
	ResourceMemoryName           string `yaml:"resourceMemoryName"`
	ResourceCoreName             string `yaml:"resourceCoreName"`
	ResourceMemoryPercentageName string `yaml:"resourceMemoryPercentageName"`
	ResourcePriority             string `yaml:"resourcePriorityName"`
	OverwriteEnv                 bool   `yaml:"overwriteEnv"`
	DefaultMemory                int32  `yaml:"defaultMemory"`
	DefaultCores                 int32  `yaml:"defaultCores"`
	DefaultGPUNum                int32  `yaml:"defaultGPUNum"`
	MemoryFactor                 int32  `yaml:"memoryFactor"`
	// TODO Whether these should be removed
	DisableCoreLimit    bool                        `yaml:"disableCoreLimit"`
	MigProfileAllowlist []device.AllowedMigProfiles `yaml:"migProfileAllowlist"`
	// GPUCorePolicy through webhook automatic injected to container env
	GPUCorePolicy GPUCoreUtilizationPolicy `yaml:"gpuCorePolicy"`
	// RuntimeClassName is the name of the runtime class to be added to pod.spec.runtimeClassName
	RuntimeClassName string `yaml:"runtimeClassName"`
}

// These configs can be specified for each node by using Nodeconfig.
type NodeDefaultConfig struct {
	DeviceSplitCount          *uint    `yaml:"deviceSplitCount" json:"devicesplitcount"`
	DeviceMemoryScaling       *float64 `yaml:"deviceMemoryScaling" json:"devicememoryscaling"`
	DeviceCoreScaling         *float64 `yaml:"deviceCoreScaling" json:"devicecorescaling"`
	PreConfiguredDeviceMemory *int64   `yaml:"preConfiguredDeviceMemory" json:"preconfigureddevicememory"`
	// LogLevel is LIBCUDA_LOG_LEVEL value
	LogLevel *LibCudaLogLevel `yaml:"libCudaLogLevel" json:"libcudaloglevel"`
	// EnableNUMATopology advertises the physical GPU's NUMA node on each vGPU
	// replica so kubelet's TopologyManager can align CPU and GPU NUMA nodes.
	// Defaults to false to preserve existing admission behavior.
	EnableNUMATopology *bool `yaml:"enableNumaTopology" json:"enablenumatopology"`
}

type FilterDevice struct {
	// UUID is the device ID.
	UUID []string `json:"uuid"`
	// Index is the device index.
	Index []uint `json:"index"`
}

type DevicePluginConfigs struct {
	Nodeconfig []struct {
		// These configs is shared and will overwrite those in NvidiaConfig.
		NodeDefaultConfig            `json:",inline"`
		Name                         string        `json:"name"`
		OperatingMode                string        `json:"operatingmode"`
		Migstrategy                  string        `json:"migstrategy"`
		FilterDevice                 *FilterDevice `json:"filterdevices"`
		EnableGetPreferredAllocation bool          `json:"enablegetpreferredallocation"`
	} `json:"nodeconfig"`
}

type DeviceConfig struct {
	*spec.Config

	ResourceName *string
	DebugMode    *bool
}

type NvidiaGPUDevices struct {
	config                NvidiaConfig
	ReportedGPUNum        map[string]int64  // key: nodeName, value: reported GPU count
	ReportedRegisterAnnos map[string]string // key: nodeName, value: last observed register annotation
	mu                    sync.Mutex        // protects concurrent access to reported node state
}

func InitNvidiaDevice(nvconfig NvidiaConfig) *NvidiaGPUDevices {
	klog.InfoS("initializing nvidia device", "resourceName", nvconfig.ResourceCountName, "resourceMem", nvconfig.ResourceMemoryName, "DefaultGPUNum", nvconfig.DefaultGPUNum)
	_, ok := device.InRequestDevices[NvidiaGPUDevice]
	if !ok {
		device.InRequestDevices[NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
		device.SupportDevices[NvidiaGPUDevice] = "hami.io/vgpu-devices-allocated"
		util.HandshakeAnnos[NvidiaGPUDevice] = HandshakeAnnos
	}
	if err := ValidateMigProfileAllowlist(nvconfig.MigProfileAllowlist); err != nil {
		klog.Fatalf("invalid MIG profile allowlist: %v", err)
	}
	MemoryFactor = nvconfig.MemoryFactor
	return &NvidiaGPUDevices{
		config:                nvconfig,
		ReportedGPUNum:        make(map[string]int64),
		ReportedRegisterAnnos: make(map[string]string),
	}
}

func (dev *NvidiaGPUDevices) CommonWord() string {
	return NvidiaGPUDevice
}

func ParseConfig(fs *flag.FlagSet) {
}

func FilterDeviceToRegister(uuid, indexStr string) bool {
	if DevicePluginFilterDevice == nil || (len(DevicePluginFilterDevice.UUID) == 0 && len(DevicePluginFilterDevice.Index) == 0) {
		return false
	}
	uuidMap, indexMap := make(map[string]struct{}), make(map[uint]struct{})
	for _, u := range DevicePluginFilterDevice.UUID {
		uuidMap[u] = struct{}{}
	}
	for _, index := range DevicePluginFilterDevice.Index {
		indexMap[index] = struct{}{}
	}
	if uuid != "" {
		if _, ok := uuidMap[uuid]; ok {
			return true
		}
	}
	if indexStr != "" {
		index, err := strconv.Atoi(indexStr)
		if err != nil {
			klog.Errorf("Error converting index to int: %v", err)
			return false
		}
		if _, ok := indexMap[uint(index)]; ok {
			return true
		}
	}
	return false
}

func (dev *NvidiaGPUDevices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(HandshakeAnnos, nn)
}

func (dev *NvidiaGPUDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	current := int64(0)
	quantity := n.Status.Allocatable.Name(corev1.ResourceName(dev.config.ResourceCountName), resource.DecimalSI)
	if quantity != nil {
		current = quantity.Value()
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()

	reported := dev.ReportedGPUNum[n.Name]
	klog.V(3).InfoS("checking device health for node", "nodeName", n.Name, "deviceType", devType, "currentDevices", current, "reportedDevices", reported)

	handshakeHealthy, handshakeChanged := device.CheckHealth(devType, dev.config.ResourceCountName, n)
	registerAnno := n.Annotations[RegisterAnnos]
	reportedRegisterAnno := dev.ReportedRegisterAnnos[n.Name]

	if current == 0 {
		if reported == 0 {
			return handshakeHealthy, handshakeChanged
		}
		dev.ReportedGPUNum[n.Name] = current
		dev.ReportedRegisterAnnos[n.Name] = registerAnno
		return false, handshakeChanged
	}

	if reported != current {
		dev.ReportedGPUNum[n.Name] = current
		dev.ReportedRegisterAnnos[n.Name] = registerAnno
		return true, true
	}

	if reportedRegisterAnno != registerAnno {
		dev.ReportedRegisterAnnos[n.Name] = registerAnno
		return handshakeHealthy, true
	}

	return handshakeHealthy, handshakeChanged
}

func (dev *NvidiaGPUDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.LockNode(n.Name, NodeLockNvidia, p)
}

func (dev *NvidiaGPUDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
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
	return nodelock.ReleaseNodeLock(n.Name, NodeLockNvidia, p, false)
}

func (dev *NvidiaGPUDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
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
		klog.InfoS("no nvidia gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*device.DeviceInfo{}, errors.New("no gpu found on node")
	}
	for idx := range nodedevices {
		nodedevices[idx].DeviceVendor = dev.CommonWord()
	}
	pairScores, ok := n.Annotations[RegisterGPUPairScore]
	if !ok {
		klog.V(5).InfoS("no topology score found", "node", n.Name)
	} else {
		devicePairScores, err := device.DecodePairScores(pairScores)
		if err != nil {
			klog.ErrorS(err, "failed to decode pair scores", "node", n.Name, "pair scores", pairScores)
			return []*device.DeviceInfo{}, err
		}
		if devicePairScores != nil {
			// fit pair score to device info
			for _, deviceInfo := range nodedevices {
				uuid := deviceInfo.ID

				for _, devicePairScore := range *devicePairScores {
					if devicePairScore.ID == uuid {
						deviceInfo.DevicePairScore = devicePairScore
						break
					}
				}
			}
		}
	}
	devDecoded := device.EncodeNodeDevices(nodedevices)
	klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devDecoded)
	return nodedevices, nil
}

func (dev *NvidiaGPUDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	/*gpu related */
	if err := dev.validateMemoryPercentage(ctr); err != nil {
		return false, err
	}
	priority, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourcePriority)]
	if ok {
		ctr.Env = append(ctr.Env, corev1.EnvVar{
			Name:  util.TaskPriority,
			Value: fmt.Sprint(priority.Value()),
		})
	}

	if dev.config.GPUCorePolicy != "" &&
		dev.config.GPUCorePolicy != DefaultCorePolicy {
		ctr.Env = append(ctr.Env, corev1.EnvVar{
			Name:  util.CoreLimitSwitch,
			Value: string(dev.config.GPUCorePolicy),
		})
	}

	hasResource := dev.mutateContainerResource(ctr)
	if dev.defaultExclusiveCoreIfNeeded(ctr) {
		hasResource = true
	}

	if hasResource {
		// Set runtime class name if it is not set by user and the runtime class name is configured
		if p.Spec.RuntimeClassName == nil && dev.config.RuntimeClassName != "" {
			p.Spec.RuntimeClassName = &dev.config.RuntimeClassName
		}
	}

	if !hasResource && dev.config.OverwriteEnv {
		ctr.Env = append(ctr.Env, corev1.EnvVar{
			Name:  "NVIDIA_VISIBLE_DEVICES",
			Value: "none",
		})
	}
	return hasResource, nil
}

func (dev *NvidiaGPUDevices) validateMemoryPercentage(ctr *corev1.Container) error {
	if pct, ok := resourceValue(ctr, corev1.ResourceName(dev.config.ResourceMemoryPercentageName)); ok {
		if pct < 0 || pct > 100 {
			return fmt.Errorf("invalid %s value %d in container %s: must be an integer between 0 and 100", dev.config.ResourceMemoryPercentageName, pct, ctr.Name)
		}
	}
	return nil
}

func (dev *NvidiaGPUDevices) mutateContainerResource(ctr *corev1.Container) bool {
	_, resourceNameOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCountName)]
	if resourceNameOK {
		return true
	}

	_, resourceCoresOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCoreName)]
	_, resourceMemOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
	_, resourceMemPercentageOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryPercentageName)]

	if resourceCoresOK || resourceMemOK || resourceMemPercentageOK {
		if dev.config.DefaultGPUNum > 0 {
			ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCountName)] = *resource.NewQuantity(int64(dev.config.DefaultGPUNum), resource.BinarySI)
			return true
		}
	}
	return false
}

func (dev *NvidiaGPUDevices) defaultExclusiveCoreIfNeeded(ctr *corev1.Container) bool {
	if ctr == nil {
		return false
	}

	countName := corev1.ResourceName(dev.config.ResourceCountName)
	if countName == "" || !resourcePresent(ctr, countName) {
		return false
	}

	coreName := corev1.ResourceName(dev.config.ResourceCoreName)
	if coreName == "" || resourcePresent(ctr, coreName) {
		return false
	}

	exclusive := false
	if pct, ok := resourceValue(ctr, corev1.ResourceName(dev.config.ResourceMemoryPercentageName)); ok {
		exclusive = pct == 100
	} else if dev.config.ResourceMemoryName == "" {
		exclusive = true
	} else if _, ok := resourceValue(ctr, corev1.ResourceName(dev.config.ResourceMemoryName)); !ok {
		exclusive = true
	}

	if !exclusive {
		return false
	}

	if ctr.Resources.Limits == nil {
		ctr.Resources.Limits = corev1.ResourceList{}
	}
	ctr.Resources.Limits[coreName] = *resource.NewQuantity(100, resource.DecimalSI)
	return true
}

func resourceValue(ctr *corev1.Container, name corev1.ResourceName) (int64, bool) {
	if name == "" || ctr == nil {
		return 0, false
	}
	if qty, ok := ctr.Resources.Limits[name]; ok {
		return qty.Value(), true
	}
	if qty, ok := ctr.Resources.Requests[name]; ok {
		return qty.Value(), true
	}
	return 0, false
}

func resourcePresent(ctr *corev1.Container, name corev1.ResourceName) bool {
	if ctr == nil || name == "" {
		return false
	}
	if _, ok := ctr.Resources.Limits[name]; ok {
		return true
	}
	if _, ok := ctr.Resources.Requests[name]; ok {
		return true
	}
	return false
}

func checkGPUtype(annos map[string]string, cardtype string) bool {
	return device.CheckType(annos, cardtype, GPUInUse, GPUNoUse)
}

func assertNuma(annos map[string]string) bool {
	numabind, ok := annos[NumaBind]
	if ok {
		enforce, err := strconv.ParseBool(numabind)
		if err == nil && enforce {
			return true
		}
	}
	return false
}

func (dev *NvidiaGPUDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool) {
	typeCheck := checkGPUtype(annos, d.Type)
	mode, ok := annos[AllocateMode]
	if ok && !strings.Contains(mode, d.Mode) {
		typeCheck = false
	}
	if strings.Compare(n.Type, NvidiaGPUDevice) == 0 {
		return typeCheck, assertNuma(annos)
	}
	return false, false
}

func (dev *NvidiaGPUDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[NvidiaGPUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.InRequestDevices[NvidiaGPUDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[NvidiaGPUDevice]] = deviceStr
		if allocationData, hasAllocations := EncodeMigAllocations(devlist); hasAllocations {
			(*annoinput)[MigAllocationsAnnotation] = allocationData
		}
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.InRequestDevices[NvidiaGPUDevice], deviceStr)
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.SupportDevices[NvidiaGPUDevice], deviceStr)
	}
	return *annoinput
}

func (dev *NvidiaGPUDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	resourceName := corev1.ResourceName(dev.config.ResourceCountName)
	resourceMem := corev1.ResourceName(dev.config.ResourceMemoryName)
	resourceMemPercentage := corev1.ResourceName(dev.config.ResourceMemoryPercentageName)
	resourceCores := corev1.ResourceName(dev.config.ResourceCoreName)
	v, ok := ctr.Resources.Limits[resourceName]
	if !ok {
		v, ok = ctr.Resources.Requests[resourceName]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			memnum := 0
			mem, ok := ctr.Resources.Limits[resourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[resourceMem]
			}
			if ok {
				memnums, parsed := mem.AsInt64()
				factor := max(int64(dev.config.MemoryFactor), 1)
				if !parsed || memnums < 0 || memnums > int64(math.MaxInt32)/factor {
					klog.ErrorS(nil, "nvidia memory request is not a plain integer within the int32 range; rejecting to avoid silent under-allocation",
						"container", ctr.Name)
					return device.ContainerDeviceRequest{}
				}
				if factor > 1 {
					rawMemnums := memnums
					memnums = memnums * factor
					klog.V(4).Infof("Update memory request. before %d, after %d, factor %d", rawMemnums, memnums, factor)
				}
				memnum = int(memnums)
			}
			mempnum := int32(101)
			mem, ok = ctr.Resources.Limits[resourceMemPercentage]
			if !ok {
				mem, ok = ctr.Resources.Requests[resourceMemPercentage]
			}
			if ok {
				mempnums, ok := mem.AsInt64()
				if ok {
					if mempnums > 100 {
						klog.ErrorS(nil, "memory percentage request out of range, clamping to 100", "container", ctr.Name, "requested", mempnums)
						mempnums = 100
					}
					if mempnums > 0 {
						mempnum = int32(mempnums)
					} else {
						// 0 would inject CUDA_DEVICE_MEMORY_LIMIT=0m, which hami-core reads as "no limit", so keep the "unset" sentinel and let the default below apply, like nvidia.com/gpumem: 0.
						klog.ErrorS(nil, "memory percentage request is not positive, ignoring it", "container", ctr.Name, "requested", mempnums)
						mempnum = 101
					}
				}
			}
			if mempnum == 101 && memnum == 0 {
				if dev.config.DefaultMemory != 0 {
					memnum = int(dev.config.DefaultMemory)
				} else {
					mempnum = 100
				}
			}
			corenum := dev.config.DefaultCores
			core, ok := ctr.Resources.Limits[resourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[resourceCores]
			}
			if ok {
				corenums, ok := core.AsInt64()
				if ok {
					corenum = int32(corenums)
				}
			}
			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             NvidiaGPUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         int32(corenum),
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *NvidiaGPUDevices) CustomFilterRule(allocated *device.PodDevices, request device.ContainerDeviceRequest, toAllocate device.ContainerDevices, devusage *device.DeviceUsage) bool {
	if devusage.Mode == MigMode {
		occupied := occupiedMigPlacements(devusage.MigAllocationsInUse)
		for _, existing := range toAllocate {
			if existing.UUID != devusage.ID {
				continue
			}
			_, placement, ok := selectMigCandidate(devusage.MigProfiles, occupied, existing.Usedmem)
			if !ok {
				return false
			}
			occupied = append(occupied, placement)
		}
		_, _, ok := selectMigCandidate(devusage.MigProfiles, occupied, request.Memreq)
		return ok
	}
	return true
}

func selectMigCandidate(profiles []device.MigProfile, occupied []device.MigPlacement, memory int32) (device.MigProfile, device.MigPlacement, bool) {
	candidates := append([]device.MigProfile(nil), profiles...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].MemoryMB != candidates[j].MemoryMB {
			return candidates[i].MemoryMB < candidates[j].MemoryMB
		}
		return candidates[i].SliceCount < candidates[j].SliceCount
	})
	for _, profile := range candidates {
		if profile.MemoryMB < memory {
			continue
		}
		placement, ok := selectMigPlacement(profiles, occupied, profile.Name)
		if ok {
			return profile, placement, true
		}
	}
	return device.MigProfile{}, device.MigPlacement{}, false
}

func (dev *NvidiaGPUDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *NvidiaGPUDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	if n.Mode == MigMode {
		profile, placement, ok := selectMigCandidate(n.MigProfiles, occupiedMigPlacements(n.MigAllocationsInUse), ctr.Usedmem)
		if !ok {
			return errors.New("MIG profile and placement allocation failed")
		}
		ctr.Usedmem = profile.MemoryMB
		ctr.Usedcores = profile.Core
		if ctr.CustomInfo == nil {
			ctr.CustomInfo = make(map[string]any)
		}
		ctr.CustomInfo[MigProfileCustomInfo] = profile.Name
		ctr.CustomInfo[MigPlacementCustomInfo] = placement
		n.MigAllocationsInUse = append(n.MigAllocationsInUse, device.MigAllocation{Profile: profile.Name, Placement: placement})
	}
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func fitQuota(pod *corev1.Pod, tmpDevs map[string]device.ContainerDevices, allocated *device.PodDevices, ns string, devUUID string, memreq int64, coresreq int64) bool {
	hypo := device.PodDevices{}
	if allocated != nil {
		for devType, podSingle := range *allocated {
			hypo[devType] = append(device.PodSingleDevice{}, podSingle...)
		}
	}
	cur := append(device.ContainerDevices{}, tmpDevs[NvidiaGPUDevice]...)
	cur = append(cur, device.ContainerDevice{
		UUID:      devUUID,
		Type:      NvidiaGPUDevice,
		Usedmem:   int32(memreq),
		Usedcores: int32(coresreq),
	})
	hypo[NvidiaGPUDevice] = append(hypo[NvidiaGPUDevice], cur)

	var mem, core int64
	for _, ctrDevs := range device.CollapseInitContainerUsage(pod, hypo)[NvidiaGPUDevice] {
		for _, val := range ctrDevs {
			mem += int64(val.Usedmem)
			core += int64(val.Usedcores)
		}
	}

	klog.V(4).Infoln("Allocating...", mem, "cores", core)
	return device.GetLocalCache().FitQuota(ns, mem, MemoryFactor, core, NvidiaGPUDevice)
}
func (nv *NvidiaGPUDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]device.ContainerDevices
	tmpDevs = make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	gpuPolicy := util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, pod)
	needTopology := gpuPolicy == util.GPUSchedulerPolicyTopology.String()
	isMutex := gpuPolicy == util.GPUSchedulerPolicyMutex.String()
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)
		if !dev.Health {
			reason[common.CardNotHealth]++
			klog.V(5).InfoS(common.CardNotHealth, "pod", klog.KObj(pod), "device", dev.ID, "health", dev.Health)
			continue
		}
		found, numa := nv.checkType(pod.GetAnnotations(), *dev, k)
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
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, GPUUseUUID, GPUNoUseUUID, nv.CommonWord()) {
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
		if !fitQuota(pod, tmpDevs, allocated, pod.Namespace, dev.ID, int64(memreq), int64(k.Coresreq)) {
			reason[common.ResourceQuotaNotFit]++
			klog.V(3).InfoS(common.ResourceQuotaNotFit, "pod", pod.Name, "memreq", memreq, "coresreq", k.Coresreq)
			continue
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
		// CustomFilterRule must see the resolved memory request, not the raw (possibly zero) Memreq field.
		resolvedReq := request
		resolvedReq.Memreq = memreq
		if !nv.CustomFilterRule(allocated, resolvedReq, tmpDevs[k.Type], dev) {
			// In MIG mode, CustomFilterRule rejects when the requested memory
			// does not fit an allowed profile with a free placement on this
			// device. Surface this as a distinct reason so users can tell
			// placement infeasibility apart from generic filter failure.
			if dev.Mode == MigMode {
				reason[common.CardMigTopologyInfeasible]++
				klog.V(5).InfoS(common.CardMigTopologyInfeasible, "pod", klog.KObj(pod), "device", dev.ID, "device index", i, "allocations", dev.MigAllocationsInUse)
				continue
			}
			reason[common.CardNotFoundCustomFilterRule]++
			klog.V(5).InfoS(common.CardNotFoundCustomFilterRule, "pod", klog.KObj(pod), "device", dev.ID, "device index", i)
			continue
		}

		if k.Nums > 0 {
			klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
			if !needTopology {
				k.Nums--
			}
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
				Idx:       int(dev.Index),
				UUID:      dev.ID,
				Type:      k.Type,
				Usedmem:   memreq,
				Usedcores: k.Coresreq,
			})
		}
		if k.Nums == 0 && !needTopology {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}
		if dev.Mode == "mig" {
			i++
		}
	}
	if needTopology {
		if len(tmpDevs[k.Type]) == int(originReq) {
			klog.V(5).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}
		if len(tmpDevs[k.Type]) > int(originReq) {
			if originReq == 1 {
				// If requesting a device, select the card with the worst connection to other cards (lowest total score).
				lowestDevices := computeWorstSingleCard(nodeInfo, request, tmpDevs)
				tmpDevs[k.Type] = lowestDevices
				klog.V(5).InfoS("device allocate success", "pod", klog.KObj(pod), "worst device", lowestDevices)
			} else {
				// If requesting multiple devices, select the best combination of cards.
				combinations := generateCombinations(request, tmpDevs)
				combination := computeBestCombination(nodeInfo, combinations)
				tmpDevs[k.Type] = combination
				klog.V(5).InfoS("device allocate success", "pod", klog.KObj(pod), "best device combination", tmpDevs)
			}
			return true, tmpDevs, ""
		}
	}
	if len(tmpDevs[k.Type]) > 0 {
		reason[common.AllocatedCardsInsufficientRequest] = len(tmpDevs[k.Type])
		klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs[k.Type]))
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}

func (dev *NvidiaGPUDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  dev.config.ResourceCountName,
		ResourceMemoryName: dev.config.ResourceMemoryName,
		ResourceCoreName:   dev.config.ResourceCoreName,
		MemoryFactor:       dev.config.MemoryFactor,
	}
}

func generateCombinations(request device.ContainerDeviceRequest, tmpDevs map[string]device.ContainerDevices) []device.ContainerDevices {
	k := request
	num := int(k.Nums)
	devices := tmpDevs[k.Type]
	// This code mainly performs permutations and combinations to generate all non-repetitive subsets.
	// For example, if the request is 3 GPUs, and the available GPUs are ["GPU0", "GPU1", "GPU2", "GPU3"],
	// it will generate all combinations of 3 GPUs from the available GPUs.
	// Result：[["GPU0", "GPU1", "GPU2"],["GPU0", "GPU1", "GPU3"],["GPU0", "GPU2", "GPU3"],["GPU1", "GPU2", "GPU3"]]
	var result []device.ContainerDevices
	var helper func(device.ContainerDevices, int, int, device.ContainerDevices)

	helper = func(arr device.ContainerDevices, start, k int, current device.ContainerDevices) {
		if k == 0 {
			temp := make(device.ContainerDevices, len(current))
			copy(temp, current)
			result = append(result, temp)
			return
		}

		for i := start; i <= len(arr)-k; i++ {
			current = append(current, arr[i])
			helper(arr, i+1, k-1, current)
			current = current[:len(current)-1]
		}
	}

	helper(devices, 0, num, device.ContainerDevices{})
	return result
}

func getDevicePairScoreMap(nodeInfo *device.NodeInfo) map[string]*device.DevicePairScore {
	deviceScoreMap := make(map[string]*device.DevicePairScore)

	for _, dev := range nodeInfo.Devices[NvidiaGPUDevice] {
		deviceScoreMap[dev.ID] = &dev.DevicePairScore
	}

	return deviceScoreMap
}

func computeWorstSingleCard(nodeInfo *device.NodeInfo, request device.ContainerDeviceRequest, tmpDevs map[string]device.ContainerDevices) device.ContainerDevices {
	worstScore := 0
	worstDevices := device.ContainerDevices{}
	found := false
	deviceScoreMap := getDevicePairScoreMap(nodeInfo)
	// Iterate through all devices to find the one with the lowest score
	devices := tmpDevs[request.Type]

	for _, dev1 := range devices {
		totalScore := 0
		scoreMapDev1 := deviceScoreMap[dev1.UUID]
		for _, dev2 := range devices {
			if dev1.UUID == dev2.UUID {
				continue
			}
			totalScore += scoreMapDev1.Scores[dev2.UUID]
		}
		if !found || totalScore < worstScore {
			found = true
			worstScore = totalScore
			worstDevices = device.ContainerDevices{dev1}
		}
	}
	return worstDevices
}

func computeBestCombination(nodeInfo *device.NodeInfo, combinations []device.ContainerDevices) device.ContainerDevices {
	bestScore := -1
	bestCombination := device.ContainerDevices{}
	found := false
	deviceScoreMap := getDevicePairScoreMap(nodeInfo)
	// Iterate through all combinations to find the one with the highest score
	for _, partition := range combinations {
		totalScore := 0

		for i := 0; i < len(partition)-1; i++ {
			dev1 := partition[i]
			scoreMapDev1 := deviceScoreMap[dev1.UUID]
			for z := i + 1; z < len(partition); z++ {
				dev2 := partition[z]
				totalScore += scoreMapDev1.Scores[dev2.UUID]
			}
		}

		if !found || totalScore > bestScore {
			found = true
			bestScore = totalScore
			bestCombination = partition
		}
	}
	return bestCombination
}
