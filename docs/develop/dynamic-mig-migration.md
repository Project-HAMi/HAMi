# Migrating to HAMi Dynamic MIG

This guide is intended for two groups of users:

- users of the MIG Geometry/Template implementation on the HAMi `master` branch; and
- users of NVIDIA GPU Operator MIG Manager who manage fixed MIG geometries.

The goal of this migration is not to promise that nodes will never need to be drained again. It is to remove draining from the routine profile-switching path. The scheduler reserves a specific MIG profile and physical placement for each Pod, the device plugin creates the corresponding GI/CI on demand, and the instance is reclaimed when the Pod terminates.

> The current implementation does not support a seamless rolling migration that preserves legacy MIG Pods. For the initial handover, cordon, drain, upgrade, and validate nodes one at a time. After migration, routine mixed-profile scheduling usually no longer requires draining a node merely to switch the geometry of an entire GPU.

## Why migrate

Operating a fixed geometry typically starts by selecting a whole-GPU layout such as `all-1g`, `all-3g`, or a mixed configuration. When the workload mix changes and the current layout cannot satisfy a request, operators must clear the GPU, destroy the existing GI/CI instances, and apply a different layout.

NVIDIA MIG Manager can trigger reconfiguration by changing `nvidia.com/mig.config`, but NVIDIA still requires that no user workloads are running on GPUs being reconfigured. Enabling or disabling MIG mode can also require a GPU reset or node reboot in some environments. Production procedures therefore commonly cordon or drain the node first. See the [NVIDIA GPU Operator MIG documentation](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-operator-mig.html).

The implementation on the HAMi `master` branch is also centered on predefined geometries. When a request cannot fit the current geometry, the whole GPU must be switched to another template. This model works well for stable, long-lived resource pools, but mixed inference workloads, bursty profile demand, and frequently created short-lived jobs expose several costs:

- operators must maintain memory, compute, instance counts, and geometry combinations for every GPU model;
- changing the layout affects the entire GPU, not just the instance required by a new request;
- running instances prevent geometry reconfiguration;
- unused instances in a fixed layout continue to occupy slices; and
- draining nodes and recreating workloads become part of capacity management.

The current dynamic MIG implementation uses a reservation-first model:

```text
device plugin publishes profiles and legal placements discovered through NVML
                                  ↓
scheduler selects GPU + profile + placement for a Pod
                                  ↓
Pod annotation persists the logical reservation
                                  ↓
device plugin creates the GI/CI at that placement
                                  ↓
device plugin records MIG UUID, GI ID, and CI ID
                                  ↓
the exact CI/GI is destroyed when the Pod terminates
```

The main differences are:

| Area | Fixed Geometry / MIG Manager | HAMi Dynamic MIG |
| --- | --- | --- |
| Layout scope | Node or whole GPU | Per-Pod profile and placement |
| Profile capability | Manually configured geometry | Allowlist defines policy; NVML supplies actual capability |
| Instance creation | Pre-created fixed instance pool | Created from the reservation during `Allocate` |
| Instance reclamation | Usually retained until reconfiguration | Exact GI/CI reclaimed after the Pod terminates |
| Workload mix changes | May require a whole-GPU layout switch | Scheduled directly when a legal free placement exists |
| Restart recovery | Depends on the existing layout | Pod annotations are verified against NVML and adopted |

Dynamic MIG does not remove MIG hardware constraints. A slice occupied by a GI cannot be converted in place into an overlapping layout. Fragmentation can temporarily prevent placement of a large profile. Enabling or disabling MIG mode, driver maintenance, and rollback can still require draining or rebooting a node.

## Protocol changes to understand before migration

### Configuration changes from geometries to a profile allowlist

HAMi `master` configures complete geometries:

```yaml
nvidia:
  knownMigGeometries:
    - models: ["A100-SXM4-40GB"]
      allowedGeometries:
        - - name: 1g.5gb
            core: 14
            memory: 5120
            count: 7
        - - name: 2g.10gb
            core: 28
            memory: 10240
            count: 3
          - name: 1g.5gb
            core: 14
            memory: 5120
            count: 1
```

