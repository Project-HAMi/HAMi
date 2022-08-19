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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPCIeID(t *testing.T) {
	d := &Device{
		pcie: &pcie{
			domain:   0,
			bus:      3,
			device:   15,
			function: 1,
		},
	}
	id, err := d.GetPCIeID()
	assert.NoError(t, err)
	assert.Equal(t, "0000:03:0f.1", id)
}

func TestGetNumFromFile(t *testing.T) {
	path := "/tmp/device_plugin_cndev_ut"
	f, err := os.Create(path)
	assert.NoError(t, err)

	data := []byte("4\n")
	_, err = f.Write(data)
	assert.NoError(t, err)
	num, err := getNumFromFile(path)
	assert.NoError(t, err)
	assert.Equal(t, 4, num)

	err = f.Close()
	assert.NoError(t, err)
	err = os.Remove(path)
	assert.NoError(t, err)
}
