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

// dcgmInitRetryInterval bounds how often ensureInit retries dcgm.Init after a
// failure, so a transiently unavailable driver/libdcgm does not lock the
// collector into "returns 0" forever, without re-initializing on every scrape.
const dcgmInitRetryInterval = 30 * time.Second

// migProfUtilFields are the DCGM fields read at FE_GPU_I (GPU instance)
// granularity. DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO (alias of the deprecated
// DCGM_FI_PROF_GR_ENGINE_ACTIVE) is a ratio in [0,1] for the fraction of time
// the graphics engine was active — the closest MIG-instance metric to NVML
// utilization.gpu's semantics. Plain DCGM_FI_DEV_GPU_UTIL is only available at
// FE_GPU granularity for the whole card, so it cannot be used here.
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

// dcgmHierarchyQuerier is the subset of go-dcgm used to build the
// (parent GPU UUID, NVML GPU-instance ID) → DCGM entity-ID map; faked in
// unit tests.
type dcgmHierarchyQuerier interface {
	GetGPUInstanceHierarchy() (dcgm.MigHierarchy_v2, error)
}

// realDCGMHierarchyQuerier forwards to go-dcgm's package-level function.
type realDCGMHierarchyQuerier struct{}

func (realDCGMHierarchyQuerier) GetGPUInstanceHierarchy() (dcgm.MigHierarchy_v2, error) {
	return dcgm.GetGPUInstanceHierarchy()
}

// dcgmWholeGPUCollector lazily brings up an embedded DCGM host engine (the
// first time a MIG-allocated container actually needs a utilization value)
// and then serves NVML-incompatible utilization reads on that path. Embedded
// mode is chosen so a separate nv-hostengine process / dcgm-exporter sidecar
// is not required; DCGM starts the host engine inside the vGPUmonitor process.
// The host engine runs until this process exits.
type dcgmWholeGPUCollector struct {
	mu          sync.Mutex
	initialized bool
	initErr     error
	lastAttempt time.Time

	querier          dcgmLatestValuesQuerier
	hierarchyQuerier dcgmHierarchyQuerier

	// migHierarchy maps (parent GPU UUID, NVML instance ID) → DCGM entity ID,
	// refreshed with the field update loop so new MIG instances show up.
	migHierarchy map[migHierarchyKey]uint
	hierarchyAt  time.Time
}

// newDCGMWholeGPUCollector returns a collector. Passing nil for q selects the
// real go-dcgm querier; a fake may be injected in tests.
func newDCGMWholeGPUCollector(q dcgmLatestValuesQuerier) *dcgmWholeGPUCollector {
	if q == nil {
		q = realDCGMQuerier{}
	}
	return &dcgmWholeGPUCollector{
		querier:          q,
		hierarchyQuerier: realDCGMHierarchyQuerier{},
	}
}

// migHierarchyKey pairs a parent GPU UUID with the per-parent NVML instance
// ID, since that NVML ID alone is ambiguous across GPUs on multi-GPU hosts.
type migHierarchyKey struct {
	parentGpuUuid  string
	nvmlInstanceId uint
}

// ensureInit lazily starts the embedded host engine. A failed attempt is
// retried after dcgmInitRetryInterval; a successful one is permanent and
// starts the field-update loop.
func (c *dcgmWholeGPUCollector) ensureInit() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialized {
		return nil
	}
	if c.initErr != nil && time.Since(c.lastAttempt) < dcgmInitRetryInterval {
		return c.initErr
	}
	c.lastAttempt = time.Now()
	if _, err := dcgm.Init(dcgm.Embedded); err != nil {
		c.initErr = err
		klog.Warningf("wholegpu: dcgm embedded init failed, will retry in %s: %v", dcgmInitRetryInterval, err)
		return c.initErr
	}
	c.initialized = true
	c.initErr = nil
	klog.Infof("wholegpu: dcgm embedded host engine started")
	go c.fieldUpdateLoop()
	return nil
}

