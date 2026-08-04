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

package cdi

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
)

func TestImexChannelCDILib(t *testing.T) {
	channels := imex.Channels{
		{
			ID:       "0",
			Path:     "/dev/nvidia-caps-imex-channels/channel0",
			HostPath: "/dev/nvidia-caps-imex-channels/channel0",
		},
	}

	handler := &cdiHandler{
		vendor:       "nvidia.com",
		imexChannels: channels,
	}

	gen := handler.newImexChannelSpecGenerator()
	if gen == nil {
		t.Fatalf("expected non-nil SpecGenerator")
	}

	spec, err := gen.GetSpec()
	if err != nil {
		t.Fatalf("expected no error from GetSpec, got %v", err)
	}

	if spec == nil {
		t.Fatalf("expected non-nil spec")
	}

	rawSpec := spec.Raw()
	if rawSpec == nil {
		t.Fatalf("expected non-nil raw spec")
	}

	if rawSpec.Kind != "nvidia.com/imex-channel" {
		t.Errorf("expected Kind 'nvidia.com/imex-channel', got %s", rawSpec.Kind)
	}
	if len(rawSpec.Devices) != 1 {
		t.Fatalf("expected 1 device spec, got %d", len(rawSpec.Devices))
	}
	if rawSpec.Devices[0].Name != "0" {
		t.Errorf("expected device name '0', got %s", rawSpec.Devices[0].Name)
	}
}
