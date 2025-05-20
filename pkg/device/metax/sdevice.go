/*
Copyright 2025 The HAMi Authors.

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

package metax

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	MetaxSGPUCommonWord = "Metax-SGPU"
	MetaxSGPUDevice     = "Metax-SGPU"

	MetaxNodeLock = "hami.io/mutex.lock"
)

var (
	MetaxResourceNameVCount  string
	MetaxResourceNameVCore   string
	MetaxResourceNameVMemory string
)

type MetaxSDevices struct {
}

func InitMetaxSDevice(config MetaxConfig) *MetaxSDevices {
	MetaxResourceNameVCount = config.ResourceVCountName
	MetaxResourceNameVCore = config.ResourceVCoreName
	MetaxResourceNameVMemory = config.ResourceVMemoryName

	util.InRequestDevices[MetaxSGPUDevice] = "hami.io/metax-sgpu-devices-to-allocate"
	util.SupportDevices[MetaxSGPUDevice] = "hami.io/metax-sgpu-devices-allocated"

	return &MetaxSDevices{}
}

func (sdev *MetaxSDevices) CommonWord() string {
	return MetaxSGPUCommonWord
}

func (sdev *MetaxSDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVCount)]
	return ok, nil
}

func (sdev *MetaxSDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	anno, ok := n.Annotations[MetaxSDeviceAnno]
	if !ok {
		return []*util.DeviceInfo{}, errors.New("annos not found " + MetaxSDeviceAnno)
	}

	metaxSDevices := []*MetaxSDeviceInfo{}
	err := json.Unmarshal([]byte(anno), &metaxSDevices)
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal metax sdevices", "node", n.Name, "sdevice annotation", anno)
		return []*util.DeviceInfo{}, err
	}

	if len(metaxSDevices) == 0 {
		klog.InfoS("no metax sgpu device found", "node", n.Name, "sdevice annotation", anno)
		return []*util.DeviceInfo{}, errors.New("no sdevice found on node")
	}

	klog.V(5).Infof("node[%s] metax sdevice information: %s", n.Name, NodeMetaxSDeviceInfo(metaxSDevices).String())
	return convertMetaxSDeviceToHAMIDevice(metaxSDevices), nil
}

func (sdev *MetaxSDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[MetaxSGPUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := util.EncodePodSingleDevice(devlist)

		// hami
		(*annoinput)[util.InRequestDevices[MetaxSGPUDevice]] = deviceStr
		(*annoinput)[util.SupportDevices[MetaxSGPUDevice]] = deviceStr
		klog.Infof("pod add annotation key [%s, %s], values is [%s]",
			util.InRequestDevices[MetaxSGPUDevice], util.SupportDevices[MetaxSGPUDevice], deviceStr)

		// metax
		metaxPodDevice := convertHAMIPodDeviceToMetaxPodDevice(devlist)
		klog.Infof("metaxPodDevice allocated info: %s", metaxPodDevice.String())

		byte, err := json.Marshal(metaxPodDevice)
		if err != nil {
			klog.Errorf("metaxPodDevice marshal failed, origin values is [%s]", deviceStr)
		}

		(*annoinput)[MetaxAllocatedSDevices] = string(byte)
		(*annoinput)[MetaxPredicateTime] = strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	return *annoinput
}

func (sdev *MetaxSDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	found := false

	for _, val := range p.Spec.Containers {
		if (sdev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	return nodelock.LockNode(n.Name, MetaxNodeLock, p)
}

func (sdev *MetaxSDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	found := false

	for _, val := range p.Spec.Containers {
		if (sdev.GenerateResourceRequests(&val).Nums) > 0 {
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	return nodelock.ReleaseNodeLock(n.Name, MetaxNodeLock, p, false)
}

func (sdev *MetaxSDevices) NodeCleanUp(nn string) error {
	return nil
}

func (sdev *MetaxSDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, MetaxSGPUDevice) == 0 {
		return true, true, false
	}

	return false, false, false
}

func (sdev *MetaxSDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	useUUIDAnno, ok := annos[MetaxUseUUID]
	if ok {
		klog.V(5).Infof("check UUID for metax, useUUID[%s], deviceID[%s]", useUUIDAnno, d.ID)

		useUUIDs := strings.Split(useUUIDAnno, ",")
		if slices.Contains(useUUIDs, d.ID) {
			klog.V(5).Infof("check UUID pass, the deviceID[%s]", d.ID)
			return true
		}
		return false
	}

	noUseUUIDAnno, ok := annos[MetaxNoUseUUID]
	if ok {
		klog.V(5).Infof("check UUID for metax, nouseUUID[%s], deviceID[%s]", noUseUUIDAnno, d.ID)

		noUseUUIDs := strings.Split(noUseUUIDAnno, ",")
		if slices.Contains(noUseUUIDs, d.ID) {
			klog.V(5).Infof("check UUID failed to pass, the deviceID[%s]", d.ID)
			return false
		}
		return true
	}

	return true
}

func (sdev *MetaxSDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	devices, _ := sdev.GetNodeDevices(*n)

	return len(devices) > 0, true
}

func (sdev *MetaxSDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	value, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVCount)]
	if !ok {
		return util.ContainerDeviceRequest{}
	}

	count, ok := value.AsInt64()
	if !ok {
		klog.Errorf("container<%s> resource<%s> cannot decode to int64",
			ctr.Name, MetaxResourceNameVCount)
		return util.ContainerDeviceRequest{}
	}

	core := int64(100)
	coreQuantity, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVCore)]
	if ok {
		if v, ok := coreQuantity.AsInt64(); ok {
			core = v
		}
	}

	mem := int64(0)
	memQuantity, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceNameVMemory)]
	if ok {
		if v, ok := memQuantity.AsInt64(); ok {
			hasUnit := strings.IndexFunc(memQuantity.String(), func(c rune) bool {
				return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			}) >= 0

			// if user not set unit, default unit is Gi
			if hasUnit {
				mem = v / 1024 / 1024
			} else {
				mem = v * 1024
			}
		}
	}

	memPercent := 0
	if mem == 0 {
		memPercent = 100
	}

	klog.Infof("container<%s> request<count:%d, mem:%d, memPercent:%d, core:%d>",
		ctr.Name, count, mem, memPercent, core)
	return util.ContainerDeviceRequest{
		Nums:             int32(count),
		Type:             MetaxSGPUDevice,
		Memreq:           int32(mem),
		MemPercentagereq: int32(memPercent),
		Coresreq:         int32(core),
	}
}

func (sdev *MetaxSDevices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	return true
}

func (sdev *MetaxSDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	return 0
}

func (sdev *MetaxSDevices) AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem

	return nil
}
