/*
 * Copyright (c), NVIDIA CORPORATION.  All rights reserved.
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
	"fmt"
	"strings"
)

// DeviceListStrategies defines which strategies are enabled and should
// be used when passing the device list to the container runtime.
type DeviceListStrategies map[string]bool

// NewDeviceListStrategies constructs a new DeviceListStrategy
func NewDeviceListStrategies(strategies []string) (DeviceListStrategies, error) {
	ret := map[string]bool{
		DeviceListStrategyEnvvar:         false,
		DeviceListStrategyVolumeMounts:   false,
		DeviceListStrategyCDIAnnotations: false,
	}
	for _, s := range strategies {
		if _, ok := ret[s]; !ok {
			return nil, fmt.Errorf("invalid strategy: %v", s)
		}
		ret[s] = true
	}

	return DeviceListStrategies(ret), nil
}

// Includes returns whether the given strategy is present in the set of strategies.
func (s DeviceListStrategies) Includes(strategy string) bool {
	return s[strategy]
}

// IsCDIEnabled returns whether any of the strategies being used require CDI.
func (s DeviceListStrategies) IsCDIEnabled() bool {
	for k, v := range s {
		if strings.HasPrefix(k, "cdi-") && v {
			return true
		}
	}
	return false
}
