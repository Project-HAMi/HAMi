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

package dashboards

import (
	"encoding/json"
	"os"
	"testing"
)

type dashboard struct {
	Panels []panel `json:"panels"`
}

type panel struct {
	Title       string `json:"title"`
	FieldConfig struct {
		Defaults struct {
			Min        *float64 `json:"min"`
			Max        *float64 `json:"max"`
			Unit       string   `json:"unit"`
			Thresholds struct {
				Steps []struct {
					Value *float64 `json:"value"`
				} `json:"steps"`
			} `json:"thresholds"`
		} `json:"defaults"`
	} `json:"fieldConfig"`
	Targets []struct {
		Expr string `json:"expr"`
	} `json:"targets"`
}

func TestNodeGPUMemoryAllocatedRatioPanelUsesFractionScale(t *testing.T) {
	data, err := os.ReadFile("hami-vgpu-dashboard.json")
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}

	var d dashboard
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("parse dashboard: %v", err)
	}

	const title = "Node GPU memory allocated ratio"
	var got *panel
	for i := range d.Panels {
		if d.Panels[i].Title == title {
			got = &d.Panels[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("panel %q not found", title)
	}

	if len(got.Targets) != 1 || got.Targets[0].Expr != `hami_node_gpu_memory_allocated_ratio{node=~"$node"}` {
		t.Fatalf("panel %q must query the node memory allocation ratio directly", title)
	}
	if got.FieldConfig.Defaults.Unit != "percentunit" {
		t.Errorf("unit = %q, want percentunit for a 0-1 ratio", got.FieldConfig.Defaults.Unit)
	}
	if got.FieldConfig.Defaults.Min == nil || *got.FieldConfig.Defaults.Min != 0 {
		t.Errorf("min = %v, want 0", got.FieldConfig.Defaults.Min)
	}
	if got.FieldConfig.Defaults.Max == nil || *got.FieldConfig.Defaults.Max != 1 {
		t.Errorf("max = %v, want 1", got.FieldConfig.Defaults.Max)
	}
	for _, step := range got.FieldConfig.Defaults.Thresholds.Steps {
		if step.Value != nil && (*step.Value < 0 || *step.Value > 1) {
			t.Errorf("threshold %v is outside the metric's 0-1 scale", *step.Value)
		}
	}
}
