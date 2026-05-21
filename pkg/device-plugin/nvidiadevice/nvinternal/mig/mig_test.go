/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mig

import (
	"testing"
)

func TestGetMigCapabilityDevicePaths_noFile(t *testing.T) {
	result, err := GetMigCapabilityDevicePaths()
	if err != nil {
		t.Fatalf("GetMigCapabilityDevicePaths() unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("GetMigCapabilityDevicePaths() = %v, want nil on non-MIG machine", result)
	}
}
