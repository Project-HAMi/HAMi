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

package cdi

import (
	"fmt"
	"path/filepath"

	nvdevice "github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	roottransform "github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/transform/root"
	"github.com/sirupsen/logrus"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

const (
	cdiRoot = "/var/run/cdi"
)

// cdiHandler creates CDI specs for devices assocatied with the device plugin.
type cdiHandler struct {
	logger           *logrus.Logger
	nvml             nvml.Interface
	nvdevice         nvdevice.Interface
	driverRoot       string
	targetDriverRoot string
	nvidiaCTKPath    string
	cdiRoot          string
	vendor           string
	deviceIDStrategy string

	enabled      bool
	gdsEnabled   bool
	mofedEnabled bool

	cdilibs map[string]nvcdi.Interface
}

var _ Interface = &cdiHandler{}

// newHandler constructs a new instance of the 'cdi' interface.
func newHandler(opts ...Option) (Interface, error) {
	c := &cdiHandler{}
	for _, opt := range opts {
		opt(c)
	}

	if !c.enabled {
		return &null{}, nil
	}

	if c.logger == nil {
		c.logger = logrus.StandardLogger()
	}
	if c.nvml == nil {
		c.nvml = nvml.New()
	}
	if c.nvdevice == nil {
		c.nvdevice = nvdevice.New(nvdevice.WithNvml(c.nvml))
	}
	if c.deviceIDStrategy == "" {
		c.deviceIDStrategy = "uuid"
	}
	if c.driverRoot == "" {
		c.driverRoot = "/"
	}
	if c.targetDriverRoot == "" {
		c.targetDriverRoot = c.driverRoot
	}

	deviceNamer, err := nvcdi.NewDeviceNamer(c.deviceIDStrategy)
	if err != nil {
		return nil, err
	}

	c.cdilibs = make(map[string]nvcdi.Interface)

	c.cdilibs["gpu"], err = nvcdi.New(
		nvcdi.WithLogger(c.logger),
		nvcdi.WithNvmlLib(c.nvml),
		nvcdi.WithDeviceLib(c.nvdevice),
		nvcdi.WithNVIDIACTKPath(c.nvidiaCTKPath),
		nvcdi.WithDriverRoot(c.driverRoot),
		nvcdi.WithDeviceNamers(deviceNamer),
		nvcdi.WithVendor(c.vendor),
		nvcdi.WithClass("gpu"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create nvcdi library: %v", err)
	}

	var additionalModes []string
	if c.gdsEnabled {
		additionalModes = append(additionalModes, "gds")
	}
	if c.mofedEnabled {
		additionalModes = append(additionalModes, "mofed")
	}

	for _, mode := range additionalModes {
		lib, err := nvcdi.New(
			nvcdi.WithLogger(c.logger),
			nvcdi.WithNVIDIACTKPath(c.nvidiaCTKPath),
			nvcdi.WithDriverRoot(c.driverRoot),
			nvcdi.WithVendor(c.vendor),
			nvcdi.WithMode(mode),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create nvcdi library: %v", err)
		}
		c.cdilibs[mode] = lib
	}

	return c, nil
}

// CreateSpecFile creates a CDI spec file for the specified devices.
func (cdi *cdiHandler) CreateSpecFile() error {
	for class, cdilib := range cdi.cdilibs {
		cdi.logger.Infof("Generating CDI spec for resource: %s/%s", cdi.vendor, class)

		if class == "gpu" {
			ret := cdi.nvml.Init()
			if ret != nvml.SUCCESS {
				return fmt.Errorf("failed to initialize NVML: %v", ret)
			}
			defer cdi.nvml.Shutdown()
		}

		spec, err := cdilib.GetSpec()
		if err != nil {
			return fmt.Errorf("failed to get CDI spec: %v", err)
		}

		err = roottransform.New(
			roottransform.WithRoot(cdi.driverRoot),
			roottransform.WithTargetRoot(cdi.targetDriverRoot),
		).Transform(spec.Raw())
		if err != nil {
			return fmt.Errorf("failed to transform driver root in CDI spec: %v", err)
		}

		raw := spec.Raw()
		specName, err := cdiapi.GenerateNameForSpec(raw)
		if err != nil {
			return fmt.Errorf("failed to generate spec name: %v", err)
		}

		err = spec.Save(filepath.Join(cdiRoot, specName+".json"))
		if err != nil {
			return fmt.Errorf("failed to save CDI spec: %v", err)
		}
	}

	return nil
}

// QualifiedName constructs a CDI qualified device name for the specified resources.
// Note: This assumes that the specified id matches the device name returned by the naming strategy.
func (cdi *cdiHandler) QualifiedName(class string, id string) string {
	return cdiapi.QualifiedName(cdi.vendor, class, id)
}
