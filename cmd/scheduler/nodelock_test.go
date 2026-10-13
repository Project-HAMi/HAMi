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

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

// HAMI_NODELOCK_EXPIRE is read at package init, so each case runs in a child
// process that starts with it set.
func TestNodeLockTimeout(t *testing.T) {
	if os.Getenv("HAMI_TEST_CHILD") == "1" {
		if flag := os.Getenv("HAMI_TEST_FLAG"); flag != "" {
			if err := rootCmd.Flags().Parse([]string{flag}); err != nil {
				t.Fatal(err)
			}
		}
		if got, want := config.NodeLockTimeout.String(), os.Getenv("HAMI_TEST_WANT"); got != want {
			t.Fatalf("node lock timeout = %s, want %s", got, want)
		}
		return
	}
	tests := []struct {
		name, env, flag, want string
	}{
		{"env sets the default", "10m", "", "10m0s"},
		{"explicit flag wins over env", "10m", "--node-lock-timeout=2m", "2m0s"},
		{"invalid env keeps 5m", "abc", "", "5m0s"},
		{"zero env keeps 5m", "0s", "", "5m0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestNodeLockTimeout$")
			cmd.Env = append(os.Environ(), "HAMI_TEST_CHILD=1", "HAMI_NODELOCK_EXPIRE="+tt.env,
				"HAMI_TEST_FLAG="+tt.flag, "HAMI_TEST_WANT="+tt.want)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		})
	}
}
