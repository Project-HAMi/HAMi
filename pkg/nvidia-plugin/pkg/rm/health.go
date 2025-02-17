/*
 * Copyright (c) 2019-2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rm

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

const (
	// envDisableHealthChecks defines the environment variable that is checked to determine whether healthchecks
	// should be disabled. If this envvar is set to "all" or contains the string "xids", healthchecks are
	// disabled entirely. If set, the envvar is treated as a comma-separated list of Xids to ignore. Note that
	// this is in addition to the Application errors that are already ignored.
	envDisableHealthChecks = "DP_DISABLE_HEALTHCHECKS"
	allHealthChecks        = "xids"
)

// CheckHealth performs health checks on a set of devices, writing to the 'unhealthy' channel with any unhealthy devices
func (r *nvmlResourceManager) checkHealth(stop <-chan interface{}, devices Devices, unhealthy chan<- *Device) error {
	disableHealthChecks := strings.ToLower(os.Getenv(envDisableHealthChecks))
	if disableHealthChecks == "all" {
		disableHealthChecks = allHealthChecks
	}
	if strings.Contains(disableHealthChecks, "xids") {
		return nil
	}

	ret := r.nvml.Init()
	if ret != nvml.SUCCESS {
		if *r.config.Flags.FailOnInitError {
			return fmt.Errorf("failed to initialize NVML: %v", ret)
		}
		return nil
	}
	defer func() {
		ret := r.nvml.Shutdown()
		if ret != nvml.SUCCESS {
			klog.Infof("Error shutting down NVML: %v", ret)
		}
	}()

	// FIXME: formalize the full list and document it.
	// http://docs.nvidia.com/deploy/xid-errors/index.html#topic_4
	// Application errors: the GPU should still be healthy
	applicationErrorXids := []uint64{
		13, // Graphics Engine Exception
		31, // GPU memory page fault
		43, // GPU stopped processing
		45, // Preemptive cleanup, due to previous errors
		68, // Video processor exception
	}

	skippedXids := make(map[uint64]bool)
	for _, id := range applicationErrorXids {
		skippedXids[id] = true
	}

	for _, additionalXid := range getAdditionalXids(disableHealthChecks) {
		skippedXids[additionalXid] = true
	}

	eventSet, ret := r.nvml.EventSetCreate()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("failed to create event set: %v", ret)
	}
	defer func() {
		_ = eventSet.Free()
	}()

	parentToDeviceMap := make(map[string]*Device)
	deviceIDToGiMap := make(map[string]uint32)
	deviceIDToCiMap := make(map[string]uint32)

	eventMask := uint64(nvml.EventTypeXidCriticalError | nvml.EventTypeDoubleBitEccError | nvml.EventTypeSingleBitEccError)
	for _, d := range devices {
		uuid, gi, ci, err := r.getDevicePlacement(d)
		if err != nil {
			klog.Warningf("Could not determine device placement for %v: %v; Marking it unhealthy.", d.ID, err)
			unhealthy <- d
			continue
		}
		deviceIDToGiMap[d.ID] = gi
		deviceIDToCiMap[d.ID] = ci
		parentToDeviceMap[uuid] = d

		gpu, ret := r.nvml.DeviceGetHandleByUUID(uuid)
		if ret != nvml.SUCCESS {
			klog.Infof("unable to get device handle from UUID: %v; marking it as unhealthy", ret)
			unhealthy <- d
			continue
		}

		supportedEvents, ret := gpu.GetSupportedEventTypes()
		if ret != nvml.SUCCESS {
			klog.Infof("unable to determine the supported events for %v: %v; marking it as unhealthy", d.ID, ret)
			unhealthy <- d
			continue
		}

		ret = gpu.RegisterEvents(eventMask&supportedEvents, eventSet)
		if ret == nvml.ERROR_NOT_SUPPORTED {
			klog.Warningf("Device %v is too old to support healthchecking.", d.ID)
		}
		if ret != nvml.SUCCESS {
			klog.Infof("Marking device %v as unhealthy: %v", d.ID, ret)
			unhealthy <- d
		}
	}

	for {
		select {
		case <-stop:
			return nil
		default:
		}

		e, ret := eventSet.Wait(5000)
		if ret == nvml.ERROR_TIMEOUT {
			continue
		}
		if ret != nvml.SUCCESS {
			klog.Infof("Error waiting for event: %v; Marking all devices as unhealthy", ret)
			for _, d := range devices {
				unhealthy <- d
			}
			continue
		}

		if e.EventType != nvml.EventTypeXidCriticalError {
			klog.Infof("Skipping non-nvmlEventTypeXidCriticalError event: %+v", e)
			continue
		}

		if skippedXids[e.EventData] {
			klog.Infof("Skipping event %+v", e)
			continue
		}

		klog.Infof("Processing event %+v", e)
		eventUUID, ret := e.Device.GetUUID()
		if ret != nvml.SUCCESS {
			// If we cannot reliably determine the device UUID, we mark all devices as unhealthy.
			klog.Infof("Failed to determine uuid for event %v: %v; Marking all devices as unhealthy.", e, ret)
			for _, d := range devices {
				unhealthy <- d
			}
			continue
		}

		d, exists := parentToDeviceMap[eventUUID]
		if !exists {
			klog.Infof("Ignoring event for unexpected device: %v", eventUUID)
			continue
		}

		if d.IsMigDevice() && e.GpuInstanceId != 0xFFFFFFFF && e.ComputeInstanceId != 0xFFFFFFFF {
			gi := deviceIDToGiMap[d.ID]
			ci := deviceIDToCiMap[d.ID]
			if !(gi == e.GpuInstanceId && ci == e.ComputeInstanceId) {
				continue
			}
			klog.Infof("Event for mig device %v (gi=%v, ci=%v)", d.ID, gi, ci)
		}

		klog.Infof("XidCriticalError: Xid=%d on Device=%s; marking device as unhealthy.", e.EventData, d.ID)
		unhealthy <- d
	}
}

// getAdditionalXids returns a list of additional Xids to skip from the specified string.
// The input is treaded as a comma-separated string and all valid uint64 values are considered as Xid values. Invalid values
// are ignored.
func getAdditionalXids(input string) []uint64 {
	if input == "" {
		return nil
	}

	var additionalXids []uint64
	for _, additionalXid := range strings.Split(input, ",") {
		trimmed := strings.TrimSpace(additionalXid)
		if trimmed == "" {
			continue
		}
		xid, err := strconv.ParseUint(trimmed, 10, 64)
		if err != nil {
			klog.Infof("Ignoring malformed Xid value %v: %v", trimmed, err)
			continue
		}
		additionalXids = append(additionalXids, xid)
	}

	return additionalXids
}

// getDevicePlacement returns the placement of the specified device.
// For a MIG device the placement is defined by the 3-tuple <parent UUID, GI, CI>
// For a full device the returned 3-tuple is the device's uuid and 0xFFFFFFFF for the other two elements.
func (r *nvmlResourceManager) getDevicePlacement(d *Device) (string, uint32, uint32, error) {
	if !d.IsMigDevice() {
		return d.GetUUID(), 0xFFFFFFFF, 0xFFFFFFFF, nil
	}
	return r.getMigDeviceParts(d)
}

// getMigDeviceParts returns the parent GI and CI ids of the MIG device.
func (r *nvmlResourceManager) getMigDeviceParts(d *Device) (string, uint32, uint32, error) {
	if !d.IsMigDevice() {
		return "", 0, 0, fmt.Errorf("cannot get GI and CI of full device")
	}

	uuid := d.GetUUID()
	// For older driver versions, the call to DeviceGetHandleByUUID will fail for MIG devices.
	mig, ret := r.nvml.DeviceGetHandleByUUID(uuid)
	if ret == nvml.SUCCESS {
		parentHandle, ret := mig.GetDeviceHandleFromMigDeviceHandle()
		if ret != nvml.SUCCESS {
			return "", 0, 0, fmt.Errorf("failed to get parent device handle: %v", ret)
		}

		parentUUID, ret := parentHandle.GetUUID()
		if ret != nvml.SUCCESS {
			return "", 0, 0, fmt.Errorf("failed to get parent uuid: %v", ret)
		}
		gi, ret := mig.GetGpuInstanceId()
		if ret != nvml.SUCCESS {
			return "", 0, 0, fmt.Errorf("failed to get GPU Instance ID: %v", ret)
		}

		ci, ret := mig.GetComputeInstanceId()
		if ret != nvml.SUCCESS {
			return "", 0, 0, fmt.Errorf("failed to get Compute Instance ID: %v", ret)
		}
		//nolint:gosec  // We know that the values returned from Get*InstanceId are within the valid uint32 range.
		return parentUUID, uint32(gi), uint32(ci), nil
	}
	return parseMigDeviceUUID(uuid)
}

// parseMigDeviceUUID splits the MIG device UUID into the parent device UUID and ci and gi
func parseMigDeviceUUID(mig string) (string, uint32, uint32, error) {
	tokens := strings.SplitN(mig, "-", 2)
	if len(tokens) != 2 || tokens[0] != "MIG" {
		return "", 0, 0, fmt.Errorf("unable to parse UUID as MIG device")
	}

	tokens = strings.SplitN(tokens[1], "/", 3)
	if len(tokens) != 3 || !strings.HasPrefix(tokens[0], "GPU-") {
		return "", 0, 0, fmt.Errorf("unable to parse UUID as MIG device")
	}

	gi, err := toUint32(tokens[1])
	if err != nil {
		return "", 0, 0, fmt.Errorf("unable to parse UUID as MIG device")
	}

	ci, err := toUint32(tokens[2])
	if err != nil {
		return "", 0, 0, fmt.Errorf("unable to parse UUID as MIG device")
	}

	return tokens[0], gi, ci, nil
}

func toUint32(s string) (uint32, error) {
	u, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	//nolint:gosec  // Since we parse s with a 32-bit size this will not overflow.
	return uint32(u), nil
}
