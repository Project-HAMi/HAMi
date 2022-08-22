// Copyright 2020 Cambricon, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cndev

import (
	"fmt"
	"log"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	err := Init()
	if err != nil {
		log.Fatal(err)
	}
	ret := m.Run()
	if ret != 0 {
		os.Exit(ret)
	}
	err = Release()
	if err != nil {
		log.Fatal(err)
	}
}

func TestGetDeviceCount(t *testing.T) {
	count, err := GetDeviceCount()
	assert.NoError(t, err)
	assert.Equal(t, uint(8), count)
}

func TestGetDeviceModel(t *testing.T) {
	model := GetDeviceModel(uint(0))
	assert.Equal(t, "MLU290", model)
}

func TestGetDeviceMemory(t *testing.T) {
	memory, err := GetDeviceMemory(uint(0))
	assert.NoError(t, err)
	assert.Equal(t, uint(16*1024), memory)
}

func TestGetDeviceInfo(t *testing.T) {
	uuid, _, mb, path, err := getDeviceInfo(uint(1))
	assert.NoError(t, err)
	assert.Equal(t, "/dev/cambricon_dev1", path)
	assert.Equal(t, "MLU-20001012-1916-0000-0000-000000000000", uuid)
	assert.Equal(t, fmt.Sprintf("%x", 1111111), mb)
}

func TestGetDeviceHealthState(t *testing.T) {
	health, err := getDeviceHealthState(uint(0), 1)
	assert.NoError(t, err)
	assert.Equal(t, 1, health)
}

func TestGetDevicePCIeInfo(t *testing.T) {
	pcie, err := getDevicePCIeInfo(uint(0))
	assert.NoError(t, err)
	assert.Equal(t, 0, pcie.domain)
	assert.Equal(t, 12, pcie.bus)
	assert.Equal(t, 13, pcie.device)
	assert.Equal(t, 1, pcie.function)
}

func TestGetDeviceMLULinkDevs(t *testing.T) {
	devs, err := getDeviceMLULinkDevs(uint(0))
	assert.NoError(t, err)
	assert.Equal(t, map[string]int{
		"MLU-20001012-1916-0000-0000-000000000000": 1,
		"MLU-30001012-1916-0000-0000-000000000000": 2,
		"MLU-40001012-1916-0000-0000-000000000000": 1,
		"MLU-50001012-1916-0000-0000-000000000000": 1,
		"MLU-d0001012-1916-0000-0000-000000000000": 1,
	}, devs)
}

func TestGetMLULinkGroups(t *testing.T) {
	groups, err := GetMLULinkGroups()
	assert.NoError(t, err)
	for i := range groups {
		sort.Slice(groups[i], func(x, y int) bool {
			return groups[i][x] < groups[i][y]
		})
	}
	assert.Equal(t, [][]uint{{0, 1, 2, 3, 4, 5, 6, 7}}, groups)
}
