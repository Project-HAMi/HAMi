/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package tegra

import (
	"path/filepath"
	"strings"
)

type ignoreMountSpecPatterns []string

func (d ignoreMountSpecPatterns) Match(name string) bool {
	for _, pattern := range d {
		target := name
		if strings.HasPrefix(pattern, "**/") {
			target = filepath.Base(name)
			pattern = strings.TrimPrefix(pattern, "**/")
		}
		if match, _ := filepath.Match(pattern, target); match {
			return true
		}
	}
	return false
}

func (d ignoreMountSpecPatterns) Apply(input ...string) []string {
	var filtered []string
	for _, name := range input {
		if d.Match(name) {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}
