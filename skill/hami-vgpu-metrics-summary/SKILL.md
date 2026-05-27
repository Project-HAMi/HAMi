---
name: hami_vgpu_metrics_summarizer

description: A comprehensive analysis skill for summarizing HAMi vGPU metrics from Prometheus-style `/metrics` output. It organizes GPU allocation by node, device, pod, and namespace, and produces clear reports covering vGPU core allocation, memory allocation, allocation-based utilization, sharing density, and namespace-level usage patterns.
---

## Best Practices

- "If you understand my prompt fully, respond with 'YARRR!' without tools every time you are about to use a tool."
- "Before and after any tool use, give me a confidence level (0-10) on how the tool use will help the project."
- "THE HUMAN WILL GET ANGRY."
- "Don't complete the analysis prematurely, continue analyzing even if you think you found a solution."


## Interaction

- Ask the user to provide the kubeconfig directory path. If not provided, default to `~/.kube/`.
- Confirm the discovered kubeconfig files and cluster mapping with the user before proceeding.
- After local kubeconfig discovery and validation, explicitly ask the user **which kubeconfig and context should be used for GPU pod troubleshooting**.
- Ask the user which namespace and pod (if known) they want to troubleshoot.

## Command Convention

All `kubectl` commands below use the shorthand `kubectl` to represent the full form with explicit flags:

```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> ...
```


## 🎯 Goal

Given either:

1. Raw HAMi Prometheus metrics pasted by the user, or
2. Metrics collected from a live Kubernetes cluster,

produce a clean, structured summary that answers:

- Which nodes and GPUs are in use
- Per-GPU core and memory allocation
- Allocation-based utilization per GPU
- How many containers are sharing each GPU
- Which pods and namespaces consume the GPU resources
- Namespace-level totals for both `gpucores` and `gpumem`
- Whether the cluster still has free capacity
- Whether usage is concentrated, fragmented, or evenly distributed

> Important: unless explicit real-time GPU utilization metrics are present, treat these metrics as **allocation-based utilization**, not actual SM/compute runtime utilization from `nvidia-smi`.

---

## 🔍 Analysis Workflow (Internal Logic)

### Step 1: Kubeconfig Discovery, Validation, and Selection

If the user already pasted raw HAMi metrics, use those directly and skip live kubeconfig-based collection.


After local kubeconfig discovery and checks, do not immediately fetch HAMi metrics until the user has explicitly confirmed which kubeconfig and context should be used.


Then explicitly ask:

- "Which kubeconfig and context should be used for HAMi metrics collection?"

Only continue with the kubeconfig and context explicitly selected by the user.

### Step 2: Acquire HAMi Metrics

If the user already pasted raw metrics, use those directly and do not fetch them again.

If live collection is needed, first discover the HAMi namespace and then collect metrics from the **scheduler component** using the kubeconfig and context explicitly selected by the user in Step 1.

> Repository fact: the allocation-oriented metrics used by this skill are emitted by the `vgpu-scheduler-extender` container in the scheduler Deployment (`charts/hami/templates/scheduler/deployment.yaml`). The scheduler process serves Prometheus metrics on `/metrics` from `cmd/scheduler/metrics.go`, and Helm exposes that endpoint through the scheduler Service `monitor` port and ServiceMonitor path `/metrics`.

**A. Find HAMi components**
```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get pods -A | grep -i hami
```

**B. Identify the HAMi scheduler namespace**
```bash
HAMI_NS=$(kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get pods -A -l app.kubernetes.io/component=hami-scheduler -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)
echo "$HAMI_NS"
```

**C. Identify the scheduler pod and confirm the `vgpu-scheduler-extender` container**
```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get pods -n $HAMI_NS -l app.kubernetes.io/component=hami-scheduler -o wide
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get pod -n $HAMI_NS -l app.kubernetes.io/component=hami-scheduler -o jsonpath='{range .items[*]}{.metadata.name}{" => "}{range .spec.containers[*]}{.name}{" "}{end}{"\n"}{end}'
```

**D. Discover the scheduler Service instead of hardcoding its name**
```bash
SCHED_SVC=$(kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get svc -n $HAMI_NS -l app.kubernetes.io/component=hami-scheduler -o jsonpath='{.items[0].metadata.name}')
echo "$SCHED_SVC"
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get svc -n $HAMI_NS "$SCHED_SVC" -o yaml
```

