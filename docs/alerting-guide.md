# HAMi GPU Metrics Alerting Guide and Runbooks

This guide provides operational guidance, metric semantics, reference PromQL queries, Helm configuration options, and runbooks for setting up Prometheus alerting on HAMi GPU metrics.

---

## 1. Overview of HAMi Metrics

HAMi exports cluster-level and container-level metrics through the scheduler monitor endpoint and the `vGPUmonitor` component. These metrics enable cluster operators to track GPU memory utilization, compute core allocations, resource quota usage, and device health across heterogeneous hardware backends (NVIDIA, AMD, Ascend, Hygon, Metax, Biren, etc.).

---

## 2. Key Metrics Reference

| Metric Name | Type | Description | Labels | Backend Applicability |
| --- | --- | --- | --- | --- |
| `hami_gpu_memory_limit_bytes` | Gauge | Total physical GPU memory capacity on the node in bytes | `node`, `device_uuid`, `device_index`, `device_type` | All |
| `hami_gpu_core_limit_ratio` | Gauge | Total compute core capacity limit on the node | `node`, `device_uuid`, `device_index`, `device_type` | All |
| `hami_gpu_memory_allocated_bytes` | Gauge | Total GPU memory currently allocated on the node in bytes | `node`, `device_uuid`, `device_index`, `total_core`, `device_type` | All |
| `hami_gpu_core_allocated_ratio` | Gauge | Total compute core percentage currently allocated on the node | `node`, `device_uuid`, `device_index`, `device_type` | All |
| `hami_gpu_shared_count` | Gauge | Number of containers sharing the GPU device | `node`, `device_uuid`, `device_index`, `device_type` | All |
| `hami_node_gpu_memory_allocated_ratio` | Gauge | Ratio of memory allocated versus total memory on a GPU device | `node`, `device_uuid`, `device_index` | All |
| `hami_vgpu_memory_allocated_bytes` | Gauge | Per-container allocated GPU memory in bytes | `node`, `pod`, `namespace`, `container_index`, `device_uuid` | All |
| `hami_vgpu_core_allocated_ratio` | Gauge | Per-container allocated compute core percentage | `node`, `pod`, `namespace`, `container_index`, `device_uuid` | All |
| `hami_resource_quota_used` | Gauge | Per-namespace HAMi resource quota consumption | `namespace`, `quota_name`, `limit` | All |
| `hami_node_gpu_mig_instance_info` | Gauge | Realized MIG instance placement and identity details | `node`, `device_uuid`, `device_index`, `mig_uuid`, `profile`, `gpu_instance_id`, `compute_instance_id`, `placement_start`, `placement_size` | NVIDIA MIG |

---

## 3. Standard Operational Alerts & PromQL

### 3.1. Node GPU Memory Exhaustion (`HAMiGPUMemoryNearlyExhausted`)

- **Description**: Fires when a node's scheduler-allocated GPU memory percentage exceeds 90%.
- **PromQL**:
  ```promql
  100 * hami_node_gpu_memory_allocated_ratio > 90
  ```
- **Duration (`for`)**: `10m`
- **Default Severity**: `warning`
- **Runbook**:
  1. Identify the affected node and `device_uuid` from the alert labels:
     `kubectl get nodes -o wide`
  2. Inspect workloads running on that node:
     `kubectl get pods -A -o wide --field-selector spec.nodeName=<node>`
  3. Verify if new pod scheduling attempts are failing due to `CardInsufficientMemory`.
  4. Consider expanding node pool capacity or tuning pod memory allocations.

---

### 3.2. Node GPU Core Exhaustion (`HAMiGPUCoresNearlyExhausted`)

- **Description**: Fires when a device's scheduler-allocated GPU compute cores exceed 90%.
- **PromQL**:
  ```promql
  hami_gpu_core_allocated_ratio > 90
  ```
- **Duration (`for`)**: `10m`
- **Default Severity**: `warning`
- **Runbook**:
  1. Check device core allocation and usage metrics.
  2. Verify if pods requiring exclusive GPU core allocation (`coresreq = 100`) are stuck in Pending state.

---

### 3.3. Container vGPU Memory Near Limit (`HAMivGPUMemoryNearLimit`)

- **Description**: Fires when a container is consuming over 95% of its allocated vGPU memory limit and may hit an out-of-memory error.
- **PromQL**:
  ```promql
  100 * (hami_vgpu_memory_allocated_bytes / hami_gpu_memory_limit_bytes) > 95
  ```
- **Duration (`for`)**: `5m`
- **Default Severity**: `warning`
- **Runbook**:
  1. Identify the container, pod, and namespace from alert labels.
  2. Inspect container memory logs and application heap usage.
  3. Increase the container `gpu.memory` resource limit if necessary.

---

### 3.4. Scheduler Metrics Absent (`HAMiSchedulerMetricsAbsent`)

- **Description**: Fires when HAMi scheduler metrics are completely absent, indicating a potential scheduler monitor service outage.
- **PromQL**:
  ```promql
  absent(hami_gpu_memory_limit_bytes) == 1
  ```
- **Duration (`for`)**: `15m`
- **Default Severity**: `critical` (Disabled by default in Helm chart)
- **Runbook**:
  1. Check HAMi scheduler pod status:
     `kubectl get pods -n hami-system -l app.kubernetes.io/component=scheduler`
  2. Verify scheduler logs for crashes or endpoint failures:
     `kubectl logs -n hami-system -l app.kubernetes.io/component=scheduler`

---

## 4. Helm Chart Monitoring Configuration

HAMi Helm chart supports opt-in `PrometheusRule` provisioning. The following table lists key Helm values under `prometheus.alerts`:

| Parameter | Description | Default Value |
| --- | --- | --- |
| `prometheus.enabled` | Enable Prometheus integration (ServiceMonitor and alerts) | `false` |
| `prometheus.alerts.enabled` | Install a `PrometheusRule` with HAMi GPU alerts | `false` |
| `prometheus.alerts.labels` | Extra labels on the `PrometheusRule` for operator `ruleSelector` | `{}` |
| `prometheus.alerts.rules.gpuMemoryNearlyExhausted` | Threshold and duration for node GPU memory alert | `{enabled: true, threshold: 90, for: 10m, severity: warning}` |
| `prometheus.alerts.rules.gpuCoresNearlyExhausted` | Threshold and duration for node GPU cores alert | `{enabled: true, threshold: 90, for: 10m, severity: warning}` |
| `prometheus.alerts.rules.vgpuMemoryNearLimit` | Threshold and duration for container vGPU memory alert | `{enabled: true, threshold: 95, for: 5m, severity: warning}` |
| `prometheus.alerts.rules.schedulerMetricsAbsent` | Duration and severity for missing scheduler metrics alert | `{enabled: false, for: 15m, severity: critical}` |

---

## 5. Threshold Tuning Guidelines for Operators

Different workload profiles require different alert thresholds:

1. **AI Training Workloads**: Training jobs typically require dedicated memory and compute. Set `gpuMemoryNearlyExhausted` threshold to 95% and monitor `hami_gpu_shared_count > 1` to ensure non-oversubscribed allocation.
2. **Inference Workloads**: Inference microservices often rely on time-slicing. Monitor latency metrics alongside `hami_gpu_shared_count` to ensure container density does not impact SLA.
3. **Heterogeneous Hardware Backends**:
   - **NVIDIA MIG Mode**: Track `hami_node_gpu_mig_instance_info` to ensure MIG profiles match pod requests.
   - **AMD / Ascend Backends**: Ensure core allocation metrics are normalized to percentages when evaluating `hami_gpu_core_allocated_ratio`.
