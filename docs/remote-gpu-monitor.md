# Remote GPU monitoring

HAMi runs `remoteGPUmonitor` as a separate container in each chart-managed
Lupine server Pod. It scrapes the adjacent server's HTTP `/metrics` endpoint
and resolves workload identities through a cluster-wide, read-only Pod informer.
No NVML, CUDA library, host PID namespace or host filesystem mount is needed by
this component. Its ServiceAccount only reads Pods.

Build the HAMi image with `make docker` (or `make remoteGPUmonitor` for the binary).
The regular HAMi image includes the binary through `version.mk`; the monitor
uses `devicePlugin.monitor.image`. Use a Lupine image built with monitoring and
with support for the optional `LUPINE_WORKLOAD_ID` metadata field (Lupine
commit `b990d4aa2c33719e649604f6657bcfc9f5d7f895` or later). Older clients
or servers still work, but samples without workload identity cannot be attributed
to containers.

```yaml
devices:
  remotegpu:
    enabled: true
    monitor:
      enabled: true
      port: 31993
      resources:
        requests:
          cpu: 50m
          memory: 64Mi
```

For externally managed Lupine servers, deploy the binary separately with the
read-only Pod RBAC from `remotemonitor.yaml`, and pass `--node=<GPU server node>`,
`--lupine-metrics-url=http://<server>:<port>/metrics` and
`--metrics-bind-address=:31993`. The chart only creates monitors for its own
server DaemonSet. The metrics listen port must be free on host-network nodes.

## Identity and attribution

Admission overwrites `HAMI_REMOTE_POD_UID` using the Downward API, then sets
`LUPINE_WORKLOAD_ID=$(HAMI_REMOTE_POD_UID)/<container-name>`. Kubelet expands the
UID when launching each container, including init containers. The monitor
requires an exact Pod UID, container name and GPU UUID match against the
scheduler's allocation annotation on the specified GPU server node. Recreated
Pods cannot inherit old Pod samples; unknown identities and unallocated GPUs
are omitted. This metadata is attribution, not authentication.

The allocation annotation preserves init-container entries before app-container
entries. Their ordering provides `vdevice_index`. Multiple Lupine processes in
one container sum their memory; SM utilization percentages sum and cap at 100.
Lupine exports the workload identity as `client_id`, without a separate
`workload_id` label. It already aggregates processes whose identity, address,
hostname, process name and GPU match. HAMi sums any remaining source series
into one container/GPU metric, without exporting process labels. A repeated
source series with the same complete Lupine label set makes the scrape fail
to avoid double counting. No Prometheus recording rule is required.

Unset or invalid identities, and conflicting identities across connections of
one process, fall back to process IDs in Lupine. These do not match HAMi's
Pod UID/container identity and are omitted from container usage metrics.

## Metrics

The local and remote monitors share metric descriptors, including names, HELP
text, units and label lists. Both attach `zone="vGPU"`.

| Metric | Labels | Unit |
| --- | --- | --- |
| `hami_host_gpu_memory_used_bytes` | node, device_index, device_uuid, device_type | bytes |
| `hami_host_gpu_utilization_ratio` | node, device_index, device_uuid, device_type | 0–100 percent |
| `hami_vgpu_memory_used_bytes` | namespace, pod, container, vdevice_index, device_uuid | bytes |
| `hami_container_device_memory_bytes` | same container labels | bytes |
| `hami_vgpu_memory_limit_bytes` | same container labels | bytes |
| `hami_container_device_utilization_ratio` | same container labels | 0–100 percent |
| `hami_remote_gpumonitor_collect_success` | node | 1 for success, 0 for failure |

Memory limits use the allocated card capacity, capped by an injected
`CUDA_DEVICE_MEMORY_LIMIT` when present. Utilization is a sum of process samples,
not a precise union of concurrent GPU activity. Metrics Lupine cannot supply
(temperature, power, ECC, memory-controller utilization and last-kernel time)
are omitted. Missing usage samples are omitted rather than synthesized as zero.

A scrape has a three-second timeout and a 16 MiB body limit. HTTP errors,
malformed exposition, unavailable NVML or an unsynchronized Pod cache emit only
the failure metric; no previous usage is replayed. `/healthz` checks the process
and `/readyz` checks informer synchronization. A Service and optional
ServiceMonitor expose metrics when `prometheus.enabled` and the ServiceMonitor
CRD are present. Existing local monitor jobs can also export physical GPU
metrics on the server node; select the appropriate scrape job to avoid summing
physical usage twice.
