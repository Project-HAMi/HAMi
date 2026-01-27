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

package config

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"time"

	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/amd"
	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/awsneuron"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/enflame"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/iluvatar"
	"github.com/Project-HAMi/HAMi/pkg/device/kunlun"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/device/vastai"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

var (
	QPS                float32
	Burst              int
	Timeout            int
	HTTPBind           string
	SchedulerName      string
	MetricsBindAddress string

	DefaultMem         int32
	DefaultCores       int32
	DefaultResourceNum int32

	// NodeSchedulerPolicy is config this scheduler node to use `binpack` or `spread`. default value is binpack.
	NodeSchedulerPolicy = util.NodeSchedulerPolicyBinpack.String()

	// NodeLabelSelector is scheduler filter node by node label.
	NodeLabelSelector map[string]string

	// NodeLockTimeout is the timeout for node locks.
	NodeLockTimeout time.Duration

	// If set to false, When Pod.Spec.SchedulerName equals to the const DefaultSchedulerName in k8s.io/api/core/v1 package, webhook will not overwrite it, default value is true.
	ForceOverwriteDefaultScheduler bool

	HostName                     string
	LeaderElect                  bool
	LeaderElectResourceName      string
	LeaderElectResourceNamespace string
)

type Config struct {
	NvidiaConfig    nvidia.NvidiaConfig       `yaml:"nvidia"`
	MetaxConfig     metax.MetaxConfig         `yaml:"metax"`
	HygonConfig     hygon.HygonConfig         `yaml:"hygon"`
	CambriconConfig cambricon.CambriconConfig `yaml:"cambricon"`
	MthreadsConfig  mthreads.MthreadsConfig   `yaml:"mthreads"`
	IluvatarConfig  []iluvatar.IluvatarConfig `yaml:"iluvatars"`
	EnflameConfig   enflame.EnflameConfig     `yaml:"enflame"`
	KunlunConfig    kunlun.KunlunConfig       `yaml:"kunlun"`
	AWSNeuronConfig awsneuron.AWSNeuronConfig `yaml:"awsneuron"`
	AMDGPUConfig    amd.AMDConfig             `yaml:"amd"`
	VastaiConfig    vastai.VastaiConfig       `yaml:"vastai"`
	VNPUs           []ascend.VNPUConfig       `yaml:"vnpus"`
}

var (
	HandshakeAnnos = map[string]string{}
	RegisterAnnos  = map[string]string{}
	configFile     string
	DebugMode      bool
)

