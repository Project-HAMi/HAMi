/*
Copyright 2024 The HAMi Authors.

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

package cdi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExternalHandlerQualifiedName(t *testing.T) {
	h := newExternalHandler("k8s.device-plugin.nvidia.com")
	require.Equal(t, "k8s.device-plugin.nvidia.com/gpu=0", h.QualifiedName("gpu", "0"))
	// CreateSpecFile is a no-op and AdditionalDevices is empty for externally
	// managed specs.
	require.NoError(t, h.CreateSpecFile())
	require.Empty(t, h.AdditionalDevices())
}

func TestExternalHandlerDefaultVendor(t *testing.T) {
	h := newExternalHandler("")
	require.Equal(t, DefaultVendor+"/gpu=GPU-abc", h.QualifiedName("gpu", "GPU-abc"))
}
