/**
# Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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

package lookup

import (
	"fmt"
	"path/filepath"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup/symlinks"
)

type symlinkChain struct {
	file
}

type symlink struct {
	file
}

// NewSymlinkChainLocator creats a locator that can be used for locating files through symlinks.
func NewSymlinkChainLocator(opts ...Option) Locator {
	f := newFileLocator(opts...)
	l := symlinkChain{
		file: *f,
	}

	return &l
}

// NewSymlinkLocator creats a locator that can be used for locating files through symlinks.
func NewSymlinkLocator(opts ...Option) Locator {
	f := newFileLocator(opts...)
	l := symlink{
		file: *f,
	}

	return &l
}

// Locate finds the specified pattern at the specified root.
// If the file is a symlink, the link is followed and all candidates to the final target are returned.
func (p symlinkChain) Locate(pattern string) ([]string, error) {
	candidates, err := p.file.Locate(pattern)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return candidates, nil
	}

	found := make(map[string]bool)
	for len(candidates) > 0 {
		candidate := candidates[0]
		candidates = candidates[:len(candidates)-1]
		if found[candidate] {
			continue
		}
		found[candidate] = true

		target, err := symlinks.Resolve(candidate)
		if err != nil {
			return nil, fmt.Errorf("error resolving symlink: %v", err)
		}

		if !filepath.IsAbs(target) {
			target, err = filepath.Abs(filepath.Join(filepath.Dir(candidate), target))
			if err != nil {
				return nil, fmt.Errorf("failed to construct absolute path: %v", err)
			}
		}

		p.logger.Debugf("Resolved link: '%v' => '%v'", candidate, target)
		if !found[target] {
			candidates = append(candidates, target)
		}
	}

	var filenames []string
	for f := range found {
		filenames = append(filenames, f)
	}
	return filenames, nil
}

// Locate finds the specified pattern at the specified root.
// If the file is a symlink, the link is resolved and the target returned.
func (p symlink) Locate(pattern string) ([]string, error) {
	candidates, err := p.file.Locate(pattern)
	if err != nil {
		return nil, err
	}
	if len(candidates) != 1 {
		return nil, fmt.Errorf("failed to uniquely resolve symlink %v: %v", pattern, candidates)
	}

	target, err := filepath.EvalSymlinks(candidates[0])
	if err != nil {
		return nil, fmt.Errorf("failed to resolve link: %v", err)
	}

	return []string{target}, err
}