// fieldUpdateLoop periodically forces fresh field samples from the host
// engine (EntityGetLatestValues only returns cached samples, so PROF ratios
// would otherwise go stale) and refreshes the MIG hierarchy cache.
func (c *dcgmWholeGPUCollector) fieldUpdateLoop() {
	if err := dcgm.UpdateAllFields(); err != nil {
		klog.V(4).Infof("wholegpu: initial dcgm.UpdateAllFields failed: %v", err)
	}
	c.refreshMigHierarchy()
	tick := time.NewTicker(dcgmFieldUpdateInterval)
	for range tick.C {
		if err := dcgm.UpdateAllFields(); err != nil {
			klog.V(4).Infof("wholegpu: dcgm.UpdateAllFields failed: %v", err)
		}
		c.refreshMigHierarchy()
	}
}

// refreshMigHierarchy rebuilds the (parent GPU UUID, NVML instance ID) → DCGM
// entity ID map. Failure is non-fatal: GpuInstanceSmUtil degrades to 0 until
// the next refresh succeeds.
func (c *dcgmWholeGPUCollector) refreshMigHierarchy() {
	h, err := c.hierarchyQuerier.GetGPUInstanceHierarchy()
	if err != nil {
		klog.V(4).Infof("wholegpu: dcgm.GetGPUInstanceHierarchy failed: %v", err)
		return
	}
	m := make(map[migHierarchyKey]uint, h.Count)
	for i := uint(0); i < h.Count; i++ {
		entry := h.EntityList[i]
		if entry.Entity.EntityGroupId != dcgm.FE_GPU_I {
			continue
		}
		key := migHierarchyKey{
			parentGpuUuid:  entry.Info.GpuUuid,
			nvmlInstanceId: entry.Info.NvmlInstanceId,
		}
		m[key] = entry.Entity.EntityId
	}
	c.mu.Lock()
	c.migHierarchy = m
	c.hierarchyAt = time.Now()
	c.mu.Unlock()
}

// GpuInstanceSmUtil returns the MIG GPU instance's graphics-engine
// utilization as an integer percentage 0-100 matching DeviceSmUtil's contract,
// or 0 when DCGM cannot provide a value.
//
// parentGpuUuid is the parent GPU's NVML UUID, needed because DCGM entity IDs
// are global and the per-parent NVML giID alone is ambiguous across GPUs.
// uuidForLog is only used for traceable log lines.
func (c *dcgmWholeGPUCollector) GpuInstanceSmUtil(parentGpuUuid string, nvmlInstanceId uint, uuidForLog string) uint64 {
	if err := c.ensureInit(); err != nil {
		return 0
	}
	entityID, ok := c.lookupMIGEntityID(parentGpuUuid, nvmlInstanceId)
	if !ok {
		klog.V(4).Infof("wholegpu: no DCGM entity for MIG instance %s/%d (uuid %s)", parentGpuUuid, nvmlInstanceId, uuidForLog)
		return 0
	}
	vals, err := c.querier.EntityGetLatestValues(dcgm.FE_GPU_I, entityID, migProfUtilFields)
	if err != nil {
		klog.V(4).Infof("wholegpu: EntityGetLatestValues(FE_GPU_I %d, %s) failed: %v", entityID, uuidForLog, err)
		return 0
	}
	for _, v := range vals {
		if v.Status != 0 {
			klog.V(4).Infof("wholegpu: FE_GPU_I %d field %d status %d (uuid %s)", entityID, v.FieldID, v.Status, uuidForLog)
			continue
		}
		// DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO is a ratio in [0, 1]; scale it to
		// the 0-100 percentage reported by DeviceSmUtil and clamp for safety.
		pct := math.Round(v.Float64() * 100)
		if pct < 0 {
			pct = 0
		} else if pct > 100 {
			pct = 100
		}
		return uint64(pct)
	}
	return 0
}

// lookupMIGEntityID returns the DCGM entity ID for a (parent GPU UUID, NVML
// instance ID) pair, or false if the hierarchy has not been refreshed yet or
// no matching MIG entity exists.
func (c *dcgmWholeGPUCollector) lookupMIGEntityID(parentGpuUuid string, nvmlInstanceId uint) (uint, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.migHierarchy == nil {
		return 0, false
	}
	id, ok := c.migHierarchy[migHierarchyKey{parentGpuUuid: parentGpuUuid, nvmlInstanceId: nvmlInstanceId}]
	return id, ok
}
