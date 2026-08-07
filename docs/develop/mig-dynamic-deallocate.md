# Dynamic MIG Architecture

## Context

NVIDIA MIG divides a physical GPU into hardware-isolated compute instances. GPU models expose profile capacity and placement rules through NVML, while Kubernetes schedules workloads through declarative resources.

Dynamic MIG connects these layers through a reservation-first architecture. The node publishes hardware capability, the scheduler reserves an exact profile and placement, and the device plugin realizes that reservation during container allocation. Pod metadata carries the allocation identity across the workload lifecycle.

## Design Goals

The architecture pursues five outcomes:

1. Hardware capability originates from the node that owns the GPU.
2. Scheduling decisions include physical MIG placement.
3. Runtime realization follows the scheduler reservation exactly.
4. Workload metadata supports reconciliation and restart recovery.
5. Cross-component contracts remain compact and stable as GPU density grows.

## Principles

### Hardware authority

NVML defines profile capacity and legal placement. Device plugin discovery translates this information into a scheduler-facing capability contract.

### Explicit reservation

The scheduler selects the physical GPU, MIG profile, and placement before binding. Each accepted allocation immediately contributes to scheduler occupancy.

### Clear ownership

The scheduler owns placement policy. The device plugin owns hardware mutation. Pod annotations provide the durable handoff between both responsibilities.

### Convergent lifecycle

A stable reservation key supports idempotent realization. Reconciliation aligns managed hardware with active workload reservations.

## Architecture

```text
                        Kubernetes control plane

  +----------------+       Node capability       +----------------+
  | Device Plugin  | --------------------------> | HAMi Scheduler |
  |                |                             |                |
  | NVML discovery |       Pod reservation       | Placement      |
  | GI/CI manager  | <-------------------------- | policy         |
  | Reconciler     |                             | Capacity model |
  +-------+--------+                             +--------+-------+
          |                                               |
          | exact GI/CI realization                       | bind
          v                                               v
  +----------------+                             +----------------+
  | NVIDIA GPU     |                             | Workload Pod   |
  | MIG topology   |                             | Allocation     |
  | and instances  |                             | annotation     |
  +----------------+                             +----------------+
```

### Device plugin

The device plugin is the node hardware authority. It discovers allowlisted profiles, publishes compact capability, prepares MIG-ready GPUs, realizes reservations, records runtime MIG UUIDs, adopts active instances after restart, and reconciles managed instances with active Pods.

### HAMi scheduler

The scheduler is the policy and reservation authority. It reads node capability, reconstructs topology occupancy, matches workload demand to a profile, selects a legal placement, and persists the reservation before binding.

### Pod allocation record

The Pod allocation annotation is the persistent system record connecting scheduling, runtime realization, reconciliation, and restart recovery.

## Capability Contract

The device plugin publishes MIG profiles inside the NVIDIA node registration annotation. The contract carries scheduler-relevant fields:

```json
{
  "name": "2g.10gb",
  "memoryMB": 9984,
  "core": 29,
  "sliceCount": 2,
  "placements": [
    {"start": 0, "size": 2},
    {"start": 2, "size": 2},
    {"start": 4, "size": 2}
  ]
}
```

| Field | Purpose |
| --- | --- |
| `name` | Profile identity across scheduler and device plugin |
| `memoryMB` | Workload capacity matching |
| `core` | Scheduler resource accounting |
| `sliceCount` | Deterministic profile ordering |
| `placements` | Legal topology choices reported by NVML |

Device-local discovery data stays inside the device plugin process. This compact boundary controls node annotation growth and keeps the shared API aligned with scheduling needs.

The profile allowlist expresses cluster policy, while NVML supplies capacity and topology. Together they define the capability visible to the scheduler.

## Reservation Contract

The scheduler stores reservations in `hami.io/vgpu-mig-allocations`:

