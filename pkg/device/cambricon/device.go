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

package cambricon

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	CambriconMLUDevice     = "MLU"
	CambriconMLUCommonWord = "MLU"
	MluMemSplitLimit       = "CAMBRICON_SPLIT_MEMS"
	MluMemSplitIndex       = "CAMBRICON_SPLIT_VISIBLE_DEVICES"
	MluMemSplitEnable      = "CAMBRICON_SPLIT_ENABLE"
	MLUInUse               = "cambricon.com/use-mlutype"
	MLUNoUse               = "cambricon.com/nouse-mlutype"
	// MLUUseUUID annotation specifies a comma-separated list of MLU UUIDs to use.
	MLUUseUUID = "cambricon.com/use-gpuuuid"
	// MLUNoUseUUID annotation specifies a comma-separated list of MLU UUIDs to exclude.
	MLUNoUseUUID          = "cambricon.com/nouse-gpuuuid"
	DsmluLockTime         = "cambricon.com/dsmlu.lock"
	DsmluProfile          = "CAMBRICON_DSMLU_PROFILE"
	DsmluResourceAssigned = "CAMBRICON_DSMLU_ASSIGNED"
	retry                 = 5
)

var (
	MLUResourceCount  string
	MLUResourceMemory string
	MLUResourceCores  string
)

type CambriconConfig struct {
	ResourceCountName  string `yaml:"resourceCountName"`
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	ResourceCoreName   string `yaml:"resourceCoreName"`
}

type CambriconDevices struct {
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&MLUResourceCount, "cambricon-mlu-name", "cambricon.com/mlu", "cambricon mlu resource count")
	fs.StringVar(&MLUResourceMemory, "cambricon-mlu-memory", "cambricon.com/mlu.smlu.vmemory", "cambricon mlu memory resource")
	fs.StringVar(&MLUResourceCores, "cambricon-mlu-cores", "cambricon.com/mlu.smlu.vcore", "cambricon mlu core resource")
}

func InitMLUDevice(config CambriconConfig) *CambriconDevices {
	MLUResourceCount = config.ResourceCountName
	MLUResourceMemory = config.ResourceMemoryName
	MLUResourceCores = config.ResourceCoreName
	_, ok := device.InRequestDevices[CambriconMLUDevice]
	if !ok {
		device.InRequestDevices[CambriconMLUDevice] = "hami.io/cambricon-mlu-devices-to-allocate"
		device.SupportDevices[CambriconMLUDevice] = "hami.io/cambricon-mlu-devices-allocated"
	}
	return &CambriconDevices{}
}

func (dev *CambriconDevices) CommonWord() string {
	return CambriconMLUCommonWord
}

func (dev *CambriconDevices) setNodeLock(node *corev1.Node) error {
	ctx := context.Background()
	if _, ok := node.Annotations[DsmluLockTime]; ok {
		return fmt.Errorf("node %s is locked", node.Name)
	}

	patchedAnnotation, err := json.Marshal(
		map[string]any{
			"metadata": map[string]map[string]string{"annotations": {
				DsmluLockTime: time.Now().Format(time.RFC3339),
			}}})
	if err != nil {
		klog.ErrorS(err, "Failed to patch node annotation", "node", node.Name)
		return fmt.Errorf("patch node annotation %v", err)
	}

	_, err = client.GetClient().CoreV1().Nodes().Patch(ctx, node.Name, types.StrategicMergePatchType, patchedAnnotation, metav1.PatchOptions{})
	for i := 0; i < retry && err != nil; i++ {
		klog.ErrorS(err, "Failed to patch node annotation", "node", node.Name, "retry", i)
		time.Sleep(time.Duration(rand.Intn(i+1)) * 10 * time.Millisecond)
		_, err = client.GetClient().CoreV1().Nodes().Patch(ctx, node.Name, types.StrategicMergePatchType, patchedAnnotation, metav1.PatchOptions{})
	}
	if err != nil {
		return fmt.Errorf("setNodeLock exceeds retry count %d", retry)
	}
	klog.InfoS("Node lock set", "node", node.Name)
	return nil
}

