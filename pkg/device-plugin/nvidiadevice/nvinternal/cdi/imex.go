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
	"tags.cncf.io/container-device-interface/specs-go"

	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
)

type imexChannelCDILib struct {
	vendor       string
	imexChannels imex.Channels
}

func (cdi *cdiHandler) newImexChannelSpecGenerator() nvcdi.SpecGenerator {
	lib := &imexChannelCDILib{
		vendor:       cdi.vendor,
		imexChannels: cdi.imexChannels,
	}

	return lib
}

// GetSpec returns the CDI specs for IMEX channels.
func (l *imexChannelCDILib) GetSpec(...string) (spec.Interface, error) {
	var deviceSpecs []specs.Device
	for _, channel := range l.imexChannels {
		deviceSpec := specs.Device{
			Name: channel.ID,
			ContainerEdits: specs.ContainerEdits{
				DeviceNodes: []*specs.DeviceNode{
					{
						Path:     channel.Path,
						HostPath: channel.HostPath,
					},
				},
			},
		}
		deviceSpecs = append(deviceSpecs, deviceSpec)
	}
	return spec.New(
		spec.WithDeviceSpecs(deviceSpecs),
		spec.WithVendor(l.vendor),
		spec.WithClass("imex-channel"),
	)
}
