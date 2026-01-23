/*
 * Copyright (c) 2023, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cdi

import (
	"github.com/NVIDIA/go-nvlib/pkg/nvlib/info"

	"k8s.io/klog/v2"
)

// New is a factory method that creates a CDI handler for creating CDI specs.
func New(opts ...Option) (Interface, error) {
	infolib := info.New()

	hasNVML, _ := infolib.HasNvml()
	if !hasNVML {
		klog.Warning("No valid resources detected, creating a null CDI handler")
		return NewNullHandler(), nil
	}

	return newHandler(opts...)
}
