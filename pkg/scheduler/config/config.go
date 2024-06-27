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

package config

import "github.com/Project-HAMi/HAMi/pkg/scheduler/policy"

var (
	HTTPBind           string
	SchedulerName      string
	DefaultMem         int32
	DefaultCores       int32
	DefaultResourceNum int32
	MetricsBindAddress string
	Debug              bool

	// NodeSchedulerPolicy is config this scheduler node to use `binpack` or `spread`. default value is binpack.
	NodeSchedulerPolicy = policy.NodeSchedulerPolicyBinpack.String()
	// GPUSchedulerPolicy is config this scheduler GPU to use `binpack` or `spread`. default value is spread.
	GPUSchedulerPolicy = policy.GPUSchedulerPolicySpread.String()
)