```json
{
  "containerIndex": 0,
  "deviceIndex": 0,
  "gpuUUID": "GPU-xxxxxxxx",
  "profile": "2g.10gb",
  "placement": {"start": 2, "size": 2},
  "migUUID": "MIG-xxxxxxxx",
  "gpuInstanceID": 4,
  "computeInstanceID": 0
}
```

The scheduler writes container identity, parent GPU, profile, and placement. The device plugin adds the MIG UUID, GPU Instance ID, and Compute Instance ID after realization. These fields form one allocation identity across control-plane and node lifecycle operations. The parent GPU UUID and GPU Instance ID provide a direct correlation key for DCGM metrics carrying `UUID` and `GPU_I_ID` labels.

## Core Workflows

### Capability publication

The device plugin discovers the allowlisted profile set through NVML and publishes the compact capability contract in the node registration annotation. Scheduler state refresh turns this contract into per-GPU topology capacity.

### Scheduling and reservation

The scheduler rebuilds occupancy from active Pod reservations. Each placement occupies the interval `[start, start + size)`. Profile selection follows workload capacity, and placement selection follows deterministic packing. Capacity pressure keeps the workload in the Kubernetes Pending phase.

### Runtime realization

During kubelet allocation, the device plugin resolves the reservation and verifies it against current NVML capability. The MIG instance manager serializes mutation per physical GPU, creates the GI and CI at the selected placement, resolves their runtime IDs, and enriches the Pod allocation record.

The reservation key combines physical GPU, profile, and placement. Repeated allocation requests converge on the same managed instance.

### Lifecycle reconciliation

The reconciler derives desired allocations from active Pods assigned to the node. It compares this desired set with managed instances and releases completed workload allocations. Reconciliation runs periodically and during new allocation activity, supporting steady-state cleanup and prompt capacity reuse.

A complete Kubernetes state snapshot authorizes each cleanup cycle and preserves conservative hardware lifecycle behavior.

### Restart recovery

Startup combines active Pod reservations with NVML process activity to identify GPUs carrying active work. Idle GPUs enter a clean MIG-ready state. Active records containing profile, placement, and MIG UUID are verified through NVML and adopted into the new manager process.

This flow preserves workload identity and re-establishes lifecycle ownership after a device plugin restart.

## Consistency Model

Dynamic MIG follows a reservation-first sequence:

1. Node capability becomes scheduler input.
2. The scheduler persists an exact reservation.
3. The device plugin realizes that reservation.
4. The device plugin enriches the record with runtime identity.
5. Reconciliation converges managed hardware toward active reservations.

Each phase has one authority and one durable handoff. This separation keeps placement policy independent from NVML mutation while preserving a shared allocation identity.

## Operational Model

Dynamic MIG mode is selected per node through device plugin configuration. Workloads select MIG allocation through the `nvidia.com/vgpu-mode: mig` annotation. GPU Operator integration supplies the NVIDIA driver and container runtime path.

Operational visibility centers on:

- discovered profiles and placements per GPU;
- scheduler reservation decisions and capacity pressure;
- GI/CI realization and runtime identity assignment;
- reconciliation activity and capacity reuse;
- startup adoption results;
- workload GPU progress across lifecycle events.

End-to-end validation uses sustained CUDA workloads across mixed profiles, capacity saturation, workload replacement, device plugin restart, and burst allocation. UUID stability and continuous GPU progress demonstrate lifecycle continuity.

## Evolution Direction

The architecture provides stable extension points for:

- pluggable placement policies;
- node-level fragmentation scoring;
- placement-aware scheduler metrics;
- event-driven lifecycle acceleration;
- CDI synchronization for dynamic MIG identities;
- richer multi-GPU and heterogeneous-node validation.

The capability contract, reservation contract, and hardware ownership boundary remain the foundation for these extensions.

## Decision Summary

Dynamic MIG treats hardware topology as node-owned capability, placement as a scheduler reservation, and GI/CI mutation as a device plugin responsibility. Pod metadata carries allocation identity across the control plane and runtime. This model gives HAMi a deterministic, topology-aware foundation for dynamic MIG scheduling and lifecycle management.
