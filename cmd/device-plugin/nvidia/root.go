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

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type root string

func (r root) join(parts ...string) string {
	return filepath.Join(append([]string{string(r)}, parts...)...)
}

// getDevRoot returns the dev root associated with the root.
// If the root is not a dev root, this defaults to "/".
func (r root) getDevRoot() string {
	if r.isDevRoot() {
		return string(r)
	}
	return "/"
}

// isDevRoot checks whether the specified root is a dev root.
// A dev root is defined as a root containing a /dev folder.
func (r root) isDevRoot() bool {
	stat, err := os.Stat(filepath.Join(string(r), "dev"))
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func (r root) tryResolveLibrary(libraryName string) string {
	if r == "" || r == "/" {
		return libraryName
	}

	librarySearchPaths := []string{
		"/usr/lib64",
		"/usr/lib/x86_64-linux-gnu",
		"/usr/lib/aarch64-linux-gnu",
		"/lib64",
		"/lib/x86_64-linux-gnu",
		"/lib/aarch64-linux-gnu",
	}

	for _, d := range librarySearchPaths {
		l := r.join(d, libraryName)
		resolved, err := resolveLink(l)
		if err != nil {
			continue
		}
		return resolved
	}

	return libraryName
}

// resolveLink finds the target of a symlink or the file itself in the
// case of a regular file.
// This is equivalent to running `readlink -f ${l}`.
func resolveLink(l string) (string, error) {
	resolved, err := filepath.EvalSymlinks(l)
	if err != nil {
		return "", fmt.Errorf("error resolving link '%v': %w", l, err)
	}
	return resolved, nil
}
