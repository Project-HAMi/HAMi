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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type MetaxDevices struct {
}

const (
	MetaxGPUDevice       = "Metax-GPU"
	MetaxGPUCommonWord   = "Metax-GPU"
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
	devlist, ok := pd[MetaxGPUDevice]
	if ok && len(devlist) > 0 {
		deviceStr := util.EncodePodSingleDevice(devlist)

		(*annoinput)[util.InRequestDevices[MetaxGPUDevice]] = deviceStr
		(*annoinput)[util.SupportDevices[MetaxGPUDevice]] = deviceStr
		klog.Infof("pod add annotation key [%s, %s], values is [%s]",
			util.InRequestDevices[MetaxGPUDevice], util.SupportDevices[MetaxGPUDevice], deviceStr)
	}

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
	count, _ := n.Status.Capacity.Name(corev1.ResourceName(MetaxResourceCount), resource.DecimalSI).AsInt64()

	return count > 0, true
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

func parseMetaxAnnos(annos string, index int) float32 {
	scoreMap := map[int]int{}
	err := json.Unmarshal([]byte(annos), &scoreMap)
	if err != nil {
		klog.Warningf("annos[%s] Unmarshal failed, %v", annos, err)
		return 0
	}

	res, ok := scoreMap[index]
	if !ok {
		klog.Warningf("scoreMap[%v] not contains [%d]", scoreMap, index)
		return 0
	}

	return float32(res)
}

func (dev *MetaxDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	sum := 0
	for _, dev := range podDevices {
		sum += len(dev)
	}

	res := float32(0)
	if policy == string(util.NodeSchedulerPolicyBinpack) {
		lossAnno, ok := node.Annotations[MetaxAnnotationLoss]
		if ok {
			// it's preferred to select the node with lower loss
			loss := parseMetaxAnnos(lossAnno, sum)
			res = 2000 - loss

			klog.InfoS("Detected annotations", "policy", policy, "key", MetaxAnnotationLoss, "value", lossAnno, "requesting", sum, "extract", loss)
		}
	} else if policy == string(util.NodeSchedulerPolicySpread) {
		scoreAnno, ok := node.Annotations[MetaxAnnotationScore]
		if ok {
			// it's preferred to select the node with higher score
			// But we have to give it a smaller value because of Spread policy
			score := parseMetaxAnnos(scoreAnno, sum)
			res = 2000 - score

			klog.InfoS("Detected annotations", "policy", policy, "key", MetaxAnnotationScore, "value", scoreAnno, "requesting", sum, "extract", score)
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
