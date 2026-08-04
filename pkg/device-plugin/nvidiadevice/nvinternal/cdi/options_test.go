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
	"testing"

	"github.com/stretchr/testify/require"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
)

func TestOptions(t *testing.T) {
	testCases := []struct {
		name     string
		option   Option
		validate func(*testing.T, *cdiHandler)
	}{
		{
			name:   "WithDeviceListStrategies",
			option: WithDeviceListStrategies(spec.DeviceListStrategies{"cdi": true}),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, spec.DeviceListStrategies{"cdi": true}, c.deviceListStrategies)
			},
		},
		{
			name:   "WithDriverRoot",
			option: WithDriverRoot("/driver-root"),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, "/driver-root", c.driverRoot)
			},
		},
		{
			name:   "WithDevRoot",
			option: WithDevRoot("/dev-root"),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, "/dev-root", c.devRoot)
			},
		},
		{
			name:   "WithTargetDriverRoot",
			option: WithTargetDriverRoot("/target-driver-root"),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, "/target-driver-root", c.targetDriverRoot)
			},
		},
		{
			name:   "WithTargetDevRoot",
			option: WithTargetDevRoot("/target-dev-root"),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, "/target-dev-root", c.targetDevRoot)
			},
		},
		{
			name:   "WithNvidiaCTKPath",
			option: WithNvidiaCTKPath("/nvidia-ctk"),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, "/nvidia-ctk", c.nvidiaCTKPath)
			},
		},
		{
			name:   "WithDeviceIDStrategy",
			option: WithDeviceIDStrategy("uuid"),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, "uuid", c.deviceIDStrategy)
			},
		},
		{
			name:   "WithVendor",
			option: WithVendor("nvidia.com"),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Equal(t, "nvidia.com", c.vendor)
			},
		},
		{
			name:   "WithGdrcopyEnabled",
			option: WithGdrcopyEnabled(true),
			validate: func(t *testing.T, c *cdiHandler) {
				require.True(t, c.gdrcopyEnabled)
			},
		},
		{
			name:   "WithGdsEnabled",
			option: WithGdsEnabled(true),
			validate: func(t *testing.T, c *cdiHandler) {
				require.True(t, c.gdsEnabled)
			},
		},
		{
			name:   "WithMofedEnabled",
			option: WithMofedEnabled(true),
			validate: func(t *testing.T, c *cdiHandler) {
				require.True(t, c.mofedEnabled)
			},
		},
		{
			name: "WithImexChannels",
			option: WithImexChannels(imex.Channels{
				{ID: "channel1", Path: "/path1"},
			}),
			validate: func(t *testing.T, c *cdiHandler) {
				require.Len(t, c.imexChannels, 1)
				require.Equal(t, "channel1", c.imexChannels[0].ID)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := &cdiHandler{}
			tc.option(handler)
			tc.validate(t, handler)
		})
	}
}
