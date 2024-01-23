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

package tegra

import (
	"fmt"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/discover"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/logger"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/lookup"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/platform-support/tegra/csv"
)

// newDiscovererFromCSVFiles creates a discoverer for the specified CSV files. A logger is also supplied.
// The constructed discoverer is comprised of a list, with each element in the list being associated with a
// single CSV files.
func (o tegraOptions) newDiscovererFromCSVFiles() (discover.Discover, error) {
	if len(o.csvFiles) == 0 {
		o.logger.Warningf("No CSV files specified")
		return discover.None{}, nil
	}

	targetsByType := getTargetsFromCSVFiles(o.logger, o.csvFiles)

	devices := discover.NewDeviceDiscoverer(
		o.logger,
		lookup.NewCharDeviceLocator(lookup.WithLogger(o.logger), lookup.WithRoot(o.driverRoot)),
		o.driverRoot,
		targetsByType[csv.MountSpecDev],
	)

	directories := discover.NewMounts(
		o.logger,
		lookup.NewDirectoryLocator(lookup.WithLogger(o.logger), lookup.WithRoot(o.driverRoot)),
		o.driverRoot,
		targetsByType[csv.MountSpecDir],
	)

	// Libraries and symlinks use the same locator.
	libraries := discover.NewMounts(
		o.logger,
		o.symlinkLocator,
		o.driverRoot,
		targetsByType[csv.MountSpecLib],
	)

	symlinkTargets := o.ignorePatterns.Apply(targetsByType[csv.MountSpecSym]...)
	o.logger.Debugf("Filtered symlink targets: %v", symlinkTargets)
	symlinks := discover.NewMounts(
		o.logger,
		o.symlinkLocator,
		o.driverRoot,
		symlinkTargets,
	)
	createSymlinks := o.createCSVSymlinkHooks(symlinkTargets, libraries)

	d := discover.Merge(
		devices,
		directories,
		libraries,
		symlinks,
		createSymlinks,
	)

	return d, nil
}

// getTargetsFromCSVFiles returns the list of mount specs from the specified CSV files.
// These are aggregated by mount spec type.
// TODO: We use a function variable here to allow this to be overridden for testing.
// This should be properly mocked.
var getTargetsFromCSVFiles = func(logger logger.Interface, files []string) map[csv.MountSpecType][]string {
	targetsByType := make(map[csv.MountSpecType][]string)
	for _, filename := range files {
		targets, err := loadCSVFile(logger, filename)
		if err != nil {
			logger.Warningf("Skipping CSV file %v: %v", filename, err)
			continue
		}
		for _, t := range targets {
			targetsByType[t.Type] = append(targetsByType[t.Type], t.Path)
		}
	}
	return targetsByType
}

// loadCSVFile loads the specified CSV file and returns the list of mount specs
func loadCSVFile(logger logger.Interface, filename string) ([]*csv.MountSpec, error) {
	// Create a discoverer for each file-kind combination
	targets, err := csv.NewCSVFileParser(logger, filename).Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV file: %v", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	return targets, nil
}
