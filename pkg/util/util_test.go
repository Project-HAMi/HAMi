/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
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

package util

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
)

func TestEmptyContainerDevicesCoding(t *testing.T) {
	cd1 := ContainerDevices{}
	s := EncodeContainerDevices(cd1)
	fmt.Println(s)
	cd2, _ := DecodeContainerDevices(s)
	assert.DeepEqual(t, cd1, cd2)
}

func TestEmptyPodDeviceCoding(t *testing.T) {
	pd1 := PodDevices{}
	s := EncodePodDevices(pd1)
	fmt.Println(s)
	pd2, _ := DecodePodDevices(s)
	assert.DeepEqual(t, pd1, pd2)
}

func TestPodDevicesCoding(t *testing.T) {
	pd1 := PodDevices{
		ContainerDevices{
			ContainerDevice{"UUID1", "Type1", 1000, 30},
		},
		ContainerDevices{},
		ContainerDevices{
			ContainerDevice{"UUID1", "Type1", 1000, 30},
		},
	}
	s := EncodePodDevices(pd1)
	fmt.Println(s)
	pd2, _ := DecodePodDevices(s)
	assert.DeepEqual(t, pd1, pd2)
}
