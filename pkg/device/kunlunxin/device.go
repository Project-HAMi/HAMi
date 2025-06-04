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

package kunlunxin

import (
	"errors"
	"flag"
	"fmt"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"strings"
)

const (
	XPUDevice      = "XPU"
	XPUCommonWord  = "XPU"
	NodeLock       = "hami.io/mutex.lock"
	RegisterAnnos  = "hami.io/node-register-xpu"
	HandshakeAnnos = "hami.io/node-handshake-xpu"
	UseUUIDAnno    = "hami.io/use-xpu-uuid"
	NoUseUUIDAnno  = "hami.io/no-use-xpu-uuid"
)

var (
	XpuResourceCount  string
	XpuResourceMemory string
)

type Devices struct {
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&XpuResourceCount, "xpu-name", "kunlunxin.com/xpu", "kunlunxin resource name")
	fs.StringVar(&XpuResourceMemory, "xpu-memory-name", "kunlunxin.com/xpu-memory", "kunlunxin resource memory name")
}

type XPUConfig struct {
	XpuResourceName       string `yaml:"resourceCountName"`
	XpuResourceMemoryName string `yaml:"resourceMemoryName"`
}

func InitXPUDevice(config XPUConfig) *Devices {
	XpuResourceCount = config.XpuResourceName
	XpuResourceMemory = config.XpuResourceMemoryName
	util.InRequestDevices[XPUDevice] = "hami.io/xpu-devices-to-allocate"
	util.SupportDevices[XPUDevice] = "hami.io/xpu-devices-allocated"
	util.HandshakeAnnos[XPUDevice] = HandshakeAnnos
	return &Devices{}
}

func (dev *Devices) trimMemory(m int64) int64 {
	temps := []int64{24576, 49152}
	for _, temp := range temps {
		if m <= temp {
			return temp
		}
	}
	return 98304
}

func (dev *Devices) CommonWord() string {
	return XPUDevice
}

func (dev *Devices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(XpuResourceCount)]
	if !ok {
		return false, nil
	}
	memory, ok := ctr.Resources.Limits[corev1.ResourceName(XpuResourceMemory)]
	if ok {
		trimMem := dev.trimMemory(memory.Value())
		ctr.Resources.Limits[corev1.ResourceName(XpuResourceMemory)] = resource.MustParse(fmt.Sprint(trimMem))
		ctr.Resources.Requests[corev1.ResourceName(XpuResourceMemory)] = resource.MustParse(fmt.Sprint(trimMem))
		return true, nil
	}
	return true, nil
}

func (dev *Devices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return util.CheckHealth(devType, n)
}

func (dev *Devices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(HandshakeAnnos, nn)
}

func (dev *Devices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	anno, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*util.DeviceInfo{}, fmt.Errorf("annos not found %s", RegisterAnnos)
	}
	nodeDevices, err := util.UnMarshalNodeDevices(anno)
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal node devices", "node", n.Name, "device annotation", anno)
		return []*util.DeviceInfo{}, err
	}
	if len(nodeDevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", anno)
		return []*util.DeviceInfo{}, errors.New("no device found on node")
	}
	return nodeDevices, nil
}

func (dev *Devices) PatchAnnotations(annoInput *map[string]string, pd util.PodDevices) map[string]string {
	commonWord := dev.CommonWord()
	devList, ok := pd[commonWord]
	if ok && len(devList) > 0 {
		deviceStr := util.EncodePodSingleDevice(devList)
		(*annoInput)[util.InRequestDevices[commonWord]] = deviceStr
		(*annoInput)[util.SupportDevices[commonWord]] = deviceStr
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", util.InRequestDevices[commonWord], deviceStr)
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", util.SupportDevices[commonWord], deviceStr)
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

func (dev *Devices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, dev.CommonWord()) == 0 {
		if d.Used == 0 {
			return true, true, false
		}
		avgMem := d.Usedmem / d.Used
		if avgMem == n.Memreq {
			return true, true, false
		}
		klog.V(5).Infof("split not match %v", d)
		return true, false, false
	}
	return false, false, false
}

func (dev *Devices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[UseUUIDAnno]
	if ok {
		klog.V(5).Infof("check uuid for xpu user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		for _, uuid := range userUUIDs {
			if d.ID == uuid {
				return true
			}
		}
		return false
	}

	noUserUUID, ok := annos[NoUseUUIDAnno]
	if ok {
		klog.V(5).Infof("check uuid for xpu not user uuid [%s], device id is %s", noUserUUID, d.ID)
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

func (dev *Devices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Infof("Counting %s devices", dev.CommonWord())
	xpuResourceCount := corev1.ResourceName(XpuResourceCount)
	xpuResourceMem := corev1.ResourceName(XpuResourceMemory)
	v, ok := ctr.Resources.Limits[xpuResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[xpuResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			memnum := 0
			mem, ok := ctr.Resources.Limits[xpuResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[xpuResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					m := dev.trimMemory(memnums)
					memnum = int(m)
				}
			}
			cores := memnum * 100 / 98304
			mempnum := 0
			if memnum == 0 {
				mempnum = 100
				cores = 100
			}
			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             dev.CommonWord(),
				Memreq:           int32(memnum), //int32(dev.config.MemoryMax),
				MemPercentagereq: int32(mempnum),
				Coresreq:         int32(cores),
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *Devices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	return true
}

func (dev *Devices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	return 0
}

func (dev *Devices) AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}
