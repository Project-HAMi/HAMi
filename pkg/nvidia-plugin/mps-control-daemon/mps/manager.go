/**
# Copyright 2024 NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package mps

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/rm"
)

type Manager interface {
	Daemons() ([]*Daemon, error)
}

type manager struct {
	infolib   info.Interface
	nvmllib   nvml.Interface
	devicelib device.Interface
	config    *nvidia.DeviceConfig
}

type nullManager struct{}

// Daemons creates the required set of MPS daemons for the specified options.
func NewDaemons(infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, opts ...Option) ([]*Daemon, error) {
	manager, err := New(infolib, nvmllib, devicelib, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create MPS manager: %w", err)
	}
	return manager.Daemons()
}

// New creates a manager for MPS daemons.
// If MPS is not configured, a manager is returned that manages no daemons.
func New(infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, opts ...Option) (Manager, error) {
	m := &manager{
		infolib:   infolib,
		nvmllib:   nvmllib,
		devicelib: devicelib,
	}
	for _, opt := range opts {
		opt(m)
	}

	if strategy := m.config.Sharing.SharingStrategy(); strategy != spec.SharingStrategyMPS {
		klog.InfoS("Sharing strategy is not MPS; skipping MPS manager creation", "strategy", strategy)
		return &nullManager{}, nil
	}

	return m, nil
}

func (m *manager) Daemons() ([]*Daemon, error) {
	resourceManagers, err := rm.NewNVMLResourceManagers(m.infolib, m.nvmllib, m.devicelib, m.config)
	if err != nil {
		return nil, err
	}
	var daemons []*Daemon
	for _, resourceManager := range resourceManagers {
		// We don't create daemons if there are no devices associated with the resource manager.
		if len(resourceManager.Devices()) == 0 {
			klog.InfoS("No devices associated with resource", "resource", resourceManager.Resource())
			continue
		}
		// Check if the resources are shared.
		// TODO: We should add a more explicit check for MPS specifically
		if !rm.AnnotatedIDs(resourceManager.Devices().GetIDs()).AnyHasAnnotations() {
			klog.InfoS("Resource is not shared", "resource", "resource", resourceManager.Resource())
			continue
		}
		// Check if MIG devices are included.
		for _, rmDevice := range resourceManager.Devices() {
			if rmDevice.IsMigDevice() {
				klog.Warning("MPS sharing is not supported for MIG devices; skipping daemon creation")
				continue
			}
			if err := (*mpsDevice)(rmDevice).assertReplicas(); err != nil {
				return nil, fmt.Errorf("invalid MPS configuration: %w", err)
			}
		}
		daemon := NewDaemon(resourceManager, ContainerRoot)
		daemons = append(daemons, daemon)
	}

	return daemons, nil
}

// Daemons always returns an empty slice for a nullManager.
func (m *nullManager) Daemons() ([]*Daemon, error) {
	return nil, nil
}
