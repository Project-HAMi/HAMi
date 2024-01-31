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

package device

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvlib/pkg/nvml"
)

const (
	// AttributeMediaExtensions holds the string representation for the media extension MIG profile attribute.
	AttributeMediaExtensions = "me"
)

// MigProfile represents a specific MIG profile.
// Examples include "1g.5gb", "2g.10gb", "1c.2g.10gb", or "1c.1g.5gb+me", etc.
type MigProfile interface {
	String() string
	GetInfo() MigProfileInfo
	Equals(other MigProfile) bool
	Matches(profile string) bool
}

// MigProfileInfo holds all info associated with a specific MIG profile
type MigProfileInfo struct {
	C              int
	G              int
	GB             int
	Attributes     []string
	GIProfileID    int
	CIProfileID    int
	CIEngProfileID int
}

var _ MigProfile = &MigProfileInfo{}

// NewProfile constructs a new Profile struct using info from the giProfiles and ciProfiles used to create it.
func (d *devicelib) NewMigProfile(giProfileID, ciProfileID, ciEngProfileID int, migMemorySizeMB, deviceMemorySizeBytes uint64) (MigProfile, error) {
	giSlices := 0
	switch giProfileID {
	case nvml.GPU_INSTANCE_PROFILE_1_SLICE,
		nvml.GPU_INSTANCE_PROFILE_1_SLICE_REV1,
		nvml.GPU_INSTANCE_PROFILE_1_SLICE_REV2:
		giSlices = 1
	case nvml.GPU_INSTANCE_PROFILE_2_SLICE,
		nvml.GPU_INSTANCE_PROFILE_2_SLICE_REV1:
		giSlices = 2
	case nvml.GPU_INSTANCE_PROFILE_3_SLICE:
		giSlices = 3
	case nvml.GPU_INSTANCE_PROFILE_4_SLICE:
		giSlices = 4
	case nvml.GPU_INSTANCE_PROFILE_6_SLICE:
		giSlices = 6
	case nvml.GPU_INSTANCE_PROFILE_7_SLICE:
		giSlices = 7
	case nvml.GPU_INSTANCE_PROFILE_8_SLICE:
		giSlices = 8
	default:
		return nil, fmt.Errorf("invalid GPU Instance Profile ID: %v", giProfileID)
	}

	ciSlices := 0
	switch ciProfileID {
	case nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE,
		nvml.COMPUTE_INSTANCE_PROFILE_1_SLICE_REV1:
		ciSlices = 1
	case nvml.COMPUTE_INSTANCE_PROFILE_2_SLICE:
		ciSlices = 2
	case nvml.COMPUTE_INSTANCE_PROFILE_3_SLICE:
		ciSlices = 3
	case nvml.COMPUTE_INSTANCE_PROFILE_4_SLICE:
		ciSlices = 4
	case nvml.COMPUTE_INSTANCE_PROFILE_6_SLICE:
		ciSlices = 6
	case nvml.COMPUTE_INSTANCE_PROFILE_7_SLICE:
		ciSlices = 7
	case nvml.COMPUTE_INSTANCE_PROFILE_8_SLICE:
		ciSlices = 8
	default:
		return nil, fmt.Errorf("invalid Compute Instance Profile ID: %v", ciProfileID)
	}

	var attrs []string
	switch giProfileID {
	case nvml.GPU_INSTANCE_PROFILE_1_SLICE_REV1,
		nvml.GPU_INSTANCE_PROFILE_2_SLICE_REV1:
		attrs = append(attrs, AttributeMediaExtensions)
	}

	p := &MigProfileInfo{
		C:              ciSlices,
		G:              giSlices,
		GB:             int(getMigMemorySizeGB(deviceMemorySizeBytes, migMemorySizeMB)),
		Attributes:     attrs,
		GIProfileID:    giProfileID,
		CIProfileID:    ciProfileID,
		CIEngProfileID: ciEngProfileID,
	}

	return p, nil
}

// AssertValidMigProfileFormat checks if the string is in the proper format to represent a MIG profile
func (d *devicelib) AssertValidMigProfileFormat(profile string) error {
	_, _, _, _, err := parseMigProfile(profile)
	return err
}

// ParseMigProfile converts a string representation of a MigProfile into an object
func (d *devicelib) ParseMigProfile(profile string) (MigProfile, error) {
	profiles, err := d.GetMigProfiles()
	if err != nil {
		return nil, fmt.Errorf("error getting list of possible MIG profiles: %v", err)
	}

	for _, p := range profiles {
		if p.Matches(profile) {
			return p, nil
		}
	}

	return nil, fmt.Errorf("unable to parse profile string into a valid profile")
}

// String returns the string representation of a Profile
func (p MigProfileInfo) String() string {
	var suffix string
	if len(p.Attributes) > 0 {
		suffix = "+" + strings.Join(p.Attributes, ",")
	}
	if p.C == p.G {
		return fmt.Sprintf("%dg.%dgb%s", p.G, p.GB, suffix)
	}
	return fmt.Sprintf("%dc.%dg.%dgb%s", p.C, p.G, p.GB, suffix)
}

