/*
Copyright 2026 The HAMi Authors.

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

package cdi

import (
	"testing"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
)

func TestOptions(t *testing.T) {
	c := &cdiHandler{}

	strategies := spec.DeviceListStrategies{"cdi": true}
	WithDeviceListStrategies(strategies)(c)
	if !c.deviceListStrategies.Includes("cdi") {
		t.Errorf("WithDeviceListStrategies failed, got %v", c.deviceListStrategies)
	}

	WithDriverRoot("/driver-root")(c)
	if c.driverRoot != "/driver-root" {
		t.Errorf("WithDriverRoot failed, got %s", c.driverRoot)
	}

	WithDevRoot("/dev-root")(c)
	if c.devRoot != "/dev-root" {
		t.Errorf("WithDevRoot failed, got %s", c.devRoot)
	}

	WithTargetDriverRoot("/target-driver-root")(c)
	if c.targetDriverRoot != "/target-driver-root" {
		t.Errorf("WithTargetDriverRoot failed, got %s", c.targetDriverRoot)
	}

	WithTargetDevRoot("/target-dev-root")(c)
	if c.targetDevRoot != "/target-dev-root" {
		t.Errorf("WithTargetDevRoot failed, got %s", c.targetDevRoot)
	}

	WithNvidiaCTKPath("/usr/bin/nvidia-ctk")(c)
	if c.nvidiaCTKPath != "/usr/bin/nvidia-ctk" {
		t.Errorf("WithNvidiaCTKPath failed, got %s", c.nvidiaCTKPath)
	}

	WithDeviceIDStrategy("uuid")(c)
	if c.deviceIDStrategy != "uuid" {
		t.Errorf("WithDeviceIDStrategy failed, got %s", c.deviceIDStrategy)
	}

	WithVendor("nvidia.com")(c)
	if c.vendor != "nvidia.com" {
		t.Errorf("WithVendor failed, got %s", c.vendor)
	}

	WithGdrcopyEnabled(true)(c)
	if !c.gdrcopyEnabled {
		t.Errorf("WithGdrcopyEnabled failed")
	}

	WithGdsEnabled(true)(c)
	if !c.gdsEnabled {
		t.Errorf("WithGdsEnabled failed")
	}

	WithMofedEnabled(true)(c)
	if !c.mofedEnabled {
		t.Errorf("WithMofedEnabled failed")
	}

	ch := imex.Channels{&imex.Channel{ID: "0", Path: "/dev/channel0"}}
	WithImexChannels(ch)(c)
	if len(c.imexChannels) != 1 || c.imexChannels[0].ID != "0" {
		t.Errorf("WithImexChannels failed, got %v", c.imexChannels)
	}
}
