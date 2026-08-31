/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package cdi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
	specs "tags.cncf.io/container-device-interface/specs-go"
)

// CreateMigSpecFile creates or updates a dynamic CDI specification file for a MIG instance.
// It writes to a .tmp file first and renames it atomically to prevent incomplete JSON reads.
func (cdi *cdiHandler) CreateMigSpecFile(migUUID string, devicePath string, caps map[string]string) error {
	if migUUID == "" {
		return fmt.Errorf("empty MIG UUID provided for CDI spec creation")
	}

	sanitizedUUID := strings.ReplaceAll(migUUID, "/", "_")
	specFileName := fmt.Sprintf("hami-mig-%s.json", sanitizedUUID)
	targetPath := filepath.Join(cdiRoot, specFileName)
	tmpPath := filepath.Join(cdiRoot, fmt.Sprintf(".%s.tmp", specFileName))

	if err := os.MkdirAll(cdiRoot, 0755); err != nil {
		return fmt.Errorf("failed to create cdi root dir %s: %w", cdiRoot, err)
	}

	deviceNodes := []*specs.DeviceNode{}
	if devicePath != "" {
		deviceNodes = append(deviceNodes, &specs.DeviceNode{
			Path:     devicePath,
			HostPath: devicePath,
		})
	}
	for capPath, capDevNode := range caps {
		if capDevNode != "" {
			deviceNodes = append(deviceNodes, &specs.DeviceNode{
				Path:     capPath,
				HostPath: capDevNode,
			})
		} else if capPath != "" {
			deviceNodes = append(deviceNodes, &specs.DeviceNode{
				Path:     capPath,
				HostPath: capPath,
			})
		}
	}

	spec := specs.Spec{
		Version: specs.CurrentVersion,
		Kind:    fmt.Sprintf("%s/mig", cdi.vendor),
		Devices: []specs.Device{
			{
				Name: migUUID,
				ContainerEdits: specs.ContainerEdits{
					Env: []string{
						fmt.Sprintf("NVIDIA_VISIBLE_DEVICES=%s", migUUID),
					},
					DeviceNodes: deviceNodes,
				},
			},
		},
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal CDI spec for MIG %s: %w", migUUID, err)
	}

	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temp CDI file %s: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write CDI spec data: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync temp CDI file: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp CDI file to %s: %w", targetPath, err)
	}

	klog.InfoS("atomically created dynamic MIG CDI spec", "uuid", migUUID, "path", targetPath)
	return nil
}

// DeleteMigSpecFile removes the dynamic CDI spec file for the given MIG instance UUID.
func (cdi *cdiHandler) DeleteMigSpecFile(migUUID string) error {
	if migUUID == "" {
		return nil
	}
	sanitizedUUID := strings.ReplaceAll(migUUID, "/", "_")
	specFileName := fmt.Sprintf("hami-mig-%s.json", sanitizedUUID)
	targetPath := filepath.Join(cdiRoot, specFileName)

	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove MIG CDI spec file %s: %w", targetPath, err)
	}

	klog.InfoS("removed dynamic MIG CDI spec", "uuid", migUUID, "path", targetPath)
	return nil
}
