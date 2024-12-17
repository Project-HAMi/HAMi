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

	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/iluvatar"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type Devices interface {
	CommonWord() string
	MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error)
	CheckHealth(devType string, n *corev1.Node) (bool, bool)
	NodeCleanUp(nn string) error
	GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error)
	CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool)
	// CheckUUID is check current device id whether in GPUUseUUID or GPUNoUseUUID set, return true is check success.
	CheckUUID(annos map[string]string, d util.DeviceUsage) bool
	LockNode(n *corev1.Node, p *corev1.Pod) error
	ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error
	GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest
	PatchAnnotations(annoinput *map[string]string, pd util.PodDevices) map[string]string
	CustomFilterRule(allocated *util.PodDevices, request util.ContainerDeviceRequest, toAllicate util.ContainerDevices, device *util.DeviceUsage) bool
	ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, policy string) float32
	AddResourceUsage(n *util.DeviceUsage, ctr *util.ContainerDevice) error
	// This should not be associated with a specific device object
	//ParseConfig(fs *flag.FlagSet)
}

type Config struct {
	NvidiaConfig    nvidia.NvidiaConfig       `yaml:"nvidia"`
	MetaxConfig     metax.MetaxConfig         `yaml:"metax"`
	HygonConfig     hygon.HygonConfig         `yaml:"hygon"`
	CambriconConfig cambricon.CambriconConfig `yaml:"cambricon"`
	MthreadsConfig  mthreads.MthreadsConfig   `yaml:"mthreads"`
	IluvatarConfig  iluvatar.IluvatarConfig   `yaml:"iluvatar"`
	VNPUs           []ascend.VNPUConfig       `yaml:"vnpus"`
}

var (
	HandshakeAnnos  = map[string]string{}
	RegisterAnnos   = map[string]string{}
	devices         map[string]Devices
	DevicesToHandle []string
	configFile      string
	DebugMode       bool
)

func GetDevices() map[string]Devices {
	return devices
}

func InitDevicesWithConfig(config *Config) {
	devices = make(map[string]Devices)
	DevicesToHandle = []string{}
	devices[nvidia.NvidiaGPUDevice] = nvidia.InitNvidiaDevice(config.NvidiaConfig)
	devices[cambricon.CambriconMLUDevice] = cambricon.InitMLUDevice(config.CambriconConfig)
	devices[hygon.HygonDCUDevice] = hygon.InitDCUDevice(config.HygonConfig)
	devices[iluvatar.IluvatarGPUDevice] = iluvatar.InitIluvatarDevice(config.IluvatarConfig)
	devices[mthreads.MthreadsGPUDevice] = mthreads.InitMthreadsDevice(config.MthreadsConfig)
	devices[metax.MetaxGPUDevice] = metax.InitMetaxDevice(config.MetaxConfig)

	DevicesToHandle = append(DevicesToHandle, nvidia.NvidiaGPUCommonWord)
	DevicesToHandle = append(DevicesToHandle, cambricon.CambriconMLUCommonWord)
	DevicesToHandle = append(DevicesToHandle, hygon.HygonDCUCommonWord)
	DevicesToHandle = append(DevicesToHandle, iluvatar.IluvatarGPUCommonWord)
	DevicesToHandle = append(DevicesToHandle, mthreads.MthreadsGPUCommonWord)
	DevicesToHandle = append(DevicesToHandle, metax.MetaxGPUCommonWord)
	for _, dev := range ascend.InitDevices(config.VNPUs) {
		devices[dev.CommonWord()] = dev
		DevicesToHandle = append(DevicesToHandle, dev.CommonWord())
	}
}

func InitDevices() {
	if len(devices) > 0 {
		return
	}
	config, err := LoadConfig(configFile)
	klog.Infoln("reading config=", config, "configfile=", configFile)
	if err != nil {
		klog.Fatalf("failed to load device config file %s: %v", configFile, err)
	}
	InitDevicesWithConfig(config)
}

