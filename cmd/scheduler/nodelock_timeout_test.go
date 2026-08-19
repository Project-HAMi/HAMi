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

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

// TestNodeLockTimeoutFlagDefault guards the precedence of flag, then
// environment, then default. nodelock's init applies HAMI_NODELOCK_EXPIRE
// before this package's init registers flags, so the flag default must be
// nodelock.NodeLockTimeout. Hard-coding a duration here instead would discard
// the environment value on every start, which is the bug in #2692.
func TestNodeLockTimeoutFlagDefault(t *testing.T) {
	f := rootCmd.Flags().Lookup("node-lock-timeout")
	if f == nil {
		t.Fatal("node-lock-timeout flag is not registered")
	}
	if want := nodelock.NodeLockTimeout.String(); f.DefValue != want {
		t.Errorf("node-lock-timeout default = %s, want %s (nodelock.NodeLockTimeout)", f.DefValue, want)
	}
	if config.NodeLockTimeout != nodelock.NodeLockTimeout {
		t.Errorf("config.NodeLockTimeout = %v, want %v", config.NodeLockTimeout, nodelock.NodeLockTimeout)
	}
}
