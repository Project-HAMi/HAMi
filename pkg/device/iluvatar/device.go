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
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type Devices struct {
	config           VGPUConfig
	nodeRegisterAnno string
	useUUIDAnno      string
	noUseUUIDAnno    string
	handshakeAnno    string
}

const (
	IluvatarGPUDevice       = "Iluvatar"
	IluvatarGPUCommonWord   = "Iluvatar"
	IluvatarDeviceSelection = "iluvatar.ai/predicate-gpu-idx-"
	// IluvatarUseUUID is user can use specify Iluvatar device for set Iluvatar UUID.
	IluvatarUseUUID = "iluvatar.ai/use-gpuuuid"
	// IluvatarNoUseUUID is user can not use specify Iluvatar device for set Iluvatar UUID.
	IluvatarNoUseUUID = "iluvatar.ai/nouse-gpuuuid"
	RegisterAnnos     = "hami.io/node-iluvatar-register"
	HandshakeAnnos    = "hami.io/node-handshake"
	NodeLock          = "hami.io/mutex.lock"
)

const (
	ManagerSocket   = "/var/run/iluvatar-gpu-manager.sock"
	KubeletSocket   = "kubelet.sock"
	MemoryBlockSize = 268435456
)

var (
	IluvatarResourceCount  string = "iluvatar.ai/vgpu"
	IluvatarResourceMemory string = "iluvatar.ai/vcuda-memory"
	IluvatarResourceCores  string = "iluvatar.ai/vcuda-core"
	IluvatarResourcePrefix string = "iluvatar.ai/"

	ResourceCountTemplate  string = "iluvatar.ai/%s-vgpu"
	ResourceMemoryTemplate string = "iluvatar.ai/%s-memory"
	ResourceCoresTemplate  string = "iluvatar.ai/%s-core"
)

type IluvatarConfig struct {
	Driver                   string `json:"driver"             yaml:"driver"`
	QueryPort                int
	KubeConfig               string
	DevicePluginPath         string
	ContainerRuntimeEndpoint string
	DeviceSplitCount         int
	DeviceCoreScaling        int
	MaxDeviceNum             int
	ResourceCountName        string `yaml:"resourceCountName"`
	ResourceMemoryName       string `yaml:"resourceMemoryName"`
	ResourceCoreName         string `yaml:"resourceCoreName"`

	VGPUs []VGPUConfig `yaml:"iluvatars"`
}

