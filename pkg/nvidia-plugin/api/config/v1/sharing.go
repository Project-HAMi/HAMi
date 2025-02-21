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

// Sharing encapsulates the set of sharing strategies that are supported.
type Sharing struct {
	// TimeSlicing defines the set of replicas to be made for timeSlicing available resources.
	TimeSlicing ReplicatedResources `json:"timeSlicing,omitempty" yaml:"timeSlicing,omitempty"`
	// MPS defines the set of replicas to be shared using MPS
	MPS *ReplicatedResources `json:"mps,omitempty"         yaml:"mps,omitempty"`
}

type SharingStrategy string

const (
	SharingStrategyMPS         = SharingStrategy("mps")
	SharingStrategyNone        = SharingStrategy("none")
	SharingStrategyTimeSlicing = SharingStrategy("time-slicing")
)

// SharingStrategy returns the active sharing strategy.
func (s *Sharing) SharingStrategy() SharingStrategy {
	if s.MPS != nil && s.MPS.isReplicated() {
		return SharingStrategyMPS
	}

	if s.TimeSlicing.isReplicated() {
		return SharingStrategyTimeSlicing
	}
	return SharingStrategyNone
}

// ReplicatedResources returns the resources associated with the active sharing strategy.
func (s *Sharing) ReplicatedResources() *ReplicatedResources {
	if s.MPS != nil {
		return s.MPS
	}
	return &s.TimeSlicing
}
