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

package main

import (
	"os"
	"testing"
)

func TestValidateEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
	}{
		{
			name: "required var set",
			envVars: map[string]string{
				"HOOK_PATH": "/some/path",
			},
			wantErr: false,
		},
		{
			name: "required var set, optional var also set",
			envVars: map[string]string{
				"HOOK_PATH":     "/some/path",
				"OTHER_ENV_VAR": "value",
			},
			wantErr: false,
		},
		{
			name:    "required var missing",
			envVars: map[string]string{},
			wantErr: true,
		},
		{
			name: "required var empty string still counts as set",
			envVars: map[string]string{
				"HOOK_PATH": "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, envVar := range []string{"HOOK_PATH", "OTHER_ENV_VAR"} {
				os.Unsetenv(envVar)
			}
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			err := ValidateEnvVars()
			if tt.wantErr && err == nil {
				t.Errorf("ValidateEnvVars() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateEnvVars() unexpected error: %v", err)
			}
		})
	}
}
