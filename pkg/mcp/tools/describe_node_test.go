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

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/mcp/client"
)

func TestParseDeviceString(t *testing.T) {
	// Format: "UUID,Index,Memory,Cores,Type,Numa,Health[,Count[,Mode]]"
	// Two devices separated by ":". Trailing colon allowed.
	in := "GPU-aaa,0,16384,100,A100,0,true,1,exclusive:GPU-bbb,1,8192,50,V100,1,false,1,timeslicing:"
	devs := parseDeviceString(in)
	if len(devs) != 2 {
		t.Fatalf("expected 2 devices, got %d (%+v)", len(devs), devs)
	}

	d0 := devs[0]
	if d0.ID != "GPU-aaa" || d0.Index != 0 || d0.Memory != 16384 || d0.Cores != 100 ||
		d0.Type != "A100" || d0.Numa != 0 || !d0.Health || d0.Mode != "exclusive" {
		t.Errorf("device 0 mismatch: %+v", d0)
	}

	d1 := devs[1]
	if d1.ID != "GPU-bbb" || d1.Index != 1 || d1.Memory != 8192 || d1.Cores != 50 ||
		d1.Type != "V100" || d1.Numa != 1 || d1.Health || d1.Mode != "timeslicing" {
		t.Errorf("device 1 mismatch: %+v", d1)
	}
}

func TestParseDeviceString_TooFewFields(t *testing.T) {
	if devs := parseDeviceString("only,three,fields"); len(devs) != 0 {
		t.Errorf("expected 0 devices for malformed entry, got %d", len(devs))
	}
}

func TestDescribeNodeTool_Handler(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "n1",
			Labels: map[string]string{
				"role": "worker",
			},
			Annotations: map[string]string{
				"hami.io/node-devices-to-register": "GPU-1,0,8192,50,T4,0,true,1,exclusive:",
			},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
				corev1.ResourceCPU:                    resource.MustParse("8"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
			},
		},
	}

	cs := fake.NewClientset(node)
	k8s := client.NewK8sClientFromInterface(cs)
	tool := NewDescribeNodeTool(k8s)

	if tool.Tool().Name != "describe_node" {
		t.Errorf("unexpected tool name: %s", tool.Tool().Name)
	}

	t.Run("missing node name returns error", func(t *testing.T) {
		res, _, err := tool.Handler()(context.Background(), nil, struct {
			Node string `json:"node"`
		}{})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !res.IsError {
			t.Errorf("expected error result for empty node")
		}
	})

	t.Run("non-existent node returns error", func(t *testing.T) {
		res, _, err := tool.Handler()(context.Background(), nil, struct {
			Node string `json:"node"`
		}{Node: "missing"})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !res.IsError {
			t.Errorf("expected error result for missing node")
		}
	})

	t.Run("returns description with GPU devices", func(t *testing.T) {
		res, _, err := tool.Handler()(context.Background(), nil, struct {
			Node string `json:"node"`
		}{Node: "n1"})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected error result: %s", extractText(res))
		}

		var desc NodeDescription
		if err := json.Unmarshal([]byte(extractText(res)), &desc); err != nil {
			t.Fatalf("invalid response JSON: %v", err)
		}
		if desc.Name != "n1" {
			t.Errorf("expected name n1, got %s", desc.Name)
		}
		if desc.Labels["role"] != "worker" {
			t.Errorf("expected role label, got %v", desc.Labels)
		}
		if len(desc.GPUDevices) != 1 || desc.GPUDevices[0].ID != "GPU-1" {
			t.Errorf("expected 1 GPU device with ID GPU-1, got %+v", desc.GPUDevices)
		}
		if !strings.Contains(desc.Capacity["nvidia.com/gpu"], "1") {
			t.Errorf("expected capacity nvidia.com/gpu=1, got %v", desc.Capacity)
		}
	})
}
