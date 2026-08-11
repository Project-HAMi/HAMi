# HAMi Support for Sidecar Container GPU Resource Accounting — Design

## Problem Summary

Native sidecar containers are declared in
`spec.initContainers` with `restartPolicy: Always`, but unlike regular init
containers they run for the whole pod lifetime, next to the app
containers. HAMi ([initContainer-design.md](./initContainer-design.md))
classifies only by which list a container appears in. There is no
`RestartPolicy` check anywhere, so a sidecar gets run-to-completion
semantics it doesn't have.

This design adds sidecars as a third container class.

## The Problem in HAMi Today

**1. Under-accounting: `CollapseInitContainerUsage`
(`pkg/device/initContainer.go`).** Classification is by index
(`cidx < numInit`), so a sidecar lands in the init-peak (`max`) bucket. A
4000 MiB sidecar plus a 4000 MiB app container on one card is accounted
as `max(4000, 4000) = 4000`; real demand is 8000, so the scheduler can
oversubscribe the card.

**2. The shrink gate never opens.** The shrink waits for every init
container to be `Terminated`. A sidecar never terminates, so pods with one
never release their init containers' memory. Case 4 of the init design
stops working.

**3. A bug waiting to happen — `AppContainersOnlyDeviceUsage`.** The shrink
target skips all init containers, sidecars included — unreachable today
only because gate (2) never opens. Fix the gate but not the target, and a
running sidecar's usage gets dropped from accounting. The two have to
change together.

## The Core Idea

The upstream formula (what the apiserver itself charges):

```
effective = max( max over non-sidecar init_i ( init_i + sum(sidecars started before init_i) ),
                 sum(apps) + sum(all sidecars) )
```

**Simplification** (same spirit as the init design's assumption): use
`sum(all sidecars)` instead of the ordering-aware term. Per device UUID
and resource (count, mem, cores):

```
effective[uuid] = sidecar_sum[uuid] + max( init_peak[uuid], app_sum[uuid] )
```

where `init_peak` is the max over non-sidecar init containers. Sidecars are
just a floor on top of the existing formula. It can only over-reserve, and
only during the init phase in an ordering corner case.

**Classification:**

```
isSidecar(c) := c ∈ spec.initContainers && c.RestartPolicy != nil &&
                *c.RestartPolicy == corev1.ContainerRestartPolicyAlways
```

The nil check makes this safe everywhere: absent field means no sidecars,
behavior stays exactly as today. No version gating, no new config.

## Proposal

- **Admission quota check:**
  `effectiveReq = sidecarReq + max(initReq, appReq)`; memory factor applied
  once to the result.
- **Scheduler fit & scoring:** a steady-state pass fits sidecars plus app
  containers cumulatively against a fresh node copy; the init pass fits
  each non-sidecar init against a fresh copy pre-charged with the sidecar
  usage; merge per UUID via `max()`.
- **Usage recording:** `CollapseInitContainerUsage` routes sidecars into
  the app-sum bucket and adds their sum to the init peak; add/update/delete
  symmetry stays as it is.

Annotations don't change, but a sidecar keeps its position in the init
range of `hami.io/vgpu-devices-allocated`. Readers (device plugin
`Allocate`, WebUI) must classify through `pod.Spec`, not by index.

### Cases (one node, single 24Gi GPU)

- **Oversubscription prevented:** sidecar 10Gi + app 10Gi. Before:
  accounted `max(10,10) = 10Gi`, so a later 12Gi pod schedules → real
  demand 32Gi. After: accounted 20Gi → the 12Gi pod is rejected.
- **Shrink restored:** init 20Gi + sidecar 2Gi + app 10Gi. Before: gate
  never opens, 20Gi held forever. After: admission
  `2 + max(20,10) = 22Gi`; once the init exits 0, usage shrinks to 12Gi.
- No sidecars, or terminal phase: identical to the init design.

## Shrink Rules

Same three rules as the init design, with **non-sidecar** inserted: shrink
to steady-state usage (apps + sidecars) once non-sidecar inits exit 0;
hold on non-zero exit; zero at terminal phase. A sidecar can crash-loop
through `Terminated` states; since sidecars are out of the gate entirely,
restarts don't affect it — worth a test. If all init containers are
sidecars, the gate is satisfied immediately and the shrink recomputes the
stored value (delta 0). `initContainerResourceReleased` keeps its
semantics. Rename `AppContainersOnlyDeviceUsage` (e.g.
`SteadyStateDeviceUsage`) so an un-migrated caller fails to compile instead
of silently dropping sidecar usage.

## Interaction with Kubernetes ResourceQuota

The apiserver already charges the sidecar-aware formula, so both sides
agree at admission. As before, the shrink only frees capacity inside
HAMi — fine, since the sidecar's share has to be held until the pod ends
anyway.