func InitDevicesWithConfig(config *Config) error {
	if err := validateConfig(config); err != nil {
		klog.Errorf("Invalid configuration: %v", err)
		return err
	}

	klog.Info("Initializing devices with configuration")

	device.DevicesMap = make(map[string]device.Devices)
	device.DevicesToHandle = []string{}
	var initErrors []error

	// Helper function to initialize devices and handle errors
	initializeDevice := func(deviceType string, commonWord string, initFunc func(any) (device.Devices, error), config any) {
		klog.Infof("Initializing %s device", commonWord)
		dev, err := initFunc(config)
		if err != nil {
			klog.Errorf("Failed to initialize %s device: %v", commonWord, err)
			initErrors = append(initErrors, fmt.Errorf("%s: %v", commonWord, err))
			return
		}
		device.DevicesMap[dev.CommonWord()] = dev
		device.DevicesToHandle = append(device.DevicesToHandle, commonWord)
		klog.Infof("%s device initialized successfully", commonWord)
	}

	// Wrapper for each device's initialization function to include type assertion check
	deviceInitializers := []struct {
		deviceType string
		commonWord string
		initFunc   func(any) (device.Devices, error)
		config     any
	}{
		{nvidia.NvidiaGPUDevice, nvidia.NvidiaGPUDevice, func(cfg any) (device.Devices, error) {
			nvidiaConfig, ok := cfg.(nvidia.NvidiaConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", nvidia.NvidiaGPUDevice)
			}
			return nvidia.InitNvidiaDevice(nvidiaConfig), nil
		}, config.NvidiaConfig},
		{cambricon.CambriconMLUDevice, cambricon.CambriconMLUCommonWord, func(cfg any) (device.Devices, error) {
			cambriconConfig, ok := cfg.(cambricon.CambriconConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", cambricon.CambriconMLUCommonWord)
			}
			return cambricon.InitMLUDevice(cambriconConfig), nil
		}, config.CambriconConfig},
		{hygon.HygonDCUDevice, hygon.HygonDCUCommonWord, func(cfg any) (device.Devices, error) {
			hygonConfig, ok := cfg.(hygon.HygonConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", hygon.HygonDCUCommonWord)
			}
			return hygon.InitDCUDevice(hygonConfig), nil
		}, config.HygonConfig},
		{enflame.EnflameGCUDevice, enflame.EnflameGCUCommonWord, func(cfg any) (device.Devices, error) {
			enflameConfig, ok := cfg.(enflame.EnflameConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", enflame.EnflameGCUCommonWord)
			}
			return enflame.InitGCUDevice(enflameConfig), nil
		}, config.EnflameConfig},
		{enflame.EnflameVGCUDevice, enflame.EnflameVGCUCommonWord, func(cfg any) (device.Devices, error) {
			enflameConfig, ok := cfg.(enflame.EnflameConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", enflame.EnflameVGCUCommonWord)
			}
			return enflame.InitEnflameDevice(enflameConfig), nil
		}, config.EnflameConfig},
		{mthreads.MthreadsGPUDevice, mthreads.MthreadsGPUCommonWord, func(cfg any) (device.Devices, error) {
			mthreadsConfig, ok := cfg.(mthreads.MthreadsConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", mthreads.MthreadsGPUCommonWord)
			}
			return mthreads.InitMthreadsDevice(mthreadsConfig), nil
		}, config.MthreadsConfig},
		{metax.MetaxGPUDevice, metax.MetaxGPUCommonWord, func(cfg any) (device.Devices, error) {
			metaxConfig, ok := cfg.(metax.MetaxConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", metax.MetaxGPUCommonWord)
			}
			return metax.InitMetaxDevice(metaxConfig), nil
		}, config.MetaxConfig},
		{metax.MetaxSGPUDevice, metax.MetaxSGPUCommonWord, func(cfg any) (device.Devices, error) {
			metaxConfig, ok := cfg.(metax.MetaxConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", metax.MetaxGPUCommonWord)
			}
			return metax.InitMetaxSDevice(metaxConfig), nil
		}, config.MetaxConfig},
		{kunlun.KunlunGPUDevice, kunlun.KunlunGPUCommonWord, func(cfg any) (device.Devices, error) {
			kunlunConfig, ok := cfg.(kunlun.KunlunConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", kunlun.KunlunGPUCommonWord)
			}
			return kunlun.InitKunlunDevice(kunlunConfig), nil
		}, config.KunlunConfig},
		{kunlun.XPUDevice, kunlun.XPUCommonWord, func(cfg any) (device.Devices, error) {
			kunlunConfig, ok := cfg.(kunlun.KunlunConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", kunlun.XPUDevice)
			}
			return kunlun.InitKunlunVDevice(kunlunConfig), nil
		}, config.KunlunConfig},
		{awsneuron.AWSNeuronDevice, awsneuron.AWSNeuronCommonWord, func(cfg any) (device.Devices, error) {
			awsneuronConfig, ok := cfg.(awsneuron.AWSNeuronConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", awsneuron.AWSNeuronCommonWord)
			}
			return awsneuron.InitAWSNeuronDevice(awsneuronConfig), nil
		}, config.AWSNeuronConfig},
		{amd.AMDDevice, amd.AMDCommonWord, func(cfg any) (device.Devices, error) {
			amdGPUConfig, ok := cfg.(amd.AMDConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", amd.AMDCommonWord)
			}
			return amd.InitAMDGPUDevice(amdGPUConfig), nil
		}, config.AMDGPUConfig},
		{vastai.VastaiDevice, vastai.VastaiCommonWord, func(cfg any) (device.Devices, error) {
			vastaiConfig, ok := cfg.(vastai.VastaiConfig)
			if !ok {
				return nil, fmt.Errorf("invalid configuration for %s", vastai.VastaiCommonWord)
			}
			return vastai.InitVastaiDevice(vastaiConfig), nil
		}, config.VastaiConfig},
	}

	// Initialize all devices using the wrapped functions
	for _, initializer := range deviceInitializers {
		initializeDevice(initializer.deviceType, initializer.commonWord, initializer.initFunc, initializer.config)
	}

	// Initialize Ascend devices
	for _, dev := range ascend.InitDevices(config.VNPUs) {
		commonWord := dev.CommonWord()
		device.DevicesMap[commonWord] = dev
		device.DevicesToHandle = append(device.DevicesToHandle, commonWord)
		klog.Infof("Ascend device %s initialized", commonWord)
	}

	// Initialize Iluvatar devices
	for _, dev := range iluvatar.InitIluvatarDevice(config.IluvatarConfig) {
		commonWord := dev.CommonWord()
		device.DevicesMap[commonWord] = dev
		device.DevicesToHandle = append(device.DevicesToHandle, commonWord)
		klog.Infof("Iluvatar device %s initialized", commonWord)
	}

	if len(initErrors) > 0 {
		return fmt.Errorf("errors occurred during initialization: %v", initErrors)
	}

	klog.Info("All devices initialized successfully")
	return nil
}

