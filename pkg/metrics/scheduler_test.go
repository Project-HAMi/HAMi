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

import (
	"strings"
	"testing"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSchedulerOutcomeMetrics(t *testing.T) {
	m := NewSchedulerOutcomeMetrics()
	m.ObserveAllocation("filter", "NVIDIA")
	m.ObserveAllocationFailure("filter", "NVIDIA", FailureReasonNoFit)
	m.ObserveBindRollback("bind", "NVIDIA", FailureReasonBind)
	m.ObserveStaleReservation("reconcile", "NVIDIA", FailureReasonStale)
	m.ObserveReconciliationError("reconcile", "NVIDIA", FailureReasonStale)

	want := strings.NewReader(`
# HELP hami_scheduler_allocations_total Successful HAMi device allocations.
# TYPE hami_scheduler_allocations_total counter
hami_scheduler_allocations_total{device_type="NVIDIA",failure_reason="none",phase="filter"} 1
# HELP hami_scheduler_allocation_failures_total HAMi device allocation failures.
# TYPE hami_scheduler_allocation_failures_total counter
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="no_fit",phase="filter"} 1
# HELP hami_scheduler_bind_rollbacks_total HAMi bind operations that released a reservation after failure.
# TYPE hami_scheduler_bind_rollbacks_total counter
hami_scheduler_bind_rollbacks_total{device_type="NVIDIA",failure_reason="bind",phase="bind"} 1
# HELP hami_scheduler_stale_reservations_total Stale HAMi device reservations encountered or removed.
# TYPE hami_scheduler_stale_reservations_total counter
hami_scheduler_stale_reservations_total{device_type="NVIDIA",failure_reason="stale",phase="reconcile"} 1
# HELP hami_scheduler_reconciliation_errors_total HAMi scheduler reconciliation errors.
# TYPE hami_scheduler_reconciliation_errors_total counter
hami_scheduler_reconciliation_errors_total{device_type="NVIDIA",failure_reason="stale",phase="reconcile"} 1
`)
	if err := promtestutil.CollectAndCompare(m, want,
		"hami_scheduler_allocations_total",
		"hami_scheduler_allocation_failures_total",
		"hami_scheduler_bind_rollbacks_total",
		"hami_scheduler_stale_reservations_total",
		"hami_scheduler_reconciliation_errors_total",
	); err != nil {
		t.Fatalf("unexpected scheduler outcome metrics:\n%s", err)
	}
}

func TestSchedulerFailureReasons(t *testing.T) {
	m := NewSchedulerOutcomeMetrics()
	for _, reason := range []SchedulerFailureReason{
		FailureReasonNoFit,
		FailureReasonLookup,
		FailureReasonIdentity,
		FailureReasonLock,
		FailureReasonAnnotationPatch,
		FailureReasonBind,
		FailureReasonInternal,
		FailureReasonStale,
	} {
		m.ObserveAllocationFailure("filter", "NVIDIA", reason)
	}

	want := strings.NewReader(`
# HELP hami_scheduler_allocation_failures_total HAMi device allocation failures.
# TYPE hami_scheduler_allocation_failures_total counter
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="annotation_patch",phase="filter"} 1
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="bind",phase="filter"} 1
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="identity",phase="filter"} 1
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="internal",phase="filter"} 1
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="lock",phase="filter"} 1
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="lookup",phase="filter"} 1
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="no_fit",phase="filter"} 1
hami_scheduler_allocation_failures_total{device_type="NVIDIA",failure_reason="stale",phase="filter"} 1
`)
	if err := promtestutil.CollectAndCompare(m, want, "hami_scheduler_allocation_failures_total"); err != nil {
		t.Fatalf("unexpected failure reasons:\n%s", err)
	}
}
