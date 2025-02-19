/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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
	rt "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource/testing"
)

func TestGPUResourceLabeler(t *testing.T) {
	device := rt.NewFullGPU()

	testCases := []struct {
		description    string
		count          int
		sharing        spec.Sharing
		expectedLabels Labels
	}{
		{
			description: "zero count returns empty",
		},
		{
			description: "no sharing",
			count:       1,
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
			},
		},
		{
			description: "time-slicing ignores non-matching resource",
			count:       1,
			sharing: spec.Sharing{
				TimeSlicing: spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/not-gpu",
							Replicas: 2,
						},
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
			},
		},
		{
			description: "time-slicing appends suffix and doubles count",
			count:       1,
			sharing: spec.Sharing{
				TimeSlicing: spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "time-slicing",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-SHARED",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
			},
		},
		{
			description: "time-slicing renamed does not append suffix and doubles count",
			count:       1,
			sharing: spec.Sharing{
				TimeSlicing: spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Rename:   "nvidia.com/gpu.shared",
							Replicas: 2,
						},
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "time-slicing",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
			},
		},
		{
			description: "mps ignores non-matching resource",
			count:       1,
			sharing: spec.Sharing{
				MPS: &spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/not-gpu",
							Replicas: 2,
						},
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
			},
		},
		{
			description: "mps appends suffix and doubles count",
			count:       1,
			sharing: spec.Sharing{
				MPS: &spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Replicas: 2,
						},
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "mps",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-SHARED",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
			},
		},
		{
			description: "mps renamed does not append suffix and doubles count",
			count:       1,
			sharing: spec.Sharing{
				MPS: &spec.ReplicatedResources{
					Resources: []spec.ReplicatedResource{
						{
							Name:     "nvidia.com/gpu",
							Rename:   "nvidia.com/gpu.shared",
							Replicas: 2,
						},
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "mps",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL",
				"nvidia.com/gpu.family":           "ampere",
				"nvidia.com/gpu.compute.major":    "8",
				"nvidia.com/gpu.compute.minor":    "0",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			config := &spec.Config{
				Sharing: tc.sharing,
			}
			l, err := NewGPUResourceLabeler(config, device, tc.count)
			require.NoError(t, err)

			labels, err := l.Labels()
			require.NoError(t, err)

			require.EqualValues(t, tc.expectedLabels, labels)
		})
	}

}

func TestSanitise(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{
			input:    "a space separated string",
			expected: "a-space-separated-string",
		},
		{
			input:    "some(thing)else",
			expected: "somethingelse",
		},
		{
			input:    "some ( thing )else",
			expected: "some-thing-else",
		},
		{
			input:    "NVIDIA-TITAN-X-(Pascal)",
			expected: "NVIDIA-TITAN-X-Pascal",
		},
		{
			input:    " input  with multiple   spaces   ",
			expected: "input-with-multiple-spaces",
		},
		{
			input:    "some [ / thing / ]else",
			expected: "some-thing-else",
		},
		{
			input:    "some / thing /else",
			expected: "some-thing-else",
		},
		{
			input:    "some-thing.else_new",
			expected: "some-thing.else_new",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			require.EqualValues(t, tc.expected, sanitise(tc.input))
		})
	}

}

func TestMigResourceLabeler(t *testing.T) {

	device := rt.NewMigDevice(1, 2, 300)
	rt.NewMigEnabledDevice(device)

	testCases := []struct {
		description    string
		resourceName   spec.ResourceName
		count          int
		timeSlicing    spec.ReplicatedResources
		expectedLabels Labels
	}{
		{
			description: "zero count returns empty",
		},
		{
			description:  "no sharing",
			resourceName: "nvidia.com/gpu",
			count:        1,
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "1",
				"nvidia.com/gpu.sharing-strategy": "none",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-1g.300gb",
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
			description:  "shared appends suffix and doubles count",
			resourceName: "nvidia.com/gpu",
			count:        1,
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
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "time-slicing",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-1g.300gb-SHARED",
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
			description:  "renamed does not append suffix and doubles count",
			resourceName: "nvidia.com/gpu",
			count:        1,
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Rename:   "nvidia.com/gpu.shared",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/gpu.count":            "1",
				"nvidia.com/gpu.replicas":         "2",
				"nvidia.com/gpu.sharing-strategy": "time-slicing",
				"nvidia.com/gpu.memory":           "300",
				"nvidia.com/gpu.product":          "MOCKMODEL-MIG-1g.300gb",
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
			description:  "mig mixed appends shared",
			resourceName: "nvidia.com/mig-1g.1gb",
			count:        1,
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/gpu",
						Rename:   "nvidia.com/gpu.shared",
						Replicas: 2,
					},
					{
						Name:     "nvidia.com/mig-1g.1gb",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/mig-1g.1gb.count":            "1",
				"nvidia.com/mig-1g.1gb.replicas":         "2",
				"nvidia.com/mig-1g.1gb.sharing-strategy": "time-slicing",
				"nvidia.com/mig-1g.1gb.memory":           "300",
				"nvidia.com/mig-1g.1gb.product":          "MOCKMODEL-MIG-1g.300gb-SHARED",
				"nvidia.com/mig-1g.1gb.multiprocessors":  "0",
				"nvidia.com/mig-1g.1gb.slices.gi":        "1",
				"nvidia.com/mig-1g.1gb.slices.ci":        "2",
				"nvidia.com/mig-1g.1gb.engines.copy":     "0",
				"nvidia.com/mig-1g.1gb.engines.decoder":  "0",
				"nvidia.com/mig-1g.1gb.engines.encoder":  "0",
				"nvidia.com/mig-1g.1gb.engines.jpeg":     "0",
				"nvidia.com/mig-1g.1gb.engines.ofa":      "0",
			},
		},
		{
			description:  "mig mixed rename does not append",
			resourceName: "nvidia.com/mig-1g.1gb",
			count:        1,
			timeSlicing: spec.ReplicatedResources{
				Resources: []spec.ReplicatedResource{
					{
						Name:     "nvidia.com/mig-1g.1gb",
						Rename:   "nvidia.com/mig-1g.1gb.shared",
						Replicas: 2,
					},
				},
			},
			expectedLabels: Labels{
				"nvidia.com/mig-1g.1gb.count":            "1",
				"nvidia.com/mig-1g.1gb.replicas":         "2",
				"nvidia.com/mig-1g.1gb.sharing-strategy": "time-slicing",
				"nvidia.com/mig-1g.1gb.memory":           "300",
				"nvidia.com/mig-1g.1gb.product":          "MOCKMODEL-MIG-1g.300gb",
				"nvidia.com/mig-1g.1gb.multiprocessors":  "0",
				"nvidia.com/mig-1g.1gb.slices.gi":        "1",
				"nvidia.com/mig-1g.1gb.slices.ci":        "2",
				"nvidia.com/mig-1g.1gb.engines.copy":     "0",
				"nvidia.com/mig-1g.1gb.engines.decoder":  "0",
				"nvidia.com/mig-1g.1gb.engines.encoder":  "0",
				"nvidia.com/mig-1g.1gb.engines.jpeg":     "0",
				"nvidia.com/mig-1g.1gb.engines.ofa":      "0",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			config := &spec.Config{
				Sharing: spec.Sharing{
					TimeSlicing: tc.timeSlicing,
				},
			}
			l, err := NewMIGResourceLabeler(tc.resourceName, config, device, tc.count)
			require.NoError(t, err)

			labels, err := l.Labels()
			require.NoError(t, err)

			require.EqualValues(t, tc.expectedLabels, labels)
		})
	}
}
