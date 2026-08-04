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

	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	"github.com/stretchr/testify/require"
)

func TestQualifiedName(t *testing.T) {
	handler := &cdiHandler{
		vendor: "nvidia.com",
	}
	name := handler.QualifiedName("gpu", "0")
	require.Equal(t, "nvidia.com/gpu=0", name)
}

func TestAdditionalDevices(t *testing.T) {
	handler := &cdiHandler{
		vendor:          "nvidia.com",
		additionalModes: []string{"gdrcopy", "gds"},
		cdilibs: map[string]nvcdi.SpecGenerator{
			"gdrcopy": &imexChannelCDILib{},
		},
	}
	devices := handler.AdditionalDevices()
	require.Len(t, devices, 1)
	require.Equal(t, "nvidia.com/gdrcopy=all", devices[0])
}
