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
const MPSV3SMCountAnnotation = "hami.io/mps-sm-count"
const MPSV3GeometryKey = "mpsV3Geometry"
const MPSV3ModeKey = "mpsV3ComputeMode"
const MPSV3GPUSetKey = "mpsV3GPUSet"

const (
	MPSV3NamespaceCap          = "namespace-cap"
	MPSV3ServerTimeslice       = "server-timeslice"
	MPSV3ComputeModeAnnotation = "hami.io/mps-compute-mode"
	MPSV3ComputeModeKey        = "mpsV3ComputeStrategy"
	MPSV3SMShareableAnnotation = "hami.io/mps-sm-shareable"
	MPSV3SMMaxChunksAnnotation = "hami.io/mps-sm-max-chunks"
)

type MPSV3NodeConfig struct {
	ComputeMode         string `json:"computeMode,omitempty" yaml:"computeMode,omitempty"`
	Timeslice           string `json:"timeslice,omitempty" yaml:"timeslice,omitempty"`
	TimesliceYieldLevel *int   `json:"timesliceYieldLevel,omitempty" yaml:"timesliceYieldLevel,omitempty"`
}

func (c MPSV3NodeConfig) Resolve() (MPSV3NodeConfig, error) {
	if c.ComputeMode == "" {
		c.ComputeMode = MPSV3NamespaceCap
	}
	if c.ComputeMode == MPSV3NamespaceCap {
		if c.Timeslice != "" || c.TimesliceYieldLevel != nil {
			return c, fmt.Errorf("MPS timeslice settings require %s", MPSV3ServerTimeslice)
		}
		return c, nil
	}
	if c.ComputeMode != MPSV3ServerTimeslice {
		return c, fmt.Errorf("unknown MPS compute mode %q", c.ComputeMode)
	}
	if c.Timeslice == "" {
		c.Timeslice = "default"
	}
	switch c.Timeslice {
	case "default", "short", "medium", "long":
	default:
		return c, fmt.Errorf("unknown MPS timeslice %q", c.Timeslice)
	}
	if c.TimesliceYieldLevel != nil && *c.TimesliceYieldLevel != 1 && *c.TimesliceYieldLevel != 2 {
		return c, fmt.Errorf("MPS timesliceYieldLevel must be omitted, 1 or 2")
	}
	return c, nil
}

type MPSV3SMPolicy struct {
	Shareable bool   `json:"shareable,omitempty"`
	MaxChunks string `json:"maxChunks,omitempty"`
}

func MPSV3Request(annotations map[string]string) (string, int64, MPSV3SMPolicy, error) {
	mode := MPSV3NamespaceCap
	if value, ok := annotations[MPSV3ComputeModeAnnotation]; ok {
		mode = value
	}
	var policy MPSV3SMPolicy
	if mode != MPSV3NamespaceCap && mode != MPSV3ServerTimeslice {
		return mode, 0, policy, fmt.Errorf("unknown MPS compute mode %q", mode)
	}
	sms, err := MPSV3RequestedSMs(annotations)
	if err != nil {
		return mode, 0, policy, err
	}
	shareable, hasShareable := annotations[MPSV3SMShareableAnnotation]
	maxChunks, hasMaxChunks := annotations[MPSV3SMMaxChunksAnnotation]
	if (sms > 0 && mode != MPSV3NamespaceCap) || (sms == 0 && (hasShareable || hasMaxChunks)) {
		return mode, 0, policy, fmt.Errorf("SM partition options require a static SM request in namespace-cap mode")
	}
	if hasShareable {
		switch shareable {
		case "true":
			policy.Shareable = true
		case "false":
		default:
			return mode, 0, policy, fmt.Errorf("%s must be true or false", MPSV3SMShareableAnnotation)
		}
	}
	if hasMaxChunks {
		if maxChunks != "unlimited" {
			n, err := strconv.ParseUint(maxChunks, 10, 32)
			if err != nil {
				return mode, 0, policy, fmt.Errorf("%s must be 0, a positive integer or unlimited", MPSV3SMMaxChunksAnnotation)
			}
			maxChunks = ""
			if n > 0 {
				maxChunks = strconv.FormatUint(n, 10)
			}
		}
		policy.MaxChunks = maxChunks
	}
	return mode, sms, policy, nil
}

