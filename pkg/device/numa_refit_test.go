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

package device

import (
	"encoding/json"
	"testing"

	"gotest.tools/v3/assert"
)

func TestNumaRefitRequestJSONRoundTrip(t *testing.T) {
	request := NumaRefitRequest{
		PodUID:             "8a7e2f31-1a67-4d7b-8f1e-3a2b1c4d5e6f",
		PodNamespace:       "default",
		PodName:            "numa-pod",
		NodeName:           "node-1",
		ContainerIndex:     1,
		ContainerName:      "main",
		DeviceType:         "NVIDIA",
		AllowedDeviceUUIDs: []string{"GPU-aaaa", "GPU-bbbb"},
	}

	raw, err := json.Marshal(request)
	assert.NilError(t, err)

	var decoded NumaRefitRequest
	assert.NilError(t, json.Unmarshal(raw, &decoded))
	assert.DeepEqual(t, decoded, request)
}

func TestNumaRefitRequestJSONFieldNames(t *testing.T) {
	raw, err := json.Marshal(NumaRefitRequest{ContainerName: "main"})
	assert.NilError(t, err)

	var fields map[string]any
	assert.NilError(t, json.Unmarshal(raw, &fields))
	for _, key := range []string{"podUID", "podNamespace", "podName", "nodeName", "containerIndex", "containerName", "deviceType", "allowedDeviceUUIDs"} {
		_, ok := fields[key]
		assert.Assert(t, ok, "missing wire field %q", key)
	}
	assert.Equal(t, fields["containerName"], "main")
}

func TestNumaRefitResponseCarriesEncodedContainerDevices(t *testing.T) {
	devices := ContainerDevices{
		{UUID: "GPU-aaaa", Type: "NVIDIA", Usedmem: 20000, Usedcores: 30},
	}
	response := NumaRefitResponse{
		Succeeded:        true,
		ContainerDevices: EncodeContainerDevices(devices),
	}

	raw, err := json.Marshal(response)
	assert.NilError(t, err)

	var decoded NumaRefitResponse
	assert.NilError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, decoded.Succeeded, true)

	roundTripped, err := DecodeContainerDevices(decoded.ContainerDevices)
	assert.NilError(t, err)
	// Idx is not part of the wire format, so it comes back as the sentinel
	// UnsetContainerDeviceIdx rather than whatever devices set it to (here, 0).
	want := ContainerDevices{
		{Idx: UnsetContainerDeviceIdx, UUID: "GPU-aaaa", Type: "NVIDIA", Usedmem: 20000, Usedcores: 30},
	}
	assert.DeepEqual(t, roundTripped, want)
}

func TestNumaRefitResponseFailureOmitsDevices(t *testing.T) {
	raw, err := json.Marshal(NumaRefitResponse{Succeeded: false, FailureReason: "no allowed device fits"})
	assert.NilError(t, err)

	var fields map[string]any
	assert.NilError(t, json.Unmarshal(raw, &fields))
	_, hasDevices := fields["containerDevices"]
	assert.Assert(t, !hasDevices)
	assert.Equal(t, fields["failureReason"], "no allowed device fits")
}
