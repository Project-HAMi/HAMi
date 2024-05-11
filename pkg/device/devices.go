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

package device

import (
	"context"
	"flag"
	"os"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/api"
	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/iluvatar"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type Devices interface {
	MutateAdmission(ctr *corev1.Container) (bool, error)
	CheckHealth(devType string, n *corev1.Node) (bool, bool)
	NodeCleanUp(nn string) error
	GetNodeDevices(n corev1.Node) ([]*api.DeviceInfo, error)
	CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool)
	// CheckUUID is check current device id whether in GPUUseUUID or GPUNoUseUUID set, return true is check success.
	CheckUUID(annos map[string]string, d util.DeviceUsage) bool
	LockNode(n *corev1.Node, p *corev1.Pod) error
	ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error
	GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest
	PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string
	ParseConfig(fs *flag.FlagSet)
}

var (
	HandshakeAnnos  = map[string]string{}
	RegisterAnnos   = map[string]string{}
	DevicesToHandle []string
)

var devices map[string]Devices
var DebugMode bool

func GetDevices() map[string]Devices {
	return devices
}

func init() {
	devices = make(map[string]Devices)
	devices[cambricon.CambriconMLUDevice] = cambricon.InitMLUDevice()
	devices[nvidia.NvidiaGPUDevice] = nvidia.InitNvidiaDevice()
	devices[hygon.HygonDCUDevice] = hygon.InitDCUDevice()
	devices[iluvatar.IluvatarGPUDevice] = iluvatar.InitIluvatarDevice()
	devices[ascend.AscendDevice] = ascend.InitDevice()
	DevicesToHandle = []string{}
	DevicesToHandle = append(DevicesToHandle, nvidia.NvidiaGPUCommonWord)
	DevicesToHandle = append(DevicesToHandle, cambricon.CambriconMLUCommonWord)
	DevicesToHandle = append(DevicesToHandle, hygon.HygonDCUCommonWord)
	DevicesToHandle = append(DevicesToHandle, iluvatar.IluvatarGPUCommonWord)
	DevicesToHandle = append(DevicesToHandle, ascend.AscendDevice)
}

func PodAllocationTrySuccess(nodeName string, devName string, lockName string, pod *corev1.Pod) {
	refreshed, err := client.GetClient().CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("get pods %s/%s error: %+v", pod.Namespace, pod.Name, err)
		return
	}
	annos := refreshed.Annotations[util.InRequestDevices[devName]]
	klog.Infoln("TrySuccess:", annos)
	for _, val := range DevicesToHandle {
		if strings.Contains(annos, val) {
			return
		}
	}
	klog.Infoln("AllDevicesAllocateSuccess releasing lock")
	PodAllocationSuccess(nodeName, pod, lockName)
}

func PodAllocationSuccess(nodeName string, pod *corev1.Pod, lockname string) {
	newannos := make(map[string]string)
	newannos[util.DeviceBindPhase] = util.DeviceBindSuccess
	err := util.PatchPodAnnotations(pod, newannos)
	if err != nil {
		klog.Errorf("patchPodAnnotations failed:%v", err.Error())
	}
	err = nodelock.ReleaseNodeLock(nodeName, lockname)
	if err != nil {
		klog.Errorf("release lock failed:%v", err.Error())
	}
}

func PodAllocationFailed(nodeName string, pod *corev1.Pod, lockname string) {
	newannos := make(map[string]string)
	newannos[util.DeviceBindPhase] = util.DeviceBindFailed
	err := util.PatchPodAnnotations(pod, newannos)
	if err != nil {
		klog.Errorf("patchPodAnnotations failed:%v", err.Error())
	}
	err = nodelock.ReleaseNodeLock(nodeName, lockname)
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
