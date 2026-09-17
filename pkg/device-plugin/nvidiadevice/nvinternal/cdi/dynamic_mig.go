/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * Copyright (c) 2026 The HAMi Authors.
 */

package cdi

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi"
	nvcdspec "github.com/NVIDIA/nvidia-container-toolkit/pkg/nvcdi/spec"
	"k8s.io/klog/v2"
	cdiparser "tags.cncf.io/container-device-interface/pkg/parser"
	cdspec "tags.cncf.io/container-device-interface/specs-go"
)

// DynamicMIGClass isolates HAMi-owned runtime MIG entries from the base GPU spec.
const DynamicMIGClass = "dynamic-mig"
const dynamicMIGFilePrefix = "hami-dynamic-mig-"
const dynamicMIGUUIDAnnotation = "hami.io/mig-uuid"
const dynamicMIGParentAnnotation = "hami.io/parent-gpu-uuid"
const dynamicMIGGeometryAnnotation = "hami.io/mig-geometry"

// DynamicMIGDevice identifies one live, HAMi-managed GPU/compute instance pair.
type DynamicMIGDevice struct {
	MIGUUID           string
	ParentGPUUUID     string
	ParentMinor       int
	GPUInstanceID     uint32
	ComputeInstanceID uint32
}

// DynamicMIGInterface is supported by a CDI handler when CDI injection is enabled.
type DynamicMIGInterface interface {
	EnsureDynamicMIGDevice(DynamicMIGDevice) (string, error)
	RemoveDynamicMIGDevice(string) error
	ReplaceDynamicMIGDevices([]DynamicMIGDevice) error
}

var _ DynamicMIGInterface = &cdiHandler{}

// DynamicMIGName maps any supported MIG UUID to a CDI-safe, stable device name.
func DynamicMIGName(uuid string) (string, error) {
	if uuid == "" || !strings.HasPrefix(uuid, "MIG-") {
		return "", fmt.Errorf("invalid MIG UUID %q", uuid)
	}
	sum := sha256.Sum256([]byte(uuid))
	return "mig-" + hex.EncodeToString(sum[:]), nil
}

func (cdi *cdiHandler) dynamicMIGPath(uuid string) (string, string, error) {
	name, err := DynamicMIGName(uuid)
	if err != nil {
		return "", "", err
	}
	root := cdi.dynamicMIGRoot
	if root == "" {
		root = cdiRoot
	}
	return filepath.Join(root, dynamicMIGFilePrefix+name+".json"), name, nil
}

