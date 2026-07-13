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

package rm

import (
	"fmt"
	"testing"
	"time"
)

// checkHealth returns a nil error whenever health checks are disabled (for
// example DP_DISABLE_HEALTHCHECKS=all). CheckHealth must not dereference that
// nil error.
func TestCheckHealthNilErrorDoesNotPanic(t *testing.T) {
	t.Setenv(envDisableHealthChecks, "all")

	r := &nvmlResourceManager{}
	stop := make(chan interface{})
	unhealthy := make(chan *Device, 1)
	disableNVML := make(chan bool, 1)
	ack := make(chan bool, 1)

	done := make(chan error, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- fmt.Errorf("CheckHealth panicked: %v", p)
			}
		}()
		done <- r.CheckHealth(stop, unhealthy, disableNVML, ack)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CheckHealth returned an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CheckHealth did not return")
	}
}
