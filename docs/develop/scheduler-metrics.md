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
pod, namespace, node, UUID, or request identifiers. `failure_reason` uses this
complete, fixed set:

| Value | Meaning |
| --- | --- |
| `none` | The operation succeeded. |
| `no_fit` | No candidate node could satisfy the device request. |
| `lookup` | A required pod or node could not be read from the scheduler cache. |
| `identity` | The bind request did not match the current pod identity or target. |
| `lock` | Device reservation locking failed. |
| `annotation_patch` | A required pod annotation update failed. |
| `bind` | Another bind operation failed. |
| `internal` | Internal usage, scoring, discovery, or cleanup failed. |
| `stale` | Cached allocation state was missing, inconsistent, or obsolete. |

A successful filter reservation uses `failure_reason="none"`; bind failures
are reported by the failure and rollback counters. Reconciliation counters
describe observations, so a condition encountered during multiple
reconciliation cycles may increment more than once.
