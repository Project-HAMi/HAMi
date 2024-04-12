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

package spec

import (
	"fmt"
	"os"

	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/transform"
	"tags.cncf.io/container-device-interface/pkg/cdi"
	"tags.cncf.io/container-device-interface/pkg/parser"
	"tags.cncf.io/container-device-interface/specs-go"
)

type builder struct {
	raw         *specs.Spec
	version     string
	vendor      string
	class       string
	deviceSpecs []specs.Device
	edits       specs.ContainerEdits
	format      string

	mergedDeviceOptions []transform.MergedDeviceOption
	noSimplify          bool
	permissions         os.FileMode
}

// newBuilder creates a new spec builder with the supplied options
func newBuilder(opts ...Option) *builder {
	s := &builder{}
	for _, opt := range opts {
		opt(s)
	}
	if s.raw != nil {
		s.noSimplify = true
		vendor, class := parser.ParseQualifier(s.raw.Kind)
		s.vendor = vendor
		s.class = class
	}

	if s.version == "" {
		s.version = DetectMinimumVersion
	}
	if s.vendor == "" {
		s.vendor = "nvidia.com"
	}
	if s.class == "" {
		s.class = "gpu"
	}
	if s.format == "" {
		s.format = FormatYAML
	}
	if s.permissions == 0 {
		s.permissions = 0600
	}
	return s
}

// Build builds a CDI spec form the spec builder.
func (o *builder) Build() (*spec, error) {
	raw := o.raw
	if raw == nil {
		raw = &specs.Spec{
			Version:        o.version,
			Kind:           fmt.Sprintf("%s/%s", o.vendor, o.class),
			Devices:        o.deviceSpecs,
			ContainerEdits: o.edits,
		}
	}

	if raw.Version == DetectMinimumVersion {
		minVersion, err := cdi.MinimumRequiredVersion(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to get minimum required CDI spec version: %v", err)
		}
		raw.Version = minVersion
	}

	if !o.noSimplify {
		err := transform.NewSimplifier().Transform(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to simplify spec: %v", err)
		}
	}

	if len(o.mergedDeviceOptions) > 0 {
		merge, err := transform.NewMergedDevice(o.mergedDeviceOptions...)
		if err != nil {
			return nil, fmt.Errorf("failed to create merged device transformer: %v", err)
		}
		if err := merge.Transform(raw); err != nil {
			return nil, fmt.Errorf("failed to merge devices: %v", err)
		}
	}

	s := spec{
		Spec:        raw,
		format:      o.format,
		permissions: o.permissions,
	}

	return &s, nil
}

// Option defines a function that can be used to configure the spec builder.
type Option func(*builder)

// WithDeviceSpecs sets the device specs for the spec builder
func WithDeviceSpecs(deviceSpecs []specs.Device) Option {
	return func(o *builder) {
		o.deviceSpecs = deviceSpecs
	}
}

// WithEdits sets the container edits for the spec builder
func WithEdits(edits specs.ContainerEdits) Option {
	return func(o *builder) {
		o.edits = edits
	}
}

// WithVersion sets the version for the spec builder
func WithVersion(version string) Option {
	return func(o *builder) {
		o.version = version
	}
}

// WithVendor sets the vendor for the spec builder
func WithVendor(vendor string) Option {
	return func(o *builder) {
		o.vendor = vendor
	}
}

// WithClass sets the class for the spec builder
func WithClass(class string) Option {
	return func(o *builder) {
		o.class = class
	}
}

// WithFormat sets the output file format
func WithFormat(format string) Option {
	return func(o *builder) {
		o.format = format
	}
}

// WithNoSimplify sets whether the spec must be simplified
func WithNoSimplify(noSimplify bool) Option {
	return func(o *builder) {
		o.noSimplify = noSimplify
	}
}

// WithRawSpec sets the raw spec for the spec builder
func WithRawSpec(raw *specs.Spec) Option {
	return func(o *builder) {
		o.raw = raw
	}
}

// WithPermissions sets the permissions for the generated spec file
func WithPermissions(permissions os.FileMode) Option {
	return func(o *builder) {
		o.permissions = permissions
	}
}

// WithMergedDeviceOptions sets the options for generating a merged device.
func WithMergedDeviceOptions(opts ...transform.MergedDeviceOption) Option {
	return func(o *builder) {
		o.mergedDeviceOptions = opts
	}
}
