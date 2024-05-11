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

package iluvatar

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type IluvatarDevices struct {
}

const (
	IluvatarGPUDevice       = "Iluvatar"
	IluvatarGPUCommonWord   = "Iluvatar"
	IluvatarDeviceSelection = "iluvatar.ai/predicate-gpu-idx-"
	// IluvatarUseUUID is user can use specify Iluvatar device for set Iluvatar UUID.
	IluvatarUseUUID = "iluvatar.ai/use-gpuuuid"
	// IluvatarNoUseUUID is user can not use specify Iluvatar device for set Iluvatar UUID.
	IluvatarNoUseUUID = "iluvatar.ai/nouse-gpuuuid"
)

var (
	IluvatarResourceCount  string
	IluvatarResourceMemory string
	IluvatarResourceCores  string
)

func InitIluvatarDevice() *IluvatarDevices {
	util.InRequestDevices[IluvatarGPUDevice] = "hami.io/iluvatar-vgpu-devices-to-allocate"
	util.SupportDevices[IluvatarGPUDevice] = "hami.io/iluvatar-vgpu-devices-allocated"
	return &IluvatarDevices{}
}

func (dev *IluvatarDevices) ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&IluvatarResourceCount, "iluvatar-name", "iluvatar.ai/vgpu", "iluvatar resource count")
	fs.StringVar(&IluvatarResourceMemory, "iluvatar-memory", "iluvatar.ai/vcuda-memory", "iluvatar memory resource")
	fs.StringVar(&IluvatarResourceCores, "iluvatar-cores", "iluvatar.ai/vcuda-core", "iluvatar core resource")
}

func (dev *IluvatarDevices) MutateAdmission(ctr *corev1.Container) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(IluvatarResourceCount)]
	if ok {
		if count.Value() > 1 {
			ctr.Resources.Limits[corev1.ResourceName(IluvatarResourceCores)] = *resource.NewQuantity(count.Value()*int64(100), resource.DecimalSI)
		}
	}
	return ok, nil
}

func (dev *IluvatarDevices) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	nodedevices := []*api.DeviceInfo{}
	i := 0
	cards, _ := n.Status.Capacity.Name(corev1.ResourceName(IluvatarResourceCores), resource.DecimalSI).AsInt64()
	memoryTotal, _ := n.Status.Capacity.Name(corev1.ResourceName(IluvatarResourceMemory), resource.DecimalSI).AsInt64()
	for int64(i)*100 < cards {
		i++
		nodedevices = append(nodedevices, &api.DeviceInfo{
			Index:   i,
			Id:      n.Name + "-iluvatar-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  int32(memoryTotal * 256 * 100 / cards),
			Devcore: 100,
			Type:    IluvatarGPUDevice,
			Numa:    0,
			Health:  true,
		})
	}
	return nodedevices, nil
}

func (dev *IluvatarDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[IluvatarGPUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[util.InRequestDevices[IluvatarGPUDevice]] = util.EncodePodSingleDevice(devlist)
		(*annoinput)[util.SupportDevices[IluvatarGPUDevice]] = util.EncodePodSingleDevice(devlist)
		for idx, dp := range devlist {
			annoKey := IluvatarDeviceSelection + fmt.Sprint(idx)
			value := ""
			for _, val := range dp {
				value = value + fmt.Sprint(val.Idx) + ","
			}
			if len(value) > 0 {
				(*annoinput)[annoKey] = strings.TrimRight(value, ",")
			}
		}
	}
	return *annoinput
}

func (dev *IluvatarDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *IluvatarDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *IluvatarDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *IluvatarDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, IluvatarGPUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *IluvatarDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[IluvatarUseUUID]
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

	noUserUUID, ok := annos[IluvatarNoUseUUID]
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

func (dev *IluvatarDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *IluvatarDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Counting iluvatar devices")
	iluvatarResourceCount := corev1.ResourceName(IluvatarResourceCount)
	iluvatarResourceMem := corev1.ResourceName(IluvatarResourceMemory)
	iluvatarResourceCores := corev1.ResourceName(IluvatarResourceCores)
	v, ok := ctr.Resources.Limits[iluvatarResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[iluvatarResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found iluvatar devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[iluvatarResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[iluvatarResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums) * 256
				}
			}
			corenum := int32(0)
			core, ok := ctr.Resources.Limits[iluvatarResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[iluvatarResourceCores]
			}
			if ok {
				corenums, ok := core.AsInt64()
				if ok {
					corenum = int32(corenums)
				}
			}

			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             IluvatarGPUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}
