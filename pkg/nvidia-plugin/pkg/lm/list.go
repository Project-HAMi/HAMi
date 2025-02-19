/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

package lm

import "fmt"

// list represents a list of labelers that iself implements the Labeler interface.
type list []Labeler

// Merge converts a set of labelers to a single composite labeler.
func Merge(labelers ...Labeler) Labeler {
	l := list(labelers)

	return l
}

// Labels returns the labels from a set of labelers. Labels later in the list
// overwrite earlier labels.
func (labelers list) Labels() (Labels, error) {
	allLabels := make(Labels)
	for _, labeler := range labelers {
		labels, err := labeler.Labels()
		if err != nil {
			return nil, fmt.Errorf("error generating labels: %v", err)
		}
		for k, v := range labels {
			allLabels[k] = v
		}
	}

	return allLabels, nil
}