The current implementation configures only the profiles that the cluster permits:

```yaml
nvidia:
  migProfileAllowlist:
    - models: ["A100-SXM4-40GB"]
      profiles: ["1g.5gb", "2g.10gb", "3g.20gb", "7g.40gb"]
```

Operators no longer duplicate `core`, `memory`, `count`, or legal placement data. The node that owns the GPU discovers those values through NVML. The allowlist remains important: it defines which profiles the scheduler may use instead of automatically exposing every capability reported by the driver.

When a legacy configuration contains several geometries, migration normally takes the union of their profile names. For example:

```text
7 × 1g
3 × 2g + 1 × 1g
2 × 3g
1 × 7g
```

becomes:

```yaml
profiles: ["1g.5gb", "2g.10gb", "3g.20gb", "7g.40gb"]
```

Verify profile names for each actual GPU model. Do not infer them solely from nominal memory capacity. Start with the model mappings in the current Chart and the device plugin discovery logs, then validate them with the target driver and hardware.

### Allocation identity moves from a UUID suffix to a Pod annotation

The legacy implementation encodes the template and slot in the device identifier, for example:

```text
GPU-xxxxxxxx[1-2]
```

The current implementation stores the complete allocation identity in `hami.io/vgpu-mig-allocations`:

```json
[
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
]
```

The scheduler records the parent GPU, profile, and placement. After creating the instance, the device plugin adds the MIG UUID, GI ID, and CI ID. This annotation is the durable contract used to rebuild scheduler occupancy, recover after a device plugin restart, and reclaim instances. Users must not create or modify it manually.

Legacy Pods do not contain this complete identity. A legacy template/slot index alone cannot reliably determine the physical placement for every GPU model and existing hardware layout. The current implementation therefore fails safely instead of guessing and risking overlapping slice allocations. This is the primary reason legacy MIG Pods must be drained during the initial upgrade.

## Support boundaries

### Operations that normally no longer require draining after migration

- creating new Pods with different allowlisted profiles;
- deleting a Pod and reclaiming its MIG instance;
- reusing a legal free placement; and
- adopting active instances after a device plugin restart when their complete runtime annotations can be verified through NVML.

### Operations that can still require draining or rebooting

- the initial migration from the legacy geometry model;
- transferring hardware mutation ownership from NVIDIA MIG Manager;
- enabling or disabling MIG mode on a physical GPU;
- driver upgrades, GPU resets, or platform-required node reboots;
- rolling back to a version that understands only legacy geometries and UUID encoding;
- satisfying a new layout that would require moving a running GI/CI; and
- repairing a state in which HAMi Pod annotations cannot be correlated with NVML hardware state.

## Migrating from HAMi master

### Migration principle

Do not allow a legacy scheduler and a current device plugin to serve MIG requests together:

- the legacy scheduler produces template/slot encoding and does not create the new MIG reservation annotation;
- the current device plugin requires an explicit profile and placement in that reservation; and
- the current scheduler reads `migProfiles` capability, while legacy nodes publish `migtemplate`.

A mixed-version deployment can conservatively report no capacity or fail during `Allocate`. Stop new MIG scheduling, upgrade the control plane, and then upgrade device plugins one node at a time.

### Recommended procedure

