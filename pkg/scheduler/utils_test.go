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

package scheduler

import (
	"os"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Mock environment variables for testing.
func mockEnvVars(vars map[string]string) func() {
	originalEnv := os.Environ()
	os.Clearenv()
	for k, v := range vars {
		os.Setenv(k, v)
	}
	return func() {
		os.Clearenv()
		for _, e := range originalEnv {
			pair := strings.SplitN(e, "=", 2)
			if len(pair) == 2 {
				os.Setenv(pair[0], pair[1])
			}
		}
	}
}

func Test_getNodeSelectorFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv map[string]string // Environment variables to set up for each test
		want     map[string]string
	}{
		{
			name: "No NODE_SELECTOR_ env vars",
			setupEnv: map[string]string{
				"UNRELATED_ENV_VAR": "value",
			},
			want: map[string]string{},
		},
		{
			name: "Single NODE_SELECTOR_ env var",
			setupEnv: map[string]string{
				"NODE_SELECTOR_REGION": "us-west-2",
			},
			want: map[string]string{
				"region": "us-west-2",
			},
		},
		{
			name: "Multiple NODE_SELECTOR_ env vars",
			setupEnv: map[string]string{
				"NODE_SELECTOR_REGION":   "us-west-2",
				"NODE_SELECTOR_INSTANCE": "large",
				"UNRELATED_ENV_VAR":      "value",
			},
			want: map[string]string{
				"region":   "us-west-2",
				"instance": "large",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variables and defer their cleanup
			cleanup := mockEnvVars(tt.setupEnv)
			defer cleanup()

			if got := getNodeSelectorFromEnv(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getNodeSelectorFromEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_filterNodesBySelector(t *testing.T) {
	// Create mock nodes to use in tests
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					"region": "us-west-2",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					"region": "eu-central-1",
				},
			},
		},
	}

	tests := []struct {
		name         string
		nodes        []*corev1.Node
		nodeSelector map[string]string
		wantFiltered []*corev1.Node
	}{
		{
			name:         "Filter by region",
			nodes:        nodes,
			nodeSelector: map[string]string{"region": "us-west-2"},
			wantFiltered: []*corev1.Node{nodes[0]},
		},
		{
			name:         "No matching nodes",
			nodes:        nodes,
			nodeSelector: map[string]string{"instance-type": "large"},
			wantFiltered: nil,
		},
		{
			name:         "Empty node selector",
			nodes:        nodes,
			nodeSelector: map[string]string{},
			wantFiltered: nodes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filterNodesBySelector(tt.nodes, tt.nodeSelector); !reflect.DeepEqual(got, tt.wantFiltered) {
				t.Errorf("filterNodesBySelector() = %v, want %v", got, tt.wantFiltered)
			}
		})
	}
}
