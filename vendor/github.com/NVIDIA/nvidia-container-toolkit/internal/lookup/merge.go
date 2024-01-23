/**
# Copyright 2023 NVIDIA CORPORATION
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

package lookup

import "errors"

type first []Locator

// First returns a locator that returns the first non-empty match
func First(locators ...Locator) Locator {
	var f first
	for _, l := range locators {
		if l == nil {
			continue
		}
		f = append(f, l)
	}
	return f
}

// Locate returns the results for the first locator that returns a non-empty non-error result.
func (f first) Locate(pattern string) ([]string, error) {
	var allErrors []error
	for _, l := range f {
		if l == nil {
			continue
		}
		candidates, err := l.Locate(pattern)
		if err != nil {
			allErrors = append(allErrors, err)
			continue
		}
		if len(candidates) > 0 {
			return candidates, nil
		}
	}

	return nil, errors.Join(allErrors...)
}
