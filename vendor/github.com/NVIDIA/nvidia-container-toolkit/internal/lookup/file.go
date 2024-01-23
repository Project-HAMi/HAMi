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

package lookup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
)

// ErrNotFound indicates that a specified pattern or file could not be found.
var ErrNotFound = errors.New("not found")

// file can be used to locate file (or file-like elements) at a specified set of
// prefixes. The validity of a file is determined by a filter function.
type file struct {
	logger     logger.Interface
	root       string
	prefixes   []string
	filter     func(string) error
	count      int
	isOptional bool
}

// Option defines a function for passing options to the NewFileLocator() call
type Option func(*file)

// WithRoot sets the root for the file locator
func WithRoot(root string) Option {
	return func(f *file) {
		f.root = root
	}
}

// WithLogger sets the logger for the file locator
func WithLogger(logger logger.Interface) Option {
	return func(f *file) {
		f.logger = logger
	}
}

// WithSearchPaths sets the search paths for the file locator.
func WithSearchPaths(paths ...string) Option {
	return func(f *file) {
		f.prefixes = paths
	}
}

// WithFilter sets the filter for the file locator
// The filter is called for each candidate file and candidates that return nil are considered.
func WithFilter(assert func(string) error) Option {
	return func(f *file) {
		f.filter = assert
	}
}

// WithCount sets the maximum number of candidates to discover
func WithCount(count int) Option {
	return func(f *file) {
		f.count = count
	}
}

// WithOptional sets the optional flag for the file locator
// If the optional flag is set, the locator will not return an error if the file is not found.
func WithOptional(optional bool) Option {
	return func(f *file) {
		f.isOptional = optional
	}
}

// NewFileLocator creates a Locator that can be used to find files with the specified options.
func NewFileLocator(opts ...Option) Locator {
	return newFileLocator(opts...)
}

func newFileLocator(opts ...Option) *file {
	f := &file{}
	for _, opt := range opts {
		opt(f)
	}
	if f.logger == nil {
		f.logger = logger.New()
	}
	if f.filter == nil {
		f.filter = assertFile
	}
	// Since the `Locate` implementations rely on the root already being specified we update
	// the prefixes to include the root.
	f.prefixes = getSearchPrefixes(f.root, f.prefixes...)

	return f
}

// getSearchPrefixes generates a list of unique paths to be searched by a file locator.
//
// For each of the unique prefixes <p> specified, the path <root><p> is searched, where <root> is the
// specified root. If no prefixes are specified, <root> is returned as the only search prefix.
//
// Note that an empty root is equivalent to searching relative to the current working directory, and
// if the root filesystem should be searched instead, root should be specified as "/" explicitly.
//
// Also, a prefix of "" forces the root to be included in returned set of paths. This means that if
// the root in addition to another prefix must be searched the function should be called with:
//
//	getSearchPrefixes("/root", "", "another/path")
//
// and will result in the search paths []{"/root", "/root/another/path"} being returned.
func getSearchPrefixes(root string, prefixes ...string) []string {
	seen := make(map[string]bool)
	var uniquePrefixes []string
	for _, p := range prefixes {
		if seen[p] {
			continue
		}
		seen[p] = true
		uniquePrefixes = append(uniquePrefixes, filepath.Join(root, p))
	}

	if len(uniquePrefixes) == 0 {
		uniquePrefixes = append(uniquePrefixes, root)
	}

	return uniquePrefixes
}

var _ Locator = (*file)(nil)

// Locate attempts to find files with names matching the specified pattern.
// All prefixes are searched and any matching candidates are returned. If no matches are found, an error is returned.
func (p file) Locate(pattern string) ([]string, error) {
	var filenames []string

visit:
	for _, prefix := range p.prefixes {
		pathPattern := filepath.Join(prefix, pattern)
		candidates, err := filepath.Glob(pathPattern)
		if err != nil {
			p.logger.Debugf("Checking pattern '%v' failed: %v", pathPattern, err)
		}

		for _, candidate := range candidates {
			p.logger.Debugf("Checking candidate '%v'", candidate)
			err := p.filter(candidate)
			if err != nil {
				p.logger.Debugf("Candidate '%v' does not meet requirements: %v", candidate, err)
				continue
			}
			filenames = append(filenames, candidate)
			if p.count > 0 && len(filenames) == p.count {
				p.logger.Debugf("Found %d candidates; ignoring further candidates", len(filenames))
				break visit
			}
		}
	}

	if !p.isOptional && len(filenames) == 0 {
		return nil, fmt.Errorf("pattern %v %w", pattern, ErrNotFound)
	}
	return filenames, nil
}

// assertFile checks whether the specified path is a regular file
func assertFile(filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("error getting info for %v: %v", filename, err)
	}

	if info.IsDir() {
		return fmt.Errorf("specified path '%v' is a directory", filename)
	}

	return nil
}
