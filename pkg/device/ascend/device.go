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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type Devices struct {
	config           VNPUConfig
	nodeRegisterAnno string
	useUUIDAnno      string
	noUseUUIDAnno    string
	handshakeAnno    string
}

type RuntimeInfo struct {
	UUID string `json:"UUID,omitempty"`
	Temp string `json:"temp,omitempty"`
}

//const (
//	useUUIDAnno   = "huawei.com/use-ascend-uuid"
//	noUseUUIDAnno = "huawei.com/no-use-ascend-uuid"
//)

var (
	enableAscend bool
	configFile   string
)

func (dev *Devices) trimMemory(m int64) (int64, string) {
	for i := 0; i < len(dev.config.Templates); i++ {
		if m <= dev.config.Templates[i].Memory {
			return dev.config.Templates[i].Memory, dev.config.Templates[i].Name
		}
	}
	if m <= dev.config.MemoryCapacity {
		return dev.config.MemoryAllocatable, ""
	}
	return 0, ""
}

func InitDevices() []*Devices {
	var devs []*Devices
	if !enableAscend {
		return devs
	}
	config, err := LoadConfig(configFile)
	if err != nil {
		klog.Fatalf("failed to load ascend vnpu config file %s: %v", configFile, err)
	}
	for _, vnpu := range config.VNPUs {
		commonWord := vnpu.CommonWord
		dev := &Devices{
			config:           vnpu,
			nodeRegisterAnno: fmt.Sprintf("hami.io/node-register-%s", commonWord),
			useUUIDAnno:      fmt.Sprintf("hami.io/use-%s-uuid", commonWord),
			noUseUUIDAnno:    fmt.Sprintf("hami.io/no-use-%s-uuid", commonWord),
			handshakeAnno:    fmt.Sprintf("hami.io/node-handshake-%s", commonWord),
		}
		sort.Slice(dev.config.Templates, func(i, j int) bool {
			return dev.config.Templates[i].Memory < dev.config.Templates[j].Memory
		})
		util.InRequestDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-to-allocate", commonWord)
		util.SupportDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-allocated", commonWord)
		util.HandshakeAnnos[commonWord] = dev.handshakeAnno
		devs = append(devs, dev)
		klog.Infof("load ascend vnpu config %s: %v", commonWord, dev.config)
	}
	return devs
}

func ParseConfig(fs *flag.FlagSet) {
	fs.BoolVar(&enableAscend, "enable-ascend", false, "enable ascend device")
	fs.StringVar(&configFile, "ascend-config-file", "", "ascend vnpu config file")
}

func (dev *Devices) CommonWord() string {
	return dev.config.CommonWord
}

func (dev *Devices) MutateAdmission(ctr *corev1.Container) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)]
	if !ok {
		return false, nil
	}
	trimMem := dev.config.MemoryAllocatable
	memory, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)]
	if ok {
		trimMem, _ = dev.trimMemory(memory.Value())
		if trimMem <= 0 {
			return false, fmt.Errorf("%s %d is invalid", dev.config.ResourceMemoryName, memory.Value())
		}
	}
	if count.Value() > 1 {
		if trimMem != dev.config.MemoryAllocatable {
			return true, errors.New("vNPU nor supported for multiple devices")
		}
	}
	ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceMemoryName)] = resource.MustParse(fmt.Sprint(trimMem))
	ctr.Resources.Requests[corev1.ResourceName(dev.config.ResourceMemoryName)] = resource.MustParse(fmt.Sprint(trimMem))
	return true, nil
}

func (dev *Devices) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	anno, ok := n.Annotations[dev.nodeRegisterAnno]
	if !ok {
		return []*api.DeviceInfo{}, fmt.Errorf("annos not found %s", dev.nodeRegisterAnno)
	}
	nodeDevices, err := util.UnMarshalNodeDevices(anno)
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal node devices", "node", n.Name, "device annotation", anno)
		return []*api.DeviceInfo{}, err
	}
	if len(nodeDevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", anno)
		return []*api.DeviceInfo{}, errors.New("no device found on node")
	}
	return nodeDevices, nil
}

func (dev *Devices) PatchAnnotations(annoInput *map[string]string, pd util.PodDevices) map[string]string {
	commonWord := dev.CommonWord()
	devList, ok := pd[commonWord]
	if ok && len(devList) > 0 {
		(*annoInput)[util.InRequestDevices[commonWord]] = util.EncodePodSingleDevice(devList)
		(*annoInput)[util.SupportDevices[commonWord]] = util.EncodePodSingleDevice(devList)
		(*annoInput)["predicate-time"] = strconv.FormatInt(time.Now().Unix(), 10)
		allocateStr := fmt.Sprintf("huawei.com/%s", dev.CommonWord())
		var rtInfo []RuntimeInfo
		for _, dp := range devList {
			for _, val := range dp {
				_, temp := dev.trimMemory(int64(val.Usedmem))
				rtInfo = append(rtInfo, RuntimeInfo{
					UUID: val.UUID,
					Temp: temp,
				})
			}
		}
		s, err := json.Marshal(rtInfo)
		if err != nil {
			klog.ErrorS(err, "failed to marshal runtime info", "runtime info", rtInfo)
		}
		(*annoInput)[allocateStr] = string(s)
	}
	return *annoInput
}

func (dev *Devices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *Devices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *Devices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(dev.handshakeAnno, nn)
}

func (dev *Devices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, dev.CommonWord()) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *Devices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[dev.useUUIDAnno]
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

	noUserUUID, ok := annos[dev.noUseUUIDAnno]
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

func (dev *Devices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return util.CheckHealth(devType, n)
}

func (dev *Devices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Infof("Counting %s devices", dev.config.CommonWord)
	ascendResourceCount := corev1.ResourceName(dev.config.ResourceName)
	ascendResourceMem := corev1.ResourceName(dev.config.ResourceMemoryName)
	v, ok := ctr.Resources.Limits[ascendResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[ascendResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found AscendDevices devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[ascendResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[ascendResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					m, _ := dev.trimMemory(memnums)
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
				Type:             dev.CommonWord(),
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}