func (dev *CambriconDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
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
	if _, ok := n.Annotations[DsmluLockTime]; !ok {
		return dev.setNodeLock(n)
	}
	lockTime, err := time.Parse(time.RFC3339, n.Annotations[DsmluLockTime])
	if err != nil {
		return err
	}
	if time.Since(lockTime) > time.Minute*2 {
		klog.InfoS("Node lock expired", "node", n.Name, "lockTime", lockTime)
		err = dev.ReleaseNodeLock(n, p)
		if err != nil {
			klog.ErrorS(err, "Failed to release node lock", "node", n.Name)
			return err
		}
		return dev.setNodeLock(n)
	}
	return fmt.Errorf("node %s has been locked within 2 minutes", n.Name)
}

func (dev *CambriconDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	if n.Annotations == nil {
		return nil
	}
	if _, ok := n.Annotations[DsmluLockTime]; !ok {
		klog.InfoS("Node lock not set", "node", n.Name)
		return nil
	}

	newNode := n.DeepCopy()
	delete(newNode.Annotations, DsmluLockTime)
	_, err := client.GetClient().CoreV1().Nodes().Update(context.Background(), newNode, metav1.UpdateOptions{})
	for i := 0; i < retry && err != nil; i++ {
		klog.ErrorS(err, "Failed to patch node annotation", "node", n.Name, "retry", i)
		time.Sleep(time.Duration(rand.Intn(i+1)) * 10 * time.Millisecond)
		_, err = client.GetClient().CoreV1().Nodes().Update(context.Background(), newNode, metav1.UpdateOptions{})
	}
	if err != nil {
		return fmt.Errorf("releaseNodeLock exceeds retry count %d", retry)
	}
	delete(n.Annotations, DsmluLockTime)
	klog.InfoS("Node lock released", "node", n.Name)
	return nil
}

