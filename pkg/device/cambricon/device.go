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
	"errors"
	"flag"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	HandshakeAnnos         = "hami.io/node-handshake-mlu"
	RegisterAnnos          = "hami.io/node-mlu-register"
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
	MLUNoUseUUID = "cambricon.com/nouse-gpuuuid"
)

var (
	MLUResourceCount  string
	MLUResourceMemory string
)

type CambriconDevices struct {
}

func InitMLUDevice() *CambriconDevices {
	util.HandshakeAnnos[CambriconMLUDevice] = HandshakeAnnos
	return &CambriconDevices{}
}

func (dev *CambriconDevices) NodeCleanUp(nn string) error {
	return util.MarkAnnotationsToDelete(CambriconMLUDevice, nn)
}

func (dev *CambriconDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return util.CheckHealth(devType, n)
}

func (dev *CambriconDevices) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	devEncoded, ok := n.Annotations[RegisterAnnos]
	if !ok {
		return []*api.DeviceInfo{}, errors.New("annos not found " + RegisterAnnos)
	}
	nodedevices, err := util.DecodeNodeDevices(devEncoded)
	if err != nil {
		klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", devEncoded)
		return []*api.DeviceInfo{}, err
	}
	if len(nodedevices) == 0 {
		klog.InfoS("no gpu device found", "node", n.Name, "device annotation", devEncoded)
		return []*api.DeviceInfo{}, errors.New("no gpu found on node")
	}
	devDecoded := util.EncodeNodeDevices(nodedevices)
	klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", devDecoded)
	return nodedevices, nil
}

func (dev *CambriconDevices) AssertNuma(annos map[string]string) bool {
	return false
}

func (dev *CambriconDevices) ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&MLUResourceCount, "mlu-name", "cambricon.com/mlunum", "mlu resource count")
	fs.StringVar(&MLUResourceMemory, "mlu-memory", "cambricon.com/mlumem", "mlu memory resource")
}

func (dev *CambriconDevices) MutateAdmission(ctr *corev1.Container) bool {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(MLUResourceMemory)]
	if ok {
		if ctr.Lifecycle == nil {
			ctr.Lifecycle = &corev1.Lifecycle{PostStart: nil}
		}
		ctr.Lifecycle.PostStart = &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{Command: []string{"/usr/bin/smlu-containerd"}}}
		return true
	}
	_, ok = ctr.Resources.Limits[corev1.ResourceName(MLUResourceCount)]
	return ok
}

func checkMLUtype(annos map[string]string, cardtype string) bool {
	inuse, ok := annos[MLUInUse]
	if ok {
		if !strings.Contains(inuse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(inuse)) {
				return true
			}
		} else {
			for _, val := range strings.Split(inuse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return true
				}
			}
		}
		return false
	}
	nouse, ok := annos[MLUNoUse]
	if ok {
		if !strings.Contains(nouse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(nouse)) {
				return false
			}
		} else {
			for _, val := range strings.Split(nouse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return false
				}
			}
		}
		return true
	}
	return true
}

func (dev *CambriconDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Contains(n.Type, CambriconMLUDevice) {
		if !strings.Contains(d.Type, "370") && n.Memreq != 0 {
			return true, false, false
		}
		if strings.Contains(d.Type, "370") && n.Memreq == 0 && d.Used > 0 {
			return true, false, false
		}
		return true, checkMLUtype(annos, d.Type), false
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
	v, ok := ctr.Resources.Limits[mluResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[mluResourceCount]
	} else {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found mlu devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[mluResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[mluResourceMem]
			} else {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums)
				}
			}
			return util.ContainerDeviceRequest{
				Nums:   int32(n),
				Type:   CambriconMLUDevice,
				Memreq: int32(memnum),
			}
		}
	}
	return util.ContainerDeviceRequest{}
}

func (dev *CambriconDevices) PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string {
	return *annoinput
}
