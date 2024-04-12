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

package csv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
)

const (
	// DefaultMountSpecPath is default location of CSV files that define the modifications required to the OCI spec
	DefaultMountSpecPath = "/etc/nvidia-container-runtime/host-files-for-container.d"
)

// DefaultFileList returns the list of CSV files that are used by default.
func DefaultFileList() []string {
	files := []string{
		"devices.csv",
		"drivers.csv",
		"l4t.csv",
	}

	var paths []string
	for _, file := range files {
		paths = append(paths, filepath.Join(DefaultMountSpecPath, file))
	}

	return paths
}

// GetFileList returns the (non-recursive) list of CSV files in the specified
// folder
func GetFileList(root string) ([]string, error) {
	contents, err := os.ReadDir(root)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read the contents of %v: %v", root, err)
	}

	var csvFilePaths []string
	for _, c := range contents {
		if c.IsDir() {
			continue
		}
		if c.Name() == ".csv" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(c.Name()))
		if ext != ".csv" {
			continue
		}

		csvFilePaths = append(csvFilePaths, filepath.Join(root, c.Name()))
	}

	return csvFilePaths, nil
}

// BaseFilesOnly filters out non-base CSV files from the list of CSV files.
func BaseFilesOnly(filenames []string) []string {
	filter := map[string]bool{
		"l4t.csv":     true,
		"drivers.csv": true,
		"devices.csv": true,
	}

	var selected []string
	for _, file := range filenames {
		base := filepath.Base(file)
		if filter[base] {
			selected = append(selected, file)
		}
	}

	return selected
}

// Parser specifies an interface for parsing MountSpecs
type Parser interface {
	Parse() ([]*MountSpec, error)
}

type csv struct {
	logger   logger.Interface
	filename string
}

// NewCSVFileParser creates a new parser for reading MountSpecs from the specified CSV file
func NewCSVFileParser(logger logger.Interface, filename string) Parser {
	p := csv{
		logger:   logger,
		filename: filename,
	}

	return &p
}

// Parse parses the csv file and returns a list of MountSpecs in the file
func (p csv) Parse() ([]*MountSpec, error) {
	reader, err := os.Open(p.filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open %v for reading: %v", p.filename, err)
	}
	defer reader.Close()

	return p.parseFromReader(reader), nil
}

// parseFromReader parses the specified file and returns a list of required jetson mounts
func (p csv) parseFromReader(reader io.Reader) []*MountSpec {
	var targets []*MountSpec

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		target, err := NewMountSpecFromLine(line)
		if err != nil {
			p.logger.Debugf("Skipping invalid mount spec '%v': %v", line, err)
			continue
		}
		targets = append(targets, target)
	}

	return targets
}
