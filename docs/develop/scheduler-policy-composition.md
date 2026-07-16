# Composable Scheduler Policy Chain + Mutex Policy

## Summary

HAMi's node- and GPU-level scheduling policies (`hami.io/node-scheduler-policy`,
`hami.io/gpu-scheduler-policy`) currently accept a single policy name
(`binpack`, `spread`, or `topology-aware`). Internally, `NodeScoreList.Less()`
and `DeviceUsageList.Less()` each hold a single `if/else` on that one string.

This has two consequences tracked in the v2.10 roadmap (#1889):

1. **No policy combination.** A cluster operator cannot say "prefer NUMA
   locality, then binpack as a tiebreaker" — only one dimension can be active
   at a time. This is the root cause of #1806 (GPU scheduler policy sort bug):
   `gpu_policy.go`'s `Less()` already *hardcodes* a two-dimension comparison
   (NUMA, then Score), but that combination isn't configurable or extensible
   to other dimensions like topology or binpack/spread.
2. **No mutual-exclusion policy.** There's no way to declare "pods A and B
   must never land on the same device/node," which is a hard constraint, not
   a scoring preference.

This proposal splits the work into two independent, additive changes.

> **Revision note:** this version addresses review feedback from
> gemini-code-assist and coderabbitai on the initial draft — specifically:
> NUMA sort direction was not policy-symmetric, empty chains could produce
> unstable sort order, `PolicyDimension` didn't account for the
> `NodeScoreList` case, chain construction/propagation at call sites wasn't
> specified, the mutex filter hook lacked pod context, node-scope mutex
> filtering placement was suboptimal, and the mutex check as originally
> written was vulnerable to a TOCTOU race. All are addressed below.

## Proposal

### Part 1 — Composable policy chain (fixes #1806)

Replace the single `Policy string` field's `if/else` in `Less()` with an
ordered list of comparators, evaluated lexicographically (first dimension
wins; ties fall through to the next).

#### Two separate interfaces, not one

The original draft proposed a single `PolicyDimension` interface operating
on `*DeviceListsScore`. That doesn't fit `NodeScoreList`, whose elements are
`*NodeScore`. We need two interfaces sharing only parsing/registration:

```go
// pkg/scheduler/policy/dimension.go (new)

// DeviceDimension is one pluggable GPU-level ordering rule.
type DeviceDimension interface {
    Name() string
    // Compare returns <0 if a should sort before b, 0 if tied, >0 otherwise.
    Compare(a, b *DeviceListsScore) int
}

// NodeDimension is one pluggable node-level ordering rule.
type NodeDimension interface {
    Name() string
    Compare(a, b *NodeScore) int
}
```

#### NUMA direction must match policy, not be fixed

The original draft's `numaDimension` always sorted NUMA descending. That's
wrong: today's actual behavior (`gpu_policy.go`) is policy-dependent —
`binpack` sorts NUMA **descending** with score ascending; `spread` (the
default) sorts NUMA **ascending** with score descending. A single fixed
direction silently breaks the default spread path. Fix: split into
direction-aware dimensions instead of one dimension with an implicit
direction:

```go
type numaAscDimension struct{}
func (numaAscDimension) Name() string { return "numa-asc" }
func (numaAscDimension) Compare(a, b *DeviceListsScore) int {
    return int(a.Device.Numa) - int(b.Device.Numa)
}

type numaDescDimension struct{}
func (numaDescDimension) Name() string { return "numa-desc" }
func (numaDescDimension) Compare(a, b *DeviceListsScore) int {
    return int(b.Device.Numa) - int(a.Device.Numa)
}

type binpackDimension struct{}
func (binpackDimension) Name() string { return "binpack" }
func (binpackDimension) Compare(a, b *DeviceListsScore) int {
    if a.Score < b.Score { return -1 }
    if a.Score > b.Score { return 1 }
    return 0
}

type spreadDimension struct{}
func (spreadDimension) Name() string { return "spread" }
func (spreadDimension) Compare(a, b *DeviceListsScore) int {
    return -binpackDimension{}.Compare(a, b)
}
```

Today's exact default behavior is therefore expressible as the chain
`numa-asc,spread`, and today's binpack behavior as `numa-desc,binpack` —
both bit-for-bit equivalent to current `Less()`, which is the backward
compatibility bar this proposal must clear.

#### `Less()` and guaranteed non-empty chain

```go
func (l DeviceUsageList) Less(i, j int) bool {
    for _, dim := range l.Chain {
        if c := dim.Compare(l.DeviceLists[i], l.DeviceLists[j]); c != 0 {
            return c < 0
        }
    }
    return false
}
```

An empty `Chain` would make every comparison return `false`, producing an
unstable sort. The parser guarantees `Chain` is never empty, but the
mapping is **not** "any legacy input → the default chain" — that would be
wrong, since it would collapse `binpack` into spread's behavior. The rule is:

- Recognized legacy values map to their specific equivalent chain:
  `"binpack"` → `[numa-desc, binpack]`, `"spread"` → `[numa-asc, spread]`.
- Only **empty or unrecognized** input falls back to the default chain
  (`[numa-asc, spread]`, matching today's actual default policy).

This keeps the invariant ("`Chain` is never empty") enforced in exactly one
place — the parser — without conflating "recognized" with "default."

#### Chain construction and propagation — explicit call sites

The original draft didn't specify where chains get built, which is where
the actual policy currently flows from config/annotation into the sort. This
must change at both existing entry points:

- `pkg/scheduler/scheduler.go` (`buildNodeUsage`): currently calls
  `util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, task)` and
  assigns the resulting raw string directly to `DeviceUsageList.Policy`.

  **This raw string is not only used for sorting.** A separate consumer,
  `pkg/device/nvidia/device.go`'s `Fit()`, independently re-derives the same
  policy string via its own `GetGPUSchedulerPolicyByPod` call to compute
  `needTopology`, which drives NVIDIA-specific device-combination selection
  logic that has nothing to do with `Less()`. If `ParseDeviceChain` returned
  only `[]DeviceDimension`, that would discard the raw string and leave this
  a coincidence — it would keep working today only because `Fit()` happens
  to re-read the annotation itself, not because this design guarantees it.
  That's fragile and not explicitly stated, which is what review feedback on
  this section correctly flagged.

  Fix: the parser returns a tagged result, not a bare chain, and
  `DeviceUsageList` keeps the raw string alongside the chain rather than
  replacing it:

  ```go
  type ParsedDevicePolicy struct {
      Chain        []DeviceDimension // used by Less()
      LegacyPolicy string            // raw input, always preserved verbatim
  }

  func ParseDeviceChain(policyStr string) ParsedDevicePolicy {
      switch policyStr {
      case util.GPUSchedulerPolicyBinpack.String():
          return ParsedDevicePolicy{
              Chain:        []DeviceDimension{numaDescDimension{}, binpackDimension{}},
              LegacyPolicy: policyStr,
          }
      case util.GPUSchedulerPolicyTopology.String():
          // Sort behavior for the fallback/tiebreak ordering matches
          // today's default (topology-aware isn't "binpack", so current
          // Less() already falls through to the spread branch) — but
          // LegacyPolicy is preserved so Fit()'s needTopology check keeps
          // working, unchanged and explicitly, not by coincidence.
          return ParsedDevicePolicy{
              Chain:        []DeviceDimension{numaAscDimension{}, spreadDimension{}},
              LegacyPolicy: policyStr,
          }
      case util.GPUSchedulerPolicySpread.String(), "":
          return ParsedDevicePolicy{
              Chain:        []DeviceDimension{numaAscDimension{}, spreadDimension{}},
              LegacyPolicy: policyStr,
          }
      default:
          // comma-separated chain (new format) parsed here; unrecognized
          // tokens dropped with a warning, empty result -> default chain.
      }
  }
  ```

  `DeviceUsageList.Policy string` becomes `DeviceUsageList.LegacyPolicy
  string` (renamed, not removed) plus the new `Chain []DeviceDimension`
  field — so any existing or future consumer that needs the raw string
  (like `Fit()`) has an explicit, typed field to read instead of an
  implicit assumption that it's unaffected. A compatibility test must
  assert `Fit()`'s `needTopology` behavior is bit-for-bit unchanged when
  `LegacyPolicy == "topology-aware"`.
- `pkg/scheduler/score.go` (`calcScoreWithOptions`): currently resolves
  `userNodePolicy` from the pod annotation or `config.NodeSchedulerPolicy`
  and assigns it to `NodeScoreList.Policy`. This needs the analogous
  `policy.ParseNodeChain(policyStr string) ParsedNodePolicy` call and the
  same `Chain` + `LegacyPolicy` field split on `NodeScoreList` (node-level
  policy has no equivalent second consumer today, but keeping the two
  structs symmetric avoids the same trap resurfacing later).
- Both parsers accept the existing single-value strings (`"binpack"`,
  `"spread"`) as well as the new comma-separated form
  (`"numa-asc,spread"`), so this is additive at the annotation/config
  layer — no existing Helm values or annotations need to change.

#### Config/annotation format

```yaml
hami.io/gpu-scheduler-policy: "numa-asc,binpack"
```

Bare legacy values (`"binpack"`, `"spread"`) continue to work, resolving via
the parser to their equivalent full chain as shown above.

**`topology-aware` is a distinct case, not a scoring dimension.** As
detailed above, it is handled explicitly by `ParseDeviceChain` (not treated
as an unrecognized token) and its raw string is preserved via
`LegacyPolicy` specifically because a second consumer (`Fit()`'s
`needTopology` check) depends on it directly. A compatibility test
asserting `hami.io/gpu-scheduler-policy: "topology-aware"` produces
identical sort *and* fit/allocation behavior before and after this change
is required before this is considered complete. Whether `topology-aware`
can later be expressed as a composable dimension (e.g. combined with
binpack as a tiebreaker) is left as a follow-up, not part of this
proposal's initial scope.

Other unknown dimension names (i.e., typos, not `topology-aware`) are
dropped with a warning log rather than causing a hard failure; if dropping
leaves the chain empty, it falls back to the default chain per the
guarantee above.

### Part 2 — Exclusion (`mutex`) policy (new)

**Naming change from the original draft:** the project already uses the
`hami.io/mutex.lock` node annotation (`pkg/util/nodelock`) as part of the
existing per-node scheduling-lock mechanism. Calling the new feature
`mutex-group` would collide with that existing concept even though the
literal keys differ. This proposal renames it to
`hami.io/exclusion-group` / `hami.io/exclusion-scope` to avoid confusion.

Unlike the sort dimensions above, exclusion is a hard filter, not a scoring
preference: "these pods must never land on the same device/node."

#### The original hook doesn't have enough context — corrected design

The initial draft proposed reusing `CustomFilterRule` from
`pkg/device/devices.go`. That signature only receives already-allocated
devices and the candidate device/request — it has no access to the
candidate pod's own annotations, no exclusion scope, and (for node-scope) no
visibility into which *other* pods occupy the candidate node. That's not
enough to implement this correctly, so exclusion is instead implemented as
scheduler-level filtering with explicit inputs, not a device-plugin hook:

- New pod-level state needed at filter time: the scheduling pod's own
  `hami.io/exclusion-group` value, and — for every pod already bound to the
  candidate device/node — their `hami.io/exclusion-group` value. The
  scheduler already tracks per-device `PodInfos []*PodInfo` on
  `DeviceUsage`; this proposal needs to confirm whether `PodInfo` carries
  annotations already or whether a pod-lister/informer lookup by
  namespace/name is required, and if so, that lookup must go through an
  indexed cache (not a live API read) to avoid latency under high pod churn.
  This is called out as an open question below rather than assumed.

- **Empty-group rule (explicit contract, not left implicit):** a missing or
  empty `hami.io/exclusion-group` value means "this pod participates in no
  exclusion group." Two pods that both lack the annotation must **never**
  be treated as matching — the check is only ever a comparison between two
  *non-empty, equal* group values. Without this rule, unannotated pods
  would accidentally exclude each other, since two empty strings are
  trivially "equal."

- **Scope-source rule (symmetric, not one-sided):** the original draft read
  `hami.io/exclusion-scope` from the scheduling pod only. That's broken:
  if pod A (same group, `exclusion-scope: node`) already occupies node1,
  and pod B (same group, default `exclusion-scope: device`) is being
  placed, a one-sided check only consults B's own scope and would happily
  place B on a *different* device on node1 — silently violating A's
  node-wide exclusion. The "must never co-locate" guarantee has to hold
  regardless of which pod's annotation is consulted.

  Fix: **the stricter scope always wins, from either side.** When checking
  a candidate device/node against an incumbent pod in the same exclusion
  group, the effective scope for that check is the broader of the two —
  if *either* the scheduling pod or the incumbent pod specifies
  `node`, the check is node-scoped; only if *both* specify (or default to)
  `device` is the check device-scoped. This makes the guarantee hold no
  matter which pod happens to be scheduled first, at the cost of the
  scheduling pod needing to know the scope of every incumbent pod sharing
  its group — which is already required data per the empty-group rule
  above (the scheduler must inspect every same-group incumbent's
  annotations regardless).

#### Device-scope vs node-scope, and where each runs

- **Device-scope** (default): filtering happens inside `fitInDevices`
  (`pkg/scheduler/score.go`), after per-device data is available, before
  scoring that device.
- **Node-scope**: filtering should run at the **top of the per-node
  goroutine** in `calcScoreWithOptions`, before `fitInDevices` is called at
  all — there's no reason to run device fitting/scoring logic for a node
  that's already excluded outright. This corrects the original draft, which
  put the check inside `fitInDevices` for both scopes.

#### Concurrency safety (fixes the TOCTOU gap)

A plain "read current occupants, then decide" check races: two concurrent
scheduling attempts (node scoring already runs concurrently per node in
`calcScoreWithOptions`) can both observe no conflict and then bind
concurrently, violating the "must never co-locate" guarantee. Rather than
inventing new locking, this proposal reuses the **existing**
`pkg/util/nodelock` mechanism (`LockNode` / `SetNodeLock` / `ReleaseNodeLock`),
which already serializes bind-time operations per node for exactly this
class of race. Exclusion-group membership checks and the eventual bind are
performed while holding that per-node lock, giving atomicity without a new
subsystem.

## User Stories

### Story 1 — Policy combination

- cluster resources:
  - node1: 4 GPUs, split across NUMA 0 and NUMA 1
- request:
  - pod1: needs 2 GPUs, annotation `hami.io/gpu-scheduler-policy: "numa-asc,binpack"`
- scheduler result:
  - GPUs are first grouped by ascending NUMA node, then binpacked within the
    winning NUMA group.

### Story 2 — Exclusion

- request:
  - pod1, pod2 both carry `hami.io/exclusion-group: "team-a-exclusive"`,
    both default to `exclusion-scope: device`
- scheduler result:
  - pod2 is filtered away from any device pod1 already occupies, even if
    that device would otherwise score highest — and the check/bind happens
    under the same per-node lock `nodelock` already provides, so two
    concurrent scheduling attempts can't both succeed.

### Story 3 — Asymmetric scope (the case that motivated the symmetric rule)

- request:
  - pod1: `exclusion-group: "team-a-exclusive"`, `exclusion-scope: node`,
    already bound to node1
  - pod2: `exclusion-group: "team-a-exclusive"`, `exclusion-scope: device`
    (default) — a *different*, otherwise-free device on node1 would
    normally score highest for pod2
- scheduler result:
  - pod2 is still filtered away from all of node1, not just pod1's
    specific device. The effective scope for this check is the broader of
    the two pods' scopes (`node`, from pod1), even though pod2's own
    annotation only says `device`. A one-sided check (pod2's scope only)
    would have incorrectly allowed this placement.

## Design Details / Open Questions

- **Confirm `PodInfo` annotation availability**: does `DeviceUsage.PodInfos`
  already carry the annotations needed for exclusion-group checks, or does
  this require a pod-lister lookup? If the latter, must be backed by an
  informer cache, not a live API call, to avoid scheduling latency at scale.
- Dimension chain length: cap at a small number (e.g. 3) to keep `Less()`
  cheap under high pod churn — needs a benchmark once implemented.
- Backward compatibility: `numa-asc,spread` and `numa-desc,binpack` must be
  verified bit-for-bit equivalent to current `Less()` output via a
  legacy-mode test suite before this is considered complete, not just
  reasoned about in this doc.
- Should chain-length validation reject configs with conflicting dimensions
  (e.g. both `binpack` and `spread` in the same chain)? Leaning yes — hard
  error at parse time rather than silently taking the first one.

## References

- Roadmap: #1889 ("New schedule policy-(mutex)", "Scheduler policy
  combination")
- Bug: #1806 (GPU scheduler policy sort bug)
- Existing pattern to extend: `pkg/scheduler/policy/gpu_policy.go`
  (`DeviceUsageList.Less`), `pkg/scheduler/policy/node_policy.go`
  (`NodeScoreList.Less`)
- Existing per-node locking, reused for exclusion atomicity:
  `pkg/util/nodelock/nodelock.go`
- Prior art for proposal format: `docs/develop/scheduler-policy.md`,
  `docs/develop/dynamic-mig.md`