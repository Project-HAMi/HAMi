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

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/util"
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
	// MLUUseUUID is user can use specify MLU device for set MLU UUID.
	MLUUseUUID = "cambricon.com/use-gpuuuid"
	// MLUNoUseUUID is user can not use specify MLU device for set MLU UUID.
	MLUNoUseUUID          = "cambricon.com/nouse-gpuuuid"
	DsmluLockTime         = "cambricon.com/dsmlu.lock"
	DsmluProfile          = "CAMBRICON_DSMLU_PROFILE"
	DsmluResourceAssigned = "CAMBRICON_DSMLU_ASSIGHED"
	retry                 = 5
)

var (
	MLUResourceCount  string
	MLUResourceMemory string
	MLUResourceCores  string
)

type CambriconDevices struct {
}

func (dev *CambriconDevices) ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&MLUResourceCount, "cambricon-mlu-name", "cambricon.com/mlu", "cambricon mlu resource count")
	fs.StringVar(&MLUResourceMemory, "cambricon-mlu-memory", "cambricon.com/mlu.smlu.vmemory", "cambricon mlu memory resource")
	fs.StringVar(&MLUResourceCores, "cambricon-mlu-cores", "cambricon.com/mlu.smlu.vcore", "cambricon mlu core resource")
}

func InitMLUDevice() *CambriconDevices {
	util.InRequestDevices[CambriconMLUDevice] = "hami.io/cambricon-mlu-devices-to-allocate"
	util.SupportDevices[CambriconMLUDevice] = "hami.io/cambricon-mlu-devices-allocated"
	return &CambriconDevices{}
}

func (dev *CambriconDevices) setNodeLock(node *corev1.Node) error {
	ctx := context.Background()
	if _, ok := node.ObjectMeta.Annotations[DsmluLockTime]; ok {
		return fmt.Errorf("node %s is locked", node.Name)
	}

	patchedAnnotation, err := json.Marshal(
		map[string]interface{}{
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
		time.Sleep(time.Duration(rand.Intn(i)) * 10 * time.Millisecond)
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
	if _, ok := n.ObjectMeta.Annotations[DsmluLockTime]; !ok {
		return dev.setNodeLock(n)
	}
	lockTime, err := time.Parse(time.RFC3339, n.ObjectMeta.Annotations[DsmluLockTime])
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

	patchData := []byte(`[
				{
					"op": "remove",
					"path": "/metadata/annotations/cambricon.com~1dsmlu.lock"
				}
			]`)

	_, err := client.GetClient().CoreV1().Nodes().Patch(context.TODO(), n.Name, types.JSONPatchType, patchData, metav1.PatchOptions{})
	for i := 0; i < retry && err != nil; i++ {
		klog.ErrorS(err, "Failed to patch node annotation", "node", n.Name, "retry", i)
		time.Sleep(time.Duration(rand.Intn(i)) * 10 * time.Millisecond)
		_, err = client.GetClient().CoreV1().Nodes().Patch(context.TODO(), n.Name, types.JSONPatchType, patchData, metav1.PatchOptions{})
	}
	if err != nil {
		return fmt.Errorf("releaseNodeLock exceeds retry count %d", retry)
	}
	klog.InfoS("Node lock released", "node", n.Name)
	return nil
}

func (dev *CambriconDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *CambriconDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *CambriconDevices) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	nodedevices := []*api.DeviceInfo{}
	i := 0
	cards, _ := n.Status.Capacity.Name(corev1.ResourceName(MLUResourceCores), resource.DecimalSI).AsInt64()
	memoryTotal, _ := n.Status.Capacity.Name(corev1.ResourceName(MLUResourceMemory), resource.DecimalSI).AsInt64()
	for int64(i)*100 < cards {
		nodedevices = append(nodedevices, &api.DeviceInfo{
			Index:   i,
			Id:      n.Name + "-cambricon-mlu-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  int32(memoryTotal * 256 * 100 / cards),
			Devcore: 100,
			Type:    CambriconMLUDevice,
			Numa:    0,
			Health:  true,
		})
		i++
	}
	return nodedevices, nil
}

func (dev *CambriconDevices) AssertNuma(annos map[string]string) bool {
	return false
}

func (dev *CambriconDevices) MutateAdmission(ctr *corev1.Container) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(MLUResourceCount)]
	return ok, nil
}

func (dev *CambriconDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, CambriconMLUDevice) == 0 {
		return true, true, false
	}
	return false, false, false
}

func (dev *CambriconDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[MLUUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for mlu user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		for _, uuid := range userUUIDs {
			if d.ID == uuid {
				return true
			}
		}
		return false
	}

	noUserUUID, ok := annos[MLUNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for mlu not user uuid [%s], device id is %s", noUserUUID, d.ID)
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

func (dev *CambriconDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Counting mlu devices")
	mluResourceCount := corev1.ResourceName(MLUResourceCount)
	mluResourceMem := corev1.ResourceName(MLUResourceMemory)
	mluResourceCores := corev1.ResourceName(MLUResourceCores)
	v, ok := ctr.Resources.Limits[mluResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[mluResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found iluvatar devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[mluResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[mluResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
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

			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             CambriconMLUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{
		Nums: 0,
	}
}

func (dev *CambriconDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[CambriconMLUDevice]
	if ok {
		(*annoinput)[DsmluResourceAssigned] = "false"
		(*annoinput)[DsmluProfile] = fmt.Sprintf("%d_%d_%d", devlist[0][0].Idx, devlist[0][0].Usedcores, devlist[0][0].Usedmem/256)
	}
	return *annoinput
}
