# Dynamic MIG instance lifecycle

HAMi keeps dynamically created NVIDIA MIG instances available after a Pod releases them. This avoids repeated GI/CI creation when another Pod requests the same profile and placement.

## Ownership requirement

Dynamic MIG mode requires HAMi to be the only controller that creates or destroys MIG instances on GPUs managed by HAMi. Running another MIG lifecycle controller on the same GPU is unsupported because HAMi cannot safely distinguish or coordinate external layout changes.

HAMi writes one ownership record per dynamic MIG instance under `/var/run/cdi/hami-dynamic-mig-state`. The record remains while the instance is active or idle and is removed only after permanent destruction. This record is independent of the container injection strategy, so restart recovery also works in legacy non-CDI mode.

At startup, HAMi validates each ownership record against the live MIG UUID, parent GPU, profile, placement, GI ID, and CI ID before restoring the instance as idle. Existing GI/CI pairs without a valid HAMi ownership record are preserved but are not reused or reclaimed. Operators may set `devices.nvidia.adoptExistingMIGInstances=true` only when HAMi exclusively owns the complete MIG layout on the node. This explicit opt-in claims unmarked GI/CI pairs and creates ownership records for them.

## Lifecycle

```text
Create ──> Active ──Pod release──> Idle ──matching request──> Active
                                      |
                                      └─placement pressure──> Reclaim ──> Delete
```

- `Active` instances are protected from reclamation.
- A confirmed Pod release changes an instance to `Idle`; it does not destroy the GI/CI.
- A request with the same GPU, profile slice count, and placement reuses the idle instance and its MIG UUID.
- If an idle instance overlaps a new scheduler-selected placement, HAMi destroys only the blocking idle instance before creating the new layout.
- A failed allocation still destroys instances created by that failed request instead of caching partial work.
- NVML errors fail closed. An instance that cannot be destroyed is retained in an error state and is not reused.

Idle TTL and cache-size limits are optional roadmap extensions. The current lifecycle reclaims idle instances only when their placement blocks a new allocation.

## Restart recovery

At device-plugin startup, HAMi first adopts runtime identities referenced by active Pod annotations. It then loads the durable ownership records and reconciles them with the actual NVML GI/CI layout. Verified HAMi-owned instances without an active Pod are restored as idle. Unknown instances are preserved unless explicit adoption is enabled. With adoption enabled, complete GI/CI pairs are restored as idle, and orphan GIs without a CI are removed only on GPUs that have no live Pod allocation. Orphan GIs on busy GPUs are always preserved.

If Pod state, MIG identity, or NVML geometry cannot be verified, startup recovery returns an error instead of modifying uncertain hardware state.

## CDI lifecycle

The CDI specification belongs to the MIG instance, not to a Pod:

- The CDI file remains while an instance is `Active` or `Idle`.
- Reusing an idle instance reuses and validates its existing CDI entry.
- The CDI file is removed only after the GI/CI is permanently destroyed.
- Failed CDI removal is queued and retried by the existing reconciler.
- A tracked instance left in `Error` after a transient NVML cleanup failure is retried by reconciliation. Its CDI entry is removed only after hardware destruction succeeds.

Legacy non-CDI allocation remains unchanged.

## Validation

The state machine, restart recovery, exact reuse, placement-pressure reclamation, active-instance protection, CDI retention, and CDI cleanup retry can be tested with mocked NVML and CDI interfaces. Before release, changes affecting allocation must also be validated on a MIG-capable NVIDIA GPU as required by `CONTRIBUTING.md`.
