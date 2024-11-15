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
	"strconv"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	HandshakeAnnos      = "hami.io/node-handshake"
	RegisterAnnos       = "hami.io/node-nvidia-register"
	NvidiaGPUDevice     = "NVIDIA"
	NvidiaGPUCommonWord = "GPU"
	GPUInUse            = "nvidia.com/use-gputype"
	GPUNoUse            = "nvidia.com/nouse-gputype"
	NumaBind            = "nvidia.com/numa-bind"
	NodeLockNvidia      = "hami.io/mutex.lock"
	// GPUUseUUID is user can use specify GPU device for set GPU UUID.
	GPUUseUUID = "nvidia.com/use-gpuuuid"
	// GPUNoUseUUID is user can not use specify GPU device for set GPU UUID.
	GPUNoUseUUID = "nvidia.com/nouse-gpuuuid"
)

var (
	NodeName          string
	RuntimeSocketFlag string
	DisableCoreLimit  *bool

	// DevicePluginFilterDevice need device-plugin filter this device, don't register this device.
	DevicePluginFilterDevice *FilterDevice
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
	DisableCoreLimit             bool    `yaml:"disableCoreLimit"`
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
	return nodelock.ReleaseNodeLock(n.Name, NodeLockNvidia)
}

func (dev *NvidiaGPUDevices) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	devEncoded, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*api.DeviceInfo{}, errors.New("annos not found " + RegisterAnnos)
	}
	nodedevices, err := util.DecodeNodeDevices(devEncoded)
	if err != nil {
		klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", devEncoded)
		return []*api.DeviceInfo{}, err
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*api.DeviceInfo{}, errors.New("no gpu found on node")
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
			Name:  api.TaskPriority,
			Value: fmt.Sprint(priority.Value()),
		})
	}

	_, resourceNameOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCountName)]
	if resourceNameOK {
		return resourceNameOK, nil
	}

	_, resourceCoresOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCoreName)]
	_, resourceMemOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
	_, resourceMemPercentageOK := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryPercentageName)]

	if resourceCoresOK || resourceMemOK || resourceMemPercentageOK {
		if dev.config.DefaultGPUNum > 0 {
			ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCountName)] = *resource.NewQuantity(int64(dev.config.DefaultGPUNum), resource.BinarySI)
			resourceNameOK = true
		}
	}

	if !resourceNameOK && dev.config.OverwriteEnv {
		ctr.Env = append(ctr.Env, corev1.EnvVar{
			Name:  "NVIDIA_VISIBLE_DEVICES",
			Value: "none",
		})
	}
	return resourceNameOK, nil
}

func checkGPUtype(annos map[string]string, cardtype string) bool {
	cardtype = strings.ToUpper(cardtype)
	if inuse, ok := annos[GPUInUse]; ok {
		useTypes := strings.Split(inuse, ",")
		if !ContainsSliceFunc(useTypes, func(useType string) bool {
			return strings.Contains(cardtype, strings.ToUpper(useType))
		}) {
			return false
		}
	}
	if unuse, ok := annos[GPUNoUse]; ok {
		unuseTypes := strings.Split(unuse, ",")
		if ContainsSliceFunc(unuseTypes, func(unuseType string) bool {
			return strings.Contains(cardtype, strings.ToUpper(unuseType))
		}) {
			return false
		}
	}
	return true
}

func ContainsSliceFunc[S ~[]E, E any](s S, match func(E) bool) bool {
	for _, e := range s {
		if match(e) {
			return true
		}
	}
	return false
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
	if strings.Compare(n.Type, NvidiaGPUDevice) == 0 {
		return true, checkGPUtype(annos, d.Type), assertNuma(annos)
	}
	return false, false, false
}

func (dev *NvidiaGPUDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[GPUUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for nvidia user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		for _, uuid := range userUUIDs {
			if d.ID == uuid {
				return true
			}
		}
		return false
	}

	noUserUUID, ok := annos[GPUNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for nvidia not user uuid [%s], device id is %s", noUserUUID, d.ID)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		for _, uuid := range noUserUUIDs {
			if d.ID == uuid {
				return false
			}
		}
		return true
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

func (dev *NvidiaGPUDevices) CustomFilterRule(allocated *util.PodDevices, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	return true
}

func (dev *NvidiaGPUDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	return 0
}
