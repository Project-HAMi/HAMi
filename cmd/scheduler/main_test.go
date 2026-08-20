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

package main

import (
	"testing"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

func TestResolveNodeLockTimeout(t *testing.T) {
	originalTimeout := nodelock.NodeLockTimeout
	originalConfigTimeout := config.NodeLockTimeout
	defer func() {
		nodelock.NodeLockTimeout = originalTimeout
		config.NodeLockTimeout = originalConfigTimeout
	}()

	t.Run("neither flag nor env set - preserves default", func(t *testing.T) {
		nodelock.NodeLockTimeout = time.Minute * 5
		config.NodeLockTimeout = time.Minute * 5

		resolveNodeLockTimeout(false)

		if nodelock.NodeLockTimeout != time.Minute*5 {
			t.Errorf("expected %v, got %v", time.Minute*5, nodelock.NodeLockTimeout)
		}
	})

	t.Run("env set, flag not set - preserves env setting", func(t *testing.T) {
		// Simulate env var being parsed by nodelock package init
		envDuration := time.Minute * 10
		nodelock.NodeLockTimeout = envDuration
		// config.NodeLockTimeout still has flag default 5m
		config.NodeLockTimeout = time.Minute * 5

		resolveNodeLockTimeout(false)

		if nodelock.NodeLockTimeout != envDuration {
			t.Errorf("expected %v from env, got %v", envDuration, nodelock.NodeLockTimeout)
		}
	})

	t.Run("flag explicitly set - overrides with flag value", func(t *testing.T) {
		// Even if env set NodeLockTimeout to 10m
		nodelock.NodeLockTimeout = time.Minute * 10
		flagDuration := time.Minute * 2
		config.NodeLockTimeout = flagDuration

		resolveNodeLockTimeout(true)

		if nodelock.NodeLockTimeout != flagDuration {
			t.Errorf("expected %v from flag, got %v", flagDuration, nodelock.NodeLockTimeout)
		}
	})
}
