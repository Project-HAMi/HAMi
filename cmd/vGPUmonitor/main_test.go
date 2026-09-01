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
	"context"
	"net"
	"testing"
)

// initMetrics must fail when it cannot bind, rather than leaving the process
// running with nothing to scrape. It binds before touching the container
// lister, so a nil lister never gets dereferenced on this path.
func TestInitMetricsReturnsErrorWhenBindFails(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy a port for the test: %v", err)
	}
	defer taken.Close()

	original := metricsBindAddress
	metricsBindAddress = taken.Addr().String()
	defer func() { metricsBindAddress = original }()

	if err := initMetrics(context.Background(), nil); err == nil {
		t.Fatalf("expected an error when %s is already in use", metricsBindAddress)
	}
}
