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
	"os/exec"
	"testing"
)

// HAMI_NODELOCK_EXPIRE is read at package init, so the check runs in a child
// process that starts with it set.
func TestNodeLockTimeoutFlagDefaultsToEnv(t *testing.T) {
	if os.Getenv("HAMI_TEST_CHILD") == "1" {
		if got := rootCmd.Flags().Lookup("node-lock-timeout").DefValue; got != "10m0s" {
			t.Fatalf("node-lock-timeout default = %s, want 10m0s", got)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNodeLockTimeoutFlagDefaultsToEnv$")
	cmd.Env = append(os.Environ(), "HAMI_TEST_CHILD=1", "HAMI_NODELOCK_EXPIRE=10m")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
