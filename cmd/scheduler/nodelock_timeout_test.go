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

func TestApplyNodeLockTimeout(t *testing.T) {
	const defaultTimeout = 5 * time.Minute

	tests := []struct {
		name    string
		flagSet bool
		flag    time.Duration
		env     time.Duration
		want    time.Duration
	}{
		{name: "flag unset keeps env value", flagSet: false, flag: defaultTimeout, env: 5 * time.Second, want: 5 * time.Second},
		{name: "flag set overrides env", flagSet: true, flag: 5 * time.Second, env: defaultTimeout, want: 5 * time.Second},
		{name: "neither set keeps default", flagSet: false, flag: defaultTimeout, env: defaultTimeout, want: defaultTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origFlag, origEnv := config.NodeLockTimeout, nodelock.NodeLockTimeout
			t.Cleanup(func() {
				config.NodeLockTimeout, nodelock.NodeLockTimeout = origFlag, origEnv
			})
			config.NodeLockTimeout, nodelock.NodeLockTimeout = tc.flag, tc.env

			applyNodeLockTimeout(tc.flagSet)

			if nodelock.NodeLockTimeout != tc.want {
				t.Errorf("nodelock.NodeLockTimeout = %v, want %v", nodelock.NodeLockTimeout, tc.want)
			}
			if config.NodeLockTimeout != tc.want {
				t.Errorf("config.NodeLockTimeout = %v, want %v", config.NodeLockTimeout, tc.want)
			}
		})
	}
}
