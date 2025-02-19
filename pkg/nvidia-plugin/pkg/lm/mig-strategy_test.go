/**
# Copyright (c) 2021-2022, NVIDIA CORPORATION.  All rights reserved.
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

package lm

import (
	"testing"

	"github.com/stretchr/testify/require"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource"
	rt "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource/testing"
)

func TestMigStrategyNoneLabels(t *testing.T) {
	testCases := []struct {
		description    string
		devices        []resource.Device
		timeSlicing    spec.ReplicatedResources
		expectedError  bool
		expectedLabels Labels
	}{
		{
			description: "no devices returns empty labels",
		},
		{
			description: "single non-mig device returns non-mig (none) labels",
			devices: []resource.Device{
				rt.NewFullGPU(),
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
			},
		},
		{
			description: "sharing is applied to single device",
			devices: []resource.Device{
				rt.NewFullGPU(),
			},
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "time-slicing",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-SHARED",
			},
		},
		{
			description: "sharing is applied to multiple devices",
			devices: []resource.Device{
				rt.NewFullGPU(),
				rt.NewFullGPU(),
			},
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "2",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "time-slicing",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-SHARED",
			},
		},
		{
			description: "sharing is not applied to single MIG device; replicas is zero",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(),
			},
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "0",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
			},
		},
		{
			description: "sharing is not applied to multiple MIG device; replicas is zero",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(),
				rt.NewMigEnabledDevice(),
			},
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "2",
				"nvidia.com/gpu.replicas":         "0",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
			},
		},
		{
			description: "sharing is applied to MIG device and non-MIG device",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(),
				rt.NewFullGPU(),
			},
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "2",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "time-slicing",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-SHARED",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			nvmlMock := rt.NewManagerMockWithDevices(tc.devices...)

			config := spec.Config{
				Flags: spec.Flags{
					CommandLineFlags: spec.CommandLineFlags{
						MigStrategy: ptr(MigStrategyNone),
					},
				},
				Sharing: spec.Sharing{
					TimeSlicing: tc.timeSlicing,
				},
			}

			none, _ := NewResourceLabeler(nvmlMock, &config)

			labels, err := none.Labels()
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.EqualValues(t, tc.expectedLabels, labels)
		})
	}
}

func TestMigStrategySingleLabels(t *testing.T) {
	testCases := []struct {
		description    string
		devices        []resource.Device
		expectedError  bool
		expectedLabels Labels
		isInvalid      bool
	}{
		{
			description: "no devices returns empty labels",
		},
		{
			description: "single non-mig device returns non-mig (none) labels",
			devices: []resource.Device{
				rt.NewFullGPU(),
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
				"nvidia.com/mig.strategy":         "single",
			},
		},
		{
			description: "multiple non-mig device returns non-mig (none) labels",
			devices: []resource.Device{
				rt.NewFullGPU(),
				rt.NewFullGPU(),
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "2",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
				"nvidia.com/mig.strategy":         "single",
			},
		},
		{
			description: "single mig-enabled device returns mig labels",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(
					rt.NewMigDevice(1, 2, 100),
				),
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "100",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-1g.100gb",
				"nvidia.com/mig.strategy":         "single",
				"nvidia.com/gpu.multiprocessors":  "0",
				"nvidia.com/gpu.slices.gi":        "1",
				"nvidia.com/gpu.slices.ci":        "2",
				"nvidia.com/gpu.engines.copy":     "0",
				"nvidia.com/gpu.engines.decoder":  "0",
				"nvidia.com/gpu.engines.encoder":  "0",
				"nvidia.com/gpu.engines.jpeg":     "0",
				"nvidia.com/gpu.engines.ofa":      "0",
			},
		},
		{
			description: "multiple mig-enabled devices returns mig labels",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(
					rt.NewMigDevice(1, 2, 100, map[string]interface{}{
						"multiprocessors": 12,
						"engines.copy":    13,
						"engines.decoder": 14,
						"engines.encoder": 15,
						"engines.jpeg":    16,
						"engines.ofa":     17,
					}),
				),
				rt.NewMigEnabledDevice(
					rt.NewMigDevice(1, 2, 100, map[string]interface{}{
						"multiprocessors": 12,
						"engines.copy":    13,
						"engines.decoder": 14,
						"engines.encoder": 15,
						"engines.jpeg":    16,
						"engines.ofa":     17,
					}),
				),
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "2",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "100",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-1g.100gb",
				"nvidia.com/mig.strategy":         "single",
				"nvidia.com/gpu.multiprocessors":  "12",
				"nvidia.com/gpu.slices.gi":        "1",
				"nvidia.com/gpu.slices.ci":        "2",
				"nvidia.com/gpu.engines.copy":     "13",
				"nvidia.com/gpu.engines.decoder":  "14",
				"nvidia.com/gpu.engines.encoder":  "15",
				"nvidia.com/gpu.engines.jpeg":     "16",
				"nvidia.com/gpu.engines.ofa":      "17",
			},
		},
		{
			description: "empty mig devices returns MIG invalid label",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(),
			},
			isInvalid: true,
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "0",
				"nvidia.com/gpu.replicas":         "0",
				"nvidia.com/gpu.sharing-strategy": "",
				"nvidia.com/gpu.memory":           "0",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-INVALID",
				"nvidia.com/mig.strategy":         "single",
			},
		},
		{
			description: "mixed mig config returns MIG invalid label",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(
					rt.NewMigDevice(1, 2, 100),
					rt.NewMigDevice(3, 4, 100),
				),
			},
			isInvalid: true,
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "0",
				"nvidia.com/gpu.replicas":         "0",
				"nvidia.com/gpu.sharing-strategy": "",
				"nvidia.com/gpu.memory":           "0",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-INVALID",
				"nvidia.com/mig.strategy":         "single",
			},
		},
		{
			description: "mixed mig enabled and disabled returns invalid config",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(
					rt.NewMigDevice(1, 2, 100),
				),
				rt.NewFullGPU(),
			},
			isInvalid: true,
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "0",
				"nvidia.com/gpu.replicas":         "0",
				"nvidia.com/gpu.sharing-strategy": "",
				"nvidia.com/gpu.memory":           "0",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-INVALID",
				"nvidia.com/mig.strategy":         "single",
			},
		},
		{
			description: "enabled, disabled, and empty returns invalid config",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(
					rt.NewMigDevice(1, 2, 100),
				),
				rt.NewFullGPU(),
				rt.NewMigEnabledDevice(),
			},
			isInvalid: true,
			expectedLabels: Labels{
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.count":            "0",
				"nvidia.com/gpu.replicas":         "0",
				"nvidia.com/gpu.sharing-strategy": "",
				"nvidia.com/gpu.memory":           "0",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-INVALID",
				"nvidia.com/mig.strategy":         "single",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			nvmlMock := rt.NewManagerMockWithDevices(tc.devices...)

			config := spec.Config{
				Flags: spec.Flags{
					CommandLineFlags: spec.CommandLineFlags{
						MigStrategy: ptr(MigStrategySingle),
					},
				},
			}

			single, _ := NewResourceLabeler(nvmlMock, &config)

			labels, err := single.Labels()
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.EqualValues(t, tc.expectedLabels, labels)
		})
	}
}

// prt returns a reference to whatever type is passed into it
func ptr[T any](x T) *T {
	return &x
}