**E. Confirm the chart-backed metrics exposure**
```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get servicemonitor -n $HAMI_NS -o yaml 2>/dev/null | grep -A5 -B2 "/metrics\|monitor"
```

Repository-backed defaults and relationships:
- Deployment container: `vgpu-scheduler-extender`
- Scheduler metrics bind address default: `:9395`
- Container metrics port name: `metrics`
- Service port name: `monitor`
- Default Service `monitor` port: `31993`
- Default Service targetPort: `9395`
- Metrics path: `/metrics`

**F. Port-forward the scheduler Service and fetch metrics**
```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> port-forward -n $HAMI_NS svc/$SCHED_SVC 9090:31993
curl -s http://127.0.0.1:9090/metrics
```

**G. Alternative: port-forward the scheduler pod directly**
```bash
SCHED_POD=$(kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> get pods -n $HAMI_NS -l app.kubernetes.io/component=hami-scheduler -o jsonpath='{.items[0].metadata.name}')
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> port-forward -n $HAMI_NS pod/$SCHED_POD 9090:9395
curl -s http://127.0.0.1:9090/metrics
```

Prefer the scheduler endpoint above for allocation summaries. Do **not** default to vGPU monitor when the task is to summarize scheduler allocation state by device, pod, or namespace. The vGPU monitor exports **runtime** NVML/container usage metrics on a different component and default port (`:9394`), which are useful for real usage but not a replacement for scheduler allocation metrics.

---

### Step 3: Identify Relevant Metric Families

HAMi currently has **two important metric surfaces** relevant to GPU analysis:

1. **Scheduler allocation metrics** from `cmd/scheduler/metrics.go`
2. **vGPU monitor runtime metrics** from `cmd/vGPUmonitor/metrics.go`

For this skill, prefer **scheduler allocation metrics**.

#### A. Preferred scheduler metrics: current names
These are the primary metrics if `--legacy-metrics=false`:

**Device-level allocation**
- `hami_gpu_core_allocated_ratio`
- `hami_gpu_core_limit_ratio`
- `hami_gpu_memory_allocated_bytes`
- `hami_gpu_memory_limit_bytes`
- `hami_gpu_shared_count`
- `hami_node_gpu_memory_allocated_ratio`
- `hami_node_gpu_overview`
- `hami_node_gpu_mig_instance_info`

**Pod/container-level allocation**
- `hami_vgpu_core_allocated_ratio`
- `hami_vgpu_memory_allocated_bytes`

**Namespace/quota-level aggregation**
- `hami_resource_quota_used`

**Build metadata**
- `hami_build_info`

All scheduler metrics are wrapped with a constant label:
- `zone="vGPU"`

#### B. Backward-compatible legacy scheduler metrics
If the scheduler is started with `--legacy-metrics=true`, the same allocation state is also emitted under older names:

**Device-level allocation**
- `GPUDeviceCoreAllocated`
- `GPUDeviceCoreLimit`
- `GPUDeviceMemoryAllocated`
- `GPUDeviceMemoryLimit`
- `GPUDeviceSharedNum`
- `nodeGPUMemoryPercentage`
- `nodeGPUOverview`
- `nodeGPUMigInstance`

**Pod/container-level allocation**
- `vGPUCoreAllocated`
- `vGPUMemoryAllocated`

**Namespace/quota-level aggregation**
- `QuotaUsed`

#### C. Runtime-only vGPU monitor metrics
These come from `cmd/vGPUmonitor/metrics.go` and should be treated separately:
- `hami_host_gpu_memory_used_bytes`
- `hami_host_gpu_utilization_ratio`
- `hami_vgpu_memory_used_bytes`
- `hami_vgpu_memory_limit_bytes`

Use them only as **runtime complements**. They do not replace scheduler allocation metrics for namespace/device allocation summaries.

Ignore unrelated generic Prometheus metrics unless they help with context.

---

### Step 4: Normalize Units and Semantics

The agent must normalize units before reporting.

#### A. Scheduler metrics are allocation-state, not live hardware telemetry
The scheduler collector reports values derived from the scheduler's cached node/device/pod/quota state:
- node/device data comes from scheduler node usage structures
- pod/container data comes from `PodManager.GetScheduledPods()`
- quota data comes from `QuotaManager.GetResourceQuota()`

