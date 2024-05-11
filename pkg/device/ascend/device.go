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

package ascend

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type AscendDevices struct {
}

const (
	AscendDevice          = "Ascend"
	AscendDeviceSelection = "huawei.com/predicate-ascend-idx-"
	// IluvatarUseUUID is user can use specify Iluvatar device for set Iluvatar UUID.
	AscendDeviceUseUUID = "huawei.com/use-ascenduuid"
	// IluvatarNoUseUUID is user can not use specify Iluvatar device for set Iluvatar UUID.
	AscendNoUseUUID = "huawei.com/nouse-ascenduuid"
)

var (
	AscendResourceCount  string
	AscendResourceMemory string
	AscendResourceCores  string
)

func InitDevice() *AscendDevices {
	util.InRequestDevices[AscendDevice] = "hami.io/ascend-devices-to-allocate"
	util.SupportDevices[AscendDevice] = "hami.io/ascend-devices-allocated"
	return &AscendDevices{}
}

func (dev *AscendDevices) ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&AscendResourceCount, "ascend-name", "huawei.com/Ascend910", "iluvatar resource count")
	fs.StringVar(&AscendResourceMemory, "ascend-memory", "huawei.com/Ascend910-memory", "iluvatar memory resource")
}

func (dev *AscendDevices) MutateAdmission(ctr *corev1.Container) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(AscendResourceCount)]
	if ok {
		if count.Value() > 1 {
			memory, ok := ctr.Resources.Limits[corev1.ResourceName(AscendResourceMemory)]
			if ok && memory.Value() != 65536 {
				return true, errors.New("vNPU nor supported for multiple devices")
			}
			return true, nil
		}
		if count.Value() == 1 {
			memory, ok := ctr.Resources.Limits[corev1.ResourceName(AscendResourceMemory)]
			if ok {
				ctr.Resources.Limits[corev1.ResourceName(AscendResourceMemory)] = resource.MustParse(fmt.Sprint(trimMemory(memory.Value())))
				ctr.Resources.Requests[corev1.ResourceName(AscendResourceMemory)] = resource.MustParse(fmt.Sprint(trimMemory(memory.Value())))
			}
			return true, nil
		}
	}
	return false, nil
}

func (dev *AscendDevices) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	nodedevices := []*api.DeviceInfo{}
	i := 0
	cards, _ := n.Status.Capacity.Name(corev1.ResourceName(AscendResourceCount), resource.DecimalSI).AsInt64()
	for int64(i)*10 < cards {
		nodedevices = append(nodedevices, &api.DeviceInfo{
			Index:   i,
			Id:      n.Name + "-Ascend910-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  int32(65536),
			Devcore: 100,
			Type:    AscendDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *AscendDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[AscendDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[util.InRequestDevices[AscendDevice]] = util.EncodePodSingleDevice(devlist)
		(*annoinput)[util.SupportDevices[AscendDevice]] = util.EncodePodSingleDevice(devlist)
		(*annoinput)["predicate-time"] = strconv.FormatInt(time.Now().Unix(), 10)
		allocateStr := "huawei.com/Ascend910"
		for _, dp := range devlist {
			value := ""
			for _, val := range dp {
				value = value + "Ascend910-"
				if val.Usedmem == 16384 {
					value = value + "vir05_1c_16g-"
				} else if val.Usedmem == 32768 {
					value = value + "vir10_3c_32g-"
				}
				value = value + fmt.Sprint(val.Idx) + ","
			}
			if len(value) > 0 {
				(*annoinput)[allocateStr] = strings.TrimRight(value, ",")
			}
		}
	}
	return *annoinput
}

func (dev *AscendDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AscendDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *AscendDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *AscendDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, AscendDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *AscendDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[AscendDeviceUseUUID]
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

	noUserUUID, ok := annos[AscendNoUseUUID]
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

func (dev *AscendDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func trimMemory(i int64) int64 {
	if i <= 16384 {
		return 16384
	}
	if i <= 32768 {
		return 32768
	}
	return 0
}

func (dev *AscendDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Counting ascend 910B devices")
	ascendResourceCount := corev1.ResourceName(AscendResourceCount)
	ascendResourceMem := corev1.ResourceName(AscendResourceMemory)
	v, ok := ctr.Resources.Limits[ascendResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[ascendResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found ascend 910B devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[ascendResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[ascendResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(trimMemory(memnums))
				}
			}
			corenum := int32(0)

			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             AscendDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}