1. **Inventory and back up the current state.** Save the scheduler device ConfigMap, MIG node registration annotations, the list of active MIG Pods, and `nvidia-smi -L` output. Confirm that application Pods can be recreated.
2. **Stop new scheduling.** Cordon the MIG nodes being migrated so that the legacy scheduler cannot create new legacy-format allocations during the migration window.
3. **Drain legacy MIG Pods.** Wait for workloads to finish or migrate them elsewhere. Confirm that no user GPU process must be preserved. Do not restart only the device plugin and assume that legacy Pods can be adopted automatically.
4. **Migrate configuration.** Convert `knownMigGeometries` to `migProfileAllowlist`. Retain the profiles that administrators want to expose and remove manually maintained `core`, `memory`, `count`, and geometry combinations.
5. **Upgrade the scheduler.** Upgrade the scheduler and its configuration before upgrading device plugins, preventing a legacy scheduler from sending incompatible allocations to current nodes.
6. **Upgrade device plugins one node at a time.** Use a small number of nodes as canaries. On startup, idle GPUs are prepared in a clean MIG-ready state, which can destroy existing GI/CI instances on those idle GPUs.
7. **Validate node capability.** Confirm that device plugin logs show profile and placement discovery and that the Node registration annotation contains non-empty `migProfiles`.
8. **Validate the full lifecycle.** Create a MIG Pod and inspect its reservation annotation, the NVML-visible instance, and the MIG UUID visible to the container. Delete the Pod, wait for reconciliation, and confirm that the instance is released.
9. **Resume scheduling.** Uncordon nodes gradually after the canary succeeds, then restore production workloads.

The project Helm Chart now provides `migProfileAllowlist` in its default configuration. If `device-config.content` or an external ConfigMap overrides the defaults, update that custom content as well. Legacy fields are not automatically converted to the new allowlist.

## Migrating from NVIDIA MIG Manager

### Establish a single owner first

NVIDIA MIG Manager and HAMi Dynamic MIG both mutate GI/CI state. They must not manage the same physical GPU at the same time. MIG Manager can reapply a whole-GPU geometry in response to a Node label, while HAMi creates and destroys instances on demand from Pod reservations.

GPU Operator can continue to provide the driver, Container Toolkit, DCGM, and other components, but MIG Manager must no longer apply a geometry to target nodes. The exact method for disabling that reconciliation depends on the GPU Operator version and deployment policy. Before migration, verify that MIG Manager on a target node will not continue to react to changes in `nvidia.com/mig.config`.

### Recommended procedure

1. **Record the current state.** Save `nvidia.com/mig.config`, `nvidia.com/mig.config.state`, the MIG Manager ConfigMap, custom geometries, and `nvidia-smi -L` output.
2. **Cordon target nodes and migrate GPU workloads.** NVIDIA requires that no user GPU workload be running during reconfiguration. HAMi's initial handover also needs an explicitly empty and verifiable baseline.
3. **Stop MIG Manager reconciliation on target nodes.** Ensure that it cannot reapply the previous geometry after HAMi creates GI/CI instances. Deleting a MIG Manager Pod once is not sufficient if its controller configuration immediately recreates it.
4. **Keep the required GPU Operator infrastructure.** The driver and container runtime remain prerequisites for HAMi GPU access. Stopping MIG Manager does not necessarily mean uninstalling GPU Operator.
5. **Configure the HAMi node with `mig` operating mode and set `migProfileAllowlist`.** The allowlist can be built from the profiles actually required in the previous MIG Manager configuration.
6. **Start the HAMi scheduler and device plugin.** The device plugin validates profiles and placements through NVML and removes old instances from idle GPUs to establish a predictable hardware baseline.
7. **Run canaries.** Begin with one profile and one Pod, then validate mixed profiles, capacity saturation, reclamation after Pod deletion, and recovery after a device plugin restart.
8. **Expand one node at a time.** Keep an unmigrated static MIG pool as short-term fallback capacity until the dynamic pool has passed production validation.

## Validation checklist

### Node capability

- The registered GPU `mode` is `mig`.
- `migProfiles` is non-empty for every target GPU.
- Profile memory, slice count, and placements match NVML capability.
- Unsupported or non-allowlisted GPU models are not exposed accidentally.

### Scheduling and realization

- The Pod uses `nvidia.com/vgpu-mode: "mig"`.
- The scheduler writes `hami.io/vgpu-mig-allocations`.
- The selected profile satisfies the memory request and its placement does not overlap an active reservation.
- After a successful `Allocate`, the annotation contains a MIG UUID, GI ID, and CI ID.
- The MIG UUID visible in the container matches the annotation and NVML.

