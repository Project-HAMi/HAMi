/*
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
*/

package discover

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
)

// mounts is a generic discoverer for Mounts. It is customized by specifying the
// required entities as a list and a Locator that is used to find the target mounts
// based on the entry in the list.
type mounts struct {
	None
	logger   logger.Interface
	lookup   lookup.Locator
	root     string
	required []string
	sync.Mutex
	cache []Mount
}

var _ Discover = (*mounts)(nil)

// NewMounts creates a discoverer for the required mounts using the specified locator.
func NewMounts(logger logger.Interface, lookup lookup.Locator, root string, required []string) Discover {
	return newMounts(logger, lookup, root, required)
}

// newMounts creates a discoverer for the required mounts using the specified locator.
func newMounts(logger logger.Interface, lookup lookup.Locator, root string, required []string) *mounts {
	return &mounts{
		logger:   logger,
		lookup:   lookup,
		root:     filepath.Join("/", root),
		required: required,
	}
}

func (d *mounts) Mounts() ([]Mount, error) {
	if d.lookup == nil {
		return nil, fmt.Errorf("no lookup defined")
	}

	if d.cache != nil {
		d.logger.Debugf("returning cached mounts")
		return d.cache, nil
	}

	d.Lock()
	defer d.Unlock()

	uniqueMounts := make(map[string]Mount)

	for _, candidate := range d.required {
		d.logger.Debugf("Locating %v", candidate)
		located, err := d.lookup.Locate(candidate)
		if err != nil {
			d.logger.Warningf("Could not locate %v: %v", candidate, err)
			continue
		}
		if len(located) == 0 {
			d.logger.Warningf("Missing %v", candidate)
			continue
		}
		d.logger.Debugf("Located %v as %v", candidate, located)
		for _, p := range located {
			if _, ok := uniqueMounts[p]; ok {
				d.logger.Debugf("Skipping duplicate mount %v", p)
				continue
			}

			r := d.relativeTo(p)
			if r == "" {
				r = p
			}

			d.logger.Infof("Selecting %v as %v", p, r)
			uniqueMounts[p] = Mount{
				HostPath: p,
				Path:     r,
				Options: []string{
					"ro",
					"nosuid",
					"nodev",
					"bind",
				},
			}
		}
	}

	var mounts []Mount
	for _, m := range uniqueMounts {
		mounts = append(mounts, m)
	}

	d.cache = mounts

	return d.cache, nil
}

// relativeTo returns the path relative to the root for the file locator
func (d *mounts) relativeTo(path string) string {
	if d.root == "/" {
		return path
	}

	return strings.TrimPrefix(path, d.root)
}
