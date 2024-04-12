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
	"regexp"
	"strings"

	k8s "k8s.io/apimachinery/pkg/api/validation"
)

// ResourcePattern is used to match a resource name to a specific pattern
type ResourcePattern string

// ResourceName represents a valid resource name in Kubernetes
type ResourceName string

// Resource pairs a pattern matcher with a resource name.
type Resource struct {
	Pattern ResourcePattern `json:"pattern" yaml:"pattern"`
	Name    ResourceName    `json:"name"    yaml:"name"`
}

// Resources lists full GPUs and MIG devices separately.
type Resources struct {
	GPUs []Resource `json:"gpus"           yaml:"gpus"`
	MIGs []Resource `json:"mig,omitempty"  yaml:"mig,omitempty"`
}

// NewResourceName builds a resource name from the standard prefix and a name.
// An error is returned if the format is incorrect.
func NewResourceName(n string) (ResourceName, error) {
	if !strings.HasPrefix(n, ResourceNamePrefix+"/") {
		n = ResourceNamePrefix + "/" + n
	}

	if len(n) > MaxResourceNameLength {
		return "", fmt.Errorf("fully-qualified resource name must be %v characters or less: %v", MaxResourceNameLength, n)
	}

	_, name := ResourceName(n).Split()
	invalid := k8s.NameIsDNSSubdomain(name, false)
	if len(invalid) != 0 {
		return "", fmt.Errorf("incorrect format for resource name '%v': %v", n, invalid)
	}

	return ResourceName(n), nil
}

// NewResource builds a resource from a name and pattern
func NewResource(pattern, name string) (*Resource, error) {
	resourceName, err := NewResourceName(name)
	if err != nil {
		return nil, fmt.Errorf("invalid resource name: %v", err)
	}
	r := &Resource{
		Pattern: ResourcePattern(pattern),
		Name:    resourceName,
	}
	return r, nil
}

// Split splits a full resource name into prefix and name
func (r ResourceName) Split() (string, string) {
	split := strings.SplitN(string(r), "/", 2)
	if len(split) != 2 {
		return "", string(r)
	}
	return split[0], split[1]
}

// DefaultSharedRename returns the default renaming to apply when this resource is shared
func (r ResourceName) DefaultSharedRename() ResourceName {
	return r + DefaultSharedResourceNameSuffix
}

// UnmarshalJSON unmarshals raw bytes into a 'Resource' struct.
func (r *Resource) UnmarshalJSON(b []byte) error {
	res := make(map[string]json.RawMessage)
	err := json.Unmarshal(b, &res)
	if err != nil {
		return err
	}

	// Verify both fields set in the resource JSON
	pattern, patternExists := res["pattern"]
	name, nameExists := res["name"]
	if !patternExists {
		return fmt.Errorf("resources must have a 'pattern' field set")
	}
	if !nameExists {
		return fmt.Errorf("resources must have a 'name' field set")
	}

	// Set r.Pattern from the resource JSON
	err = json.Unmarshal(pattern, &r.Pattern)
	if err != nil {
		return err
	}

	// Set r.Name from the resource JSON
	err = json.Unmarshal(name, &r.Name)
	if err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON unmarshals raw bytes into a 'ResourceName' type.
func (r *ResourceName) UnmarshalJSON(b []byte) error {
	var raw string
	err := json.Unmarshal(b, &raw)
	if err != nil {
		return err
	}

	*r, err = NewResourceName(raw)
	if err != nil {
		return err
	}

	return nil
}

// AddGPUResource adds a GPU resource to the list of GPU resources.
func (r *Resources) AddGPUResource(pattern, name string) error {
	resource, err := NewResource(pattern, name)
	if err != nil {
		return err
	}
	r.GPUs = append(r.GPUs, *resource)
	return nil
}

// AddMIGResource adds a MIG resource to the list of MIG resources.
func (r *Resources) AddMIGResource(pattern, name string) error {
	resource, err := NewResource(pattern, name)
	if err != nil {
		return err
	}
	r.MIGs = append(r.MIGs, *resource)
	return nil
}

// Matches checks if the provided string matches the ResourcePattern or not.
func (p ResourcePattern) Matches(s string) bool {
	result, _ := regexp.MatchString(wildCardToRegexp(string(p)), s)
	return result
}

// wildCardToRegexp converts a wildcard pattern to a regular expression pattern.
func wildCardToRegexp(pattern string) string {
	var result strings.Builder
	for i, literal := range strings.Split(pattern, "*") {
		// Replace * with .*
		if i > 0 {
			result.WriteString(".*")
		}
		// Quote any regular expression meta characters in the literal text.
		result.WriteString(regexp.QuoteMeta(literal))
	}
	return result.String()
}
