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
	"fmt"
	"slices"
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device/awsneuron"
	"github.com/Project-HAMi/HAMi/pkg/device/kunlun"

	"gopkg.in/yaml.v2"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/enflame"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/iluvatar"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func loadTestConfig() string {
	return `
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
kunlun:
  resourceCountName: "kunlunxin.com/xpu"
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
}

func Test_LoadConfig(t *testing.T) {
	var configData Config
	err := yaml.Unmarshal([]byte(loadTestConfig()), &configData)
	assert.NilError(t, err)

	dataDrivenTests := []struct {
		name           string
		expectedConfig any
		actualConfig   any
	}{
		{"NVIDIA Config", createNvidiaConfig(), configData.NvidiaConfig},
		{"Cambricon Config", createCambriconConfig(), configData.CambriconConfig},
		{"Hygon Config", createHygonConfig(), configData.HygonConfig},
		{"Iluvatar Config", createIluvatarConfig(), configData.IluvatarConfig},
		{"Mthreads Config", createMthreadsConfig(), configData.MthreadsConfig},
		{"Metax Config", createMetaxConfig(), configData.MetaxConfig},
		{"Kunlun Config", createKunlunConfig(), configData.KunlunConfig},
	}

	for _, test := range dataDrivenTests {
		t.Run(test.name, func(t *testing.T) {
			assert.DeepEqual(t, test.expectedConfig, test.actualConfig)
		})
	}

	expectedVNPUs := createVNPUConfigs()
	assert.DeepEqual(t, configData.VNPUs, expectedVNPUs)
}

func createNvidiaConfig() nvidia.NvidiaConfig {
	return nvidia.NvidiaConfig{
		ResourceCountName:            "nvidia.com/gpu",
		ResourceMemoryName:           "nvidia.com/gpumem",
		ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
		ResourceCoreName:             "nvidia.com/gpucores",
		ResourcePriority:             "nvidia.com/priority",
		OverwriteEnv:                 false,
		DefaultMemory:                0,
		DefaultCores:                 0,
		DefaultGPUNum:                1,
	}
}

func createCambriconConfig() cambricon.CambriconConfig {
	return cambricon.CambriconConfig{
		ResourceCountName:  "cambricon.com/vmlu",
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
	}
}

func createHygonConfig() hygon.HygonConfig {
	return hygon.HygonConfig{
		ResourceCountName:  "hygon.com/dcunum",
		ResourceMemoryName: "hygon.com/dcumem",
		ResourceCoreName:   "hygon.com/dcucores",
	}
}

func createIluvatarConfig() iluvatar.IluvatarConfig {
	return iluvatar.IluvatarConfig{
		ResourceCountName:  "iluvatar.ai/vgpu",
		ResourceMemoryName: "iluvatar.ai/vcuda-memory",
		ResourceCoreName:   "iluvatar.ai/vcuda-core",
	}
}

func createMthreadsConfig() mthreads.MthreadsConfig {
	return mthreads.MthreadsConfig{
		ResourceCountName:  "mthreads.com/vgpu",
		ResourceMemoryName: "mthreads.com/sgpu-memory",
		ResourceCoreName:   "mthreads.com/sgpu-core",
	}
}

func createMetaxConfig() metax.MetaxConfig {
	return metax.MetaxConfig{
		ResourceCountName: "metax-tech.com/gpu",
	}
}

func createKunlunConfig() kunlun.KunlunConfig {
	return kunlun.KunlunConfig{
		ResourceCountName: "kunlunxin.com/xpu",
	}
}

func createVNPUConfigs() []ascend.VNPUConfig {
	return []ascend.VNPUConfig{
		{
			ChipName:           "910B",
			CommonWord:         "Ascend910A",
			ResourceName:       "huawei.com/Ascend910A",
			ResourceMemoryName: "huawei.com/Ascend910A-memory",
			MemoryAllocatable:  32768,
			MemoryCapacity:     32768,
			AICore:             30,
			Templates: []ascend.Template{
				{Name: "vir02", Memory: 2184, AICore: 2},
				{Name: "vir04", Memory: 4369, AICore: 4},
				{Name: "vir08", Memory: 8738, AICore: 8},
				{Name: "vir16", Memory: 17476, AICore: 16},
			},
		},
		{
			ChipName:           "910B2",
			CommonWord:         "Ascend910B2",
			ResourceName:       "huawei.com/Ascend910B2",
			ResourceMemoryName: "huawei.com/Ascend910B2-memory",
			MemoryAllocatable:  65536,
			MemoryCapacity:     65536,
			AICore:             24,
			AICPU:              6,
			Templates: []ascend.Template{
				{Name: "vir03_1c_8g", Memory: 8192, AICore: 3, AICPU: 1},
				{Name: "vir06_1c_16g", Memory: 16384, AICore: 6, AICPU: 1},
				{Name: "vir12_3c_32g", Memory: 32768, AICore: 12, AICPU: 3},
			},
		},
		{
			ChipName:           "910B3",
			CommonWord:         "Ascend910B",
			ResourceName:       "huawei.com/Ascend910B",
			ResourceMemoryName: "huawei.com/Ascend910B-memory",
			MemoryAllocatable:  65536,
			MemoryCapacity:     65536,
			AICore:             20,
			AICPU:              7,
			Templates: []ascend.Template{
				{Name: "vir05_1c_16g", Memory: 16384, AICore: 5, AICPU: 1},
				{Name: "vir10_3c_32g", Memory: 32768, AICore: 10, AICPU: 3},
			},
		},
		{
			ChipName:           "910B4",
			CommonWord:         "Ascend910B4",
			ResourceName:       "huawei.com/Ascend910B4",
			ResourceMemoryName: "huawei.com/Ascend910B4-memory",
			MemoryAllocatable:  32768,
			MemoryCapacity:     32768,
			AICore:             20,
			AICPU:              7,
			Templates: []ascend.Template{
				{Name: "vir05_1c_8g", Memory: 8192, AICore: 5, AICPU: 1},
				{Name: "vir10_3c_16g", Memory: 16384, AICore: 10, AICPU: 3},
			},
		},
		{
			ChipName:           "310P3",
			CommonWord:         "Ascend310P",
			ResourceName:       "huawei.com/Ascend310P",
			ResourceMemoryName: "huawei.com/Ascend310P-memory",
			MemoryAllocatable:  21527,
			MemoryCapacity:     24576,
			AICore:             8,
			AICPU:              7,
			Templates: []ascend.Template{
				{Name: "vir01", Memory: 3072, AICore: 1, AICPU: 1},
				{Name: "vir02", Memory: 6144, AICore: 2, AICPU: 2},
				{Name: "vir04", Memory: 12288, AICore: 4, AICPU: 4},
			},
		},
	}
}

func setupTest(t *testing.T) (map[string]string, map[string]Devices) {
	t.Helper()

	configMapData := loadTestConfig()
	var configData Config
	err := yaml.Unmarshal([]byte(configMapData), &configData)
	assert.NilError(t, err)

	err = InitDevicesWithConfig(&configData)
	assert.NilError(t, err)

	// Expected devices map
	expectedDevices := map[string]string{
		nvidia.NvidiaGPUDevice:       nvidia.NvidiaGPUCommonWord,
		cambricon.CambriconMLUDevice: cambricon.CambriconMLUCommonWord,
		hygon.HygonDCUDevice:         hygon.HygonDCUCommonWord,
		iluvatar.IluvatarGPUDevice:   iluvatar.IluvatarGPUCommonWord,
		mthreads.MthreadsGPUDevice:   mthreads.MthreadsGPUCommonWord,
		metax.MetaxGPUDevice:         metax.MetaxGPUCommonWord,
		metax.MetaxSGPUDevice:        metax.MetaxSGPUCommonWord,
		enflame.EnflameGPUDevice:     enflame.EnflameGPUCommonWord,
		kunlun.KunlunGPUDevice:       kunlun.KunlunGPUCommonWord,
		awsneuron.AWSNeuronDevice:    awsneuron.AWSNeuronCommonWord,
	}

	return expectedDevices, devicesMap
}

func containsString(slice []string, str string) bool {
	return slices.Contains(slice, str)
}

// Test_InitDevicesWithConfig_Success tests the initialization of devices with the provided config.
func Test_InitDevicesWithConfig_Success(t *testing.T) {
	expectedDevices, devicesMap := setupTest(t)

	assert.Assert(t, len(devicesMap) > 0, "Expected devicesMap to be populated")
	assert.Equal(t, len(DevicesToHandle), len(expectedDevices), "Expected DevicesToHandle to contain all devices")

	for deviceType, commonWord := range expectedDevices {
		assert.Assert(t, devicesMap[deviceType] != nil, fmt.Sprintf("Expected %s device to be initialized", deviceType))
		assert.Assert(t, containsString(DevicesToHandle, commonWord), fmt.Sprintf("Expected common word %s to be in DevicesToHandle", commonWord))
	}
}

// Test_InitDevicesWithConfig_InvalidConfig tests the behavior of InitDevicesWithConfig with invalid configurations.
func Test_InitDevicesWithConfig_InvalidConfig(t *testing.T) {
	// Provide an intentionally constructed invalid configuration
	configData := Config{}

	err := InitDevicesWithConfig(&configData)
	assert.ErrorContains(t, err, "all configurations are empty", "Expected initialization to fail with 'NvidiaConfig is empty' error")

}

func Test_GetDevices(t *testing.T) {
	expectedDevices, _ := setupTest(t)

	devices := GetDevices()

	assert.Assert(t, len(devices) > 0, "Expected devicesMap to be populated")
	assert.Equal(t, len(devices), len(expectedDevices), "Expected devicesMap to contain all initialized devices")

	for deviceType := range expectedDevices {
		if devices[deviceType] == nil {
			t.Errorf("Expected %s device to be initialized", deviceType)
		}
	}
}

func Test_InitDefaultDevices(t *testing.T) {
	InitDefaultDevices()
	assert.Assert(t, len(devicesMap) > 0, "Expected devicesMap to be populated")
	assert.Assert(t, len(DevicesToHandle) > 0, "Expected DevicesToHandle to be populated")
}

func Test_PodAllocationTrySuccess(t *testing.T) {
	// Initialize fake clientset and pre-load test data
	client.KubeClient = fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testpod",
			Namespace:   "default",
			Annotations: map[string]string{"test-annotation-key": "test-annotation-value", util.InRequestDevices["NVIDIA"]: "some-value"},
		},
	}

	// Add the pod to the fake clientset
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	nodeName := "test-node"
	devName := "NVIDIA"
	lockName := "test-lock"

	// Call the function under test
	PodAllocationTrySuccess(nodeName, devName, lockName, pod)

	// Refresh the pod state from the fake clientset and check the annotations
	refreshedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed pod: %v", err)
	}

	annos, ok := refreshedPod.Annotations[util.InRequestDevices[devName]]
	if !ok || annos == "" {
		t.Error("Expected annotations to be updated")
	}
}

func Test_PodAllocationSuccess(t *testing.T) {
	// Initialize fake clientset and pre-load test data
	client.KubeClient = fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testpod",
			Namespace: "default",
			Annotations: map[string]string{
				"test-annotation-key": "test-annotation-value",
			},
		},
	}

	// Add the pod to the fake clientset
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	nodeName := "test-node"
	lockName := "test-lock"

	// Update pod annotations and release the lock as part of the setup for the test
	updatePodAnnotationsAndReleaseLock(nodeName, pod, lockName, util.DeviceBindSuccess)

	// Call the function under test
	PodAllocationSuccess(nodeName, pod, lockName)

	// Refresh the pod state from the fake clientset and check the DeviceBindPhase annotation
	refreshedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed pod: %v", err)
	}

	annos, ok := refreshedPod.Annotations[util.DeviceBindPhase]
	if !ok || annos != util.DeviceBindSuccess {
		t.Errorf("Expected DeviceBindPhase annotation to be '%s', got '%s'", util.DeviceBindSuccess, annos)
	}
}
func Test_PodAllocationFailed(t *testing.T) {

	client.KubeClient = fake.NewSimpleClientset()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "testpod",
			Namespace:   "default",
			Annotations: map[string]string{"test-annotation-key": "test-annotation-value"},
		},
	}

	// add pod to the fake client
	_, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	nodeName := "test-node"
	lockName := "test-lock"

	// simulate a failed pod allocation
	PodAllocationFailed(nodeName, pod, lockName)

	// retrieve the pod from the fake client
	refreshedPod, err := client.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get refreshed pod: %v", err)
	}

	annos, ok := refreshedPod.Annotations[util.DeviceBindPhase]
	if !ok {
		t.Error("Expected DeviceBindPhase annotation to be present")
	} else if annos != util.DeviceBindFailed {
		t.Errorf("Expected DeviceBindPhase annotation to be '%s', got '%s'", util.DeviceBindFailed, annos)
	}
}

func Test_GlobalFlagSet(t *testing.T) {
	fs := GlobalFlagSet()
	fs.Parse([]string{"-debug=true", "-device-config-file=test-config-file.yaml"})
	assert.Assert(t, DebugMode, "Expected DebugMode to be true")
	assert.Equal(t, configFile, "test-config-file.yaml", "Expected configFile to be test-config-file.yaml")
}

func Test_validateConfig(t *testing.T) {
	validConfig := &Config{
		NvidiaConfig: nvidia.NvidiaConfig{
			ResourceCountName:            "nvidia.com/gpu",
			ResourceMemoryName:           "nvidia.com/gpumem",
			ResourceMemoryPercentageName: "nvidia.com/gpumem-percentage",
			ResourceCoreName:             "nvidia.com/gpucores",
			ResourcePriority:             "nvidia.com/priority",
			OverwriteEnv:                 false,
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
		},
	}
	emptyConfig := &Config{}

	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{"Valid config", validConfig, false},
		{"Empty config", emptyConfig, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateConfig(test.config)
			if test.expectError {
				assert.ErrorContains(t, err, "all configurations are empty")
			} else {
				assert.NilError(t, err)
			}
		})
	}
}