// validateConfig validates the configuration object to ensure it is complete.
func validateConfig(config *Config) error {
	if !reflect.DeepEqual(config.NvidiaConfig, nvidia.NvidiaConfig{}) ||
		!reflect.DeepEqual(config.CambriconConfig, cambricon.CambriconConfig{}) ||
		!reflect.DeepEqual(config.HygonConfig, hygon.HygonConfig{}) ||
		len(config.IluvatarConfig) > 0 ||
		!reflect.DeepEqual(config.MthreadsConfig, mthreads.MthreadsConfig{}) ||
		!reflect.DeepEqual(config.MetaxConfig, metax.MetaxConfig{}) ||
		!reflect.DeepEqual(config.KunlunConfig, kunlun.KunlunConfig{}) ||
		!reflect.DeepEqual(config.AWSNeuronConfig, awsneuron.AWSNeuronConfig{}) ||
		!reflect.DeepEqual(config.EnflameConfig, enflame.EnflameConfig{}) ||
		!reflect.DeepEqual(config.AMDGPUConfig, amd.AMDConfig{}) ||
		len(config.VNPUs) > 0 {
		return nil
	}
	return fmt.Errorf("all configurations are empty")
}

func InitDevices() {
	if len(device.DevicesMap) > 0 {
		klog.Info("Devices are already initialized, skipping initialization")
		return
	}
	klog.Infof("Loading device configuration from file: %s", configFile)
	config, err := LoadConfig(configFile)
	if err != nil {
		klog.Fatalf("Failed to load device config file %s: %v", configFile, err)
	}
	klog.Infof("Loaded config: %v", config)
	err = InitDevicesWithConfig(config)
	if err != nil {
		klog.Fatalf("Failed to initialize devices: %v", err)
	}
}