func (p MPSV3SMPolicy) Elastic() bool {
	return p.Shareable || p.MaxChunks != ""
}

func (p MPSV3SMPolicy) Validate(g MPSV3Geometry, sms int64) error {
	chunks, remainder, err := g.Partition(sms)
	if err != nil {
		return err
	}
	if p.Elastic() && remainder {
		return fmt.Errorf("elastic SM partition requires a whole number of chunks")
	}
	if p.MaxChunks != "" && p.MaxChunks != "unlimited" {
		maxChunks, err := strconv.ParseInt(p.MaxChunks, 10, 64)
		if err != nil || maxChunks < chunks || maxChunks > g.SMs/g.Chunk {
			return fmt.Errorf("SM max chunks %q outside [%d, %d]", p.MaxChunks, chunks, g.SMs/g.Chunk)
		}
	}
	return nil
}

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
func mpsV3Fits(pod *corev1.Pod, dev *device.DeviceUsage, count int64, selected device.ContainerDevices, cores int64, allocated *device.PodDevices) (int64, bool) {
	strategy, sms, smPolicy, err := MPSV3Request(pod.Annotations)
	if err != nil {
		return 0, false
	}
	if dev.Mode != MpsV3Mode {
		_, optedIn := pod.Annotations[MPSV3ComputeModeAnnotation]
		return cores, sms == 0 && !optedIn
	}
	nodeStrategy, _ := dev.CustomInfo[MPSV3ComputeModeKey].(string)
	if nodeStrategy == "" {
		if smPolicy.Elastic() {
			return 0, false
		}
		nodeStrategy = MPSV3NamespaceCap
	}
	if strategy != nodeStrategy {
		return 0, false
	}
	wanted := "percentage"
	if sms > 0 {
		wanted = "sm"
	}
	if mode, _ := dev.CustomInfo[MPSV3ModeKey].(string); mode != "" && mode != wanted {
		return 0, false
	}
	if allocated != nil && !mpsV3FitsPreviousSets((*allocated)[NvidiaGPUDevice], dev.ID, count, selected) {
		return 0, false
	}
	for _, p := range dev.PodInfos {
		if p == nil || p.Pod == nil {
			return 0, false
		}
		otherStrategy, other, _, err := MPSV3Request(p.Annotations)
		if err != nil || strategy != otherStrategy || (sms > 0) != (other > 0) {
			return 0, false
		}
		if !mpsV3FitsPreviousSets(p.Devices[NvidiaGPUDevice], dev.ID, count, selected) {
			return 0, false
		}
	}
	if sms > 0 {
		g, err := MPSV3DeviceGeometry(dev.CustomInfo)
		if err != nil {
			return 0, false
		}
		if err := smPolicy.Validate(g, sms); err != nil {
			return 0, false
		}
		_, remainder, _ := g.Partition(sms)
		if remainder {
			if allocated != nil && mpsV3HasRemainder((*allocated)[NvidiaGPUDevice], dev.ID) {
				return 0, false
			}
			for _, p := range dev.PodInfos {
				otherSMs, _ := MPSV3RequestedSMs(p.Annotations)
				_, otherRemainder, _ := g.Partition(otherSMs)
				if otherRemainder && mpsV3HasRemainder(p.Devices[NvidiaGPUDevice], dev.ID) {
					return 0, false
				}
			}
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

func mpsV3FitsPreviousSets(previous device.PodSingleDevice, id string, count int64, selected device.ContainerDevices) bool {
	for _, set := range previous {
		contains := func(uuid string) bool {
			return slices.ContainsFunc(set, func(d device.ContainerDevice) bool { return d.UUID == uuid })
		}
		if !contains(id) && !slices.ContainsFunc(selected, func(d device.ContainerDevice) bool { return contains(d.UUID) }) {
			continue
		}
		if int64(len(set)) != count || !contains(id) {
			return false
		}
		for _, chosen := range selected {
			if !slices.ContainsFunc(set, func(d device.ContainerDevice) bool { return d.UUID == chosen.UUID }) {
				return false
			}
		}
	}
	return true
}

func mpsV3HasRemainder(previous device.PodSingleDevice, id string) bool {
	for _, set := range previous {
		if slices.ContainsFunc(set, func(d device.ContainerDevice) bool { return d.UUID == id }) {
			return true
		}
	}
	return false
}
