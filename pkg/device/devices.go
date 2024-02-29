package device

import (
	"context"
	"flag"
	"os"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

type Devices interface {
	MutateAdmission(ctr *v1.Container) bool
	CheckHealth(devType string, n *v1.Node) (bool, bool)
	NodeCleanUp(nn string) error
	GetNodeDevices(n v1.Node) ([]*api.DeviceInfo, error)
	CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool)
	GenerateResourceRequests(ctr *v1.Container) util.ContainerDeviceRequest
	PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string
	ParseConfig(fs *flag.FlagSet)
}

var (
	HandshakeAnnos = map[string]string{}
	RegisterAnnos  = map[string]string{}
	/*
		KnownDevice    = map[string]string{
			nvidia.HandshakeAnnos:    nvidia.RegisterAnnos,
			cambricon.HandshakeAnnos: cambricon.RegisterAnnos,
			hygon.HandshakeAnnos:     hygon.RegisterAnnos,
		}*/
	DevicesToHandle []string
)

var devices map[string]Devices
var DebugMode bool

func GetDevices() map[string]Devices {
	return devices
}

func init() {
	devices = make(map[string]Devices)
	devices["Cambricon"] = cambricon.InitMLUDevice()
	devices["NVIDIA"] = nvidia.InitNvidiaDevice()
	devices["Hygon"] = hygon.InitDCUDevice()
	DevicesToHandle = []string{}
	DevicesToHandle = append(DevicesToHandle, nvidia.NvidiaGPUCommonWord)
	DevicesToHandle = append(DevicesToHandle, cambricon.CambriconMLUCommonWord)
	DevicesToHandle = append(DevicesToHandle, hygon.HygonDCUCommonWord)
}

func PodAllocationTrySuccess(nodeName string, pod *v1.Pod) {
	refreshed, _ := client.GetClient().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	annos := refreshed.Annotations[util.AssignedIDsToAllocateAnnotations]
	klog.Infoln("TrySuccess:", annos)
	for _, val := range DevicesToHandle {
		if strings.Contains(annos, val) {
			return
		}
	}
	klog.Infoln("AllDevicesAllocateSuccess releasing lock")
	PodAllocationSuccess(nodeName, pod)
}

func PodAllocationSuccess(nodeName string, pod *v1.Pod) {
	newannos := make(map[string]string)
	newannos[util.DeviceBindPhase] = util.DeviceBindSuccess
	err := util.PatchPodAnnotations(pod, newannos)
	if err != nil {
		klog.Errorf("patchPodAnnotations failed:%v", err.Error())
	}
	err = nodelock.ReleaseNodeLock(nodeName)
	if err != nil {
		klog.Errorf("release lock failed:%v", err.Error())
	}
}

func PodAllocationFailed(nodeName string, pod *v1.Pod) {
	newannos := make(map[string]string)
	newannos[util.DeviceBindPhase] = util.DeviceBindFailed
	err := util.PatchPodAnnotations(pod, newannos)
	if err != nil {
		klog.Errorf("patchPodAnnotations failed:%v", err.Error())
	}
	err = nodelock.ReleaseNodeLock(nodeName)
	if err != nil {
		klog.Errorf("release lock failed:%v", err.Error())
	}
}

func GlobalFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	for _, val := range devices {
		val.ParseConfig(fs)
	}
	fs.BoolVar(&DebugMode, "debug", false, "debug mode")
	klog.InitFlags(fs)
	return fs
}

/*
func DefaultDeviceRegistration(n *v1.Node) *[]api.DeviceInfo {
	nodeNames := []string{}
	nodeNames = append(nodeNames, n.Name)
	for devhandsk, devreg := range KnownDevice {
		_, ok := n.Annotations[devreg]
		if !ok {
			continue
		}
		nodedevices, err := util.DecodeNodeDevices(n.Annotations[devreg])
		if err != nil {
			klog.ErrorS(err, "failed to decode node devices", "node", n.Name, "device annotation", n.Annotations[devreg])
			continue
		}
		if len(nodedevices) == 0 {
			klog.InfoS("no node gpu device found", "node", n.Name, "device annotation", n.Annotations[devreg])
			continue
		}
		klog.V(5).InfoS("nodes device information", "node", n.Name, "nodedevices", util.EncodeNodeDevices(nodedevices))
		handshake := n.Annotations[devhandsk]
		if strings.Contains(handshake, "Requesting") {
			formertime, _ := time.Parse("2006.01.02 15:04:05", strings.Split(handshake, "_")[1])
			if time.Now().After(formertime.Add(time.Second * 60)) {
				_, ok := s.nodes[n.Name]
				if ok {
					_, ok = nodeInfoCopy[devhandsk]
					if ok && nodeInfoCopy[devhandsk] != nil {
						s.rmNodeDevice(n.Name, nodeInfoCopy[devhandsk])
						klog.Infof("node %v device %s:%v leave, %v remaining devices:%v", val.Name, devhandsk, nodeInfoCopy[devhandsk], err, s.nodes[val.Name].Devices)

						tmppat := make(map[string]string)
						tmppat[devhandsk] = "Deleted_" + time.Now().Format("2006.01.02 15:04:05")
						n, err := util.GetNode(n.Name)
						if err != nil {
							klog.Errorln("get node failed", err.Error())
							continue
						}
						util.PatchNodeAnnotations(n, tmppat)
						continue
					}
				}
			}
			continue
		} else if strings.Contains(handshake, "Deleted") {
			continue
		} else {
			tmppat := make(map[string]string)
			tmppat[devhandsk] = "Requesting_" + time.Now().Format("2006.01.02 15:04:05")
			n, err := util.GetNode(n.Name)
			if err != nil {
				klog.Errorln("get node failed", err.Error())
				continue
			}
			util.PatchNodeAnnotations(n, tmppat)
		}
		nodeInfo := &util.NodeInfo{}
		nodeInfo.ID = n.Name
		nodeInfo.Devices = make([]util.DeviceInfo, 0)
		for index, deviceinfo := range nodedevices {
			found := false
			_, ok := s.nodes[n.Name]
			if ok {
				for i1, val1 := range s.nodes[n.Name].Devices {
					if strings.Compare(val1.ID, deviceinfo.Id) == 0 {
						found = true
						s.nodes[n.Name].Devices[i1].Devmem = deviceinfo.Devmem
						s.nodes[n.Name].Devices[i1].Devcore = deviceinfo.Devcore
						break
					}
				}
			}
			if !found {
				nodeInfo.Devices = append(nodeInfo.Devices, util.DeviceInfo{
					ID:      deviceinfo.Id,
					Index:   uint(index),
					Count:   deviceinfo.Count,
					Devmem:  deviceinfo.Devmem,
					Devcore: deviceinfo.Devcore,
					Type:    deviceinfo.Type,
					Numa:    deviceinfo.Numa,
					Health:  deviceinfo.Health,
				})
			}
		}
		s.addNode(n.Name, nodeInfo)
		nodeInfoCopy[devhandsk] = nodeInfo
		if s.nodes[n.Name] != nil && nodeInfo != nil && len(nodeInfo.Devices) > 0 {
			klog.Infof("node %v device %s come node info=%v total=%v", n.Name, devhandsk, nodeInfoCopy[devhandsk], s.nodes[n.Name].Devices)
		}
	}
}
*/
