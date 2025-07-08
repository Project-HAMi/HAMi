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

package manager

import (
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

type manager struct {
	migStrategy     string
	failOnInitError bool
	nvmllib         nvml.Interface

	cdiHandler cdi.Interface
	cdiEnabled bool
	config     *nvidia.DeviceConfig
	infolib    info.Interface
}

// New creates a new plugin manager with the supplied options.
func New(opts ...Option) (Interface, error) {
	m := &manager{}
	for _, opt := range opts {
		opt(m)
	}

	if m.config == nil {
		klog.Warning("no config provided, returning a null manager")
		return &null{}, nil
	}

	if m.infolib == nil {
		m.infolib = info.New()
	}
	if m.cdiHandler == nil {
		m.cdiHandler = cdi.NewNullHandler()
	}

	mode, err := m.resolveMode()
	if err != nil {
		return nil, err
	}

	if mode != "nvml" && m.cdiEnabled {
		klog.Warning("CDI is not supported; disabling CDI.")
		m.cdiEnabled = false
	}

	switch mode {
	case "nvml":
		if m.nvmllib == nil {
			m.nvmllib = nvml.New()
		}
		ret := m.nvmllib.Init()
		if ret != nvml.SUCCESS {
			klog.Errorf("Failed to initialize NVML: %v.", ret)
			klog.Errorf("If this is a GPU node, did you set the docker default runtime to `nvidia`?")
			klog.Errorf("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
			klog.Errorf("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
			klog.Errorf("If this is not a GPU node, you should set up a toleration or nodeSelector to only deploy this plugin on GPU nodes")
			if m.failOnInitError {
				return nil, fmt.Errorf("nvml init failed: %v", ret)
			}
			klog.Warningf("nvml init failed: %v", ret)
			return &null{}, nil
		}
		defer m.nvmllib.Shutdown()

		return (*nvmlmanager)(m), nil
	case "tegra":
		return (*tegramanager)(m), nil
	case "null":
		return &null{}, nil
	}

	return nil, fmt.Errorf("unknown mode: %v", mode)
}

func (m *manager) resolveMode() (string, error) {
	// logWithReason logs the output of the has* / is* checks from the info.Interface
	logWithReason := func(f func() (bool, string), tag string) bool {
		is, reason := f()
		if !is {
			tag = "non-" + tag
		}
		klog.Infof("Detected %v platform: %v", tag, reason)
		return is
	}

	hasNVML := logWithReason(m.infolib.HasNvml, "NVML")
	isTegra := logWithReason(m.infolib.IsTegraSystem, "Tegra")

	if !hasNVML && !isTegra {
		klog.Error("Incompatible platform detected")
		klog.Error("If this is a GPU node, did you configure the NVIDIA Container Toolkit?")
		klog.Error("You can check the prerequisites at: https://github.com/NVIDIA/k8s-device-plugin#prerequisites")
		klog.Error("You can learn how to set the runtime at: https://github.com/NVIDIA/k8s-device-plugin#quick-start")
		klog.Error("If this is not a GPU node, you should set up a toleration or nodeSelector to only deploy this plugin on GPU nodes")
		if m.failOnInitError {
			return "", fmt.Errorf("platform detection failed")
		}
		return "null", nil
	}

	// The NVIDIA container stack does not yet support the use of integrated AND discrete GPUs on the same node.
	if isTegra {
		if hasNVML {
			klog.Warning("Disabling Tegra-based resources on NVML system")
			return "nvml", nil
		}
		return "tegra", nil
	}

	return "nvml", nil
}
