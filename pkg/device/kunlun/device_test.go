/*
Copyright 2025 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kunlun

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"gotest.tools/v3/assert"
)

func Test_graphSelect(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			d []*util.DeviceUsage
			c int
		}
		want1 []int
	}{
		{
			name: "full allocate",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					&util.DeviceUsage{Index: 0, Used: 0},
					&util.DeviceUsage{Index: 1, Used: 0},
					&util.DeviceUsage{Index: 2, Used: 0},
					&util.DeviceUsage{Index: 3, Used: 0},
					&util.DeviceUsage{Index: 4, Used: 0},
					&util.DeviceUsage{Index: 5, Used: 0},
					&util.DeviceUsage{Index: 6, Used: 0},
					&util.DeviceUsage{Index: 7, Used: 0},
				},
				c: 8,
			},
			want1: []int{0, 1, 2, 3, 4, 5, 6, 7},
		},
		{
			name: "full allocate not success",
			args: struct {
				d []*util.DeviceUsage
				c int
			}{
				d: []*util.DeviceUsage{
					&util.DeviceUsage{Index: 0, Used: 0},
					&util.DeviceUsage{Index: 1, Used: 0},
					&util.DeviceUsage{Index: 2, Used: 0},
					&util.DeviceUsage{Index: 3, Used: 0},
					&util.DeviceUsage{Index: 4, Used: 0},
					&util.DeviceUsage{Index: 5, Used: 1},
					&util.DeviceUsage{Index: 6, Used: 0},
					&util.DeviceUsage{Index: 7, Used: 0},
				},
				c: 8,
			},
			want1: []int{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1 := graghSelect(test.args.d, 8)
			assert.DeepEqual(t, result1, test.want1)
		})
	}
}
