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

type Ascend310P struct {
}

const (
	Ascend310PName           = "Ascend310P"
	Ascend310PSelection      = "huawei.com/predicate-ascend310p-idx-"
	Ascend310PUseUUID        = "huawei.com/use-ascend310p-uuid"
	Ascend310PNoUseUUID      = "huawei.com/no-use-ascend310p-uuid"
	Ascend310PMaxMemory      = 21 * 1024 // Just for the sake of being able to split, if it exceeds 12G, the whole card will be used.
	Ascend310PMemoryCapacity = 24 * 1024
)

var (
	Ascend310PResourceCount  string
	Ascend310PResourceMemory string
	Ascend310PResourceCores  string
)

type virTemplate struct {
	name   string
	aiCore int
	aiCPU  int
	memory int64
}

var virAscend310PTemplates = []virTemplate{
	{"vir01", 1, 1, 3 * 1024},
	{"vir02", 2, 2, 6 * 1024},
	{"vir04", 4, 4, 12 * 1024},
}

func trimAscend310PMemory(m int64) (int64, string) {
	for i := 0; i < len(virAscend310PTemplates); i++ {
		if m <= virAscend310PTemplates[i].memory {
			return virAscend310PTemplates[i].memory, virAscend310PTemplates[i].name
		}
	}
	if m <= Ascend310PMemoryCapacity {
		// use the whole card
		return Ascend310PMaxMemory, ""
	}
	return 0, ""
}

func InitAscend310P() *Ascend310P {
	util.InRequestDevices[Ascend310PName] = "hami.io/ascend310p-devices-to-allocate"
	util.SupportDevices[Ascend310PName] = "hami.io/ascend310p-devices-allocated"
	return &Ascend310P{}
}

func (dev *Ascend310P) ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&Ascend310PResourceCount, "ascend310p-name", "huawei.com/Ascend310P", "Ascend310P resource count")
	fs.StringVar(&Ascend310PResourceMemory, "ascend310p-memory", "huawei.com/Ascend310P-memory", "Ascend310P memory resource")
}

func (dev *Ascend310P) MutateAdmission(ctr *corev1.Container) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(Ascend310PResourceCount)]
	if !ok {
		return false, nil
	}
	trimMem := int64(Ascend310PMaxMemory)
	memory, ok := ctr.Resources.Limits[corev1.ResourceName(Ascend310PResourceMemory)]
	if ok {
		trimMem, _ = trimAscend310PMemory(memory.Value())
		if trimMem <= 0 {
			return false, fmt.Errorf("ascend310p memory %d is invalid", memory.Value())
		}
	}
	if count.Value() > 1 {
		if trimMem != int64(Ascend310PMaxMemory) {
			return true, errors.New("vNPU nor supported for multiple devices")
		}
	}
	ctr.Resources.Limits[corev1.ResourceName(Ascend310PResourceMemory)] = resource.MustParse(fmt.Sprint(trimMem))
	ctr.Resources.Requests[corev1.ResourceName(Ascend310PResourceMemory)] = resource.MustParse(fmt.Sprint(trimMem))
	return true, nil
}

func (dev *Ascend310P) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	nodedevices := []*api.DeviceInfo{}
	i := 0
	cards, _ := n.Status.Capacity.Name(corev1.ResourceName(Ascend310PResourceCount), resource.DecimalSI).AsInt64()
	for int64(i)*10 < cards {
		nodedevices = append(nodedevices, &api.DeviceInfo{
			Index:   i,
			Id:      n.Name + "-Ascend310P-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  Ascend310PMaxMemory,
			Devcore: 100,
			Type:    Ascend310PName,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *Ascend310P) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[Ascend310PName]
	if ok && len(devlist) > 0 {
		(*annoinput)[util.InRequestDevices[Ascend310PName]] = util.EncodePodSingleDevice(devlist)
		(*annoinput)[util.SupportDevices[Ascend310PName]] = util.EncodePodSingleDevice(devlist)
		(*annoinput)["predicate-time"] = strconv.FormatInt(time.Now().Unix(), 10)
		allocateStr := "huawei.com/Ascend310P"
		for _, dp := range devlist {
			value := ""
			for _, val := range dp {
				value = value + "Ascend310P-"
				_, temp := trimAscend310PMemory(int64(val.Usedmem))
				value = value + temp + "-"
				value = value + fmt.Sprint(val.Idx) + ","
			}
			if len(value) > 0 {
				(*annoinput)[allocateStr] = strings.TrimRight(value, ",")
			}
		}
	}
	return *annoinput
}

func (dev *Ascend310P) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *Ascend310P) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *Ascend310P) NodeCleanUp(nn string) error {
	return nil
}

func (dev *Ascend310P) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, Ascend310PName) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *Ascend310P) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[Ascend310PUseUUID]
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

	noUserUUID, ok := annos[Ascend310PNoUseUUID]
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

func (dev *Ascend310P) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *Ascend310P) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Counting Ascend310P devices")
	ascendResourceCount := corev1.ResourceName(Ascend310PResourceCount)
	ascendResourceMem := corev1.ResourceName(Ascend310PResourceMemory)
	v, ok := ctr.Resources.Limits[ascendResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[ascendResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found Ascend310P devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[ascendResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[ascendResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					m, _ := trimAscend310PMemory(memnums)
					memnum = int(m)
				}
			}
			corenum := int32(0)

			mempnum := 0
			if memnum == 0 {
				mempnum = 100
			}

			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             Ascend310PName,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}