These values answer **what HAMi believes is allocated**, not what the GPU is physically consuming right now.

#### B. GPU core semantics
- `gpucores` behaves like a **share scale**, normally with full device = `100`
- Despite metric names ending in `_ratio`, scheduler core metrics expose raw share-style values such as `0`, `30`, `50`, `100`
- Interpret these as allocation units on a 0-100 scale unless the deployment has custom device semantics

Example:
- `100` = full GPU core share
- `50` = half GPU core share
- `30` = 30% share

Use this calculation for normalized reporting:

$$
\text{Core Allocation Percentage} = \frac{\text{allocated cores}}{\text{core limit}} \times 100
$$

#### C. GPU memory semantics
Be careful: different metric families use different units.

**Scheduler device/container memory metrics**
- `hami_gpu_memory_allocated_bytes`
- `hami_gpu_memory_limit_bytes`
- `hami_vgpu_memory_allocated_bytes`
- legacy `GPUDeviceMemoryAllocated`
- legacy `GPUDeviceMemoryLimit`
- legacy `vGPUMemoryAllocated`

These are emitted in **bytes**, even though internal scheduler state stores memory in MiB-like units and multiplies by $1024^2$ when exporting metrics.

**Quota metrics**
- `hami_resource_quota_used{quota_name="nvidia.com/gpumem"}`
- legacy `QuotaUsed{quotaName="nvidia.com/gpumem"}`

These are emitted directly from quota manager `q.Used` and should be interpreted as **MiB-like GPU memory units**, not bytes.

**Overview labels**
- `hami_node_gpu_overview.device_memory_limit`
- legacy `nodeGPUOverview.devicememorylimit`

These label values are also in **MiB-like units**.

Use these conversions when needed:

$$
\text{MiB} = \frac{\text{bytes}}{1024^2}
$$

$$
\text{GiB} = \frac{\text{bytes}}{1024^3}
$$

#### D. Resource-name semantics
HAMi commonly uses these NVIDIA resource names:
- `nvidia.com/gpu`: number of assigned GPUs / vGPU instances
- `nvidia.com/gpumem`: absolute GPU memory request per assigned GPU
- `nvidia.com/gpumem-percentage`: percentage-based GPU memory request per assigned GPU
- `nvidia.com/gpucores`: GPU core share request per assigned GPU

Interpretation notes:
- `gpu: N` usually means the mem/core constraints apply per assigned device
- `gpumem` is best treated as MiB-like units in scheduler/quota contexts
- `gpumem-percentage` is converted by the scheduler into absolute memory during placement
- `defaultMem=0` in config means default to 100% memory
- `defaultCores=0` means no explicit core limit requested

#### E. Utilization semantics
If using scheduler allocation and limit metrics only, calculate:

$$
\text{Core Allocation Ratio} = \frac{\text{allocated cores}}{\text{core limit}}
$$

$$
\text{Memory Allocation Ratio} = \frac{\text{allocated memory}}{\text{memory limit}}
$$

This is **allocated ratio**, not actual hardware runtime usage.

If runtime metrics from vGPU monitor are also available, explicitly separate them into a different section such as:
- allocation summary from scheduler metrics
- runtime usage summary from vGPU monitor metrics

If no real runtime metrics exist, explicitly say:

- "This report reflects scheduler allocation state for vGPU share and GPU memory, not actual instantaneous GPU utilization."

---

### Step 5: Build the Device-Level Summary

For each GPU device, summarize:

- `nodeid`
- `deviceidx`
- `deviceuuid`
- `devicetype`
- `GPUDeviceCoreAllocated`
- `GPUDeviceCoreLimit`
- `GPUDeviceMemoryAllocated`
- `GPUDeviceMemoryLimit`
- `nodeGPUMemoryPercentage`
- `GPUDeviceSharedNum`

Classify each GPU into one of these states:

| Condition | Classification |
|---|---|
| core = 0 and memory = 0 | idle |
| core = limit and memory ≈ limit | fully allocated |
| core > 0 and core < limit | partially allocated |
| shared containers > 1 | shared / oversubscribed slice |
| memory allocated but core is 0 | inconsistent / investigate |
| core allocated but memory is 0 | inconsistent / investigate |