func InitDefaultDevices() {
	configMapdata := `
nvidia:
  resourceCountName: "nvidia.com/gpu"
  resourceMemoryName: "nvidia.com/gpumem"
  resourceMemoryPercentageName: "nvidia.com/gpumem-percentage"
  resourceCoreName: "nvidia.com/gpucores"
  resourcePriorityName: "nvidia.com/priority"
  overwriteEnv: false
  defaultMemory: 0
  defaultCores: 0
  defaultGPUNum: 1
cambricon:
  resourceCountName: "cambricon.com/vmlu"
  resourceMemoryName: "cambricon.com/mlu.smlu.vmemory"
  resourceCoreName: "cambricon.com/mlu.smlu.vcore"
hygon:
  resourceCountName: "hygon.com/dcunum"
  resourceMemoryName: "hygon.com/dcumem"
  resourceCoreName: "hygon.com/dcucores"
metax:
  resourceCountName: "metax-tech.com/gpu"
mthreads:
  resourceCountName: "mthreads.com/vgpu"
  resourceMemoryName: "mthreads.com/sgpu-memory"
  resourceCoreName: "mthreads.com/sgpu-core"
iluvatars:
  - chipName: MR-V100
    commonWord: MR-V100
    resourceCountName: iluvatar.ai/MR-V100-vgpu
    resourceMemoryName: iluvatar.ai/MR-V100.vMem
    resourceCoreName: iluvatar.ai/MR-V100.vCore
  - chipName: MR-V50
    commonWord: MR-V50
    resourceCountName: iluvatar.ai/MR-V50-vgpu
    resourceMemoryName: iluvatar.ai/MR-V50.vMem
    resourceCoreName: iluvatar.ai/MR-V50.vCore
  - chipName: BI-V150
    commonWord: BI-V150
    resourceCountName: iluvatar.ai/BI-V150-vgpu
    resourceMemoryName: iluvatar.ai/BI-V150.vMem
    resourceCoreName: iluvatar.ai/BI-V150.vCore
  - chipName: BI-V100
    commonWord: BI-V100
    resourceCountName: iluvatar.ai/BI-V100-vgpu
    resourceMemoryName: iluvatar.ai/BI-V100.vMem
    resourceCoreName: iluvatar.ai/BI-V100.vCore
kunlun:
  resourceCountName: "kunlunxin.com/xpu"
  resourceVCountName: "kunlunxin.com/vxpu"
  resourceVMemoryName: "kunlunxin.com/vxpu-memory"
awsneuron:
  resourceCountName: "aws.amazon.com/neuron"
  resourceCoreName: "aws.amazon.com/neuroncore"
amd:
  resourceCountName: "amd.com/gpu"
vnpus:
  - chipName: "910A"
    commonWord: "Ascend910A"
    resourceName: "huawei.com/Ascend910A"
    resourceMemoryName: "huawei.com/Ascend910A-memory"
    memoryAllocatable: 32768
    memoryCapacity: 32768
    aiCore: 30
    templates:
      - name: "vir02"
        memory: 2184
        aiCore: 2
      - name: "vir04"
        memory: 4369
        aiCore: 4
      - name: "vir08"
        memory: 8738
        aiCore: 8
      - name: "vir16"
        memory: 17476
        aiCore: 16
  - chipName: 910B2
    commonWord: Ascend910B2
    resourceName: huawei.com/Ascend910B2
    resourceMemoryName: huawei.com/Ascend910B2-memory
    memoryAllocatable: 65536
    memoryCapacity: 65536
    aiCore: 24
    aiCPU: 6
    templates:
      - name: vir03_1c_8g
        memory: 8192
        aiCore: 3
        aiCPU: 1
      - name: vir06_1c_16g
        memory: 16384
        aiCore: 6
        aiCPU: 1
      - name: vir12_3c_32g
        memory: 32768
        aiCore: 12
        aiCPU: 3
  - chipName: "910B3"
    commonWord: "Ascend910B3"
    resourceName: "huawei.com/Ascend910B3"
    resourceMemoryName: "huawei.com/Ascend910B3-memory"
    memoryAllocatable: 65536
    memoryCapacity: 65536
    aiCore: 20
    aiCPU: 7
    templates:
      - name: "vir05_1c_16g"
        memory: 16384
        aiCore: 5
        aiCPU: 1
      - name: "vir10_3c_32g"
        memory: 32768
        aiCore: 10
        aiCPU: 3
  - chipName: 910B4
    commonWord: Ascend910B4
    resourceName: huawei.com/Ascend910B4
    resourceMemoryName: huawei.com/Ascend910B4-memory
    memoryAllocatable: 32768
    memoryCapacity: 32768
    aiCore: 20
    aiCPU: 7
    templates:
      - name: vir05_1c_8g
        memory: 8192
        aiCore: 5
        aiCPU: 1
      - name: vir10_3c_16g
        memory: 16384
        aiCore: 10
        aiCPU: 3
  - chipName: "310P3"
    commonWord: "Ascend310P"
    resourceName: "huawei.com/Ascend310P"
    resourceMemoryName: "huawei.com/Ascend310P-memory"
    memoryAllocatable: 21527
    memoryCapacity: 24576
    aiCore: 8
    aiCPU: 7
    templates:
      - name: "vir01"
        memory: 3072
        aiCore: 1
        aiCPU: 1
      - name: "vir02"
        memory: 6144
        aiCore: 2
        aiCPU: 2
      - name: "vir04"
        memory: 12288
        aiCore: 4
        aiCPU: 4`

	var yamlData Config
	err := yaml.Unmarshal([]byte(configMapdata), &yamlData)
	if err != nil {
		klog.Fatalf("Failed to unmarshal default config: %v", err)
		return
	}

	// Initialize devices with configuration
	if err := InitDevicesWithConfig(&yamlData); err != nil {
		klog.Fatalf("Failed to initialize devices with default config: %v", err)
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
	enflame.ParseConfig(fs)
	metax.ParseConfig(fs)
	kunlun.ParseConfig(fs)
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
