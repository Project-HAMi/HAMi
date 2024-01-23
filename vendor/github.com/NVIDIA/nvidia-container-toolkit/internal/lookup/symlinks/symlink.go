/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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

package symlinks

import (
	"fmt"
	"os"
)

// Resolve returns the link target of the specified filename or the filename if it is not a link.
func Resolve(filename string) (string, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return filename, fmt.Errorf("failed to get file info: %v", info)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return filename, nil
	}

	return os.Readlink(filename)
}