func InitDefaultDevices() {
	configMapdata := `
	nvidia:
	  resourceCountName: nvidia.com/gpu
	  resourceMemoryName: nvidia.com/gpumem
	  resourceMemoryPercentageName: nvidia.com/gpumem-percentage
	  resourceCoreName: nvidia.com/gpucores
	  resourcePriorityName: nvidia.com/priority
	  overwriteEnv: false
	  defaultMemory: 0
	  defaultCores: 0
	  defaultGPUNum: 1
	cambricon:
	  resourceCountName: cambricon.com/vmlu
	  resourceMemoryName: cambricon.com/mlu.smlu.vmemory
	  resourceCoreName: cambricon.com/mlu.smlu.vcore
	hygon:
	  resourceCountName: hygon.com/dcunum
	  resourceMemoryName: hygon.com/dcumem
	  resourceCoreName: hygon.com/dcucores
	metax:
	  resourceCountName: "metax-tech.com/gpu"
	mthreads:
	  resourceCountName: "mthreads.com/vgpu"
	  resourceMemoryName: "mthreads.com/sgpu-memory"
	  resourceCoreName: "mthreads.com/sgpu-core"
	iluvatar: 
	  resourceCountName: iluvatar.ai/vgpu
	  resourceMemoryName: iluvatar.ai/vcuda-memory
	  resourceCoreName: iluvatar.ai/vcuda-core
	vnpus:
	- chipName: 910B
	  commonWord: Ascend910A
	  resourceName: huawei.com/Ascend910A
	  resourceMemoryName: huawei.com/Ascend910A-memory
	  memoryAllocatable: 32768
	  memoryCapacity: 32768
	  aiCore: 30
	  templates:
	    - name: vir02
	      memory: 2184
	      aiCore: 2
	    - name: vir04
	      memory: 4369
	      aiCore: 4
	    - name: vir08
	      memory: 8738
	      aiCore: 8
	    - name: vir16
	      memory: 17476
	      aiCore: 16
	- chipName: 910B3
	  commonWord: Ascend910B
	  resourceName: huawei.com/Ascend910B
	  resourceMemoryName: huawei.com/Ascend910B-memory
	  memoryAllocatable: 65536
	  memoryCapacity: 65536
	  aiCore: 20
	  aiCPU: 7
	  templates:
	    - name: vir05_1c_16g
	      memory: 16384
	      aiCore: 5
	      aiCPU: 1
	    - name: vir10_3c_32g
	      memory: 32768
	      aiCore: 10
	      aiCPU: 3
	- chipName: 310P3
	  commonWord: Ascend310P
	  resourceName: huawei.com/Ascend310P
	  resourceMemoryName: huawei.com/Ascend310P-memory
	  memoryAllocatable: 21527
	  memoryCapacity: 24576
	  aiCore: 8
	  aiCPU: 7
	  templates:
	    - name: vir01
	      memory: 3072
	      aiCore: 1
	      aiCPU: 1
	    - name: vir02
	      memory: 6144
	      aiCore: 2
	      aiCPU: 2
	    - name: vir04
	      memory: 12288
	      aiCore: 4
	      aiCPU: 4`
	var yamlData Config
	err := yaml.Unmarshal([]byte(configMapdata), &yamlData)
	if err != nil {
		InitDevicesWithConfig(&yamlData)
	}
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
	err = nodelock.ReleaseNodeLock(nodeName, lockname, pod, false)
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
	err = nodelock.ReleaseNodeLock(nodeName, lockname, pod, false)
	if err != nil {
		klog.Errorf("release lock failed:%v", err.Error())
	}
}

func GlobalFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	ascend.ParseConfig(fs)
	cambricon.ParseConfig(fs)
	hygon.ParseConfig(fs)
	iluvatar.ParseConfig(fs)
	nvidia.ParseConfig(fs)
	mthreads.ParseConfig(fs)
	metax.ParseConfig(fs)
	fs.BoolVar(&DebugMode, "debug", false, "debug mode")
	fs.StringVar(&configFile, "device-config-file", "", "device config file")
	klog.InitFlags(fs)
	return fs
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var yamlData Config
	err = yaml.Unmarshal(data, &yamlData)
	if err != nil {
		return nil, err
	}
	return &yamlData, nil
}
