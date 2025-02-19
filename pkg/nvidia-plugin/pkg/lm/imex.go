/**
# Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
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

package lm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"k8s.io/klog/v2"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource"
)

const (
	// ImexNodesConfigFilePath is the path to the IMEX nodes config file.
	// This file contains a list of IP addresses of the nodes in the IMEX domain.
	ImexNodesConfigFilePath = "/etc/nvidia-imex/nodes_config.cfg"
)

func newImexLabeler(config *spec.Config, devices []resource.Device) (Labeler, error) {
	var errs error
	for _, root := range imexNodesConfigFilePathSearchRoots(config) {
		configFilePath := filepath.Join(root, ImexNodesConfigFilePath)
		imexLabeler, err := imexLabelerForConfigFile(configFilePath, devices)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if imexLabeler != nil {
			klog.Infof("Using labeler for IMEX config %v", configFilePath)
			return imexLabeler, nil
		}
	}
	if errs != nil {
		return nil, errs
	}

	return empty{}, nil
}

// imexNodesConfigFilePathSearchRoots returns a list of roots to search for the IMEX nodes config file.
func imexNodesConfigFilePathSearchRoots(config *spec.Config) []string {
	// By default, search / and /config for config files.
	roots := []string{"/", "/config"}

	if config == nil || config.Flags.Plugin == nil || config.Flags.Plugin.ContainerDriverRoot == nil {
		return roots
	}

	// If a driver root is specified, it is also searched.
	return append(roots, *config.Flags.Plugin.ContainerDriverRoot)
}

func imexLabelerForConfigFile(configFilePath string, devices []resource.Device) (Labeler, error) {
	imexConfigFile, err := os.Open(configFilePath)
	if os.IsNotExist(err) {
		// No imex config file, return empty labels
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to open imex config file: %v", err)
	}
	defer imexConfigFile.Close()

	clusterUUID, cliqueID, err := getFabricIDs(devices)
	if err != nil {
		return nil, err
	}
	if clusterUUID == "" || cliqueID == "" {
		return nil, nil
	}

	imexDomainID, err := getImexDomainID(imexConfigFile)
	if err != nil {
		return nil, err
	}
	if imexDomainID == "" {
		return nil, nil
	}

	labels := Labels{
		"nvidia.com/gpu.clique":      strings.Join([]string{clusterUUID, cliqueID}, "."),
		"nvidia.com/gpu.imex-domain": strings.Join([]string{imexDomainID, cliqueID}, "."),
	}

	return labels, nil
}

func getFabricIDs(devices []resource.Device) (string, string, error) {
	uniqueClusterUUIDs := make(map[string][]int)
	uniqueCliqueIDs := make(map[string][]int)
	for i, device := range devices {
		isFabricAttached, err := device.IsFabricAttached()
		if err != nil {
			return "", "", fmt.Errorf("error checking imex capability: %v", err)
		}
		if !isFabricAttached {
			continue
		}

		clusterUUID, cliqueID, err := device.GetFabricIDs()
		if err != nil {

			return "", "", fmt.Errorf("error getting fabric IDs: %w", err)
		}

		uniqueClusterUUIDs[clusterUUID] = append(uniqueClusterUUIDs[clusterUUID], i)
		uniqueCliqueIDs[cliqueID] = append(uniqueCliqueIDs[cliqueID], i)
	}

	if len(uniqueClusterUUIDs) > 1 {
		klog.Warningf("Cluster UUIDs are non-unique: %v", uniqueClusterUUIDs)
		return "", "", nil
	}

	if len(uniqueCliqueIDs) > 1 {
		klog.Warningf("Clique IDs are non-unique: %v", uniqueCliqueIDs)
		return "", "", nil
	}

	for clusterUUID := range uniqueClusterUUIDs {
		for cliqueID := range uniqueCliqueIDs {
			return clusterUUID, cliqueID, nil
		}
	}
	return "", "", nil
}

// getImexDomainID reads the imex config file and returns a unique identifier
// based on the sorted list of IP addresses in the file.
func getImexDomainID(r io.Reader) (string, error) {
	// Read the file line by line
	var ips []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		ip := strings.TrimSpace(scanner.Text())
		if net.ParseIP(ip) == nil {
			return "", fmt.Errorf("invalid IP address in imex config file: %s", ip)
		}
		ips = append(ips, ip)
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read imex config file: %v", err)
	}

	if len(ips) == 0 {
		// No IPs in the file, return empty labels
		return "", nil
	}

	sort.Strings(ips)

	return generateContentUUID(strings.Join(ips, "\n")), nil

}

func generateContentUUID(seed string) string {
	return uuid.NewSHA1(uuid.Nil, []byte(seed)).String()
}
