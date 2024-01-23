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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// TimeSlicing defines the set of replicas to be made for timeSlicing available resources.
type TimeSlicing struct {
	RenameByDefault            bool                 `json:"renameByDefault,omitempty"            yaml:"renameByDefault,omitempty"`
	FailRequestsGreaterThanOne bool                 `json:"failRequestsGreaterThanOne,omitempty" yaml:"failRequestsGreaterThanOne,omitempty"`
	Resources                  []ReplicatedResource `json:"resources,omitempty"                  yaml:"resources,omitempty"`
}

// ReplicatedResource represents a resource to be replicated.
type ReplicatedResource struct {
	Name     ResourceName      `json:"name"             yaml:"name"`
	Rename   ResourceName      `json:"rename,omitempty" yaml:"rename,omitempty"`
	Devices  ReplicatedDevices `json:"devices"          yaml:"devices,flow"`
	Replicas int               `json:"replicas"         yaml:"replicas"`
}

// ReplicatedDevices encapsulates the set of devices that should be replicated for a given resource.
// This struct should be treated as a 'union' and only one of the fields in this struct should be set at any given time.
type ReplicatedDevices struct {
	All   bool
	Count int
	List  []ReplicatedDeviceRef
}

// ReplicatedDeviceRef can either be a full GPU index, a MIG index, or a UUID (full GPU or MIG)
type ReplicatedDeviceRef string

// IsGPUIndex checks if a ReplicatedDeviceRef is a full GPU index
func (d ReplicatedDeviceRef) IsGPUIndex() bool {
	if _, err := strconv.ParseUint(string(d), 10, 0); err != nil {
		return false
	}
	return true
}

// IsMigIndex checks if a ReplicatedDeviceRef is a MIG index
func (d ReplicatedDeviceRef) IsMigIndex() bool {
	split := strings.SplitN(string(d), ":", 2)
	if len(split) != 2 {
		return false
	}
	for _, s := range split {
		if _, err := strconv.ParseUint(s, 10, 0); err != nil {
			return false
		}
	}
	return true
}

// IsUUID checks if a ReplicatedDeviceRef is a UUID
func (d ReplicatedDeviceRef) IsUUID() bool {
	return d.IsGpuUUID() || d.IsMigUUID()
}

// IsGpuUUID checks if a ReplicatedDeviceRef is a GPU UUID
// A GPU UUID must be of the form GPU-b1028956-cfa2-0990-bf4a-5da9abb51763
func (d ReplicatedDeviceRef) IsGpuUUID() bool {
	if !strings.HasPrefix(string(d), "GPU-") {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(string(d), "GPU-"))
	return err == nil
}

// IsMigUUID checks if a ReplicatedDeviceRef is a MIG UUID
// A MIG UUID can be of one of two forms:
//   - MIG-b1028956-cfa2-0990-bf4a-5da9abb51763
//   - MIG-GPU-b1028956-cfa2-0990-bf4a-5da9abb51763/3/0
func (d ReplicatedDeviceRef) IsMigUUID() bool {
	if !strings.HasPrefix(string(d), "MIG-") {
		return false
	}
	suffix := strings.TrimPrefix(string(d), "MIG-")
	_, err := uuid.Parse(suffix)
	if err == nil {
		return true
	}
	split := strings.SplitN(suffix, "/", 3)
	if len(split) != 3 {
		return false
	}
	if !ReplicatedDeviceRef(split[0]).IsGpuUUID() {
		return false
	}
	for _, s := range split[1:] {
		_, err := strconv.ParseUint(s, 10, 0)
		if err != nil {
			return false
		}
	}
	return true
}