// GetInfo returns detailed info about a Profile
func (p MigProfileInfo) GetInfo() MigProfileInfo {
	return p
}

// Equals checks if two Profiles are identical or not
func (p MigProfileInfo) Equals(other MigProfile) bool {
	o := other.GetInfo()
	if p.C != o.C {
		return false
	}
	if p.G != o.G {
		return false
	}
	if p.GB != o.GB {
		return false
	}
	if p.GIProfileID != o.GIProfileID {
		return false
	}
	if p.CIProfileID != o.CIProfileID {
		return false
	}
	if p.CIEngProfileID != o.CIEngProfileID {
		return false
	}
	return true
}

// Matches checks if a MigProfile matches the string passed in
func (p MigProfileInfo) Matches(profile string) bool {
	c, g, gb, attrs, err := parseMigProfile(profile)
	if err != nil {
		return false
	}
	if c != p.C {
		return false
	}
	if g != p.G {
		return false
	}
	if gb != p.GB {
		return false
	}
	if len(attrs) != len(p.Attributes) {
		return false
	}
	sort.Strings(attrs)
	sort.Strings(p.Attributes)
	for i, a := range p.Attributes {
		if a != attrs[i] {
			return false
		}
	}
	return true
}

func parseMigProfile(profile string) (int, int, int, []string, error) {
	// If we are handed the empty string, we cannot parse it
	if profile == "" {
		return -1, -1, -1, nil, fmt.Errorf("profile is the empty string")
	}

	// Split by + to separate out attributes
	split := strings.SplitN(profile, "+", 2)

	// Check to make sure the c, g, and gb values match
	c, g, gb, err := parseMigProfileFields(split[0])
	if err != nil {
		return -1, -1, -1, nil, fmt.Errorf("cannot parse fields of '%v': %v", profile, err)
	}

	// If we have no attributes we are done
	if len(split) == 1 {
		return c, g, gb, nil, nil
	}

	// Make sure we have the same set of attributes
	attrs, err := parseMigProfileAttributes(split[1])
	if err != nil {
		return -1, -1, -1, nil, fmt.Errorf("cannot parse attributes of '%v': %v", profile, err)
	}

	return c, g, gb, attrs, nil
}

func parseMigProfileField(s string, field string) (int, error) {
	if strings.TrimSpace(s) != s {
		return -1, fmt.Errorf("leading or trailing spaces on '%%d%s'", field)
	}

	if !strings.HasSuffix(s, field) {
		return -1, fmt.Errorf("missing '%s' from '%%d%s'", field, field)
	}

	v, err := strconv.Atoi(strings.TrimSuffix(s, field))
	if err != nil {
		return -1, fmt.Errorf("malformed number in '%%d%s'", field)
	}

	return v, nil
}

func parseMigProfileFields(s string) (int, int, int, error) {
	var err error
	var c, g, gb int

	split := strings.SplitN(s, ".", 3)
	if len(split) == 3 {
		c, err = parseMigProfileField(split[0], "c")
		if err != nil {
			return -1, -1, -1, err
		}
		g, err = parseMigProfileField(split[1], "g")
		if err != nil {
			return -1, -1, -1, err
		}
		gb, err = parseMigProfileField(split[2], "gb")
		if err != nil {
			return -1, -1, -1, err
		}
		return c, g, gb, err
	}
	if len(split) == 2 {
		g, err = parseMigProfileField(split[0], "g")
		if err != nil {
			return -1, -1, -1, err
		}
		gb, err = parseMigProfileField(split[1], "gb")
		if err != nil {
			return -1, -1, -1, err
		}
		return g, g, gb, nil
	}

	return -1, -1, -1, fmt.Errorf("parsed wrong number of fields, expected 2 or 3")
}

func parseMigProfileAttributes(s string) ([]string, error) {
	attr := strings.Split(s, ",")
	if len(attr) == 0 {
		return nil, fmt.Errorf("empty attribute list")
	}
	unique := make(map[string]int)
	for _, a := range attr {
		if unique[a] > 0 {
			return nil, fmt.Errorf("non unique attribute in list")
		}
		if a == "" {
			return nil, fmt.Errorf("empty attribute in list")
		}
		if strings.TrimSpace(a) != a {
			return nil, fmt.Errorf("leading or trailing spaces in attribute")
		}
		if a[0] >= '0' && a[0] <= '9' {
			return nil, fmt.Errorf("attribute begins with a number")
		}
		for _, c := range a {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return nil, fmt.Errorf("non alpha-numeric character or digit in attribute")
			}
		}
		unique[a]++
	}
	return attr, nil
}

func getMigMemorySizeGB(totalDeviceMemory, migMemorySizeMB uint64) uint64 {
	const fracDenominator = 8
	const oneMB = 1024 * 1024
	const oneGB = 1024 * 1024 * 1024
	fractionalGpuMem := (float64(migMemorySizeMB) * oneMB) / float64(totalDeviceMemory)
	fractionalGpuMem = math.Ceil(fractionalGpuMem*fracDenominator) / fracDenominator
	totalMemGB := float64((totalDeviceMemory + oneGB - 1) / oneGB)
	return uint64(math.Round(fractionalGpuMem * totalMemGB))
}
