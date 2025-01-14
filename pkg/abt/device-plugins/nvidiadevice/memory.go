/*
Copyright 2025 BaiLian.

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

package nvidiadevice

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvml"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/abt/device-plugins/common"
	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/plugin"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

var _ plugin.Interface = (*NvidiaMemoryPlugin)(nil)

type NvidiaMemoryPlugin struct {
	common.BasePlugin

	nvmllib nvml.Interface
	config  nvidia.NvidiaConfig
	devices []*pluginapi.Device
}

func NewNvidiaMemoryPlugin(nvmllib nvml.Interface) (plugin.Interface, error) {
	config, err := device.LoadConfig(*plugin.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load device config file %s: %w, using default name", *plugin.ConfigFile, err)
	}

	p := &NvidiaMemoryPlugin{
		nvmllib: nvmllib,
		config:  config.NvidiaConfig,
	}
	p.BasePlugin = common.BasePlugin{
		ResourceName: config.NvidiaConfig.ResourceMemoryName,
		SocketFile:   pluginapi.DevicePluginPath + nvidiaGPUMemorySocketName,
		Server:       grpc.NewServer(),
		Srv:          p,
		StopCh:       make(chan struct{}),
		ChangedCh:    make(chan struct{}),
	}

	if err := p.setDevices(); err != nil {
		return nil, fmt.Errorf("failed to get nvidia memory: %w", err)
	}

	return p, nil
}

// ListAndWatch lists devices and update that list according to the health status
func (p *NvidiaMemoryPlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	if err := s.Send(&pluginapi.ListAndWatchResponse{Devices: p.devices}); err != nil {
		klog.Error(fmt.Sprintf("err sending nvidia memory allocate info, err %v", err))
	}
	for {
		select {
		case <-p.StopCh:
			return nil

		case <-p.ChangedCh:
			klog.Info(fmt.Sprintf("change nvidia vGPU memory to %d", len(p.devices)))
			if err := s.Send(&pluginapi.ListAndWatchResponse{Devices: p.devices}); err != nil {
				klog.Error(fmt.Sprintf("err sending nvidia memory allocate info, err %v", err))
			}
		}
	}
}

func (p *NvidiaMemoryPlugin) setDevices() error {
	var devices []*pluginapi.Device

	ret := p.nvmllib.Init()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("failed to initialize NVML: %v", ret)
	}
	defer func() {
		ret := p.nvmllib.Shutdown()
		if ret != nvml.SUCCESS {
			klog.Infof("Error shutting down NVML: %v", ret)
		}
	}()
	count, ret := p.nvmllib.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("unable to get device count: %v", p.nvmllib.ErrorString(ret))
	}
	for i := 0; i < count; i++ {
		dev, ret := p.nvmllib.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			return fmt.Errorf("unable to get device at index %d: %v", i, p.nvmllib.ErrorString(ret))
		}

		memoryTotal := 0
		memory, ret := dev.GetMemoryInfo()
		if ret == nvml.SUCCESS {
			memoryTotal = int(memory.Total)
		} else {
			klog.Error("nvml get memory error ret=", ret)
			panic(0)
		}

		factor := 1
		if p.config.GPUMemoryFactor > 0 {
			factor = int(p.config.GPUMemoryFactor)
		}
		registeredmem := int32(memoryTotal / 1024 / 1024 / factor)
		if p.config.DeviceMemoryScaling != 1 {
			registeredmem = int32(float64(registeredmem) * p.config.DeviceMemoryScaling)
		}
		for j := 0; j < int(registeredmem); j++ {
			fakeID := GenerateVirtualDeviceID(i, j)
			devices = append(devices, &pluginapi.Device{
				ID:     fakeID,
				Health: pluginapi.Healthy,
			})
		}
		devices = append(devices)
		klog.V(2).Infoln("MemoryScaling=", p.config.DeviceMemoryScaling, "registeredmem=", registeredmem, "factor=", factor)
	}
	p.devices = devices

	return nil
}

func GenerateVirtualDeviceID(id int, fakeCounter int) string {
	return fmt.Sprintf("%d-%d", id, fakeCounter)
}

func (p *NvidiaMemoryPlugin) Devices() rm.Devices {
	return map[string]*rm.Device{
		"": &rm.Device{},
	}
}
