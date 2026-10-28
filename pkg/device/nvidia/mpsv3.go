// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package nvidia

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

// Modes are comma-separated tokens. In particular, mps must not match mps-v3.
func matchesOperatingMode(requested, mode string) bool {
	for token := range strings.SplitSeq(requested, ",") {
		if strings.TrimSpace(token) == mode {
			return true
		}
	}
	return false
}

// MPSV3SMCountAnnotation selects real static SM partitions. All GPU containers
// in the Pod use the same SM count; absence selects percentage provisioning.
const MPSV3SMCountAnnotation = "nvidia.com/mps-sm-count"
const MPSV3GeometryKey = "mpsV3Geometry"
const MPSV3ModeKey = "mpsV3ComputeMode"
const MPSV3GPUSetKey = "mpsV3GPUSet"

type MPSV3Geometry struct {
	SMs   int64 `json:"sms"`
	Chunk int64 `json:"chunk"`
}

func MPSV3RequestedSMs(annotations map[string]string) (int64, error) {
	raw, ok := annotations[MPSV3SMCountAnnotation]
	if !ok {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", MPSV3SMCountAnnotation)
	}
	return n, nil
}

// Static SMs must be exactly representable; never substitute a percentage cap.
func (g MPSV3Geometry) Partition(sms int64) (int64, bool, error) {
	if g.SMs <= 0 || g.SMs > 100000 || g.Chunk <= 0 || sms <= 0 || sms > g.SMs || sms/g.Chunk == 0 {
		return 0, false, fmt.Errorf("SM count %d does not fit device geometry %+v", sms, g)
	}
	remainder := sms % g.Chunk
	if remainder != 0 && remainder != g.SMs%g.Chunk {
		return 0, false, fmt.Errorf("SM count %d cannot be represented in %d-SM chunks", sms, g.Chunk)
	}
	return sms / g.Chunk, remainder != 0, nil
}
func (g MPSV3Geometry) Percent(sms int64) (int64, error) {
	if _, _, err := g.Partition(sms); err != nil {
		return 0, err
	}
	return (sms*100 + g.SMs - 1) / g.SMs, nil
}

func MPSV3DeviceGeometry(info map[string]any) (MPSV3Geometry, error) {
	var g MPSV3Geometry
	data, err := json.Marshal(info[MPSV3GeometryKey])
	if err != nil {
		return g, err
	}
	err = json.Unmarshal(data, &g)
	if err == nil && g.SMs <= 0 {
		err = fmt.Errorf("missing MPS SM geometry")
	}
	return g, err
}

// Percentage and static SM modes must never share a physical GPU: the driver
// ignores active_thread_percentage once static partitioning is enabled.
func mpsV3Fits(pod *corev1.Pod, dev *device.DeviceUsage, count int64, selected device.ContainerDevices, cores int64) (int64, bool) {
	sms, err := MPSV3RequestedSMs(pod.Annotations)
	if err != nil {
		return 0, false
	}
	if dev.Mode != MpsV3Mode {
		return cores, sms == 0
	}
	wanted := "percentage"
	if sms > 0 {
		wanted = "sm"
	}
	if mode, _ := dev.CustomInfo[MPSV3ModeKey].(string); mode != "" && mode != wanted {
		return 0, false
	}
	for _, p := range dev.PodInfos {
		if p == nil || p.Pod == nil {
			return 0, false
		}
		other, err := MPSV3RequestedSMs(p.Annotations)
		if err != nil || (sms > 0) != (other > 0) {
			return 0, false
		}
		for _, allocated := range p.Devices[NvidiaGPUDevice] {
			uses := false
			for _, d := range allocated {
				if d.UUID == dev.ID {
					uses = true
				}
			}
			if !uses {
				continue
			}
			if int64(len(allocated)) != count {
				return 0, false
			}
			for _, chosen := range selected {
				found := false
				for _, d := range allocated {
					if d.UUID == chosen.UUID {
						found = true
					}
				}
				if !found {
					return 0, false
				}
			}
		}
	}
	if sms > 0 {
		g, err := MPSV3DeviceGeometry(dev.CustomInfo)
		if err != nil {
			return 0, false
		}
		charge, err := g.Percent(sms)
		if err != nil || (cores > 0 && cores < charge) {
			return 0, false
		}
		cores = charge
	} else if cores == 0 {
		cores = 100
	}
	for _, chosen := range selected {
		if raw, ok := chosen.CustomInfo[MPSV3GPUSetKey]; ok {
			data, _ := json.Marshal(raw)
			var ids []string
			if json.Unmarshal(data, &ids) != nil || !slices.Contains(ids, dev.ID) {
				return 0, false
			}
		}
	}
	// A live named server owns one exact GPU set. Avoid scheduling an overlapping
	// set that the node cannot safely serve through another MPS server.
	if raw, ok := dev.CustomInfo[MPSV3GPUSetKey]; ok {
		data, _ := json.Marshal(raw)
		var ids []string
		if json.Unmarshal(data, &ids) != nil || int64(len(ids)) != count {
			return 0, false
		}
		for _, d := range selected {
			if !slices.Contains(ids, d.UUID) {
				return 0, false
			}
		}
	}
	return cores, true
}
