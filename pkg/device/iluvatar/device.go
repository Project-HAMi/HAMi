package iluvatar

import (
	"flag"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

type IluvatarDevices struct {
}

const (
	//HandshakeAnnos        = "hami.sh/node-handshake-dcu"
	//#RegisterAnnos         = "hami.sh/node--register"
	IluvatarGPUDevice     = "Iluvatar"
	IluvatarGPUCommonWord = "Iluvatar"
	//DCUInUse           = "hygon.com/use-dcutype"
	//DCUNoUse           = "hygon.com/nouse-dcutype"
)

var (
	IluvatarResourceCount  string
	IluvatarResourceMemory string
	IluvatarResourceCores  string
)

func InitIluvatarDevice() *IluvatarDevices {
	return &IluvatarDevices{}
}

func (dev *IluvatarDevices) ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&IluvatarResourceCount, "iluvatar-name", "iluvatar.ai/gpu", "iluvatar resource count")
	fs.StringVar(&IluvatarResourceMemory, "iluvatar-memory", "iluvatar.ai/vcuda-memory", "iluvatar memory resource")
	fs.StringVar(&IluvatarResourceCores, "iluvatar-cores", "iluvatar.ai/vcuda-core", "iluvatar core resource")
}

func (dev *IluvatarDevices) MutateAdmission(ctr *corev1.Container) bool {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(IluvatarResourceCount)]
	return ok
}

func (dev *IluvatarDevices) GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error) {
	nodedevices := []*api.DeviceInfo{}
	return nodedevices, nil
}

/*func checkDCUtype(annos map[string]string, cardtype string) bool {
	inuse, ok := annos[DCUInUse]
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
	nouse, ok := annos[DCUNoUse]
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
}*/

func (dev *IluvatarDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *IluvatarDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	//	if strings.Compare(n.Type, HygonDCUDevice) == 0 {
	//		return true, checkDCUtype(annos, d.Type), false
	//	}
	return false, false, false
}

func (dev *IluvatarDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return util.CheckHealth(devType, n)
}

func (dev *IluvatarDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Infof("Counting dcu devices")
	dcuResourceCount := corev1.ResourceName(IluvatarResourceCount)
	dcuResourceMem := corev1.ResourceName(IluvatarResourceMemory)
	dcuResourceCores := corev1.ResourceName(IluvatarResourceCores)
	v, ok := ctr.Resources.Limits[dcuResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[dcuResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found dcu devices")
			memnum := 0
			mem, ok := ctr.Resources.Limits[dcuResourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[dcuResourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums)
				}
			}
			corenum := int32(0)
			core, ok := ctr.Resources.Limits[dcuResourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[dcuResourceCores]
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
				Type:             IluvatarGPUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			}
		}
	}
	return util.ContainerDeviceRequest{}
}
