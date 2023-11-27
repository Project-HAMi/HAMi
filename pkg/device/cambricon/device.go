package cambricon

import (
	"flag"
	"strings"

	"4pd.io/k8s-vgpu/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

const (
	HandshakeAnnos         = "4pd.io/node-handshake-mlu"
	RegisterAnnos          = "4pd.io/node-mlu-register"
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
	return &CambriconDevices{}
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
	klog.Infof("Counting mlu devices")
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