type VGPUConfig struct {
	CommonWord         string `yaml:"commonWord"`
	ChipName           string `yaml:"chipName"`
	ResourceName       string `yaml:"resourceName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
}

func InitDevices(config []VGPUConfig) []*Devices {
	var devs []*Devices
	for _, vgpu := range config {
		commonWord := vgpu.CommonWord
		dev := &Devices{
			config:           vgpu,
			nodeRegisterAnno: fmt.Sprintf("hami.io/node-register-%s", commonWord),
			useUUIDAnno:      fmt.Sprintf("hami.io/use-%s-uuid", commonWord),
			noUseUUIDAnno:    fmt.Sprintf("hami.io/no-use-%s-uuid", commonWord),
			handshakeAnno:    fmt.Sprintf("hami.io/node-handshake-%s", commonWord),
		}
		util.InRequestDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-to-allocate", commonWord)
		util.SupportDevices[commonWord] = fmt.Sprintf("hami.io/%s-devices-allocated", commonWord)
		util.HandshakeAnnos[commonWord] = dev.handshakeAnno
		devs = append(devs, dev)
		klog.Infof("load iluvatar gpu config %s: %v", commonWord, dev.config)
	}
	return devs
}

func (dev *Devices) CommonWord() string {
	return dev.config.CommonWord
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&IluvatarResourceCount, "iluvatar-name", "iluvatar.ai/vgpu", "iluvatar resource count")
	fs.StringVar(&IluvatarResourceMemory, "iluvatar-memory", "iluvatar.ai/vcuda-memory", "iluvatar memory resource")
	fs.StringVar(&IluvatarResourceCores, "iluvatar-cores", "iluvatar.ai/vcuda-core", "iluvatar core resource")
}

func (dev *Devices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceName)]
	if ok {
		if count.Value() > 1 {
			ctr.Resources.Limits[corev1.ResourceName(dev.config.ResourceCoreName)] = *resource.NewQuantity(count.Value()*int64(100), resource.DecimalSI)
		}
	}
	return ok, nil
}

func (dev *Devices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	devEncoded, ok := n.Annotations[dev.nodeRegisterAnno]
	if !ok {
		return []*util.DeviceInfo{}, errors.New("annos not found " + dev.nodeRegisterAnno)
	}
	nodedevices, err := util.DecodeNodeDevices(devEncoded)
	if err != nil {
		klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", devEncoded)
		return []*util.DeviceInfo{}, err
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no iluvatar gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*util.DeviceInfo{}, errors.New("no gpu found on node")
	}

	devDecoded := util.EncodeNodeDevices(nodedevices)
	klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devDecoded)
	return nodedevices, nil
}

func (dev *Devices) PatchAnnotations(pod *corev1.Pod, annoInput *map[string]string, pd util.PodDevices) map[string]string {
	commonWord := dev.CommonWord()
	devList, ok := pd[commonWord]
	if ok && len(devList) > 0 {
		(*annoInput)[util.InRequestDevices[commonWord]] = util.EncodePodSingleDevice(devList)
		(*annoInput)[util.SupportDevices[commonWord]] = util.EncodePodSingleDevice(devList)
		(*annoInput)["iluvatar.ai/gpu-assigned"] = "false"
		(*annoInput)["iluvatar.ai/predicate-time"] = strconv.FormatInt(time.Now().UnixNano(), 10)
		for idx, dp := range devList {
			annoKey := IluvatarDeviceSelection + fmt.Sprint(idx)
			value := ""
			for _, val := range dp {
				value = value + fmt.Sprint(val.Idx) + ","
			}
			if len(value) > 0 {
				(*annoInput)[annoKey] = strings.TrimRight(value, ",")
			}
		}
	}

	return *annoInput
}

func (dev *Devices) LockNode(n *corev1.Node, p *corev1.Pod) error {
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

	return nodelock.LockNode(n.Name, NodeLock, p)
}

func (dev *Devices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
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

	return nodelock.ReleaseNodeLock(n.Name, NodeLock, p, false)
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
		klog.V(5).Infof("check uuid for iluvatar user uuid [%s], device id is %s", userUUID, d.ID)
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
		klog.V(5).Infof("check uuid for iluvatar not user uuid [%s], device id is %s", noUserUUID, d.ID)
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
	return true, true
}

func (dev *Devices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Start to count iluvatar devices for container ", ctr.Name)
	iluvatarResourceCount := corev1.ResourceName(dev.config.ResourceName)
	iluvatarResourceMem := corev1.ResourceName(dev.config.ResourceMemoryName)
	iluvatarResourceCores := corev1.ResourceName(dev.config.ResourceCoreName)
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
					// memnum = int(memnums) * 256
					// use vmemBlcok = 256MB
					memnum = int(memnums)
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
				Type:             dev.CommonWord(),
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *Devices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	return true
}

func (dev *Devices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, previous []*util.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *Devices) AddResourceUsage(pod *corev1.Pod, n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (d *Devices) Fit(devices []*util.DeviceUsage, request util.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod, nodeInfo *util.NodeInfo, allocated *util.PodDevices) (bool, map[string]util.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]util.ContainerDevices
	tmpDevs = make(map[string]util.ContainerDevices)
	reason := make(map[string]int)
	// use reverse order
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, numa := d.CheckType(annos, *dev, k)
		if !found {
			reason[common.CardTypeMismatch]++
			klog.V(5).InfoS(common.CardTypeMismatch, "pod", klog.KObj(pod), "device", dev.ID, dev.Type, k.Type)
			continue
		}
		if numa && prevnuma != dev.Numa {
			if k.Nums != originReq {
				reason[common.NumaNotFit] += len(tmpDevs)
				klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", dev.ID, "k.nums", k.Nums, "numa", numa, "prevnuma", prevnuma, "device numa", dev.Numa)
			}
			k.Nums = originReq
			prevnuma = dev.Numa
			tmpDevs = make(map[string]util.ContainerDevices)
		}
		if !d.CheckUUID(annos, *dev) {
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
		if k.Coresreq > 100 {
			klog.ErrorS(nil, "core limit can't exceed 100", "pod", klog.KObj(pod), "device", dev.ID)
			k.Coresreq = 100
			// return false, tmpDevs
		}
		if k.Memreq > 0 {
			memreq = k.Memreq
		}
		if k.MemPercentagereq != 101 && k.Memreq == 0 {
			// This incurs an issue
			memreq = dev.Totalmem * k.MemPercentagereq / 100
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
		if k.Nums > 0 {
			klog.V(5).InfoS("find fit device", "pod", klog.KObj(pod), "device", dev.ID)
			k.Nums--
			tmpDevs[k.Type] = append(tmpDevs[k.Type], util.ContainerDevice{
				Idx:       int(dev.Index),
				UUID:      dev.ID,
				Type:      k.Type,
				Usedmem:   memreq,
				Usedcores: k.Coresreq,
			})
		}
		if k.Nums == 0 {
			klog.V(4).InfoS("device allocate success", "pod", klog.KObj(pod), "allocate device", tmpDevs)
			return true, tmpDevs, ""
		}
	}
	if len(tmpDevs) > 0 {
		reason[common.AllocatedCardsInsufficientRequest] = len(tmpDevs)
		klog.V(5).InfoS(common.AllocatedCardsInsufficientRequest, "pod", klog.KObj(pod), "request", originReq, "allocated", len(tmpDevs))
	}
	return false, tmpDevs, common.GenReason(reason, len(devices))
}