// UnmarshalJSON unmarshals raw bytes into a 'TimeSlicing' struct.
func (s *TimeSlicing) UnmarshalJSON(b []byte) error {
	ts := make(map[string]json.RawMessage)
	err := json.Unmarshal(b, &ts)
	if err != nil {
		return err
	}

	renameByDefault, exists := ts["renameByDefault"]
	if !exists {
		renameByDefault = []byte(`false`)
	}

	err = json.Unmarshal(renameByDefault, &s.RenameByDefault)
	if err != nil {
		return err
	}

	failRequestsGreaterThanOne, exists := ts["failRequestsGreaterThanOne"]
	if !exists {
		failRequestsGreaterThanOne = []byte(`false`)
	}

	err = json.Unmarshal(failRequestsGreaterThanOne, &s.FailRequestsGreaterThanOne)
	if err != nil {
		return err
	}

	resources, exists := ts["resources"]
	if !exists {
		return fmt.Errorf("no resources specified")
	}

	err = json.Unmarshal(resources, &s.Resources)
	if err != nil {
		return err
	}

	if len(s.Resources) == 0 {
		return fmt.Errorf("no resources specified")
	}

	for i, r := range s.Resources {
		if s.RenameByDefault && r.Rename == "" {
			s.Resources[i].Rename = r.Name.DefaultSharedRename()
		}
	}

	return nil
}

// UnmarshalJSON unmarshals raw bytes into a 'ReplicatedResource' struct.
func (s *ReplicatedResource) UnmarshalJSON(b []byte) error {
	rr := make(map[string]json.RawMessage)
	err := json.Unmarshal(b, &rr)
	if err != nil {
		return err
	}

	name, exists := rr["name"]
	if !exists {
		return fmt.Errorf("no resource name specified")
	}

	err = json.Unmarshal(name, &s.Name)
	if err != nil {
		return err
	}

	devices, exists := rr["devices"]
	if !exists {
		devices = []byte(`"all"`)
	}

	err = json.Unmarshal(devices, &s.Devices)
	if err != nil {
		return err
	}

	replicas, exists := rr["replicas"]
	if !exists {
		return fmt.Errorf("no replicas specified")
	}

	err = json.Unmarshal(replicas, &s.Replicas)
	if err != nil {
		return err
	}

	if s.Replicas < 2 {
		return fmt.Errorf("number of replicas must be >= 2")
	}

	rename, exists := rr["rename"]
	if !exists {
		return nil
	}

	err = json.Unmarshal(rename, &s.Rename)
	if err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON unmarshals raw bytes into a 'ReplicatedDevices' struct.
func (s *ReplicatedDevices) UnmarshalJSON(b []byte) error {
	// Match the string 'all'
	var str string
	err := json.Unmarshal(b, &str)
	if err == nil {
		if str != "all" {
			return fmt.Errorf("devices set as '%v' but the only valid string input is 'all'", str)
		}
		s.All = true
		return nil
	}

	// Match a count
	var count int
	err = json.Unmarshal(b, &count)
	if err == nil {
		if count <= 0 {
			return fmt.Errorf("devices set as '%v' but a count of devices must be > 0", count)
		}
		s.Count = count
		return nil
	}

	// Match a list
	var slice []json.RawMessage
	err = json.Unmarshal(b, &slice)
	if err == nil {
		// For each item in the list check its format and convert it to a string (if necessary)
		result := make([]ReplicatedDeviceRef, len(slice))
		for i, s := range slice {
			// Match a uint as a GPU index and convert it to a string
			var index uint
			if err = json.Unmarshal(s, &index); err == nil {
				result[i] = ReplicatedDeviceRef(strconv.Itoa(int(index)))
				continue
			}
			// Match strings as valid entries if they are GPU indices, MIG indices, or UUIDs
			var item string
			if err = json.Unmarshal(s, &item); err == nil {
				rd := ReplicatedDeviceRef(item)
				if rd.IsGPUIndex() || rd.IsMigIndex() || rd.IsUUID() {
					result[i] = rd
					continue
				}
			}
			// Treat any other entries as errors
			return fmt.Errorf("unsupported type for device in devices list: %v, %T", item, item)
		}
		s.List = result
		return nil
	}

	// No matches found
	return fmt.Errorf("unrecognized type for devices spec: %v", string(b))
}

// MarshalJSON marshals ReplicatedDevices to its raw bytes representation
func (s *ReplicatedDevices) MarshalJSON() ([]byte, error) {
	if s.All == true {
		return json.Marshal("all")
	}
	if s.Count > 0 {
		return json.Marshal(s.Count)
	}
	if s.List != nil {
		return json.Marshal(s.List)
	}
	return nil, fmt.Errorf("unmarshallable ReplicatedDevices struct: %v", s)
}
