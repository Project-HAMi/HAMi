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

package metax

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/api"
)

func TestGetNodeDevices(t *testing.T) {
	MetaxResourceCount = "metax-tech.com/gpu"

	tests := []struct {
		name     string
		node     corev1.Node
		expected []*api.DeviceInfo
		err      error
	}{
		{
			name: "Test with valid node",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName(MetaxResourceCount): *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			},
			expected: []*api.DeviceInfo{
				{
					Index:   0,
					ID:      "test-metax-0",
					Count:   100,
					Devmem:  65536,
					Devcore: 100,
					Type:    MetaxGPUDevice,
					Numa:    0,
					Health:  true,
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &MetaxDevices{}
			got, err := dev.GetNodeDevices(tt.node)
			if (err != nil) != (tt.err != nil) {
				t.Errorf("GetNodeDevices() error = %v, expected %v", err, tt.err)
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("GetNodeDevices() got %d devices, expected %d", len(got), len(tt.expected))
				return
			}

			for i, device := range got {
				if device.Index != tt.expected[i].Index {
					t.Errorf("Expected index %d, got %d", tt.expected[i].Index, device.Index)
				}
				if device.ID != tt.expected[i].ID {
					t.Errorf("Expected id %s, got %s", tt.expected[i].ID, device.ID)
				}
				if device.Devcore != tt.expected[i].Devcore {
					t.Errorf("Expected devcore %d, got %d", tt.expected[i].Devcore, device.Devcore)
				}
				if device.Devmem != tt.expected[i].Devmem {
					t.Errorf("Expected cevmem %d, got %d", tt.expected[i].Devmem, device.Devmem)
				}
			}
		})
	}
}

func TestParseMetaxAnnos(t *testing.T) {
	tests := []struct {
		name  string
		index int
		value float32
	}{
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 1,
			value: 0,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 2,
			value: 110,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 3,
			value: 270,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 4,
			value: 540,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 5,
			value: 580,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 6,
			value: 730,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 7,
			value: 930,
		},
		{
			name:  "{\"1\":0,\"2\":110,\"3\":270,\"4\":540,\"5\":580,\"6\":730,\"7\":930,\"8\":1240}",
			index: 8,
			value: 1240,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := parseMetaxAnnos(tt.name, tt.index)
			if value != tt.value {
				t.Errorf("Expected index %f, got %f", tt.value, value)
			}
		})
	}
}
