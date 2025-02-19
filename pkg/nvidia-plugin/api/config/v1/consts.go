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
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing,
* software distributed under the License is distributed on an
* "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
* KIND, either express or implied.  See the License for the
* specific language governing permissions and limitations
* under the License.
 */

/*
* Modifications Copyright The HAMi Authors. See
* GitHub history for details.
 */

package v1

import (
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
)

// Constants related to resource names
const (
	ResourceNamePrefix              = "nvidia.com"
	DefaultSharedResourceNameSuffix = ".shared"
	MaxResourceNameLength           = 63
)

// Constants representing the various MIG strategies
const (
	MigStrategyNone   = "none"
	MigStrategySingle = "single"
	MigStrategyMixed  = "mixed"
)

// Constants to represent the various device list strategies
const (
	DeviceListStrategyEnvVar         = "envvar"
	DeviceListStrategyVolumeMounts   = "volume-mounts"
	DeviceListStrategyCDIAnnotations = "cdi-annotations"
	DeviceListStrategyCDICRI         = "cdi-cri"
)

// Constants to represent the various device id strategies
const (
	DeviceIDStrategyUUID  = "uuid"
	DeviceIDStrategyIndex = "index"
)

// Constants related to generating CDI specifications
const (
	DefaultCDIAnnotationPrefix = cdiapi.AnnotationPrefix
	DefaultNvidiaCTKPath       = "/usr/bin/nvidia-ctk"
	DefaultContainerDriverRoot = "/driver-root"
)
