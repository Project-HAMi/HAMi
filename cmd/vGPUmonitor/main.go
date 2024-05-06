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
	"k8s.io/klog/v2"
)

//var addr = flag.String("listen-address", ":9394", "The address to listen on for HTTP requests.")

//const shared_directory = "/usr/local/vgpu/shared"

func main() {

	if err := ValidateEnvVars(); err != nil {
		klog.Fatalf("Failed to validate environment variables: %v", err)
	}
	cgroupDriver = 0
	errchannel := make(chan error)
	go serveInfo(errchannel)
	go initMetrics()
	go watchAndFeedback()
	for {
		err := <-errchannel
		klog.Errorf("failed to serve: %v", err)
	}
}
