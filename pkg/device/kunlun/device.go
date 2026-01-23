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

package kunlun

import (
	"fmt"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	KunlunGPUDevice       = "kunlun"
	KunlunGPUCommonWord   = "kunlun"
	KunlunDeviceSelection = "BAIDU_COM_DEVICE_IDX"
	KunlunUseUUID         = "baidu.com/use-gpuuuid"
	KunlunNoUseUUID       = "baidu.com/nouse-gpuuuid"
	InterGroupConnection  = "0-4,1-5,2-6,3-7"
	InterGroupConnection2 = "0-1-4-5,2-3-6-7,0-2-4-6,1-3-5-7,0-3-4-7,1-2-5-6"
	GroupConnection       = "0-1,0-2,0-3,1-2,1-3,2-3,4-5,4-6,4-7,5-6,5-7,6-7"
)

type KunlunDevices struct {
}

func InitKunlunDevice(config KunlunConfig) *KunlunDevices {
	KunlunResourceCount = config.ResourceCountName
	_, ok := device.SupportDevices[KunlunGPUDevice]
	if !ok {
		device.SupportDevices[KunlunGPUDevice] = "hami.io/kunlun-allocated"
	}
	return &KunlunDevices{}
}

func (dev *KunlunDevices) CommonWord() string {
	return KunlunGPUCommonWord
}

func (dev *KunlunDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(KunlunResourceCount)]
	return ok, nil
}

func (dev *KunlunDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	cards, ok := n.Status.Capacity.Name(corev1.ResourceName(KunlunResourceCount), resource.DecimalSI).AsInt64()
	if !ok || cards == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", KunlunResourceCount)
	}
	for int64(i) < cards {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:        uint(i),
			ID:           n.Name + "-kunlun-" + fmt.Sprint(i),
			Count:        100,
			Devmem:       98304,
			Devcore:      100,
			Type:         KunlunGPUDevice,
			Numa:         0,
			Health:       true,
			DeviceVendor: KunlunGPUCommonWord,
		})
		if int64(i) >= (cards / 2) {
			nodedevices[i].Numa = 1
		}
		i++
	}
	return nodedevices, nil
}

func (dev *KunlunDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[KunlunGPUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[device.SupportDevices[KunlunGPUDevice]] = device.EncodePodSingleDevice(devlist)
		for _, dp := range devlist {
			annoKey := KunlunDeviceSelection
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

func (dev *KunlunDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *KunlunDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *KunlunDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *KunlunDevices) CheckType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool) {
	if strings.Compare(n.Type, KunlunGPUDevice) == 0 {
		return true, false
	}
	return false, false
}

func (dev *KunlunDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *KunlunDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count kunlun devices for container ", ctr.Name)
	kunlunResourceCount := corev1.ResourceName(KunlunResourceCount)
	v, ok := ctr.Resources.Limits[kunlunResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[kunlunResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found kunlunxin devices")

			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             KunlunGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         0,
			}
		}
	}
	return device.ContainerDeviceRequest{}
}

func (dev *KunlunDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	current := []int{}
	prev := []int{}
	for _, dev := range previous {
		if !strings.Contains(dev.Type, KunlunGPUDevice) {
			return 0
		}
		if dev.Used > 0 {
			prev = addidx(prev, int(dev.Index))
		}
	}
	for _, ctr := range podDevices {
		for _, val := range ctr {
			if !strings.Contains(val.Type, KunlunGPUDevice) {
				return 0
			}
			current = addidx(current, val.Idx)
		}
	}
	klog.V(3).Infoln("Score kunlun device previous=", prev, "current=", current)
	return calcscore(prev, current)
}

func (dev *KunlunDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	return nil
}

func (kl *KunlunDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", request)
	tmpDevs := make(map[string]device.ContainerDevices)
	reason := make(map[string]int)

	alloc := graghSelect(devices, request, FitXPU)
	if len(alloc) == 0 {
		reason[common.NumaNotFit]++
		klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", devices, "request nums", request.Nums, "numa")
		return false, tmpDevs, common.GenReason(reason, len(reason))
	}

	for _, dev := range alloc {
		for _, val := range devices {
			if val.Index == uint(dev) {
				tmpDevs[request.Type] = append(tmpDevs[request.Type], device.ContainerDevice{
					Idx:       int(val.Index),
					UUID:      val.ID,
					Type:      request.Type,
					Usedmem:   val.Totalmem,
					Usedcores: val.Totalcore,
				})
				break
			}
		}
	}
	return true, tmpDevs, ""
}

func (dev *KunlunDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  KunlunResourceCount,
		ResourceMemoryName: "",
		ResourceCoreName:   "",
	}
}

func FitXPU(device *device.DeviceUsage, request device.ContainerDeviceRequest) bool {
	return device.Used == 0
}
