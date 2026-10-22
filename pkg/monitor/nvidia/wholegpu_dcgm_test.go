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
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"gotest.tools/v3/assert"
)

// fakeLatestValuesQuerier implements dcgmLatestValuesQuerier for tests.
type fakeLatestValuesQuerier struct {
	err  error
	vals []dcgm.FieldValue_v1

	lastEntityGroup dcgm.Field_Entity_Group
	lastID          uint
	lastFields      []dcgm.Short
}

func (f *fakeLatestValuesQuerier) EntityGetLatestValues(eg dcgm.Field_Entity_Group, id uint, fields []dcgm.Short) ([]dcgm.FieldValue_v1, error) {
	f.lastEntityGroup = eg
	f.lastID = id
	f.lastFields = fields
	return f.vals, f.err
}

// fakeHierarchyQuerier implements dcgmHierarchyQuerier for tests.
type fakeHierarchyQuerier struct {
	err       error
	hierarchy dcgm.MigHierarchy_v2
}

func (f *fakeHierarchyQuerier) GetGPUInstanceHierarchy() (dcgm.MigHierarchy_v2, error) {
	if f.err != nil {
		return dcgm.MigHierarchy_v2{}, f.err
	}
	return f.hierarchy, nil
}

// dcgmDoubleBytes packs a float64 into FieldValue_v1.Value the same little-
// endian way the real library does.
func dcgmDoubleBytes(v float64) (out [4096]byte) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	copy(out[:8], b[:])
	return
}

// newMigTestCollector builds a collector whose embedded engine is already
// marked as initialized, so tests never bring up a real DCGM host engine.
func newMigTestCollector(q dcgmLatestValuesQuerier) *dcgmWholeGPUCollector {
	c := newDCGMWholeGPUCollector(q)
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return c
}

// seedMigHierarchy pre-populates the (parent GPU UUID, NVML giID) → DCGM
// entity ID map and marks the collector as initialized.
func seedMigHierarchy(c *dcgmWholeGPUCollector, parent string, giID, dcgmEntityID uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialized = true
	if c.migHierarchy == nil {
		c.migHierarchy = make(map[migHierarchyKey]uint)
	}
	c.migHierarchy[migHierarchyKey{parentGpuUuid: parent, nvmlInstanceId: giID}] = dcgmEntityID
}