func (dev *CambriconDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *CambriconDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *CambriconDevices) GetNodeDevices(n corev1.Node) ([]*device.DeviceInfo, error) {
	nodedevices := []*device.DeviceInfo{}
	i := 0
	cards, ok := n.Status.Capacity.Name(corev1.ResourceName(MLUResourceCores), resource.DecimalSI).AsInt64()
	if !ok || cards == 0 {
		return []*device.DeviceInfo{}, fmt.Errorf("device not found %s", MLUResourceCores)
	}
	memoryTotal, _ := n.Status.Capacity.Name(corev1.ResourceName(MLUResourceMemory), resource.DecimalSI).AsInt64()
	for int64(i)*100 < cards {
		nodedevices = append(nodedevices, &device.DeviceInfo{
			Index:        uint(i),
			ID:           n.Name + "-cambricon-mlu-" + fmt.Sprint(i),
			Count:        100,
			Devmem:       int32(memoryTotal * 256 * 100 / cards),
			Devcore:      100,
			Type:         CambriconMLUDevice,
			Numa:         0,
			Health:       true,
			DeviceVendor: CambriconMLUCommonWord,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *CambriconDevices) AssertNuma(annos map[string]string) bool {
	return false
}

func (dev *CambriconDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(MLUResourceCount)]
	return ok, nil
}

func (dev *CambriconDevices) checkType(annos map[string]string, d device.DeviceUsage, n device.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, CambriconMLUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *CambriconDevices) GenerateResourceRequests(ctr *corev1.Container) device.ContainerDeviceRequest {
	klog.Info("Start to count mlu devices for container ", ctr.Name)
	mluResourceCount := corev1.ResourceName(MLUResourceCount)
	mluResourceMem := corev1.ResourceName(MLUResourceMemory)
	mluResourceCores := corev1.ResourceName(MLUResourceCores)
	for idx, val := range ctr.Resources.Limits {
		klog.Infoln("idx=", idx, "val=", val, ctr.Resources.Limits[mluResourceMem])
	}
	v, ok := ctr.Resources.Limits[mluResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[mluResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found cambricon devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[mluResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[mluResourceMem]
			}
			klog.Infoln("mluResourceMem", mem, "ok=", ok, "memoryname=", mluResourceMem)
			if ok {
				memnums, ok := mem.AsInt64()
				klog.Infoln("mluResourceMem", mem, memnums)
				if ok {
					memnum = int(memnums) * 256
				}
			}
			corenum := int32(100)
			core, ok := ctr.Resources.Limits[mluResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[mluResourceCores]
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

			return device.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             CambriconMLUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return device.ContainerDeviceRequest{
		Nums: 0,
	}
}

func (dev *CambriconDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd device.PodDevices) map[string]string {
	devlist, ok := pd[CambriconMLUDevice]
	if ok {
		(*annoinput)[DsmluResourceAssigned] = "false"
		(*annoinput)[DsmluProfile] = fmt.Sprintf("%d_%d_%d", devlist[0][0].Idx, devlist[0][0].Usedcores, devlist[0][0].Usedmem/256)
		deviceStr := device.EncodePodSingleDevice(devlist)
		(*annoinput)[device.InRequestDevices[CambriconMLUDevice]] = deviceStr
		(*annoinput)[device.SupportDevices[CambriconMLUDevice]] = deviceStr
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.InRequestDevices[CambriconMLUDevice], deviceStr)
		klog.V(5).Infof("pod add notation key [%s], values is [%s]", device.SupportDevices[CambriconMLUDevice], deviceStr)
		return *annoinput
	}
	return *annoinput
}

func (dev *CambriconDevices) ScoreNode(node *corev1.Node, podDevices device.PodSingleDevice, previous []*device.DeviceUsage, policy string) float32 {
	return 0
}

func (dev *CambriconDevices) AddResourceUsage(pod *corev1.Pod, n *device.DeviceUsage, ctr *device.ContainerDevice) error {
	n.Used++
	n.Usedcores += ctr.Usedcores
	n.Usedmem += ctr.Usedmem
	return nil
}

func (cam *CambriconDevices) Fit(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *device.NodeInfo, allocated *device.PodDevices) (bool, map[string]device.ContainerDevices, string) {
	k := request
	originReq := k.Nums
	prevnuma := -1
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", k)
	var tmpDevs map[string]device.ContainerDevices
	tmpDevs = make(map[string]device.ContainerDevices)
	reason := make(map[string]int)
	for i := len(devices) - 1; i >= 0; i-- {
		dev := devices[i]
		klog.V(4).InfoS("scoring pod", "pod", klog.KObj(pod), "device", dev.ID, "Memreq", k.Memreq, "MemPercentagereq", k.MemPercentagereq, "Coresreq", k.Coresreq, "Nums", k.Nums, "device index", i)

		_, found, numa := cam.checkType(pod.GetAnnotations(), *dev, k)
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
			tmpDevs = make(map[string]device.ContainerDevices)
		}
		if !device.CheckUUID(pod.GetAnnotations(), dev.ID, MLUUseUUID, MLUNoUseUUID, cam.CommonWord()) {
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
			//return false, tmpDevs
		}
		if k.Memreq > 0 {
			memreq = k.Memreq
		}
		if k.MemPercentagereq != 101 && k.Memreq == 0 {
			//This incurs an issue
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
			tmpDevs[k.Type] = append(tmpDevs[k.Type], device.ContainerDevice{
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

func (dev *CambriconDevices) GetResourceNames() device.ResourceNames {
	return device.ResourceNames{
		ResourceCountName:  MLUResourceCount,
		ResourceMemoryName: MLUResourceMemory,
		ResourceCoreName:   MLUResourceCores,
	}
}
