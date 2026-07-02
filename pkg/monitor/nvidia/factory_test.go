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

package nvidia

import "testing"

func TestFindFactory_RoutesByVersion(t *testing.T) {
	ensureBuiltinsRegistered()

	tests := []struct {
		name     string
		header   HeaderT
		size     int64
		expected string
	}{
		{
			name:     "v0 matches by known cache size",
			header:   HeaderT{MajorVersion: 0, MinorVersion: 0},
			size:     1197897,
			expected: "v0",
		},
		{
			name:     "v1 base matches major 1 minor <= 1",
			header:   HeaderT{MajorVersion: 1, MinorVersion: 1},
			size:     1024,
			expected: "v1",
		},
		{
			name:     "v1 sem matches major 1 minor >= 2",
			header:   HeaderT{MajorVersion: 1, MinorVersion: 2},
			size:     1024,
			expected: "v1-sem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := findFactory(&tt.header, tt.size)
			if factory == nil {
				t.Fatalf("expected factory %q, got nil", tt.expected)
			}
			if factory.Name() != tt.expected {
				t.Fatalf("expected factory %q, got %q", tt.expected, factory.Name())
			}
		})
	}
}

func TestFindFactory_UnknownVersionReturnsNil(t *testing.T) {
	ensureBuiltinsRegistered()

	factory := findFactory(&HeaderT{MajorVersion: 9, MinorVersion: 9}, 1)
	if factory != nil {
		t.Fatalf("expected nil factory for unknown version, got %q", factory.Name())
	}
}
