# Monitoring HAMi

HAMi exposes Prometheus metrics for GPU allocation and per-container GPU usage.
This document lists the metrics, explains where they come from, and shows how to
scrape them, alert on them, and visualize them.

## Where metrics come from

HAMi emits metrics from two components:

| Component | What it reports | Default metrics endpoint |
| --- | --- | --- |
| Scheduler | Cluster/node allocation view — how much GPU memory and core the scheduler has handed out, per node, device, and container | container port `9395`, exposed as NodePort `31993` (`scheduler.service.monitorPort`) |
| vGPU monitor (runs in the device-plugin DaemonSet) | Runtime usage view — actual host GPU usage and per-container vGPU usage measured inside containers | container port `9394`, exposed as NodePort `31992` (`devicePlugin.service.httpPort`) |

Both serve Prometheus text format at `/metrics`.

- **Scheduler** metrics answer "what has been allocated?" — they come from the
  scheduler's view of the cluster and always carry a `node` label.
- **vGPU monitor** metrics answer "what is actually being used?" — the host GPU
  metrics carry no `node` label (they are per device), and the container metrics
  are keyed by `namespace` / `pod` / `container`.

> Ratio metrics (names ending in `_ratio`) are reported on a **0-100** scale
> (percent), not the 0-1 scale some Prometheus conventions use. Keep that in mind
> when writing thresholds and dashboards.

## Enabling scraping

If you run the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator),
set `prometheus.enabled=true` when installing the chart. That renders a
`ServiceMonitor` for both the scheduler and the vGPU monitor (each is also gated on
the `monitoring.coreos.com/v1` `ServiceMonitor` CRD being present):

```bash
helm install hami hami-charts/hami -n kube-system \
  --set prometheus.enabled=true
```

Without the Prometheus Operator, point your own Prometheus at the two `/metrics`
endpoints directly. Both are exposed as NodePort services by default, so they are
reachable on every node:

- scheduler: `http://<node-ip>:31993/metrics` (`scheduler.service.monitorPort`)
- vGPU monitor: `http://<node-ip>:31992/metrics` (`devicePlugin.service.httpPort`)

## Metric reference

The tables below cover the current (`hami_`-prefixed) metric names. All metrics are
gauges.

### Scheduler metrics (allocation view)

| Metric | Labels | Description |
| --- | --- | --- |
| `hami_gpu_memory_limit_bytes` | `node`, `device_uuid`, `device_index`, `device_type` | Total device memory of a physical GPU, in bytes |
| `hami_gpu_memory_allocated_bytes` | `node`, `device_uuid`, `device_index`, `device_cores`, `device_type` | Device memory the scheduler has allocated on a GPU, in bytes |
| `hami_gpu_core_limit_ratio` | `node`, `device_uuid`, `device_index`, `device_type` | Device core capacity of a GPU, against which `hami_gpu_core_allocated_ratio` is measured (0-100) |
| `hami_gpu_core_allocated_ratio` | `node`, `device_uuid`, `device_index`, `device_type` | Percentage of a GPU's cores allocated by the scheduler (0-100) |
| `hami_gpu_shared_count` | `node`, `device_uuid`, `device_index`, `device_type` | Number of containers sharing a GPU |
| `hami_node_gpu_memory_allocated_ratio` | `node`, `device_uuid`, `device_index` | Percentage of a GPU's memory allocated by the scheduler (0-100) |
| `hami_node_gpu_overview` | `node`, `device_uuid`, `device_index`, `device_cores`, `device_memory_limit`, `device_type` | Per-GPU overview series carrying capacity as labels |
| `hami_node_gpu_mig_instance_info` | `node`, `device_uuid`, `device_index`, `mig_name` | GPU sharing mode: `0` hami-core, `1` MIG, `2` MPS |
| `hami_vgpu_memory_allocated_bytes` | `namespace`, `node`, `pod`, `container_index`, `device_uuid` | vGPU memory allocated to a container, in bytes |
| `hami_vgpu_core_allocated_ratio` | `namespace`, `node`, `pod`, `container_index`, `device_uuid` | vGPU cores allocated to a container (0-100) |
| `hami_resource_quota_used` | `namespace`, `quota_name`, `limit` | Device `ResourceQuota` usage |

