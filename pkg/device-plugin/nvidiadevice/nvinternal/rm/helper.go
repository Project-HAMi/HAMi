/*
<<<<<<< HEAD
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
=======
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
<<<<<<< HEAD
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
=======
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 */

package rm

<<<<<<< HEAD
// uint8Slice wraps an []uint8 with more functions.
type uint8Slice []uint8

// String turns a nil terminated uint8Slice into a string
func (s uint8Slice) String() string {
=======
// int8Slice wraps an []int8 with more functions.
type int8Slice []int8

// String turns a nil terminated int8Slice into a string
func (s int8Slice) String() string {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	var b []byte
	for _, c := range s {
		if c == 0 {
			break
		}
<<<<<<< HEAD
		b = append(b, c)
	}
	return string(b)
}
=======
		b = append(b, byte(c))
	}
	return string(b)
}

// uintPtr returns a *uint from a uint32
func uintPtr(c uint32) *uint {
	i := uint(c)
	return &i
}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
