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

package imex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

func TestGetChannels(t *testing.T) {
	t.Run("empty channels list", func(t *testing.T) {
		cfg := &spec.Config{
			Imex: spec.Imex{
				ChannelIDs: []int{},
				Required:   false,
			},
		}
		channels, err := GetChannels(cfg, "/tmp")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(channels) != 0 {
			t.Fatalf("expected 0 channels, got %d", len(channels))
		}
	})

	t.Run("missing channel not required", func(t *testing.T) {
		cfg := &spec.Config{
			Imex: spec.Imex{
				ChannelIDs: []int{999},
				Required:   false,
			},
		}
		channels, err := GetChannels(cfg, "/nonexistent-dev-root")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(channels) != 0 {
			t.Fatalf("expected 0 channels, got %d", len(channels))
		}
	})

	t.Run("missing channel required", func(t *testing.T) {
		cfg := &spec.Config{
			Imex: spec.Imex{
				ChannelIDs: []int{999},
				Required:   true,
			},
		}
		channels, err := GetChannels(cfg, "/nonexistent-dev-root")
		if err == nil {
			t.Fatalf("expected error for missing required channel, got nil")
		}
		if channels != nil {
			t.Fatalf("expected nil channels, got %v", channels)
		}
		if !strings.Contains(err.Error(), "requested IMEX channel channel999 does not exist") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})

	t.Run("channel path is a regular file instead of char device", func(t *testing.T) {
		tempDir := t.TempDir()
		devDir := filepath.Join(tempDir, "dev/nvidia-caps-imex-channels")
		if err := os.MkdirAll(devDir, 0755); err != nil {
			t.Fatalf("failed to create temp dev dir: %v", err)
		}

		filePath := filepath.Join(devDir, "channel1")
		if err := os.WriteFile(filePath, []byte("dummy"), 0644); err != nil {
			t.Fatalf("failed to write dummy file: %v", err)
		}

		cfg := &spec.Config{
			Imex: spec.Imex{
				ChannelIDs: []int{1},
				Required:   true,
			},
		}

		channels, err := GetChannels(cfg, tempDir)
		if err == nil {
			t.Fatalf("expected error for non-character device file, got nil")
		}
		if channels != nil {
			t.Fatalf("expected nil channels, got %v", channels)
		}
		if !strings.Contains(err.Error(), "is not a character device") {
			t.Fatalf("unexpected error message: %v", err)
		}
	})
}
