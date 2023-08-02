<<<<<<< HEAD
/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */
=======
/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

package info

import "strings"

// version must be set by go build's -X main.version= option in the Makefile.
var version = "unknown"

// gitCommit will be the hash that the binary was built from
// and will be populated by the Makefile
var gitCommit = ""

// GetVersionParts returns the different version components
func GetVersionParts() []string {
	v := []string{version}

	if gitCommit != "" {
		v = append(v, "commit: "+gitCommit)
	}

	return v
}

// GetVersionString returns the string representation of the version
func GetVersionString(more ...string) string {
	v := append(GetVersionParts(), more...)
	return strings.Join(v, "\n")
}
<<<<<<< HEAD

// GetVersion returns the version of the binary.
func GetVersion() string {
	return version
}
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
