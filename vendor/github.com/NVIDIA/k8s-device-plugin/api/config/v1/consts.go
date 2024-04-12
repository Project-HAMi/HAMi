/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package v1

import (
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
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
	DeviceListStrategyEnvvar         = "envvar"
	DeviceListStrategyVolumeMounts   = "volume-mounts"
	DeviceListStrategyCDIAnnotations = "cdi-annotations"
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
