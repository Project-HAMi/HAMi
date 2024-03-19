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

func (dev *CambriconDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Counting mlu devices")
	mluResourceCount := corev1.ResourceName(MLUResourceCount)
	mluResourceMem := corev1.ResourceName(MLUResourceMemory)
	v, ok := ctr.Resources.Limits[mluResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[mluResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found mlu devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[mluResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[mluResourceMem]
			}
			if ok {
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
