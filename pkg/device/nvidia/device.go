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
	"slices"
	"strconv"
	"strings"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

const (
	HandshakeAnnos       = "hami.io/node-handshake"
	RegisterAnnos        = "hami.io/node-nvidia-register"
	RegisterGPUPairScore = "hami.io/node-nvidia-score"
	NvidiaGPUDevice      = "NVIDIA"
	NvidiaGPUCommonWord  = "GPU"
	GPUInUse             = "nvidia.com/use-gputype"
	GPUNoUse             = "nvidia.com/nouse-gputype"
	NumaBind             = "nvidia.com/numa-bind"
	NodeLockNvidia       = "hami.io/mutex.lock"
	// GPUUseUUID is user can use specify GPU device for set GPU UUID.
	GPUUseUUID = "nvidia.com/use-gpuuuid"
	// GPUNoUseUUID is user can not use specify GPU device for set GPU UUID.
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
)

type MigPartedSpec struct {
	Version    string                        `json:"version"               yaml:"version"`
	MigConfigs map[string]MigConfigSpecSlice `json:"mig-configs,omitempty" yaml:"mig-configs,omitempty"`
}

// MigConfigSpec defines the spec to declare the desired MIG configuration for a set of GPUs.
type MigConfigSpec struct {
	DeviceFilter any              `json:"device-filter,omitempty" yaml:"device-filter,flow,omitempty"`
	Devices      []int32          `json:"devices"                 yaml:"devices,flow"`
	MigEnabled   bool             `json:"mig-enabled"             yaml:"mig-enabled"`
	MigDevices   map[string]int32 `json:"mig-devices"             yaml:"mig-devices"`
}

// MigConfigSpecSlice represents a slice of 'MigConfigSpec'.
type MigConfigSpecSlice []MigConfigSpec

// GPUCoreUtilizationPolicy is set nvidia gpu core isolation policy.
type GPUCoreUtilizationPolicy string

const (
	DefaultCorePolicy GPUCoreUtilizationPolicy = "default"
	ForceCorePolicy   GPUCoreUtilizationPolicy = "force"
	DisableCorePolicy GPUCoreUtilizationPolicy = "disable"
)

type NvidiaConfig struct {
	ResourceCountName            string  `yaml:"resourceCountName"`
	ResourceMemoryName           string  `yaml:"resourceMemoryName"`
	ResourceCoreName             string  `yaml:"resourceCoreName"`
	ResourceMemoryPercentageName string  `yaml:"resourceMemoryPercentageName"`
	ResourcePriority             string  `yaml:"resourcePriorityName"`
	OverwriteEnv                 bool    `yaml:"overwriteEnv"`
	DefaultMemory                int32   `yaml:"defaultMemory"`
	DefaultCores                 int32   `yaml:"defaultCores"`
	DefaultGPUNum                int32   `yaml:"defaultGPUNum"`
	DeviceSplitCount             uint    `yaml:"deviceSplitCount"`
	DeviceMemoryScaling          float64 `yaml:"deviceMemoryScaling"`
	DeviceCoreScaling            float64 `yaml:"deviceCoreScaling"`
	// TODO 这个参数是否应该直接移除
	DisableCoreLimit  bool                        `yaml:"disableCoreLimit"`
	MigGeometriesList []util.AllowedMigGeometries `yaml:"knownMigGeometries"`
	// GPUCorePolicy through webhook automatic injected to container env
	GPUCorePolicy GPUCoreUtilizationPolicy `yaml:"gpuCorePolicy"`
	// RuntimeClassName is the name of the runtime class to be added to pod.spec.runtimeClassName
	RuntimeClassName string `yaml:"runtimeClassName"`
}

type FilterDevice struct {
	// UUID is the device ID.
	UUID []string `json:"uuid"`
	// Index is the device index.
	Index []uint `json:"index"`
}

type DevicePluginConfigs struct {
	Nodeconfig []struct {
		Name                string        `json:"name"`
		OperatingMode       string        `json:"operatingmode"`
		Devicememoryscaling float64       `json:"devicememoryscaling"`
		Devicecorescaling   float64       `json:"devicecorescaling"`
		Devicesplitcount    uint          `json:"devicesplitcount"`
		Migstrategy         string        `json:"migstrategy"`
		FilterDevice        *FilterDevice `json:"filterdevices"`
	} `json:"nodeconfig"`
}

