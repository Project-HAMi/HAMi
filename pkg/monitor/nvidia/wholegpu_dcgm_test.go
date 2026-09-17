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

// dcgmDoubleBytes packs a float64 into FieldValue_v1.Value the same little-
// endian way the real library does.
func dcgmDoubleBytes(v float64) (out [4096]byte) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	copy(out[:8], b[:])
	return
}

// newMigTestCollector builds a collector whose initOnce has already fired so
// tests never bring up a real embedded DCGM engine.
func newMigTestCollector(q dcgmLatestValuesQuerier, initErr error) *dcgmWholeGPUCollector {
	c := newDCGMWholeGPUCollector(q)
	c.initErr = initErr
	c.initOnce.Do(func() {})
	return c
}

func Test_dcgmWholeGPUCollector_GpuInstanceSmUtil(t *testing.T) {
	t.Run("value is rounded to an integer percentage at FE_GPU_I", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(66.4)},
		}}
		col := newMigTestCollector(fake, nil)
		assert.Equal(t, col.GpuInstanceSmUtil(5, "MIG-x"), uint64(66))
		assert.Equal(t, fake.lastEntityGroup, dcgm.FE_GPU_I)
		assert.Equal(t, fake.lastID, uint(5))
		assert.DeepEqual(t, fake.lastFields, migProfUtilFields)
	})

	t.Run("querier error yields 0", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{err: errors.New("dcgm unreachable")}
		col := newMigTestCollector(fake, nil)
		assert.Equal(t, col.GpuInstanceSmUtil(5, "MIG-x"), uint64(0))
	})

	t.Run("empty result yields 0", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: nil}
		col := newMigTestCollector(fake, nil)
		assert.Equal(t, col.GpuInstanceSmUtil(5, "MIG-x"), uint64(0))
	})

	t.Run("non-zero field status yields 0", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: -17 /* DCGM_ST_NOT_SUPPORTED */, Value: dcgmDoubleBytes(99)},
		}}
		col := newMigTestCollector(fake, nil)
		assert.Equal(t, col.GpuInstanceSmUtil(5, "MIG-x"), uint64(0))
	})

	t.Run("cached init error degrades to 0 without hitting the querier", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(50)},
		}}
		col := newMigTestCollector(fake, errors.New("libdcgm.so.4 not found"))
		assert.Equal(t, col.GpuInstanceSmUtil(5, "MIG-x"), uint64(0))
		// The querier must not have been invoked at all.
		assert.Equal(t, fake.lastEntityGroup, dcgm.FE_NONE)
		assert.Equal(t, fake.lastID, uint(0))
	})

	t.Run("fraction below half rounds down to the nearest integer", func(t *testing.T) {
		fake := &fakeLatestValuesQuerier{vals: []dcgm.FieldValue_v1{
			{FieldID: dcgm.DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO, FieldType: dcgm.DCGM_FT_DOUBLE, Status: 0, Value: dcgmDoubleBytes(0.49)},
		}}
		col := newMigTestCollector(fake, nil)
		assert.Equal(t, col.GpuInstanceSmUtil(5, "MIG-x"), uint64(0))
	})
}
