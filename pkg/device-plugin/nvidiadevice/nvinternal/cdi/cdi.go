/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */

package cdi

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	nvcdispec "github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/transform"
	transformroot "github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/transform/root"
	"github.com/sirupsen/logrus"
	"k8s.io/klog/v2"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
)

const (
	cdiRoot = "/var/run/cdi"
)

// cdiHandler creates CDI specs for devices assocatied with the device plugin
type cdiHandler struct {
	infolib   info.Interface
	nvmllib   nvml.Interface
	devicelib device.Interface

	logger           *logrus.Logger
	driverRoot       string
	devRoot          string
	targetDriverRoot string
	targetDevRoot    string
	nvidiaCTKPath    string
	vendor           string
	deviceIDStrategy string

	deviceListStrategies spec.DeviceListStrategies

	gdsEnabled     bool
	mofedEnabled   bool
	gdrcopyEnabled bool

	imexChannels       imex.Channels
	dynamicMIGMode     bool
	dynamicMIGRoot     string
	dynamicMIGProcRoot string
	dynamicMIGLockMu   sync.Mutex
	dynamicMIGLocks    map[string]*dynamicMIGLock

	cdilibs         map[string]nvcdi.SpecGenerator
	additionalModes []string
}

var _ Interface = &cdiHandler{}

// SetDynamicMIGMode selects parent-only base CDI discovery before startup specs are written.
func (cdi *cdiHandler) SetDynamicMIGMode(enabled bool) {
	cdi.dynamicMIGMode = enabled
}

func (cdi *cdiHandler) DynamicMIGMode() bool {
	return cdi.dynamicMIGMode
}

