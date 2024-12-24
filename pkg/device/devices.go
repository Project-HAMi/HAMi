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
	devicesMap      map[string]Devices
	DevicesToHandle []string
	configFile      string
	DebugMode       bool
)

func GetDevices() map[string]Devices {
	return devicesMap
}

func InitDevicesWithConfig(config *Config) {
	klog.Info("Initializing devices with configuration")

	devicesMap = make(map[string]Devices)
	DevicesToHandle = []string{}

	initializeDevice := func(deviceType string, commonWord string, initFunc func(interface{}) Devices, config interface{}) {
		klog.Infof("Initializing %s device", commonWord)
		devicesMap[deviceType] = initFunc(config)
		DevicesToHandle = append(DevicesToHandle, commonWord)
		klog.Infof("%s device initialized", commonWord)
	}

	initializeDevice(nvidia.NvidiaGPUDevice, nvidia.NvidiaGPUCommonWord, func(cfg interface{}) Devices {
		return nvidia.InitNvidiaDevice(cfg.(nvidia.NvidiaConfig))
	}, config.NvidiaConfig)

	initializeDevice(cambricon.CambriconMLUDevice, cambricon.CambriconMLUCommonWord, func(cfg interface{}) Devices {
		return cambricon.InitMLUDevice(cfg.(cambricon.CambriconConfig))
	}, config.CambriconConfig)

	initializeDevice(hygon.HygonDCUDevice, hygon.HygonDCUCommonWord, func(cfg interface{}) Devices {
		return hygon.InitDCUDevice(cfg.(hygon.HygonConfig))
	}, config.HygonConfig)

	initializeDevice(iluvatar.IluvatarGPUDevice, iluvatar.IluvatarGPUCommonWord, func(cfg interface{}) Devices {
		return iluvatar.InitIluvatarDevice(cfg.(iluvatar.IluvatarConfig))
	}, config.IluvatarConfig)

	initializeDevice(mthreads.MthreadsGPUDevice, mthreads.MthreadsGPUCommonWord, func(cfg interface{}) Devices {
		return mthreads.InitMthreadsDevice(cfg.(mthreads.MthreadsConfig))
	}, config.MthreadsConfig)

	initializeDevice(metax.MetaxGPUDevice, metax.MetaxGPUCommonWord, func(cfg interface{}) Devices {
		return metax.InitMetaxDevice(cfg.(metax.MetaxConfig))
	}, config.MetaxConfig)

	for _, dev := range ascend.InitDevices(config.VNPUs) {
		commonWord := dev.CommonWord()
		devicesMap[commonWord] = dev
		DevicesToHandle = append(DevicesToHandle, commonWord)
		klog.Infof("Ascend device %s initialized", commonWord)
	}

	klog.Info("All devices initialized successfully")
}

func InitDevices() {
	if len(devicesMap) > 0 {
		klog.Info("Devices are already initialized, skipping initialization")
		return
	}
	klog.Infof("Loading device configuration from file: %s", configFile)
	config, err := LoadConfig(configFile)
	if err != nil {
		klog.Fatalf("Failed to load device config file %s: %v", configFile, err)
	}
	klog.Infof("Loaded config: %v", config)
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
		klog.Errorf("Error getting pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return
	}
	annos := refreshed.Annotations[util.InRequestDevices[devName]]
	klog.Infof("Trying allocation success: %s", annos)
	for _, val := range DevicesToHandle {
		if strings.Contains(annos, val) {
			return
		}
	}
	klog.Infof("All devices allocate success, releasing lock")
	PodAllocationSuccess(nodeName, pod, lockName)
}

func PodAllocationSuccess(nodeName string, pod *corev1.Pod, lockName string) {
	newAnnos := map[string]string{util.DeviceBindPhase: util.DeviceBindSuccess}
	if err := util.PatchPodAnnotations(pod, newAnnos); err != nil {
		klog.Errorf("Failed to patch pod annotations: %v", err)
	}
	err = nodelock.ReleaseNodeLock(nodeName, lockname, pod, false)
	if err != nil {
		klog.Errorf("release lock failed:%v", err.Error())
	}
}

func PodAllocationFailed(nodeName string, pod *corev1.Pod, lockName string) {
	newAnnos := map[string]string{util.DeviceBindPhase: util.DeviceBindFailed}
	if err := util.PatchPodAnnotations(pod, newAnnos); err != nil {
		klog.Errorf("Failed to patch pod annotations: %v", err)
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
	fs.BoolVar(&DebugMode, "debug", false, "Enable debug mode")
	fs.StringVar(&configFile, "device-config-file", "", "Path to the device config file")
	klog.InitFlags(fs)
	return fs
}

func LoadConfig(path string) (*Config, error) {
	klog.Infof("Reading config file from path: %s", path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var yamlData Config
	if err := yaml.Unmarshal(data, &yamlData); err != nil {
		return nil, err
	}
	klog.Info("Successfully read and parsed config file")
	return &yamlData, nil
}
