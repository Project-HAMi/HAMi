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
	"os"
	"path/filepath"
	"testing"

	specs "tags.cncf.io/container-device-interface/specs-go"
)

func TestCreateAndDeleteMigSpecFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cdi-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	oldCdiRoot := cdiRoot
	cdiRoot = tempDir
	defer func() { cdiRoot = oldCdiRoot }()

	handler := &cdiHandler{
		vendor: "k8s.device-plugin.nvidia.com",
	}

	migUUID := "MIG-GPU-12345678-1234-1234-1234-123456789abc/1/0"
	sanitizedUUID := "MIG-GPU-12345678-1234-1234-1234-123456789abc_1_0"
	specPath := filepath.Join(cdiRoot, "hami-mig-"+sanitizedUUID+".json")

	// Clean up any pre-existing test file
	_ = os.Remove(specPath)
	defer os.Remove(specPath)

	caps := map[string]string{
		"/proc/driver/nvidia/capabilities/gpu0/mig/gi1/ci0/access": "/dev/nvidia-caps/nvidia-cap3",
	}

	// 1. Test empty UUID error
	if err := handler.CreateMigSpecFile("", "/dev/nvidia0", caps); err == nil {
		t.Errorf("expected error with empty MIG UUID, got nil")
	}

	// 2. Create Spec File
	err = handler.CreateMigSpecFile(migUUID, "/dev/nvidia0", caps)
	if err != nil {
		t.Fatalf("CreateMigSpecFile failed: %v", err)
	}

	// Verify file exists
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("failed to read created CDI spec file %s: %v", specPath, err)
	}

	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("failed to unmarshal CDI spec JSON: %v", err)
	}

	if spec.Kind != "k8s.device-plugin.nvidia.com/mig" {
		t.Errorf("expected spec.Kind='k8s.device-plugin.nvidia.com/mig', got %q", spec.Kind)
	}
	if len(spec.Devices) != 1 {
		t.Fatalf("expected 1 device in spec, got %d", len(spec.Devices))
	}
	if spec.Devices[0].Name != migUUID {
		t.Errorf("expected device name %q, got %q", migUUID, spec.Devices[0].Name)
	}

	// Verify DeviceNodes Path vs HostPath mapping semantics
	nodes := spec.Devices[0].ContainerEdits.DeviceNodes
	if len(nodes) != 2 {
		t.Fatalf("expected 2 device nodes (/dev/nvidia0 and cap node), got %d", len(nodes))
	}

	var foundCap bool
	for _, node := range nodes {
		if node.Path == "/proc/driver/nvidia/capabilities/gpu0/mig/gi1/ci0/access" {
			foundCap = true
			if node.HostPath != "/dev/nvidia-caps/nvidia-cap3" {
				t.Errorf("expected HostPath='/dev/nvidia-caps/nvidia-cap3', got %q", node.HostPath)
			}
		}
	}
	if !foundCap {
		t.Errorf("expected capability DeviceNode with container path '/proc/driver/nvidia/capabilities/gpu0/mig/gi1/ci0/access' not found")
	}

	// 3. Delete Spec File
	err = handler.DeleteMigSpecFile(migUUID)
	if err != nil {
		t.Fatalf("DeleteMigSpecFile failed: %v", err)
	}

	if _, err := os.Stat(specPath); !os.IsNotExist(err) {
		t.Errorf("expected spec file %s to be deleted, but it still exists", specPath)
	}

	// 4. Delete with empty UUID is no-op
	if err := handler.DeleteMigSpecFile(""); err != nil {
		t.Errorf("expected DeleteMigSpecFile with empty UUID to be nil, got: %v", err)
	}
}