### vGPU monitor metrics (runtime usage view)

| Metric | Labels | Description |
| --- | --- | --- |
| `hami_host_gpu_memory_used_bytes` | `device_index`, `device_uuid`, `device_type` | Physical GPU memory in use, in bytes |
| `hami_host_gpu_utilization_ratio` | `device_index`, `device_uuid`, `device_type` | Physical GPU core utilization (0-100) |
| `hami_vgpu_memory_used_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | vGPU memory used by a container, in bytes |
| `hami_vgpu_memory_limit_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | vGPU memory limit for a container, in bytes |
| `hami_container_device_utilization_ratio` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Container GPU SM utilization (0-100) |
| `hami_container_device_memory_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Container device memory usage, in bytes |
| `hami_container_last_kernel_elapsed_seconds` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Seconds since the container last ran a GPU kernel |
| `hami_vgpu_memory_context_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Container device memory context size, in bytes |
| `hami_vgpu_memory_module_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Container device memory module size, in bytes |
| `hami_vgpu_memory_buffer_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Container device memory buffer size, in bytes |
| `hami_mig_device_info` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid`, `instance_id` | MIG device information for a container |

### Legacy metric names

Earlier HAMi releases used camelCase metric names (for example `GPUDeviceMemoryLimit`,
`vGPUMemoryAllocated`, `nodeGPUMemoryPercentage`). These are disabled by default and
can be re-enabled for backward compatibility with `legacyMetrics: true` (chart value)
or the `--legacy-metrics=true` flag. Prefer the `hami_`-prefixed names above for new
dashboards and alerts; the legacy names may be removed in a future release.

## Joining allocation and usage

Scheduler and monitor metrics share the `device_uuid` label, which lets you relate
the two views. The host GPU metrics carry no `node` label, so to look at one node's
devices you **filter** them against a scheduler metric that has both `node` and
`device_uuid`:

```promql
hami_host_gpu_utilization_ratio
  and on (device_uuid) hami_gpu_memory_limit_bytes{node="gpu-node-1"}
```

`and` returns the host series unchanged wherever a matching `device_uuid` exists on
the right — it filters, but does not copy any labels. To actually **attach** the
`node` label (for example to group host usage by node), use a label-preserving
arithmetic join instead:

```promql
hami_host_gpu_utilization_ratio
  * on (device_uuid) group_left(node)
  (hami_gpu_memory_limit_bytes * 0 + 1)
```

The right-hand `* 0 + 1` turns each scheduler series into `1`, so multiplying keeps
the host value intact while `group_left(node)` carries the `node` label across.

## Useful queries

```promql
# GPU memory allocated vs. capacity on a node (percent)
100 * sum by (node) (hami_gpu_memory_allocated_bytes)
    / sum by (node) (hami_gpu_memory_limit_bytes)

# Containers using more than 80% of their vGPU memory limit (percent)
100 * hami_vgpu_memory_used_bytes / (hami_vgpu_memory_limit_bytes > 0) > 80

# Number of GPUs currently shared by more than one container
count(hami_gpu_shared_count > 1)
```

## Dashboards

A ready-to-import Grafana dashboard for these metrics lives in
[`dashboards/`](../dashboards/). See its [README](../dashboards/README.md) for import
steps and the variables it exposes.

## Alerting

The Helm chart can install a `PrometheusRule` with a starter set of GPU alerts
(memory/core exhaustion, vGPU memory near limit, scheduler metrics absent). It is
opt-in:

```bash
helm upgrade hami hami-charts/hami -n kube-system \
  --set prometheus.enabled=true \
  --set prometheus.alerts.enabled=true
```

The rule renders only when the `monitoring.coreos.com/v1` `PrometheusRule` CRD is
present. Each alert has its own `threshold`, `for`, and `severity`, and you can add
labels so your Operator's `ruleSelector` picks up the rule — see the
`prometheus.alerts` section of [`charts/hami/values.yaml`](../charts/hami/values.yaml).