type DeviceConfig struct {
	*spec.Config

	ResourceName *string
	DebugMode    *bool
}

type NvidiaGPUDevices struct {
	config NvidiaConfig
}

func InitNvidiaDevice(nvconfig NvidiaConfig) *NvidiaGPUDevices {
	klog.InfoS("initializing nvidia device", "resourceName", nvconfig.ResourceCountName, "resourceMem", nvconfig.ResourceMemoryName, "DefaultGPUNum", nvconfig.DefaultGPUNum)
	util.InRequestDevices[NvidiaGPUDevice] = "hami.io/vgpu-devices-to-allocate"
	util.SupportDevices[NvidiaGPUDevice] = "hami.io/vgpu-devices-allocated"
	util.HandshakeAnnos[NvidiaGPUDevice] = HandshakeAnnos
	return &NvidiaGPUDevices{
		config: nvconfig,
	}
}

func (dev *NvidiaGPUDevices) CommonWord() string {
	return NvidiaGPUCommonWord
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
	return util.CheckHealth(devType, n)
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

func (dev *NvidiaGPUDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	devEncoded, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*util.DeviceInfo{}, errors.New("annos not found " + RegisterAnnos)
	}
	nodedevices, err := util.DecodeNodeDevices(devEncoded)
	if err != nil {
		klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", devEncoded)
		return []*util.DeviceInfo{}, err
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no nvidia gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*util.DeviceInfo{}, errors.New("no gpu found on node")
	}
	for _, val := range nodedevices {
		if val.Mode == "mig" {
			val.MIGTemplate = make([]util.Geometry, 0)
			for _, migTemplates := range dev.config.MigGeometriesList {
				found := false
				for _, migDevices := range migTemplates.Models {
					if strings.Contains(val.Type, migDevices) {
						found = true
						break
					}
				}
				if found {
					val.MIGTemplate = append(val.MIGTemplate, migTemplates.Geometries...)
					break
				}
			}
		}
	}
	if util.LookupEnvBoolOr("ENABLE_TOPOLOGY_SCORE", false) {
		pairScores, ok := n.Annotations[RegisterGPUPairScore]
		if !ok {
			klog.Warningf("no topology score found", "node", n.Name)
		} else {
			devicePairScores, err := util.DecodePairScores(pairScores)
			if err != nil {
				klog.ErrorS(err, "failed to decode pair scores", "node", n.Name, "pair scores", pairScores)
				return []*util.DeviceInfo{}, err
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
	}
	devDecoded := util.EncodeNodeDevices(nodedevices)
	klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devDecoded)
	return nodedevices, nil
}

func (dev *NvidiaGPUDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	/*gpu related */
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

func checkGPUtype(annos map[string]string, cardtype string) bool {
	cardtype = strings.ToUpper(cardtype)
	if inuse, ok := annos[GPUInUse]; ok {
		useTypes := strings.Split(inuse, ",")
		if !slices.ContainsFunc(useTypes, func(useType string) bool {
			return strings.Contains(cardtype, strings.ToUpper(useType))
		}) {
			return false
		}
	}
	if unuse, ok := annos[GPUNoUse]; ok {
		unuseTypes := strings.Split(unuse, ",")
		if slices.ContainsFunc(unuseTypes, func(unuseType string) bool {
			return strings.Contains(cardtype, strings.ToUpper(unuseType))
		}) {
			return false
		}
	}
	return true
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

func (dev *NvidiaGPUDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	typeCheck := checkGPUtype(annos, d.Type)
	mode, ok := annos[AllocateMode]
	if ok && !strings.Contains(mode, d.Mode) {
		typeCheck = false
	}
	if strings.Compare(n.Type, NvidiaGPUDevice) == 0 {
		return true, typeCheck, assertNuma(annos)
	}
	return false, false, false
}

func (dev *NvidiaGPUDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[GPUUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for nvidia user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		return slices.Contains(userUUIDs, d.ID)
	}

	noUserUUID, ok := annos[GPUNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for nvidia not user uuid [%s], device id is %s", noUserUUID, d.ID)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		return !slices.Contains(noUserUUIDs, d.ID)
	}

	return true
}

func (dev *NvidiaGPUDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[NvidiaGPUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := util.EncodePodSingleDevice(devlist)
		(*annoinput)[util.InRequestDevices[NvidiaGPUDevice]] = deviceStr
		(*annoinput)[util.SupportDevices[NvidiaGPUDevice]] = deviceStr
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", util.InRequestDevices[NvidiaGPUDevice], deviceStr)
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", util.SupportDevices[NvidiaGPUDevice], deviceStr)
	}
	return *annoinput
}

func (dev *NvidiaGPUDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
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
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums)
				}
			}
			mempnum := int32(101)
			mem, ok = ctr.Resources.Limits[resourceMemPercentage]
			if !ok {
				mem, ok = ctr.Resources.Requests[resourceMemPercentage]
			}
			if ok {
				mempnums, ok := mem.AsInt64()
				if ok {
					mempnum = int32(mempnums)
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
			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             NvidiaGPUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         int32(corenum),
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *NvidiaGPUDevices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	//memreq := request.Memreq
	deviceUsageSnapshot := device.MigUsage
	deviceUsageCurrent := util.MigInUse{
		UsageList: make(util.MIGS, 0),
	}
	deviceUsageCurrent.UsageList = append(deviceUsageCurrent.UsageList, deviceUsageSnapshot.UsageList...)
	if device.Mode == "mig" {
		if len(deviceUsageCurrent.UsageList) == 0 {
			tmpfound := false
			for tidx, templates := range device.MigTemplate {
				if templates[0].Memory < request.Memreq {
					continue
				} else {
					util.PlatternMIG(&deviceUsageCurrent, device.MigTemplate, tidx)
					tmpfound = true
					break
				}
			}
			if !tmpfound {
				klog.Infoln("MIG entry no template fit", deviceUsageCurrent.UsageList, "request=", request)
			}
		}
		for _, val := range toAllocate {
			found := false
			for idx := range deviceUsageCurrent.UsageList {
				if !deviceUsageCurrent.UsageList[idx].InUse && deviceUsageCurrent.UsageList[idx].Memory > val.Usedmem {
					deviceUsageCurrent.UsageList[idx].InUse = true
					found = true
					break
				}
			}
			if !found {
				klog.Infoln("MIG entry not found", deviceUsageCurrent.UsageList)
				return false
			}
		}
		for idx := range deviceUsageCurrent.UsageList {
			if !deviceUsageCurrent.UsageList[idx].InUse && deviceUsageCurrent.UsageList[idx].Memory > request.Memreq {
				deviceUsageCurrent.UsageList[idx].InUse = true
				klog.Infoln("MIG entry device usage true=", deviceUsageCurrent.UsageList, "request", request, "toAllocate", toAllocate)
				return true
			}
		}
		klog.Infoln("MIG entry device usage false=", deviceUsageCurrent.UsageList)
		return false
	}
	return true
}

func (dev *NvidiaGPUDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	return 0
}

func (dev *NvidiaGPUDevices) migNeedsReset(n *util.DeviceUsage) bool {
	if len(n.MigUsage.UsageList) == 0 {
		return true
	}
	for _, val := range n.MigUsage.UsageList {
		if val.InUse {
			return false
		}
	}
	n.MigUsage.UsageList = make(util.MIGS, 0)
	return true
}

func (dev *NvidiaGPUDevices) AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	if n.Mode == "mig" {
		if dev.migNeedsReset(n) {
			for tidx, templates := range n.MigTemplate {
				if templates[0].Memory < ctr.Usedmem {
					continue
				} else {
					util.PlatternMIG(&n.MigUsage, n.MigTemplate, tidx)
					ctr.Usedmem = n.MigUsage.UsageList[0].Memory
					if !strings.Contains(ctr.UUID, "[") {
						ctr.UUID = ctr.UUID + "[" + fmt.Sprint(tidx) + "-0]"
					}
					n.MigUsage.Index = int32(tidx)
					n.MigUsage.UsageList[0].InUse = true
					break
				}
			}
		} else {
			found := false
			for idx, val := range n.MigUsage.UsageList {
				if !val.InUse && val.Memory > ctr.Usedmem {
					n.MigUsage.UsageList[idx].InUse = true
					ctr.Usedmem = n.MigUsage.UsageList[idx].Memory
					if !strings.Contains(ctr.UUID, "[") {
						ctr.UUID = ctr.UUID + "[" + fmt.Sprint(n.MigUsage.Index) + "-" + fmt.Sprint(idx) + "]"
					}
					found = true
					break
				}
			}
			if !found {
				return errors.New("mig template allocate resource fail")
			}
		}
	}
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}