Example workload:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mig-canary
  annotations:
    nvidia.com/vgpu-mode: "mig"
spec:
  restartPolicy: Never
  containers:
    - name: workload
      image: ubuntu:22.04
      command: ["bash", "-c", "sleep 3600"]
      resources:
        limits:
          nvidia.com/gpu: 1
          nvidia.com/gpumem: 8000
```

This example validates resource allocation and device injection only. A production canary should use a trusted image with CUDA or NVML tools and run an actual GPU workload.

### Reclamation and recovery

- Deleting the canary Pod releases its CI/GI without affecting instances owned by other Pods.
- A subsequent Pod can reuse the same slice.
- Active GPUs are not reset by startup cleanup during a device plugin restart.
- After restart, active instances with complete annotations are verified through NVML and adopted into the manager.
- A Kubernetes API or annotation read failure skips destructive reconciliation instead of guessing and deleting instances.

### Recommended scenario coverage

Test at least:

1. creation and deletion of one `1g` Pod;
2. multiple non-overlapping `1g` instances on one GPU;
3. mixed placement of `1g`, `2g`, and `3g` instances;
4. a Pod remaining Pending when capacity is exhausted;
5. reuse of a placement after deleting a small instance;
6. a device plugin restart while a CUDA workload remains active;
7. safe failure when an annotation is missing or has only part of its runtime identity; and
8. no destructive reclamation while the Kubernetes API is temporarily unavailable.

## Rollback

### Before production workloads resume

If canary validation fails:

1. keep the node cordoned;
2. stop the current scheduler and device plugin from providing MIG service on the target node;
3. restore the legacy `knownMigGeometries` or MIG Manager configuration;
4. reapply a previously validated fixed geometry; and
5. verify device resource registration before uncordoning the node.

### After new Dynamic MIG Pods have run

Do not roll component binaries directly back to a legacy version. The legacy implementation does not understand the new reservation and placement protocol and cannot safely inherit the current manager state. Drain Dynamic MIG Pods again, stop HAMi from mutating GI/CI state, and then restore the legacy controller and fixed geometry.

## Frequently asked questions

### Does migration eliminate draining entirely?

No. Routine creation and deletion of allowlisted profiles on free slices normally does not require draining. Initial handover, MIG mode changes, driver maintenance, re-layout that requires moving active instances, and rollback still can.

### Can NVIDIA MIG Manager and HAMi manage different GPUs on the same node?

Consider this only when both systems provide explicit, stable, and verified device-level ownership isolation. This migration guide does not rely on such a setup. By default, assign MIG hardware mutation on a target node to one controller so that whole-GPU geometry reapplication cannot conflict with Pod-level creation and deletion.

### Why cannot a legacy Pod be migrated automatically from `GPU-UUID[template-slot]`?

The legacy index describes a logical position in a scheduler template. Across GPU models, driver versions, and actual hardware state, it does not uniquely prove the GI placement, MIG UUID, GI ID, and CI ID. A conversion that is not verified through NVML could map two reservations to overlapping slices. The current implementation prioritizes safety and therefore establishes the new protocol only after legacy workloads have been drained.

### Must users change their workload YAML?

Usually not. Users continue to request `nvidia.com/gpu` and `nvidia.com/gpumem` and select `nvidia.com/vgpu-mode: "mig"`. The scheduler and device plugin manage `hami.io/vgpu-mig-allocations`; it is not a user-facing API.

## Conclusion

If a cluster's MIG demand is stable for long periods, pre-partitioned node pools remain a simple and reliable option. Dynamic MIG is most valuable when the profile mix changes with Pod lifecycles, static instance pools have low utilization, or geometry switching has become a routine operational burden.

The migration itself requires one controlled drain because the legacy protocol does not contain enough information to prove the physical identity of existing instances. After migration, HAMi connects profile selection, placement reservation, GI/CI creation, and lifecycle reclamation through one convergent workflow. It reduces the frequency of routine reconfiguration and the scope of whole-GPU layout changes; it does not bypass NVIDIA MIG hardware or driver constraints.
