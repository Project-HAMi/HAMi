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
	"github.com/NVIDIA/go-nvlib/pkg/nvml"
)

// Interface provides the API to the 'device' package
type Interface interface {
	AssertValidMigProfileFormat(profile string) error
	GetDevices() ([]Device, error)
	GetMigDevices() ([]MigDevice, error)
	GetMigProfiles() ([]MigProfile, error)
	NewDevice(d nvml.Device) (Device, error)
	NewDeviceByUUID(uuid string) (Device, error)
	NewMigDevice(d nvml.Device) (MigDevice, error)
	NewMigDeviceByUUID(uuid string) (MigDevice, error)
	NewMigProfile(giProfileID, ciProfileID, ciEngProfileID int, migMemorySizeMB, deviceMemorySizeBytes uint64) (MigProfile, error)
	ParseMigProfile(profile string) (MigProfile, error)
	VisitDevices(func(i int, d Device) error) error
	VisitMigDevices(func(i int, d Device, j int, m MigDevice) error) error
	VisitMigProfiles(func(p MigProfile) error) error
}

type devicelib struct {
	nvml           nvml.Interface
	skippedDevices map[string]struct{}
	verifySymbols  *bool
	migProfiles    []MigProfile
}

var _ Interface = &devicelib{}

// New creates a new instance of the 'device' interface
func New(opts ...Option) Interface {
	d := &devicelib{}
	for _, opt := range opts {
		opt(d)
	}
	if d.nvml == nil {
		d.nvml = nvml.New()
	}
	if d.verifySymbols == nil {
		verify := true
		d.verifySymbols = &verify
	}
	if d.skippedDevices == nil {
		WithSkippedDevices(
			"DGX Display",
			"NVIDIA DGX Display",
		)(d)
	}
	return d
}

// WithNvml provides an Option to set the NVML library used by the 'device' interface
func WithNvml(nvml nvml.Interface) Option {
	return func(d *devicelib) {
		d.nvml = nvml
	}
}

// WithVerifySymbols provides an option to toggle whether to verify select symbols exist in dynamic libraries before calling them
func WithVerifySymbols(verify bool) Option {
	return func(d *devicelib) {
		d.verifySymbols = &verify
	}
}

// WithSkippedDevices provides an Option to set devices to be skipped by model name
func WithSkippedDevices(names ...string) Option {
	return func(d *devicelib) {
		if d.skippedDevices == nil {
			d.skippedDevices = make(map[string]struct{})
		}
		for _, name := range names {
			d.skippedDevices[name] = struct{}{}
		}
	}
}

// Option defines a function for passing options to the New() call
type Option func(*devicelib)
