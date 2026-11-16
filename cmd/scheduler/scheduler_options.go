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
	"fmt"

	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

func validateSchedulerOptions() error {
	if err := config.ValidateNodeAffinityScoreWeight(config.NodeAffinityScoreWeight); err != nil {
		return err
	}
	return validateDevicePluginIdentity(config.DevicePluginNamespace, config.DevicePluginServiceAccount)
}

func validateDevicePluginIdentity(namespace, serviceAccount string) error {
	if namespace == "" || serviceAccount == "" {
		return fmt.Errorf(
			"--device-plugin-namespace and --device-plugin-service-account must both be set; "+
				"they identify the ServiceAccount whose token is accepted on the /refit endpoint "+
				"(see issue #2878). Got namespace=%q service-account=%q",
			namespace, serviceAccount,
		)
	}
	return nil
}

func warnExtenderAddress(address string) {
	if !isLoopbackAddr(address) {
		klog.Warningf("--extender-bind=%s is not a loopback address: /filter and /bind authenticate no caller, "+
			"so anyone able to reach it can drop a pod's device reservation or have its annotations patched", address)
	}
}

func warnProfilingAddress(address string) {
	if !isLoopbackAddr(address) {
		klog.Warningf("--profiling-bind-address=%s is not a loopback address: pprof authenticates no caller and exposes process diagnostics", address)
	}
}