// New constructs a new instance of the 'cdi' interface
func New(infolib info.Interface, nvmllib nvml.Interface, devicelib device.Interface, opts ...Option) (Interface, error) {
	c := &cdiHandler{
		infolib:   infolib,
		nvmllib:   nvmllib,
		devicelib: devicelib,
	}
	for _, opt := range opts {
		opt(c)
	}

	if !c.deviceListStrategies.AnyCDIEnabled() {
		return &null{}, nil
	}
	hasNVML, _ := infolib.HasNvml()
	if !hasNVML {
		klog.Warning("No valid resources detected, creating a null CDI handler")
		return &null{}, nil
	}

	if c.logger == nil {
		c.logger = logrus.StandardLogger()
	}
	if c.deviceIDStrategy == "" {
		c.deviceIDStrategy = "uuid"
	}
	if c.driverRoot == "" {
		c.driverRoot = "/"
	}
	if c.devRoot == "" {
		c.devRoot = c.driverRoot
	}
	if c.targetDriverRoot == "" {
		c.targetDriverRoot = c.driverRoot
	}
	if c.targetDevRoot == "" {
		c.targetDevRoot = c.devRoot
	}

	deviceNamer, err := nvcdi.NewDeviceNamer(c.deviceIDStrategy)
	if err != nil {
		return nil, err
	}

	c.cdilibs = make(map[string]nvcdi.SpecGenerator)

	c.cdilibs["gpu"], err = nvcdi.New(
		nvcdi.WithInfoLib(c.infolib),
		nvcdi.WithNvmlLib(c.nvmllib),
		nvcdi.WithDeviceLib(c.devicelib),
		nvcdi.WithLogger(c.logger),
		nvcdi.WithNVIDIACDIHookPath(c.nvidiaCTKPath),
		nvcdi.WithDriverRoot(c.driverRoot),
		nvcdi.WithDevRoot(c.devRoot),
		nvcdi.WithDeviceNamers(deviceNamer),
		nvcdi.WithVendor(c.vendor),
		nvcdi.WithClass("gpu"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create nvcdi library: %v", err)
	}

	if len(c.imexChannels) > 0 {
		c.cdilibs["imex-channel"] = c.newImexChannelSpecGenerator()
	}

	if c.gdrcopyEnabled {
		c.additionalModes = append(c.additionalModes, "gdrcopy")
	}
	if c.gdsEnabled {
		c.additionalModes = append(c.additionalModes, "gds")
	}
	if c.mofedEnabled {
		c.additionalModes = append(c.additionalModes, "mofed")
	}

	for _, mode := range c.additionalModes {
		lib, err := nvcdi.New(
			nvcdi.WithInfoLib(c.infolib),
			nvcdi.WithLogger(c.logger),
			nvcdi.WithNVIDIACDIHookPath(c.nvidiaCTKPath),
			nvcdi.WithDriverRoot(c.driverRoot),
			nvcdi.WithDevRoot(c.devRoot),
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
	var emptySpecs []string
	for class, cdilib := range cdi.cdilibs {
		cdi.logger.Infof("Generating CDI spec for resource: %s/%s", cdi.vendor, class)

		if class == "gpu" && !cdi.dynamicMIGMode {
			ret := cdi.nvmllib.Init()
			if ret != nvml.SUCCESS {
				return fmt.Errorf("failed to initialize NVML: %v", ret)
			}
			defer func() {
				_ = cdi.nvmllib.Shutdown()
			}()
		}

		var spec nvcdispec.Interface
		var err error
		if class == "gpu" && cdi.dynamicMIGMode {
			count, ret := cdi.nvmllib.DeviceGetCount()
			if ret != nvml.SUCCESS {
				return fmt.Errorf("list parent GPUs for CDI: %s", nvml.ErrorString(ret))
			}
			if count == 0 {
				return fmt.Errorf("dynamic MIG CDI requires at least one parent GPU")
			}
			ids := make([]string, 0, count)
			for i := 0; i < count; i++ {
				gpu, ret := cdi.nvmllib.DeviceGetHandleByIndex(i)
				if ret != nvml.SUCCESS {
					return fmt.Errorf("get parent GPU %d for CDI: %s", i, nvml.ErrorString(ret))
				}
				id, ret := gpu.GetUUID()
				if ret != nvml.SUCCESS {
					return fmt.Errorf("get parent GPU UUID for CDI: %s", nvml.ErrorString(ret))
				}
				ids = append(ids, id)
			}
			spec, err = cdilib.GetSpec(ids...)
		} else {
			spec, err = cdilib.GetSpec()
		}
		if err != nil {
			return fmt.Errorf("failed to get CDI spec: %v", err)
		}

		// TODO: Once the NewDriverTransformer is merged in container-toolkit we can instantiate it directly.
		transformer := cdi.getRootTransformer()
		if err := transformer.Transform(spec.Raw()); err != nil {
			return fmt.Errorf("failed to transform driver root in CDI spec: %v", err)
		}

		specName, err := cdiapi.GenerateNameForSpec(spec.Raw())
		if err != nil {
			return fmt.Errorf("failed to generate spec name: %v", err)
		}

		err = spec.Save(filepath.Join(cdiRoot, specName+".json"))
		if err != nil {
			// TODO: This is a brittle check since it relies on exact string matches.
			// We should pull this functionality into the CDI tooling instead.
			if strings.Contains(err.Error(), "invalid device, empty device edits") {
				klog.ErrorS(err, "Ignoring empty CDI specs", "vendor", cdi.vendor, "class", class)
				emptySpecs = append(emptySpecs, class)
				continue
			}
			return fmt.Errorf("failed to save CDI spec: %v", err)
		}
	}

	// Remove the classes with empty specs from the supported types.
	for _, emptySpec := range emptySpecs {
		delete(cdi.cdilibs, emptySpec)
	}

	return nil
}

func (cdi *cdiHandler) getRootTransformer() transform.Transformer {
	driverRootTransformer := transformroot.New(
		transformroot.WithRoot(cdi.driverRoot),
		transformroot.WithTargetRoot(cdi.targetDriverRoot),
		transformroot.WithRelativeTo("host"),
	)

	if cdi.devRoot == cdi.driverRoot || cdi.devRoot == "" {
		return driverRootTransformer
	}

	ensureDev := func(p string) string {
		return filepath.Join(strings.TrimSuffix(filepath.Clean(p), "/dev"), "/dev")
	}

	devRootTransformer := transformroot.New(
		transformroot.WithRoot(ensureDev(cdi.devRoot)),
		transformroot.WithTargetRoot(ensureDev(cdi.targetDevRoot)),
		transformroot.WithRelativeTo("host"),
	)

	return transform.Merge(driverRootTransformer, devRootTransformer)
}

// QualifiedName constructs a CDI qualified device name for the specified resources.
// Note: This assumes that the specified id matches the device name returned by the naming strategy.
func (cdi *cdiHandler) QualifiedName(class string, id string) string {
	return cdiparser.QualifiedName(cdi.vendor, class, id)
}

// AdditionalDevices returns the optional CDI devices based on the device plugin
// configuration.
// Here we check for requested modes as well as whether the modes have a valid
// CDI spec associated with them.
func (cdi *cdiHandler) AdditionalDevices() []string {
	var devices []string
	for _, mode := range cdi.additionalModes {
		if cdi.cdilibs[mode] == nil {
			continue
		}
		devices = append(devices, cdi.QualifiedName(mode, "all"))
	}
	return devices
}
