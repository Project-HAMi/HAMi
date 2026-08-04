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
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
)

func TestImexChannelCDILib_GetSpec(t *testing.T) {
	channels := imex.Channels{
		{ID: "channel1", Path: "/dev/imex/channel1", HostPath: "/dev/imex/channel1"},
		{ID: "channel2", Path: "/dev/imex/channel2", HostPath: "/dev/imex/channel2"},
	}

	lib := &imexChannelCDILib{
		vendor:       "nvidia.com",
		imexChannels: channels,
	}

	spec, err := lib.GetSpec()
	require.NoError(t, err)
	require.NotNil(t, spec)

	raw := spec.Raw()
	require.Len(t, raw.Devices, 2)
	
	found1 := false
	found2 := false
	for _, dev := range raw.Devices {
		if dev.Name == "channel1" {
			found1 = true
			require.Len(t, dev.ContainerEdits.DeviceNodes, 1)
			require.Equal(t, "/dev/imex/channel1", dev.ContainerEdits.DeviceNodes[0].Path)
		} else if dev.Name == "channel2" {
			found2 = true
			require.Len(t, dev.ContainerEdits.DeviceNodes, 1)
			require.Equal(t, "/dev/imex/channel2", dev.ContainerEdits.DeviceNodes[0].Path)
		}
	}
	require.True(t, found1)
	require.True(t, found2)
}
