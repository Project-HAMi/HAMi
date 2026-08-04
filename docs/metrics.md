# vGPU Monitor Metrics

## Summary

The HAMi vGPU monitor (`vGPUmonitor`) runs as a DaemonSet with one pod per GPU
node. It reads GPU usage from NVML and per-container usage from the HAMi shared
memory region, and exposes them as Prometheus metrics.

## Accessing the metrics

The monitor serves the metrics on the `/metrics` endpoint:

- Default bind address: `:9394`
- The bind address is configurable with the `--metrics-bind-address` flag, for
  example `--metrics-bind-address=127.0.0.1:9394`.
- When installed with the helm chart, the metrics are reachable through the
  device-plugin Service (`monitorport`, default node port `31992`, forwarding
  to container port `9394`). The chart also installs a ServiceMonitor when
  `prometheus.enabled` is true and the `monitoring.coreos.com/v1` CRD is
  available.

To verify the monitor is exposing metrics on a node:

```bash
curl http://localhost:9394/metrics
```

An example `scrape_config` for Prometheus:

```yaml
scrape_configs:
- job_name: hami-vgpu-monitor
  static_configs:
  - targets: ['<node-ip>:31992']
```

All metrics collected by the monitor are registered with a constant `zone`
label. The `hami_build_info` metric is registered separately and does not
carry the `zone` label.

## Metrics

### Build metadata

| Metric | Type | Description | Labels |
|---|---|---|---|
| `hami_build_info` | Gauge | HAMi build metadata, always `1` | `version`, `revision`, `build_date`, `go_version`, `compiler`, `platform` |

### Host (physical GPU) metrics

These metrics describe the physical GPUs on the node the monitor runs on.

| Metric | Type | Description | Labels |
|---|---|---|---|
| `hami_host_gpu_memory_used_bytes` | Gauge | GPU device memory usage in bytes | `device_index`, `device_uuid`, `device_type` |
| `hami_host_gpu_utilization_ratio` | Gauge | GPU core utilization ratio (`0-100`) | `device_index`, `device_uuid`, `device_type` |

Note that `hami_host_gpu_memory_used_bytes` is not emitted for devices with a
unified memory architecture, where NVML does not report memory usage.

### Container (vGPU) metrics

These metrics describe the per-container share of a vGPU device. All of them
share the labels `namespace`, `pod`, `container`, `vdevice_index` and
`device_uuid`.

| Metric | Type | Description |
|---|---|---|
| `hami_vgpu_memory_used_bytes` | Gauge | vGPU device memory usage in bytes |
| `hami_vgpu_memory_limit_bytes` | Gauge | vGPU device memory limit in bytes |
| `hami_container_device_memory_bytes` | Gauge | Container device memory usage in bytes |
| `hami_container_device_utilization_ratio` | Gauge | Container device SM utilization ratio |
| `hami_container_last_kernel_elapsed_seconds` | Gauge | Seconds since the last kernel execution in the container |
| `hami_vgpu_memory_context_bytes` | Gauge | Container device memory context size in bytes |
| `hami_vgpu_memory_module_bytes` | Gauge | Container device memory module size in bytes |
| `hami_vgpu_memory_buffer_bytes` | Gauge | Container device memory buffer size in bytes |
| `hami_mig_device_info` | Gauge | MIG device information for the container, always `1`; only emitted for MIG devices and carries the extra `instance_id` label |

Notes:

- `hami_vgpu_memory_used_bytes` and `hami_container_device_memory_bytes`
  currently report the same underlying value.
- `hami_container_last_kernel_elapsed_seconds` is only emitted after the
  container has executed at least one kernel.

### Legacy metrics

When the monitor is started with `--legacy-metrics=true`, the legacy metric
names below are emitted alongside the ones above for backward compatibility.

| Legacy metric | Description |
|---|---|
| `HostGPUMemoryUsage` | GPU device memory usage |
| `HostCoreUtilization` | GPU core utilization |
| `vGPU_device_memory_usage_in_bytes` | vGPU device usage |
| `vGPU_device_memory_limit_in_bytes` | vGPU device limit |
| `Device_memory_desc_of_container` | Container device memory description |
| `Device_utilization_desc_of_container` | Container device utilization description |
| `Device_last_kernel_of_container` | Container device last kernel description |
| `MigInfo` | MIG device information for container |

## Example PromQL queries

Per-GPU memory usage on a node:

```promql
hami_host_gpu_memory_used_bytes
```

GPUs with the highest utilization on a node:

```promql
topk(5, hami_host_gpu_utilization_ratio)
```

Per-GPU memory usage grouped by device type on a node:

```promql
sum by (device_type) (hami_host_gpu_memory_used_bytes)
```

vGPU memory usage of every container:

```promql
hami_vgpu_memory_used_bytes
```

vGPU memory used as a fraction of the limit for every container:

```promql
hami_vgpu_memory_used_bytes / hami_vgpu_memory_limit_bytes
```

Total vGPU memory usage in the cluster:

```promql
sum(hami_vgpu_memory_used_bytes)
```

Containers using more than 80% of their vGPU memory limit:

```promql
(hami_vgpu_memory_used_bytes / hami_vgpu_memory_limit_bytes) > 0.8
```

Containers whose device utilization is above 50%:

```promql
hami_container_device_utilization_ratio > 50
```

Containers that have not run a kernel for more than 5 minutes:

```promql
hami_container_last_kernel_elapsed_seconds > 300
```

MIG instances currently in use:

```promql
hami_mig_device_info
```