func Test_dcgmWholeGPUCollector_GpuInstanceSmUtil(t *testing.T) {
	t.Run("ratio is scaled to a 0-100 percentage at FE_GPU_I", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(0.664)},
		}}
		col := newMigTestCollector(fake)
		seedMigHierarchy(col, "GPU-parent-uuid", 5, 7)
		assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-uuid", 5, "MIG-x"), uint64(66))
		// The querier must see DCGM entity ID 7, not NVML giID 5.
		assert.Equal(t, fake.lastEntityGroup, dcgm.FE_GPU_I)
		assert.Equal(t, fake.lastID, uint(7))
		assert.DeepEqual(t, fake.lastFields, migProfUtilFields)
	})

	t.Run("different parent GPUs do not collide on the same NVML giID", func(t *testing.T) {
		// Two parents both report NVML giID 0 — DCGM must see two
		// distinct entity IDs.
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(0.42)},
		}}
		col := newMigTestCollector(fake)
		seedMigHierarchy(col, "GPU-parent-0", 0, 100)
		seedMigHierarchy(col, "GPU-parent-1", 0, 200)
		assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-0", 0, "MIG-0"), uint64(42))
		assert.Equal(t, fake.lastID, uint(100))
		assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-1", 0, "MIG-1"), uint64(42))
		assert.Equal(t, fake.lastID, uint(200))
	})

	t.Run("hierarchy miss yields 0 without hitting the querier", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(0.5)},
		}}
		col := newMigTestCollector(fake)
		// No hierarchy seeded: lookup must miss and the querier must not
		// be invoked.
		assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-uuid", 5, "MIG-x"), uint64(0))
		assert.Equal(t, fake.lastEntityGroup, dcgm.FE_NONE)
		assert.Equal(t, fake.lastID, uint(0))
	})

	t.Run("querier error yields 0", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{err: errors.New("dcgm unreachable")}
		col := newMigTestCollector(fake)
		seedMigHierarchy(col, "GPU-parent-uuid", 5, 7)
		assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-uuid", 5, "MIG-x"), uint64(0))
	})

	t.Run("empty result yields 0", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: nil}
		col := newMigTestCollector(fake)
		seedMigHierarchy(col, "GPU-parent-uuid", 5, 7)
		assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-uuid", 5, "MIG-x"), uint64(0))
	})

	t.Run("non-zero field status yields 0", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: -17 /* DCGM_ST_NOT_SUPPORTED */, Value: dcgmDoubleBytes(99)},
		}}
		col := newMigTestCollector(fake)
		seedMigHierarchy(col, "GPU-parent-uuid", 5, 7)
		assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-uuid", 5, "MIG-x"), uint64(0))
	})

	t.Run("ratio is scaled, rounded and clamped to 0-100", func(t *testing.T) {
		cases := []struct {
			ratio float64
			want  uint64
		}{
			{0.49, 49},
			{0.004, 0},
			{0.006, 1},
			{1.0, 100},
			{1.5, 100},
			{-0.1, 0},
		}
		for _, tc := range cases {
			fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
				{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(tc.ratio)},
			}}
			col := newMigTestCollector(fake)
			seedMigHierarchy(col, "GPU-parent-uuid", 5, 7)
			assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-uuid", 5, "MIG-x"), tc.want)
		}
	})

	// Pins down the assumption that DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO returns a
	// [0, 1] ratio. If a DCGM version returns percentage directly (e.g. 66.4
	// meaning 66.4 %), the *100 here overshoots and the clamp holds the value
	// at 100 instead of emitting a nonsensical 6640. Note this as a possible
	// silent regression — MIG containers will show 100% utilization.
	t.Run("percentage-form input is clamped, not rescaled", func(t *testing.T) {
		cases := []struct {
			raw  float64
			name string
			want uint64
		}{
			{0.5, "mid-range ratio", 50},
			{1.0, "ratio at upper bound", 100},
			{66.4, "already a percentage — silently clamps to 100", 100},
			{150.0, "way out of range — still clamps to 100", 100},
		}
		for _, tc := range cases {
			fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
				{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(tc.raw)},
			}}
			col := newMigTestCollector(fake)
			seedMigHierarchy(col, "GPU-parent-uuid", 5, 7)
			assert.Equal(t, col.GpuInstanceSmUtil("GPU-parent-uuid", 5, "MIG-x"), tc.want, "case %s", tc.name)
		}
	})
}

func Test_dcgmWholeGPUCollector_ensureInit_retry(t *testing.T) {
	// A cached init failure must be honored within dcgmInitRetryInterval but
	// re-attempted once it elapses.
	t.Run("cached failure is honored inside the retry window", func(t *testing.T) {
		col := newDCGMWholeGPUCollector(&fakeLatestValuesQuerier{})
		col.mu.Lock()
		col.initialized = false
		col.initErr = errors.New("libdcgm.so.4 not found")
		col.lastAttempt = time.Now().Add(-dcgmInitRetryInterval / 2)
		col.mu.Unlock()
		err := col.ensureInit()
		assert.ErrorContains(t, err, "libdcgm.so.4 not found")
	})

	t.Run("cached failure is retried after the retry window elapses", func(t *testing.T) {
		// The real dcgm.Init path runs here, so assert the retry happened
		// (lastAttempt advanced) rather than its outcome.
		col := newDCGMWholeGPUCollector(&fakeLatestValuesQuerier{})
		col.mu.Lock()
		col.initialized = false
		col.initErr = errors.New("libdcgm.so.4 not found")
		col.lastAttempt = time.Now().Add(-2 * dcgmInitRetryInterval)
		before := time.Now()
		col.mu.Unlock()
		_ = col.ensureInit()
		col.mu.Lock()
		defer col.mu.Unlock()
		assert.Assert(t, !col.lastAttempt.Before(before), "ensureInit must update lastAttempt when the retry window has elapsed")
	})
}