// EnsureDynamicMIGDevice publishes one CDI entry before Allocate can return it.
func (cdi *cdiHandler) EnsureDynamicMIGDevice(dev DynamicMIGDevice) (string, error) {
	path, name, err := cdi.dynamicMIGPath(dev.MIGUUID)
	if err != nil {
		return "", err
	}
	if dev.ParentGPUUUID == "" || dev.ParentMinor < 0 {
		return "", fmt.Errorf("invalid parent GPU for MIG device %q", dev.MIGUUID)
	}
	qualified := cdiparser.QualifiedName(cdi.vendor, DynamicMIGClass, name)
	if _, _, _, err := cdiparser.ParseQualifiedName(qualified); err != nil {
		return "", fmt.Errorf("invalid dynamic MIG CDI name: %w", err)
	}
	unlock := cdi.lockDynamicMIG(name)
	defer unlock()
	expected, err := cdi.dynamicMIGSpec(dev, name)
	if err != nil {
		return "", err
	}
	// Existing files are reused only when the complete, transformed spec matches.
	// This includes NVIDIA common edits, parent-GPU edits, hooks, and root paths.
	if raw, err := os.ReadFile(path); err == nil {
		if dynamicMIGSpecMatches(raw, expected.Raw()) {
			klog.V(4).InfoS("reused dynamic MIG CDI entry", "uuid", dev.MIGUUID, "path", path)
			return qualified, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := expected.Save(path); err != nil {
		return "", fmt.Errorf("publish dynamic MIG CDI spec: %w", err)
	}
	klog.InfoS("published dynamic MIG CDI entry", "uuid", dev.MIGUUID, "path", path)
	return qualified, nil
}

// dynamicMIGSpec creates the complete CDI spec that a live MIG instance needs.
// It must be called before deciding whether an existing file may be reused.
func (cdi *cdiHandler) dynamicMIGSpec(dev DynamicMIGDevice, name string) (nvcdspec.Interface, error) {
	geometry := fmt.Sprintf("%d:%d:%d", dev.ParentMinor, dev.GPUInstanceID, dev.ComputeInstanceID)
	gi, ci, err := cdi.dynamicMIGCapabilityNodes(dev)
	if err != nil {
		return nil, err
	}
	lib, ok := cdi.cdilibs["gpu"].(nvcdi.Interface)
	if !ok {
		return nil, fmt.Errorf("NVIDIA CDI library does not support device edits")
	}
	common, err := lib.GetCommonEdits()
	if err != nil {
		return nil, fmt.Errorf("get common NVIDIA CDI edits: %w", err)
	}
	if common == nil || common.ContainerEdits == nil {
		return nil, fmt.Errorf("NVIDIA CDI library returned no common edits")
	}
	parent, err := lib.GetDeviceSpecsByID(dev.ParentGPUUUID)
	if err != nil {
		return nil, fmt.Errorf("get parent GPU CDI edits for %q: %w", dev.ParentGPUUUID, err)
	}
	if len(parent) != 1 {
		return nil, fmt.Errorf("expected one CDI entry for parent GPU %q, got %d", dev.ParentGPUUUID, len(parent))
	}
	entry := parent[0]
	entry.Name = name
	entry.Annotations = map[string]string{
		dynamicMIGUUIDAnnotation:     dev.MIGUUID,
		dynamicMIGParentAnnotation:   dev.ParentGPUUUID,
		dynamicMIGGeometryAnnotation: geometry,
	}
	entry.ContainerEdits.DeviceNodes = append(entry.ContainerEdits.DeviceNodes, gi, ci)
	spec, err := nvcdspec.New(nvcdspec.WithVendor(cdi.vendor), nvcdspec.WithClass(DynamicMIGClass),
		nvcdspec.WithDeviceSpecs([]cdspec.Device{entry}), nvcdspec.WithEdits(*common.ContainerEdits))
	if err != nil {
		return nil, fmt.Errorf("build dynamic MIG CDI spec: %w", err)
	}
	if err := cdi.getRootTransformer().Transform(spec.Raw()); err != nil {
		return nil, fmt.Errorf("transform dynamic MIG CDI spec: %w", err)
	}
	return spec, nil
}

func dynamicMIGSpecMatches(savedJSON []byte, expected *cdspec.Spec) bool {
	// Decode to an untyped value so unknown JSON fields are part of the
	// comparison too. Decoding into cdspec.Spec would silently discard them.
	var saved any
	if err := json.Unmarshal(savedJSON, &saved); err != nil {
		return false
	}
	savedJSON, err := json.Marshal(saved)
	if err != nil {
		return false
	}
	expectedJSON, err := json.Marshal(expected)
	return err == nil && bytes.Equal(savedJSON, expectedJSON)
}

// RemoveDynamicMIGDevice touches only the HAMi-owned file for this UUID.
func (cdi *cdiHandler) RemoveDynamicMIGDevice(uuid string) error {
	path, name, err := cdi.dynamicMIGPath(uuid)
	if err != nil {
		return err
	}
	unlock := cdi.lockDynamicMIG(name)
	defer unlock()
	if err := cdi.validateDynamicMIGFile(path, uuid, name); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	err = os.Remove(path)
	if err == nil {
		klog.InfoS("removed dynamic MIG CDI entry", "uuid", uuid, "path", path)
	}
	return err
}

// validateDynamicMIGFile proves the path contains the one HAMi-owned entry for uuid.
// Shared CDI directories may also contain administrator or runtime-managed files.
func (cdi *cdiHandler) validateDynamicMIGFile(path, uuid, name string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var saved cdspec.Spec
	if err := json.Unmarshal(raw, &saved); err != nil {
		return fmt.Errorf("refusing to remove dynamic MIG CDI file %q: invalid CDI spec: %w", path, err)
	}
	if saved.Kind != cdi.vendor+"/"+DynamicMIGClass || len(saved.Devices) != 1 ||
		saved.Devices[0].Name != name || saved.Devices[0].Annotations[dynamicMIGUUIDAnnotation] != uuid {
		return fmt.Errorf("refusing to remove CDI file %q: it is not the HAMi dynamic MIG entry for %q", path, uuid)
	}
	expectedPath, expectedName, err := cdi.dynamicMIGPath(uuid)
	if err != nil {
		return err
	}
	if filepath.Clean(path) != filepath.Clean(expectedPath) || name != expectedName {
		return fmt.Errorf("refusing to remove CDI file %q: filename does not match MIG UUID %q", path, uuid)
	}
	return nil
}

type dynamicMIGLock struct {
	mu    sync.Mutex
	users int
}

func (cdi *cdiHandler) lockDynamicMIG(name string) func() {
	cdi.dynamicMIGLockMu.Lock()
	if cdi.dynamicMIGLocks == nil {
		cdi.dynamicMIGLocks = make(map[string]*dynamicMIGLock)
	}
	entry := cdi.dynamicMIGLocks[name]
	if entry == nil {
		entry = &dynamicMIGLock{}
		cdi.dynamicMIGLocks[name] = entry
	}
	entry.users++
	cdi.dynamicMIGLockMu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		cdi.dynamicMIGLockMu.Lock()
		entry.users--
		if entry.users == 0 {
			delete(cdi.dynamicMIGLocks, name)
		}
		cdi.dynamicMIGLockMu.Unlock()
	}
}

// ReplaceDynamicMIGDevices runs only during startup, before Allocate is served.
func (cdi *cdiHandler) ReplaceDynamicMIGDevices(live []DynamicMIGDevice) error {
	root := cdi.dynamicMIGRoot
	if root == "" {
		root = cdiRoot
	}
	wanted := make(map[string]struct{}, len(live))
	for _, dev := range live {
		path, _, err := cdi.dynamicMIGPath(dev.MIGUUID)
		if err != nil {
			return err
		}
		if _, err := cdi.EnsureDynamicMIGDevice(dev); err != nil {
			return err
		}
		wanted[path] = struct{}{}
	}
	files, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), dynamicMIGFilePrefix) || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, file.Name())
		if _, ok := wanted[path]; ok {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var saved cdspec.Spec
		if err := json.Unmarshal(raw, &saved); err != nil || len(saved.Devices) != 1 {
			return fmt.Errorf("refusing to remove CDI file %q: it is not a valid HAMi dynamic MIG entry", path)
		}
		uuid := saved.Devices[0].Annotations[dynamicMIGUUIDAnnotation]
		name := saved.Devices[0].Name
		if err := cdi.validateDynamicMIGFile(path, uuid, name); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	klog.InfoS("reconciled dynamic MIG CDI entries at startup", "live", len(live))
	return nil
}