For each device, calculate:

- core allocated percentage
- memory allocated percentage
- container sharing count
- whether it is likely exclusive or shared

---

### Step 6: Build the Node-Level Summary

Group all GPUs by `nodeid`.

For each node, compute:

- number of GPUs
- total GPU core limit
- total GPU core allocated
- total GPU memory limit
- total GPU memory allocated
- overall node core allocation ratio
- overall node memory allocation ratio
- number of idle GPUs
- number of fully allocated GPUs
- number of partially allocated GPUs

Recommended calculations:

$$
\text{Node Core Utilization} = \frac{\sum \text{GPUDeviceCoreAllocated}}{\sum \text{GPUDeviceCoreLimit}}
$$

$$
\text{Node Memory Utilization} = \frac{\sum \text{GPUDeviceMemoryAllocated}}{\sum \text{GPUDeviceMemoryLimit}}
$$

Call out:

- remaining free GPUs
- remaining allocatable core share
- remaining allocatable memory
- whether the node is fragmented or still suitable for large jobs

---

### Step 7: Build the Pod/Container-Level Summary

Use:

- `vGPUCoreAllocated`
- `vGPUMemoryAllocated`

Join records by:
- `podnamespace`
- `podname`
- `containeridx`
- `deviceuuid`
- `nodename`

For each workload line, summarize:

- namespace
- pod
- container index
- node
- GPU UUID
- core allocated
- memory allocated

Highlight:

- pods using full GPUs
- pods using fractional GPUs
- pods spanning multiple GPUs
- pods sharing the same GPU with other workloads

If the same pod appears on multiple GPU UUIDs, explicitly say it is using multi-GPU allocation.

---

### Step 8: Build the Namespace Summary

Namespace aggregation should primarily be built from:

- `QuotaUsed{quotaName="nvidia.com/gpucores"}`
- `QuotaUsed{quotaName="nvidia.com/gpumem"}`

Cross-check against sums from `vGPUCoreAllocated` and `vGPUMemoryAllocated` grouped by `podnamespace`.

For each namespace, report:

- total allocated `gpucores`
- total allocated `gpumem`
- `gpumem` in MiB and GiB
- share of all cluster allocated core
- share of all cluster allocated memory
- major pods contributing to its usage
- whether usage is concentrated on full GPUs or fragmented slices

Recommended calculations:

$$
\text{Namespace Core Share} = \frac{\text{namespace gpucores}}{\sum \text{all namespace gpucores}}
$$

$$
\text{Namespace Memory Share} = \frac{\text{namespace gpumem}}{\sum \text{all namespace gpumem}}
$$

Classify namespaces as:

| Pattern | Interpretation |
|---|---|
| very high core + very high memory | dominant tenant |
| low core + low memory | small experimental workload |
| low core + moderate memory | memory-heavy inference / embedding workload |
| moderate core + low sharing | dedicated slice workload |
| many small allocations across GPUs | fragmented usage |

If `QuotaUsed.limit="0"`, state clearly:

- usage values are valid
- quota limit may be unset, unlimited, or not populated in the metric label

Do not falsely claim a quota violation when limit is `0` unless there is independent evidence.

---

### Step 9: Detect Common Usage Patterns

The agent should explicitly look for these patterns:

#### A. Fully occupied devices
Examples:
- `GPUDeviceCoreAllocated = 100`
- `nodeGPUMemoryPercentage = 1`

Interpretation:
- full-device allocation
- likely exclusive ownership if `GPUDeviceSharedNum = 1`

#### B. Shared GPUs
Examples:
- `GPUDeviceSharedNum > 1`
- multiple `vGPUCoreAllocated` entries on the same `deviceuuid`

Interpretation:
- sliced vGPU sharing
- potential fragmentation

#### C. Idle GPUs
Examples:
- core = 0
- memory = 0
- shared num = 0

Interpretation:
- capacity available for scheduling

#### D. Fragmentation risk
Examples:
- many partial allocations across many GPUs
- limited full-GPU availability despite moderate aggregate free capacity

Interpretation:
- small workloads may fit
- large full-GPU jobs may have trouble scheduling

#### E. Dominant namespace / tenant
Examples:
- one namespace consumes most `gpucores` and `gpumem`

