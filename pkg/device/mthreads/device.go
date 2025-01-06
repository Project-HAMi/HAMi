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

package mthreads

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type MthreadsDevices struct {
}

const (
	MthreadsGPUDevice       = "Mthreads"
	MthreadsGPUCommonWord   = "Mthreads"
	MthreadsDeviceSelection = "mthreads.com/gpu-index"
	// IluvatarUseUUID is user can use specify Iluvatar device for set Iluvatar UUID.
	MthreadsUseUUID = "mthreads.ai/use-gpuuuid"
	// IluvatarNoUseUUID is user can not use specify Iluvatar device for set Iluvatar UUID.
	MthreadsNoUseUUID        = "mthreads.ai/nouse-gpuuuid"
	MthreadsAssignedGPUIndex = "mthreads.com/gpu-index"
	MthreadsAssignedNode     = "mthreads.com/predicate-node"
	MthreadsPredicateTime    = "mthreads.com/predicate-time"
	coresPerMthreadsGPU      = 16
	memoryPerMthreadsGPU     = 96
)

var (
	MthreadsResourceCount  string
	MthreadsResourceMemory string
	MthreadsResourceCores  string
	legalMemoryslices      = []int64{2, 4, 8, 16, 32, 64, 96}
)

type MthreadsConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
}

func InitMthreadsDevice(config MthreadsConfig) *MthreadsDevices {
	MthreadsResourceCount = config.ResourceCountName
	MthreadsResourceCores = config.ResourceCoreName
	MthreadsResourceMemory = config.ResourceMemoryName
	util.InRequestDevices[MthreadsGPUDevice] = "hami.io/mthreads-vgpu-devices-to-allocate"
	util.SupportDevices[MthreadsGPUDevice] = "hami.io/mthreads-vgpu-devices-allocated"
	return &MthreadsDevices{}
}

func (dev *MthreadsDevices) CommonWord() string {
	return MthreadsGPUCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&MthreadsResourceCount, "mthreads-name", "mthreads.com/vgpu", "mthreads resource count")
	fs.StringVar(&MthreadsResourceMemory, "mthreads-memory", "mthreads.com/sgpu-memory", "mthreads memory resource")
	fs.StringVar(&MthreadsResourceCores, "mthreads-cores", "mthreads.com/sgpu-core", "mthreads core resource")
}

func (dev *MthreadsDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	count, ok := ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceCount)]
	if ok {
		if count.Value() > 1 {
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceCores)] = *resource.NewQuantity(count.Value()*int64(coresPerMthreadsGPU), resource.DecimalSI)
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceMemory)] = *resource.NewQuantity(count.Value()*int64(memoryPerMthreadsGPU), resource.DecimalSI)
			p.Annotations["mthreads.com/request-gpu-num"] = fmt.Sprint(count.Value())
			return ok, nil
		}
		mem, memok := ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceMemory)]
		if !memok {
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceCores)] = *resource.NewQuantity(count.Value()*int64(coresPerMthreadsGPU), resource.DecimalSI)
			ctr.Resources.Limits[corev1.ResourceName(MthreadsResourceMemory)] = *resource.NewQuantity(count.Value()*int64(memoryPerMthreadsGPU), resource.DecimalSI)
		} else {
			memnum, _ := mem.AsInt64()
			found := false
			for _, val := range legalMemoryslices {
				if memnum == val {
					found = true
					break
				}
			}
			if !found {
				return true, errors.New("sGPU memory request value is invalid, valid values are [1, 2, 4, 8, 16, 32, 64, 96]")
			}
		}
	}
	return ok, nil
}

func (dev *MthreadsDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	nodedevices := []*util.DeviceInfo{}
	i := 0
	cores, _ := n.Status.Capacity.Name(corev1.ResourceName(MthreadsResourceCores), resource.DecimalSI).AsInt64()
	memoryTotal, _ := n.Status.Capacity.Name(corev1.ResourceName(MthreadsResourceMemory), resource.DecimalSI).AsInt64()
	for int64(i)*coresPerMthreadsGPU < cores {
		nodedevices = append(nodedevices, &util.DeviceInfo{
			Index:   uint(i),
			ID:      n.Name + "-mthreads-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  int32(memoryTotal * 512 * coresPerMthreadsGPU / cores),
			Devcore: coresPerMthreadsGPU,
			Type:    MthreadsGPUDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *MthreadsDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[MthreadsGPUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[util.SupportDevices[MthreadsGPUDevice]] = util.EncodePodSingleDevice(devlist)
		for _, dp := range devlist {
			if len(dp) > 0 {
				value := ""
				for _, val := range dp {
					value = value + fmt.Sprint(val.Idx) + ","
				}
				if len(value) > 0 {
					(*annoinput)[MthreadsAssignedGPUIndex] = strings.TrimRight(value, ",")
					//(*annoinput)[MthreadsAssignedNode]=
					tmp := strconv.FormatInt(time.Now().UnixNano(), 10)
					(*annoinput)[MthreadsPredicateTime] = tmp
					(*annoinput)[MthreadsAssignedNode] = (*annoinput)[util.AssignedNodeAnnotations]
				}
			}
		}
	}
	klog.Infoln("annoinput", (*annoinput))
	return *annoinput
}

func (dev *MthreadsDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MthreadsDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *MthreadsDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *MthreadsDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, MthreadsGPUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *MthreadsDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[MthreadsUseUUID]
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

	noUserUUID, ok := annos[MthreadsNoUseUUID]
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

func (dev *MthreadsDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *MthreadsDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Start to count mthreads devices for container ", ctr.Name)
	mthreadsResourceCount := corev1.ResourceName(MthreadsResourceCount)
	mthreadsResourceMem := corev1.ResourceName(MthreadsResourceMemory)
	mthreadsResourceCores := corev1.ResourceName(MthreadsResourceCores)
	v, ok := ctr.Resources.Limits[mthreadsResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[mthreadsResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.InfoS("Detected mthreads device request",
				"container", ctr.Name,
				"deviceCount", n)
			memnum := 0
			mem, ok := ctr.Resources.Limits[mthreadsResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[mthreadsResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums) * 512
					klog.InfoS("Memory allocation calculated",
						"container", ctr.Name,
						"requestedMem", memnums,
						"allocatedMem", memnum)
				}
			}
			corenum := int32(0)
			core, ok := ctr.Resources.Limits[mthreadsResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[mthreadsResourceCores]
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
				Type:             MthreadsGPUDevice,
				Memreq:           int32(memnum) / int32(n),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum / int32(n),
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *MthreadsDevices) CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllocate util.ContainerDevices, device *util.DeviceUsage) bool {
	for _, ctrs := range (*allocated)[device.Type] {
		for _, ctrdev := range ctrs {
			if strings.Compare(ctrdev.UUID, device.ID) != 0 {
				klog.InfoS("Mthreads needs all devices on a device", "used", ctrdev.UUID, "allocating", device.ID)
				return false
			}
		}
	}
	return true
}

func (dev *MthreadsDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32 {
	return 0
}

func (dev *MthreadsDevices) AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}
