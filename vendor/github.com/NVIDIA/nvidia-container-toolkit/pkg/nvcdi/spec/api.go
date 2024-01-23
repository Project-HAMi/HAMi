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
	"io"

	"tags.cncf.io/container-device-interface/specs-go"
)

const (
	// DetectMinimumVersion is a constant that triggers a spec to detect the minimum required version.
	DetectMinimumVersion = "DETECT_MINIMUM_VERSION"

	// FormatJSON indicates a JSON output format
	FormatJSON = "json"
	// FormatYAML indicates a YAML output format
	FormatYAML = "yaml"
)

// Interface is the interface for the spec API
type Interface interface {
	io.WriterTo
	Save(string) error
	Raw() *specs.Spec
}
