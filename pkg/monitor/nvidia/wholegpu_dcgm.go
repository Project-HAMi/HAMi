/*
Copyright 2025 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nvidia

import (
	"math"
	"sync"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"k8s.io/klog/v2"
)

// dcgmFieldUpdateInterval is how often the collector asks the embedded host
// engine to sample fields. PROF counters need the engine to sample between
// reads for EntityGetLatestValues to return fresh values: dcgm-exporter uses
// a similar period in its capture loop.
const dcgmFieldUpdateInterval = 30 * time.Second

// migProfUtilFields are the DCGM fields read at FE_GPU_I (GPU instance)
// granularity. DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO (alias of the deprecated
// DCGM_FI_PROF_GR_ENGINE_ACTIVE) reports the fraction of time the graphics
// engine had any engine active — the closest MIG-instance metric to NVML
// utilization.gpu's semantics, and the one nvidia-smi surfaces. Plain
// DCGM_FI_DEV_GPU_UTIL is only available at FE_GPU granularity for the
// whole card, so it cannot be used here.
var migProfUtilFields = []dcgm.Short{dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO}

// dcgmLatestValuesQuerier is the subset of go-dcgm the collector needs. A
// fake is injected in unit tests so they do not require a real libdcgm.
type dcgmLatestValuesQuerier interface {
	EntityGetLatestValues(entityGroup dcgm.Field_Entity_Group, entityID uint, fields []dcgm.Short) ([]dcgm.FieldValue_v1, error)
}

// realDCGMQuerier forwards to go-dcgm's package-level function.
type realDCGMQuerier struct{}

func (realDCGMQuerier) EntityGetLatestValues(entityGroup dcgm.Field_Entity_Group, entityID uint, fields []dcgm.Short) ([]dcgm.FieldValue_v1, error) {
	return dcgm.EntityGetLatestValues(entityGroup, entityID, fields)
}

// dcgmWholeGPUCollector lazily brings up an embedded DCGM host engine (the
// first time a MIG-allocated container actually needs a utilization value)
// and then serves NVML-incompatible utilization reads on that path. Embedded
// mode is chosen so a separate nv-hostengine process / dcgm-exporter sidecar
// is not required; DCGM starts the host engine inside the vGPUmonitor process.
// The host engine runs until this process exits.
type dcgmWholeGPUCollector struct {
	initOnce sync.Once
	initErr  error
	querier  dcgmLatestValuesQuerier
}

// newDCGMWholeGPUCollector returns a collector. Passing nil for q selects the
// real go-dcgm querier; a fake may be injected in tests.
func newDCGMWholeGPUCollector(q dcgmLatestValuesQuerier) *dcgmWholeGPUCollector {
	if q == nil {
		q = realDCGMQuerier{}
	}
	return &dcgmWholeGPUCollector{querier: q}
}

// ensureInit lazily starts the embedded host engine. The failure is cached so
// a missing libdcgm (for example in a locally-built image without DCGM, or a
// dev box) degrades to returning 0 forever instead of re-attempting dlopen
// and logging a warning on every scrape.
func (c *dcgmWholeGPUCollector) ensureInit() error {
	c.initOnce.Do(func() {
		if _, err := dcgm.Init(dcgm.Embedded); err != nil {
			c.initErr = err
			klog.Warningf("wholegpu: dcgm embedded init failed, MIG utilization degraded to 0: %v", err)
			return
		}
		klog.Infof("wholegpu: dcgm embedded host engine started")
		go c.fieldUpdateLoop()
	})
	return c.initErr
}

// fieldUpdateLoop periodically requests fresh field samples from the host
// engine. dcgmEntityGetLatestValues returns cached samples, so without this
// forcing step PROF ratios would quickly go stale.
func (c *dcgmWholeGPUCollector) fieldUpdateLoop() {
	if err := dcgm.UpdateAllFields(); err != nil {
		klog.V(4).Infof("wholegpu: initial dcgm.UpdateAllFields failed: %v", err)
	}
	tick := time.NewTicker(dcgmFieldUpdateInterval)
	for range tick.C {
		if err := dcgm.UpdateAllFields(); err != nil {
			klog.V(4).Infof("wholegpu: dcgm.UpdateAllFields failed: %v", err)
		}
	}
}

// GpuInstanceSmUtil returns the MIG GPU instance's graphics-engine
// utilization as an integer percentage 0-100 matching DeviceSmUtil's
// contract, or 0 when DCGM cannot provide a value.
// uuidForLog is only used in log lines to make failures traceable back to a
// container UUID.
func (c *dcgmWholeGPUCollector) GpuInstanceSmUtil(giID uint, uuidForLog string) uint64 {
	if err := c.ensureInit(); err != nil {
		return 0
	}
	vals, err := c.querier.EntityGetLatestValues(dcgm.FE_GPU_I, giID, migProfUtilFields)
	if err != nil {
		klog.V(4).Infof("wholegpu: EntityGetLatestValues(FE_GPU_I %d, %s) failed: %v", giID, uuidForLog, err)
		return 0
	}
	for _, v := range vals {
		if v.Status != 0 {
			klog.V(4).Infof("wholegpu: FE_GPU_I %d field %d status %d (uuid %s)", giID, v.FieldID, v.Status, uuidForLog)
			continue
		}
		return uint64(math.Round(v.Float64()))
	}
	return 0
}
