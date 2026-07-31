# Composable Scheduler Policy Chain

## Summary

**Scope note:** this doc originally proposed both a composable policy chain
and a pod-group mutex/exclusion policy. The NUMA sort bug (#1806) and a
device-level mutex policy have since merged via #2011, which folded in both
concerns. Per review feedback, this revision drops everything #2011 already
covers and narrows to the one part of the original v2.10 roadmap item
(#1889) that's still open: making `DeviceUsageList.Less()` composable,
rather than a fixed set of hardcoded branches.

`pkg/scheduler/policy/gpu_policy.go`'s `Less()`, as merged today, looks like
this:

```go
func (l DeviceUsageList) Less(i, j int) bool {
    ...
    if l.Policy == util.GPUSchedulerPolicyMutex.String() {
        // busy-first, idle-tail
    }
    if l.NumaBind {
        // NUMA-grouped ordering, binpack/spread direction
    }
    // default: score primary, NUMA tiebreaker, binpack/spread direction
}
```

This correctly fixes the sort bug and adds mutex, but the mechanism is a
fixed sequence of mutually-exclusive `if` blocks keyed off `Policy string` +
`NumaBind bool`. Three consequences:

1. **Every new concern needs another `Less()` edit.** Mutex and numa-bind
   were each added as a new top-level branch. The next one (e.g. the
   resource-weighted scoring being proposed in #2220, or a future
   topology-combination need) means editing this function again, and the
   branches are already interacting in ways that aren't obviously
   commutative (mutex short-circuits before numa-bind is even checked).
2. **Dimensions can't be combined or reordered by the operator.** You can't
   express "numa-bind, then weighted-score, then binpack" — only whichever
   fixed combination the current `if` chain happens to encode.
3. **No per-dimension unit testing.** Today's tests exercise `Less()` as one
   monolithic function; a bug in the mutex branch's tiebreak (line 59,
   `return ni < nj`) can't be tested in isolation from the numa-bind or
   score-primary branches.

This proposal doesn't change today's default behavior — it's a refactor of
*how* the existing branches are expressed, so future dimensions are
additive instead of requiring another edit to a growing `if` chain.

## Proposal

Extract each existing branch into an independently testable comparator, and
compose them as an ordered chain evaluated lexicographically (first
dimension that returns non-zero wins).

```go
// pkg/scheduler/policy/dimension.go (new)

// DeviceDimension is one independently pluggable GPU-level ordering rule.
type DeviceDimension interface {
    Name() string
    // Compare returns <0 if a should sort before b, 0 if tied, >0 otherwise.
    Compare(a, b *DeviceListsScore) int
}
```

Today's three branches become three dimensions, each verified against the
current `Less()` output bit-for-bit:

```go
type mutexDimension struct{}
func (mutexDimension) Name() string { return "mutex" }
func (mutexDimension) Compare(a, b *DeviceListsScore) int {
    if a.Device.Used != b.Device.Used {
        if a.Device.Used > b.Device.Used { return -1 }
        return 1
    }
    return 0 // falls through to numa tiebreak dimension
}

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

type scoreBinpackDimension struct{}
func (scoreBinpackDimension) Name() string { return "score-binpack" }
func (scoreBinpackDimension) Compare(a, b *DeviceListsScore) int {
    if a.Score < b.Score { return -1 }
    if a.Score > b.Score { return 1 }
    return 0
}

type scoreSpreadDimension struct{}
func (scoreSpreadDimension) Name() string { return "score-spread" }
func (scoreSpreadDimension) Compare(a, b *DeviceListsScore) int {
    return -scoreBinpackDimension{}.Compare(a, b)
}
```

`Less()` becomes a chain walk instead of nested `if` blocks:

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

### Exact equivalence to today's merged behavior

The chain construction must reproduce today's four resolved paths exactly —
this is the backward-compatibility bar, verified by a legacy-mode test
comparing chain-based `Less()` output against the current implementation
across randomized device sets before this is considered complete:

| Today's condition | Equivalent chain |
|---|---|
| `Policy == mutex` | `[mutex, numa-asc]` |
| `NumaBind && binpack` | `[numa-desc, score-binpack]` |
| `NumaBind && !binpack` (spread) | `[numa-asc, score-spread]` |
| default, `binpack` | `[score-binpack, numa-asc]` |
| default, spread | `[score-spread, numa-asc]` |

### Construction and propagation

`DeviceUsageList` gains a `Chain []DeviceDimension` field. `pkg/scheduler/
scheduler.go`'s `buildNodeUsage` (or equivalent construction site) resolves
`Policy` + `NumaBind` into the matching chain from the table above via a new
`policy.BuildDeviceChain(policyStr string, numaBind bool) []DeviceDimension`
— this is additive, not a change to the annotation surface. No user-facing
annotation changes are proposed in this revision; `Policy string` and
`NumaBind bool` remain the actual configuration inputs, this only changes
what they compile down to internally.

A **follow-up** (not part of this proposal) could expose the chain directly
via annotation (e.g. `hami.io/gpu-scheduler-policy: "numa-asc,score-binpack"`)
for operators who want combinations the current four presets don't cover.
That's deliberately out of scope here to keep this change to an internal
refactor with zero behavior change, reviewable independently of any new
user-facing surface.

## Why now

`#2220` (configurable resource weights for binpack/spread scoring) is
proposing to change how `ComputeScore` weighs used/core/mem — a good
concrete example of a dimension that would slot in as a `Compare`
implementation once this lands, rather than adding a fourth flag alongside
`Policy` and `NumaBind`.

## Design Details / Open Questions

- Should `BuildDeviceChain` live in `pkg/scheduler/policy` alongside the
  dimensions, or in `pkg/scheduler/scheduler.go` next to where `Policy`/
  `NumaBind` are currently resolved from annotations? Leaning toward
  `policy` package, to keep dimension definitions and their composition
  logic together.
- `NodeScoreList` (node-level policy) has its own, simpler `Less()` with no
  mutex/numa-bind equivalent today. Out of scope for this revision; worth a
  follow-up only if node-level policy grows similarly hardcoded branches.
- Test plan: table-driven legacy-equivalence test (chain output vs. current
  `Less()` output) plus per-dimension unit tests, before any behavior
  changes are proposed on top of this refactor.

## References

- Roadmap: #1889 (scheduler policy combination remains open after mutex/#1806
  closed via #2011)
- Merged: #2011 (mutex policy + score-primary/NUMA-tiebreak fix, supersedes
  the sort-bug and mutex portions of this doc's earlier draft)
- Related, would benefit from this refactor: #2220 (configurable resource
  weights for binpack/spread scoring)
- Current implementation this proposal refactors:
  `pkg/scheduler/policy/gpu_policy.go` (`DeviceUsageList.Less`)
