# HAMi GPU Metrics Alerting Guide and Runbooks

This guide provides operational guidance, metric semantics, reference PromQL queries, and runbooks for setting up Prometheus alerting on HAMi GPU metrics.

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

## 3. Recommended Operational Alerts & PromQL

### 3.1. High Node GPU Memory Allocation (`HAMiGPUNodeMemoryHigh`)

- **Description**: Triggers when a GPU device on a node has allocated over 90% of its memory capacity.
- **PromQL**:
  ```promql
  (hami_gpu_memory_allocated_bytes / hami_gpu_memory_limit_bytes) * 100 > 90
  ```
- **Severity**: Warning (over 90%), Critical (over 98%)
- **Runbook**:
  1. Identify the node and `device_uuid` from the alert labels.
  2. Inspect workloads running on that node:
     `kubectl get pods -A -o wide --field-selector spec.nodeName=<node>`
  3. Verify if new pod scheduling attempts are failing due to `CardInsufficientMemory`.
  4. Consider expanding node pool or tuning pod memory limits.

---

### 3.2. High Node GPU Core Allocation (`HAMiGPUNodeCoreHigh`)

- **Description**: Triggers when compute core allocation on a GPU device exceeds 90%.
- **PromQL**:
  ```promql
  hami_gpu_core_allocated_ratio > 90
  ```
- **Severity**: Warning
- **Runbook**:
  1. Check node device core utilization metrics.
  2. Verify if pods requiring exclusive GPU core allocation (`coresreq = 100`) are pending.

---

### 3.3. High Container Device Sharing Contention (`HAMiGPUSharedDeviceContention`)

- **Description**: Triggers when more than 8 containers share a single physical GPU device, which may lead to time-slicing overhead and latency spikes.
- **PromQL**:
  ```promql
  hami_gpu_shared_count > 8
  ```
- **Severity**: Warning
- **Runbook**:
  1. Inspect the pods assigned to `device_uuid`.
  2. Evaluate workload latency sensitivity.
  3. Adjust scheduler time-slicing concurrency limits or node anti-affinity rules.

---

### 3.4. Namespace Resource Quota Exhaustion (`HAMiResourceQuotaNearLimit`)

- **Description**: Triggers when a namespace reaches over 85% of its allocated HAMi GPU resource quota limit.
- **PromQL**:
  ```promql
  (hami_resource_quota_used / on(namespace, quota_name) scalar(hami_resource_quota_used)) * 100 > 85
  ```
- **Severity**: Warning
- **Runbook**:
  1. Check namespace usage across GPU memory and core requests.
  2. Notify namespace owners or adjust `DeviceQuota` limits if necessary.

---

## 4. Threshold Tuning Guidelines for Operators

Different workload profiles require different alert thresholds:

1. **AI Training Workloads**: Training jobs typically require dedicated memory and compute. Set `HAMiGPUNodeMemoryHigh` threshold to 95% and monitor `hami_gpu_shared_count > 1` to ensure non-oversubscribed allocation.
2. **Inference Workloads**: Inference microservices often rely on time-slicing. Monitor latency metrics alongside `hami_gpu_shared_count` to ensure container density does not impact SLA.
3. **Heterogeneous Hardware Backends**:
   - **NVIDIA MIG Mode**: Track `hami_node_gpu_mig_instance_info` to ensure MIG profiles match pod requests.
   - **AMD / Ascend Backends**: Ensure core allocation metrics are normalized to percentages when evaluating `hami_gpu_core_allocated_ratio`.
