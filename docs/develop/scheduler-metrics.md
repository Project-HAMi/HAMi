# Scheduler metrics

HAMi exposes scheduler metrics on the configured scheduler metrics endpoint.
The allocation outcome metrics use only bounded label values:

| Metric | Meaning |
| --- | --- |
| `hami_scheduler_allocations_total` | Successful device reservations recorded by the filter phase. |
| `hami_scheduler_allocation_failures_total` | Filter and bind allocation failures. |
| `hami_scheduler_bind_rollbacks_total` | Bind failures that release a reservation. |
| `hami_scheduler_stale_reservations_total` | Stale reservations encountered or removed. |
| `hami_scheduler_reconciliation_errors_total` | Errors while rebuilding scheduler state. |

The labels are `phase`, `device_type`, and `failure_reason`. They never contain
pod, namespace, node, UUID, or request identifiers. Failure reasons are fixed
categories such as `no_fit`, `annotation_patch`, `unknown_device`, and
`invalid_mig_reservation`. A successful filter reservation uses
`failure_reason="none"`; bind failures are reported by the failure and
rollback counters. Reconciliation counters describe observations, so a
condition encountered during multiple reconciliation cycles may increment more
than once.
