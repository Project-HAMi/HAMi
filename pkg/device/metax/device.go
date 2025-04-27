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

package metax

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type MetaxDevices struct {
}

const (
	MetaxGPUDevice       = "Metax"
	MetaxGPUCommonWord   = "Metax"
	MetaxAnnotationLoss  = "metax-tech.com/gpu.topology.losses"
	MetaxAnnotationScore = "metax-tech.com/gpu.topology.scores"
)

var (
	MetaxResourceCount string
)

func InitMetaxDevice(config MetaxConfig) *MetaxDevices {
	MetaxResourceCount = config.ResourceCountName
	util.InRequestDevices[MetaxGPUDevice] = "hami.io/metax-gpu-devices-to-allocate"
	util.SupportDevices[MetaxGPUDevice] = "hami.io/metax-gpu-devices-allocated"
	return &MetaxDevices{}
}

func (dev *MetaxDevices) CommonWord() string {
	return MetaxGPUCommonWord
}

func (dev *MetaxDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(MetaxResourceCount)]
	return ok, nil
}

func (dev *MetaxDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	nodedevices := []*util.DeviceInfo{}
	i := 0
	count, _ := n.Status.Capacity.Name(corev1.ResourceName(MetaxResourceCount), resource.DecimalSI).AsInt64()
	for int64(i) < count {
		nodedevices = append(nodedevices, &util.DeviceInfo{
			Index:   uint(i),
			ID:      n.Name + "-metax-" + fmt.Sprint(i),
			Count:   1,
			Devmem:  65536,
			Devcore: 100,
			Type:    MetaxGPUDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *MetaxDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	return *annoinput
}

func (dev *MetaxDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MetaxDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MetaxDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *MetaxDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, MetaxGPUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *MetaxDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	return true
}

func (dev *MetaxDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *MetaxDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Start to count metax devices for container ", ctr.Name)
	metaxResourceCount := corev1.ResourceName(MetaxResourceCount)
	v, ok := ctr.Resources.Limits[metaxResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[metaxResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found metax devices")
			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             MetaxGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         100,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *MetaxDevices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	for _, ctrs := range (*allocated)[device.Type] {
		for _, ctrdev := range ctrs {
			if strings.Compare(ctrdev.UUID, device.ID) != 0 {
				klog.InfoS("Metax needs all devices on a device", "used", ctrdev.UUID, "allocating", device.ID)
				return false
			}
		}
	}
	return true
}

func parseMetaxAnnos(input string, index int) float32 {
	str := (strings.Split(input, ":"))[index]
	if strings.Contains(str, ",") {
		str = strings.Split(str, ",")[0]
	} else {
		str = strings.TrimRight(str, "}")
	}
	res, err := strconv.Atoi(str)
	if err == nil {
		return float32(res)
	}
	return 0
}

func (dev *MetaxDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	sum := 0
	for _, dev := range podDevices {
		sum += len(dev)
	}
	res := float32(0)
	for idx, val := range node.Annotations {
		if strings.Compare(policy, string(util.NodeSchedulerPolicyBinpack)) == 0 {
			if strings.Compare(idx, MetaxAnnotationLoss) == 0 {
				klog.InfoS("Detected annotations", "policy", policy, "key", idx, "value", val, "requesting", sum, "extract", parseMetaxAnnos(val, sum))
				res = res + float32(2000) - parseMetaxAnnos(val, sum)
				break
			}
		}
		if strings.Compare(policy, string(util.NodeSchedulerPolicySpread)) == 0 {
			klog.InfoS("Detected annotations", "policy", policy, "key", idx, "value", val, "requesting", sum, "extract", parseMetaxAnnos(val, sum))
			res = parseMetaxAnnos(val, sum)
			break
		}
	}
	return res
}

func (dev *MetaxDevices) AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}
