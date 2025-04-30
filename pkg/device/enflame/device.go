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
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type EnflameDevices struct {
	factor int
}

const (
	EnflameGPUDevice     = "Enflame"
	EnflameGPUCommonWord = "Enflame"
	// IluvatarUseUUID is user can use specify Iluvatar device for set Iluvatar UUID.
	EnflameUseUUID = "enflame.com/use-gpuuuid"
	// IluvatarNoUseUUID is user can not use specify Iluvatar device for set Iluvatar UUID.
	EnflameNoUseUUID   = "enflame.com/nouse-gpuuuid"
	PodRequestGCUSize  = "enflame.com/gcu-request-size"
	PodAssignedGCUID   = "enflame.com/gcu-assigned-id"
	PodHasAssignedGCU  = "enflame.com/gcu-assigned"
	PodAssignedGCUTime = "enflame.com/gcu-assigned-time"
	GCUSharedCapacity  = "enflame.com/gcu-shared-capacity"

	SharedResourceName = "enflame.com/shared-gcu"
	CountNoSharedName  = "enflame.com/gcu-count"
)

var (
	EnflameResourceCount      string
	EnflameResourcePercentage string
)

type EnflameConfig struct {
	ResourceCountName      string `yaml:"resourceCountName"`
	ResourcePercentageName string `yaml:"resourcePercentageName"`
	ResourceMemoryName     string `yaml:"resourceMemoryName"`
	ResourceCoreName       string `yaml:"resourceCoreName"`
}

func InitEnflameDevice(config EnflameConfig) *EnflameDevices {
	EnflameResourceCount = config.ResourceCountName
	EnflameResourcePercentage = config.ResourcePercentageName
	util.SupportDevices[EnflameGPUDevice] = "hami.io/enflame-vgpu-devices-allocated"
	return &EnflameDevices{
		factor: 0,
	}
}

func (dev *EnflameDevices) CommonWord() string {
	return EnflameGPUCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&EnflameResourceCount, "enflame-name", "enflame.com/vgcu", "enflame resource count name")
	fs.StringVar(&EnflameResourcePercentage, "enflame-resource-percentage-name", "enflame.com/vgcu-percentage", "enflame resource percentage name")
}

func (dev *EnflameDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(EnflameResourceCount)]
	if ok {
		if count.Value() > 1 {
			ctr.Resources.Limits[corev1.ResourceName(EnflameResourcePercentage)] = *resource.NewQuantity(int64(100), resource.DecimalSI)
			ctr.Resources.Limits[corev1.ResourceName(SharedResourceName)] = *resource.NewQuantity(int64(dev.factor*int(count.Value())), resource.DecimalSI)
		} else {
			percentageResource, ok := ctr.Resources.Limits[corev1.ResourceName(EnflameResourcePercentage)]
			percentage := percentageResource.Value()
			if !ok {
				percentage = 100
			}
			if percentage < 1 {
				percentage = 1
			}
			slice := float64(100) / float64(dev.factor)
			for i := 0; i < dev.factor; i++ {
				if slice*float64(i) < float64(percentage) && float64(percentage) <= slice*float64((i+1)) {
					percentage = int64(slice * float64(i+1))
					ctr.Resources.Limits[corev1.ResourceName(EnflameResourcePercentage)] = *resource.NewQuantity(percentage, resource.DecimalSI)
					ctr.Resources.Limits[corev1.ResourceName(SharedResourceName)] = *resource.NewQuantity(int64(i+1), resource.DecimalSI)
					ctr.Resources.Requests[corev1.ResourceName(EnflameResourcePercentage)] = *resource.NewQuantity(percentage, resource.DecimalSI)
					ctr.Resources.Requests[corev1.ResourceName(SharedResourceName)] = *resource.NewQuantity(int64(i+1), resource.DecimalSI)
					break
				}
			}
		}
	}
	return ok, nil
}

func (dev *EnflameDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	nodedevices := []*util.DeviceInfo{}
	i := 0
	cards, ok := n.Status.Capacity.Name(corev1.ResourceName(CountNoSharedName), resource.DecimalSI).AsInt64()
	if !ok || cards == 0 {
		return nodedevices, nil
	}
	shared, _ := n.Status.Capacity.Name(corev1.ResourceName(SharedResourceName), resource.DecimalSI).AsInt64()
	dev.factor = int(shared / cards)
	for i < int(cards) {
		nodedevices = append(nodedevices, &util.DeviceInfo{
			Index:   uint(i),
			ID:      n.Name + "-enflame-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  100,
			Devcore: 100,
			Type:    EnflameGPUDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *EnflameDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[EnflameGPUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[util.SupportDevices[EnflameGPUDevice]] = util.EncodePodSingleDevice(devlist)
		(*annoinput)[PodHasAssignedGCU] = "false"
		(*annoinput)[PodAssignedGCUTime] = strconv.FormatInt(time.Now().UnixNano(), 10)
		annoKey := PodAssignedGCUID
		value := ""
		for _, val := range devlist[0] {
			value = value + fmt.Sprint(val.Idx) + ","
		}
		if len(value) > 0 {
			(*annoinput)[annoKey] = strings.TrimRight(value, ",")
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

func (dev *EnflameDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, EnflameGPUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *EnflameDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[EnflameUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for Iluvatar user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		for _, uuid := range userUUIDs {
			if d.ID == uuid {
				return true
			}
		}
		return false
	}

	noUserUUID, ok := annos[EnflameNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for Iluvatar not user uuid [%s], device id is %s", noUserUUID, d.ID)
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

func (dev *EnflameDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *EnflameDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Start to count enflame devices for container ", ctr.Name)
	resourceCount := corev1.ResourceName(EnflameResourceCount)
	resourcePercentage := corev1.ResourceName(EnflameResourcePercentage)
	v, ok := ctr.Resources.Limits[resourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[resourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found enflame devices")
			memnum := 100
			mem, ok := ctr.Resources.Limits[resourcePercentage]
			if !ok {
				mem, ok = ctr.Resources.Requests[resourcePercentage]
			}
			if ok {
				memnum = int(mem.Value())
			}
			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             EnflameGPUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: 0,
				Coresreq:         0,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *EnflameDevices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	return true
}

func (dev *EnflameDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	return 0
}

func (dev *EnflameDevices) AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}