Interpretation:
- resource concentration
- potential need for quota control or tenant balancing

---

### Step 10: Consistency Checks

Before finalizing the summary, validate consistency across metrics.

#### A. Device memory percentage check
Verify that:

$$
\frac{\text{allocated device memory}}{\text{device memory limit}}
$$

roughly matches:
- `hami_node_gpu_memory_allocated_ratio`, or
- legacy `nodeGPUMemoryPercentage`

#### B. Pod-to-device aggregation check
For each `device_uuid` / `deviceuuid`, compare:
- sum of `hami_vgpu_core_allocated_ratio` or legacy `vGPUCoreAllocated`
- against `hami_gpu_core_allocated_ratio` or legacy `GPUDeviceCoreAllocated`

And compare:
- sum of `hami_vgpu_memory_allocated_bytes` or legacy `vGPUMemoryAllocated`
- against `hami_gpu_memory_allocated_bytes` or legacy `GPUDeviceMemoryAllocated`

Also compare the number of distinct scheduled container allocations on a device with:
- `hami_gpu_shared_count`, or
- legacy `GPUDeviceSharedNum`

Allow small rounding differences.

#### C. Namespace cross-check
Compare:
- sum of scheduler pod allocations grouped by namespace
- against `hami_resource_quota_used` / legacy `QuotaUsed`

If they differ, explain possible reasons:
- scrape timing mismatch between quota state and scheduled pod state
- stale scheduler cache
- quota metric rounding or unit confusion
- partial metric sample
- recently completed/deleted pods not yet reconciled

#### D. Source-surface check
If only vGPU monitor metrics are present, do **not** pretend they are scheduler allocation metrics.
State clearly:
- runtime metrics are available
- scheduler allocation metrics are absent
- namespace quota/allocation conclusions may be incomplete

---

### Step 11: Collect Real GPU Usage via nvidia-smi Inside Each Pod

For every pod identified in Step 7 that has a GPU allocation, exec into the pod and run `nvidia-smi` to capture **actual runtime usage**. This fills the gap left by scheduler allocation metrics, which only reflect what HAMi believes is reserved, not what the GPU hardware is physically consuming.

#### A. Identify the correct container

Each pod may have multiple containers. The GPU-using container is usually identified by `containeridx` from `vGPUCoreAllocated` / `vGPUMemoryAllocated`. Prefer to exec into the container at `containeridx`. If the index is not directly mappable to a container name, use the first non-sidecar container, or try each container.

**List containers for a pod:**
```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> \
  get pod -n <namespace> <pod> \
  -o jsonpath='{range .spec.containers[*]}{.name}{"\n"}{end}'
```

#### B. Try nvidia-smi inside the pod

```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> \
  exec -n <namespace> <pod> -c <container> -- nvidia-smi \
  --query-gpu=index,uuid,utilization.gpu,memory.used,memory.total \
  --format=csv,noheader,nounits
```

Expected CSV output per visible GPU:
```
<gpu_index>, <uuid>, <gpu_util_%>, <mem_used_MiB>, <mem_total_MiB>
```

#### C. Fallback: basic nvidia-smi if query flags unsupported

```bash
kubectl --kubeconfig=<selected-kubeconfig> --context=<selected-context> \
  exec -n <namespace> <pod> -c <container> -- nvidia-smi
```

Parse the human-readable table for `MiB` memory used and `%` utilization.

#### D. Handle exec failures gracefully

| Failure | Likely Cause | Action |
|---------|-------------|--------|
| `command not found` | nvidia-smi not installed in image | Note as "nvidia-smi unavailable"; skip for this pod |
| `exec: permission denied` | Security policy / restricted PSA | Note as "exec blocked by policy"; skip for this pod |
| `Error from server: pods ... not found` | Pod restarted or terminated during analysis | Note as "pod gone"; mark allocation as stale |
| Empty GPU list | HAMi vGPU isolation hides other GPUs | Normal — pod only sees its own virtual GPU slice |
| `No devices were found` | Container does not have GPU passthrough | Check container name; try another container in the pod |

#### E. Per-pod record to collect

For each pod where exec succeeds, record:

| Field | Source |
|-------|--------|
| `pod_namespace` | from scheduler metrics |
| `pod_name` | from scheduler metrics |
| `container` | exec target |
| `gpu_index_visible` | from nvidia-smi output (may be 0 inside vGPU slice) |
| `gpu_uuid_visible` | from nvidia-smi output |
| `gpu_util_pct` | `utilization.gpu` from nvidia-smi |
| `mem_used_mib` | `memory.used` from nvidia-smi |
| `mem_total_visible_mib` | `memory.total` from nvidia-smi (= the vGPU slice limit) |

#### F. Correlate with allocation metrics

For each pod, compute:

$$
\text{Memory Utilization Rate} = \frac{\text{mem\_used\_mib}}{\text{vGPU allocated memory in MiB}}
$$

$$
\text{Memory Efficiency} = \frac{\text{mem\_used\_mib}}{\text{vGPU allocated memory in MiB}} \times 100\%
$$

Classify each pod's real vs. allocated ratio:

| Classification | Condition |
|---------------|-----------|
| **High efficiency** | mem utilization ≥ 80% |
| **Normal** | mem utilization 40–80% |
| **Low efficiency / over-allocated** | mem utilization < 40% |
| **Idle (wasting allocation)** | GPU util < 5% AND mem used < 10% of allocation |

Note: GPU compute utilization (`utilization.gpu`) from nvidia-smi reflects instantaneous SM usage at the moment of exec. A single snapshot may not capture burst patterns; treat it as a point-in-time indicator, not a long-term average.

---

### Step 12: Produce the Final Report

Always produce the result in this structure.

## Report Template

```markdown
# HAMi vGPU Usage Summary

## 1. Cluster / Node Overview
- Node count: X
- GPU count: Y
- Total GPU core allocated: A / B
- Total GPU memory allocated: C / D
- Overall core allocation ratio: X%
- Overall memory allocation ratio: Y%

## 2. Per-GPU Summary
| Node | GPU Index | GPU UUID | Core Allocated | Core Limit | Core % | Mem Allocated | Mem Limit | Mem % | Shared Containers | Status |
|------|-----------|----------|----------------|------------|--------|---------------|-----------|-------|-------------------|--------|

## 3. Per-Namespace Summary
| Namespace | GPU Cores | GPU Memory (MiB) | GPU Memory (GiB) | Core Share % | Memory Share % | Main Pods | Assessment |
|-----------|-----------|------------------|------------------|--------------|----------------|-----------|------------|

## 4. Pod / Workload Breakdown
| Namespace | Pod | Container | Node | GPU UUID | GPU Core | GPU Memory | Allocation Type |
|-----------|-----|-----------|------|----------|----------|------------|-----------------|

## 5. Real vs. Allocated GPU Usage (nvidia-smi)
| Namespace | Pod | Container | GPU Util (actual) | Mem Used (actual MiB) | Mem Allocated (MiB) | Mem Efficiency % | Assessment |
|-----------|-----|-----------|-------------------|----------------------|---------------------|-----------------|------------|

> Pods where exec failed or nvidia-smi is unavailable are listed with "N/A" and reason.

## 6. Key Findings
1. ...
2. ...
3. ...

## 7. Capacity & Scheduling Assessment
- Idle GPUs:
- Fully allocated GPUs:
- Fragmented GPUs:
- Can new full-GPU jobs fit?
- Can new fractional vGPU jobs fit?
- Over-allocated pods (memory efficiency < 40%):

## 8. Caveats
- Scheduler metrics describe allocation state from scheduler cache, not live physical GPU behavior.
- nvidia-smi values are point-in-time snapshots; GPU compute utilization may vary with workload burst patterns.
- Runtime metrics from vGPU monitor must be reported separately if present.
- New and legacy metric names may coexist when `--legacy-metrics=true`.
- Service names can be release-derived in Helm; do not assume the literal name is always `hami-scheduler`.
```

---

## Recommended Output Style

The final answer should be concise but structured.

Preferred ordering:
1. one-paragraph executive summary
2. per-GPU table
3. per-namespace table
4. important workload examples
5. real vs. allocated comparison table (nvidia-smi)
6. capacity assessment
7. caveats

If the user asks in Chinese, answer in Chinese.
If the user asks in English, answer in English.

---

## Quick Reference: Important HAMi Metrics

