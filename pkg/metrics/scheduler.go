/*
Copyright 2026 The HAMi Authors.

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

package metrics

import "github.com/prometheus/client_golang/prometheus"

// SchedulerOutcomeMetrics records scheduler allocation and recovery outcomes.
// All label values are controlled by the scheduler and must remain bounded.
type SchedulerOutcomeMetrics struct {
	allocations          *prometheus.CounterVec
	allocationFailures   *prometheus.CounterVec
	bindRollbacks        *prometheus.CounterVec
	staleReservations    *prometheus.CounterVec
	reconciliationErrors *prometheus.CounterVec
}

var schedulerOutcomeLabels = []string{"phase", "device_type", "failure_reason"}

func newSchedulerCounter(name, help string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, schedulerOutcomeLabels)
}

// NewSchedulerOutcomeMetrics creates the counters used by the scheduler.
func NewSchedulerOutcomeMetrics() *SchedulerOutcomeMetrics {
	return &SchedulerOutcomeMetrics{
		allocations:          newSchedulerCounter("hami_scheduler_allocations_total", "Successful HAMi device allocations."),
		allocationFailures:   newSchedulerCounter("hami_scheduler_allocation_failures_total", "HAMi device allocation failures."),
		bindRollbacks:        newSchedulerCounter("hami_scheduler_bind_rollbacks_total", "HAMi bind operations that released a reservation after failure."),
		staleReservations:    newSchedulerCounter("hami_scheduler_stale_reservations_total", "Stale HAMi device reservations encountered or removed."),
		reconciliationErrors: newSchedulerCounter("hami_scheduler_reconciliation_errors_total", "HAMi scheduler reconciliation errors."),
	}
}

func (m *SchedulerOutcomeMetrics) labels(phase, deviceType, reason string) prometheus.Labels {
	return prometheus.Labels{"phase": phase, "device_type": deviceType, "failure_reason": reason}
}

// ObserveAllocation records a successful allocation.
func (m *SchedulerOutcomeMetrics) ObserveAllocation(phase, deviceType string) {
	m.allocations.With(m.labels(phase, deviceType, "none")).Inc()
}

// ObserveAllocationFailure records a failed allocation using a bounded reason.
func (m *SchedulerOutcomeMetrics) ObserveAllocationFailure(phase, deviceType, reason string) {
	m.allocationFailures.With(m.labels(phase, deviceType, reason)).Inc()
}

// ObserveBindRollback records a bind failure that released its reservation.
func (m *SchedulerOutcomeMetrics) ObserveBindRollback(phase, deviceType, reason string) {
	m.bindRollbacks.With(m.labels(phase, deviceType, reason)).Inc()
}

// ObserveStaleReservation records a stale reservation encountered or removed.
func (m *SchedulerOutcomeMetrics) ObserveStaleReservation(phase, deviceType, reason string) {
	m.staleReservations.With(m.labels(phase, deviceType, reason)).Inc()
}

// ObserveReconciliationError records an error while rebuilding scheduler state.
func (m *SchedulerOutcomeMetrics) ObserveReconciliationError(phase, deviceType, reason string) {
	m.reconciliationErrors.With(m.labels(phase, deviceType, reason)).Inc()
}

// Collect implements prometheus.Collector.
func (m *SchedulerOutcomeMetrics) Collect(ch chan<- prometheus.Metric) {
	m.allocations.Collect(ch)
	m.allocationFailures.Collect(ch)
	m.bindRollbacks.Collect(ch)
	m.staleReservations.Collect(ch)
	m.reconciliationErrors.Collect(ch)
}

// Describe implements prometheus.Collector.
func (m *SchedulerOutcomeMetrics) Describe(ch chan<- *prometheus.Desc) {
	m.allocations.Describe(ch)
	m.allocationFailures.Describe(ch)
	m.bindRollbacks.Describe(ch)
	m.staleReservations.Describe(ch)
	m.reconciliationErrors.Describe(ch)
}