func (cdi *cdiHandler) dynamicMIGCapabilityNodes(dev DynamicMIGDevice) (*cdspec.DeviceNode, *cdspec.DeviceNode, error) {
	procRoot := cdi.dynamicMIGProcRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	major, err := nvidiaCapsMajor(filepath.Join(procRoot, "devices"))
	if err != nil {
		return nil, nil, err
	}
	base := filepath.Join(procRoot, "driver/nvidia/capabilities", fmt.Sprintf("gpu%d/mig/gi%d", dev.ParentMinor, dev.GPUInstanceID))
	gi, err := capabilityNode(filepath.Join(base, "access"), major)
	if err != nil {
		return nil, nil, err
	}
	ci, err := capabilityNode(filepath.Join(base, fmt.Sprintf("ci%d/access", dev.ComputeInstanceID)), major)
	if err != nil {
		return nil, nil, err
	}
	return gi, ci, nil
}

func nvidiaCapsMajor(path string) (int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	inCharacters := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "Character devices:" {
			inCharacters = true
			continue
		}
		if strings.TrimSpace(line) == "Block devices:" {
			break
		}
		fields := strings.Fields(line)
		if inCharacters && len(fields) == 2 && fields[1] == "nvidia-caps" {
			major, err := strconv.ParseInt(fields[0], 10, 64)
			return major, err
		}
	}
	return 0, fmt.Errorf("nvidia-caps major not found in %s", path)
}

func capabilityNode(path string, major int64) (*cdspec.DeviceNode, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var minor int64 = -1
	var mode uint64
	modeFound := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.TrimSpace(parts[1])
		switch strings.TrimSpace(parts[0]) {
		case "DeviceFileMinor":
			minor, err = strconv.ParseInt(value, 10, 64)
		case "DeviceFileMode":
			mode, err = strconv.ParseUint(value, 10, 32)
			modeFound = true
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if minor < 0 || !modeFound {
		return nil, fmt.Errorf("missing MIG capability minor or mode in %s", path)
	}
	permissions := os.FileMode(mode)
	return &cdspec.DeviceNode{Path: fmt.Sprintf("/dev/nvidia-caps/nvidia-cap%d", minor), Type: "c", Major: major, Minor: minor, FileMode: &permissions}, nil
}