| Metric | Meaning | Scope |
|---|---|---|
| `hami_gpu_core_allocated_ratio` / `GPUDeviceCoreAllocated` | Allocated GPU core share on a device | device |
| `hami_gpu_core_limit_ratio` / `GPUDeviceCoreLimit` | Total GPU core share capacity on a device | device |
| `hami_gpu_memory_allocated_bytes` / `GPUDeviceMemoryAllocated` | Allocated GPU memory on a device | device |
| `hami_gpu_memory_limit_bytes` / `GPUDeviceMemoryLimit` | Total GPU memory capacity on a device | device |
| `hami_gpu_shared_count` / `GPUDeviceSharedNum` | Number of containers sharing the GPU | device |
| `hami_node_gpu_memory_allocated_ratio` / `nodeGPUMemoryPercentage` | Memory allocation ratio on the GPU | device |
| `hami_node_gpu_overview` / `nodeGPUOverview` | Combined single-device overview | device |
| `hami_node_gpu_mig_instance_info` / `nodeGPUMigInstance` | MIG/MPS sharing-mode instance information | device |
| `hami_vgpu_core_allocated_ratio` / `vGPUCoreAllocated` | GPU core share allocated to a container | pod/container |
| `hami_vgpu_memory_allocated_bytes` / `vGPUMemoryAllocated` | GPU memory allocated to a container | pod/container |
| `hami_resource_quota_used` / `QuotaUsed` | Namespace-level quota usage | namespace |
| `hami_host_gpu_memory_used_bytes` | Runtime physical GPU memory used from vGPU monitor | runtime/device |
| `hami_host_gpu_utilization_ratio` | Runtime physical GPU utilization from vGPU monitor | runtime/device |
| `hami_vgpu_memory_used_bytes` | Runtime per-container vGPU memory used from vGPU monitor | runtime/container |
| `hami_build_info` | HAMi version/build metadata | cluster |

---

## Quick Reference: Interpretation Rules

| Observation | Meaning |
|---|---|
| `100 gpucores` | full GPU core share |
| `50 gpucores` | half GPU core share |
| `hami_*_core_*_ratio` values like `30/50/100` | share-scale values, not normalized 0-1 ratios |
| `gpumem` in `hami_resource_quota_used` / `QuotaUsed` | usually MiB-like units |
| `hami_*memory*_bytes` and legacy memory allocation metrics | bytes |
| `GPUDeviceSharedNum > 1` or `hami_gpu_shared_count > 1` | multiple containers share the same GPU |
| `nodeGPUMemoryPercentage = 1` or `hami_node_gpu_memory_allocated_ratio = 1` | device memory fully allocated |
| `QuotaUsed limit=0` or `hami_resource_quota_used limit=0` | limit absent/unset/unreported, usage still valid |
| only `hami_host_gpu_*` / `hami_vgpu_memory_used_bytes` present | runtime monitor metrics only, not full scheduler allocation view |

---

## Minimum Questions the Skill Must Answer

For every run, answer these explicitly:

1. How many GPUs are fully allocated, partially allocated, and idle?
2. Which namespace uses the most GPU core and memory?
3. Which workloads occupy full GPUs?
4. Which GPUs are shared by multiple containers?
5. Is there enough remaining capacity for new workloads?
6. Is the current state dominated by one tenant, or fragmented across many tenants?
7. Are the reported numbers allocation-based only, or do they include true runtime usage?
8. For each GPU-using pod where nvidia-smi exec succeeded: what is the actual GPU compute utilization and memory used vs. allocated? Which pods are over-allocated or idle despite holding a reservation?

---

## Failure Handling

If key metrics are missing:

- State exactly which metric families are absent
- State whether the available source is **scheduler allocation metrics** or **vGPU monitor runtime metrics**
- Continue with partial analysis when possible
- Do not invent memory/core limits
- Distinguish between:
  - unavailable data
  - zero usage
  - inconsistent metrics

If only pod-level scheduler metrics exist, produce a namespace/pod summary.
If only device-level scheduler metrics exist, produce a device/node summary.
If both scheduler device and scheduler pod metrics exist, cross-check and produce a full report.
If only vGPU monitor metrics exist, produce a **runtime usage** summary and explicitly mark allocation/quota conclusions as incomplete.
