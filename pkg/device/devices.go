package device

import (
	"context"
	"flag"
	"os"
	"strings"

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
	CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool)
	GenerateResourceRequests(ctr *v1.Container) util.ContainerDeviceRequest
	ParseConfig(fs *flag.FlagSet)
}

var (
	KnownDevice = map[string]string{
		nvidia.HandshakeAnnos:    nvidia.RegisterAnnos,
		cambricon.HandshakeAnnos: cambricon.RegisterAnnos,
		hygon.HandshakeAnnos:     hygon.RegisterAnnos,
	}
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
