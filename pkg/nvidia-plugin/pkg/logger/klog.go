/**
# Copyright 2024 NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package logger

import "k8s.io/klog/v2"

type toKlog struct{}

// ToKlog allows the klog logger to be passed to functions where this is needed.
var ToKlog = &toKlog{}

// Warning forwards the arguments to the klog.Warning function.
func (l toKlog) Warning(args ...interface{}) {
	klog.Warning(args...)
}

// Warningf forwards the arguments to the klog.Warningf function.
func (l toKlog) Warningf(format string, args ...interface{}) {
	klog.Warningf(format, args...)
}
