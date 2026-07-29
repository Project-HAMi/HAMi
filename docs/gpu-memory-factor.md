# gpuMemoryFactor: Configuration and Sizing Guide

## Overview

`memoryFactor` is an integer multiplier that controls how memory requests are
interpreted by the HAMi scheduler and device plugin. When a pod requests
`N MiB` of GPU memory, the scheduler multiplies that value by `memoryFactor`
before comparing it against the device's available memory. This lets operators
express memory requests in coarser granularities (e.g. multiples of 2 MiB
instead of 1 MiB) without changing every pod spec.

**Default value:** `1` (no scaling; requests are taken at face value).

### Where to set it

`memoryFactor` lives in the scheduler's `device-config.yaml`, under the
`nvidia:` section:

```yaml
# charts/hami/templates/scheduler/device-configmap.yaml (or your own override)
nvidia:
  memoryFactor: 1   # <- change this value
```

It can also be set directly in a `device-config.yaml` file mounted into the
scheduler pod.

---

## The kubelet gRPC 4 MB limit

The Kubernetes device plugin protocol requires the device plugin to stream a
list of all virtual device IDs to kubelet via the `ListAndWatch` gRPC call.
For NVIDIA GPUs, HAMi advertises one entry per MiB of GPU memory divided by
`memoryFactor`:

```text
entries_per_gpu = ceil(totalMemMiB / memoryFactor)
total_entries   = entries_per_gpu × gpuCount
```

The kubelet gRPC server enforces a hard message-size limit of roughly **4 MB**.
Each device-plugin entry is approximately 64 bytes, so the effective ceiling is
around **60 000 entries** in a single `ListAndWatch` response.

When `total_entries > 60 000`, kubelet silently rejects the response. The
device plugin does **not** receive an explicit error from kubelet in the
protocol stream; the connection drops and is re-established, but the next
message is also too large. The observable symptoms are:

- `volcano.sh/vgpu-memory` (or `nvidia.com/gpumem`) shows **0** or a **stale**
  value in node allocatable resources.
- Pods that request GPU memory are never scheduled.

Starting from the fix for issue #2187, HAMi now:

1. **Logs a Warning at runtime** from `GetPluginDevices` whenever the generated
   entry count exceeds 60 000, including the exact minimum safe `memoryFactor`
   computed from the actual GPU memory and count detected on the node.
2. **Checks and returns the `s.Send` error** in `ListAndWatch`. If kubelet
   rejects the response (e.g. `ResourceExhausted: received message larger than
   max`), the error is logged with an actionable message and the stream is
   closed gracefully so kubelet can reconnect.

---

## Sizing formula

The **runtime check** computes total entries directly:

```text
estimatedEntries = ceil(totalMemMiB / memoryFactor) × gpuCount
```

The **minimum safe factor** is derived by rearranging the constraint
`estimatedEntries ≤ 60 000`:

```text
minFactor = ceil(totalMemMiB / floor(60000 / gpuCount))
```

Use this formula to find the smallest `memoryFactor` that keeps `total_entries`
within the kubelet limit.

### Reference table — common data-centre GPUs

| GPU model          | Memory (GiB) | totalMemMiB | 1 GPU min factor | 8 GPU min factor |
| :----------------- | :----------: | :---------: | :--------------: | :--------------: |
| NVIDIA T4          | 16           | 16 384      | 1                | 3                |
| NVIDIA A10         | 24           | 24 576      | 1                | 4                |
| NVIDIA A30         | 24           | 24 576      | 1                | 4                |
| NVIDIA A100 40 GB  | 40           | 40 960      | 1                | 6                |
| NVIDIA A100 80 GB  | 80           | 81 920      | 2                | 11               |
| NVIDIA A800 80 GB  | 80           | 81 920      | 2                | 11               |
| NVIDIA H100 80 GB  | 80           | 81 920      | 2                | 11               |
| NVIDIA H100 94 GB  | 94           | 96 256      | 2                | 13               |
| NVIDIA H200 141 GB | 141          | 144 384     | 3                | 20               |
| NVIDIA B200 180 GB | 180          | 184 320     | 4                | 25               |

> Min factor values are `ceil(totalMemMiB / floor(60000 / gpuCount))`.
> Round up to the next integer when your cluster has more GPUs per node.

### Single-GPU examples

**A800 (80 GiB), 1 GPU, factor=1 — broken:**

```text
entries = ceil(81920 / 1) × 1 = 81920   # exceeds 60000 → kubelet rejects
```

**A800 (80 GiB), 1 GPU, factor=2 — correct:**

```text
entries = ceil(81920 / 2) × 1 = 40960   # safe ✓
minFactor = ceil(81920 / floor(60000 / 1)) = ceil(81920 / 60000) = 2
```

### Multi-GPU examples

**A800 (80 GiB), 8 GPUs, factor=2 — broken:**

```text
entries = ceil(81920 / 2) × 8 = 40960 × 8 = 327680   # exceeds 60000 → rejects
```

**A800 (80 GiB), 8 GPUs, factor=11 — correct:**

```text
entries = ceil(81920 / 11) × 8 = 7448 × 8 = 59584   # safe ✓
minFactor = ceil(81920 / floor(60000 / 8))
          = ceil(81920 / 7500)
          = ceil(10.92) = 11
```

---

## Runtime validation

Validation runs at **device plugin startup**, inside `GetPluginDevices`, once
the plugin has queried the hardware. This means the warning is based on actual
GPU memory rather than a configured estimate. Look for these log lines in the
device plugin pod:

```sh
kubectl logs -n <namespace> <device-plugin-pod>
```

**Warning logged when entry count exceeds 60 000:**

```text
W ... GetPluginDevices: entry count 81920 exceeds the kubelet gRPC message
     limit of ~60000 entries (~4 MB). kubelet will reject the ListAndWatch
     response and vgpu-memory will show 0 or a stale value.
     gpuCount=1, splitCount=81920, maxGPUMemMiB=81920.
     Minimum safe memoryFactor = ceil(81920 / floor(60000 / 1)) = 2.
```

**Error logged when kubelet actually rejects the send:**

```text
E ... ListAndWatch: failed to send initial device list (81920 entries) for
     resource 'nvidia.com/gpumem': rpc error: code = ResourceExhausted ...
     If the error is 'ResourceExhausted' or 'grpc: received message larger
     than max', increase memoryFactor to at least
     ceil(totalMemMiB / floor(60000 / gpuCount));
     see the preceding GetPluginDevices warning for the exact value.
```

If you see either message, increase `memoryFactor` according to the table
above and restart the scheduler pod.

---

## Side-effects of a non-unity factor

When `memoryFactor > 1`, pod memory requests are interpreted as **logical MiB**
that are multiplied by `memoryFactor` before scheduling:

```text
scheduled_memory_MiB = request_MiB × memoryFactor
```

For example, if a pod requests `1024 MiB` and `memoryFactor=2`, HAMi allocates
`2048 MiB` on the physical GPU. This means:

- Memory requests in pod specs are expressed in units of `memoryFactor` MiB.
  Document this convention for your users.
- The minimum allocatable memory per container is `memoryFactor` MiB (one
  virtual entry).
- `deviceMemoryScaling` is applied **before** the factor and operates on the
  physical device's reported memory.

---

## Choosing the right value

1. Find your GPU's total memory in MiB: `totalMemMiB = GiB × 1024`.
2. Count the maximum number of GPUs per node: `gpuCount`.
3. Apply the formula:

   ```text
   memoryFactor = ceil(totalMemMiB / floor(60000 / gpuCount))
   ```

4. If the result is 1, keep the default. Otherwise set `memoryFactor` in
   `device-config.yaml` and redeploy the scheduler.

---

## Helm configuration

`memoryFactor` is not exposed as a top-level Helm value because it depends on
the GPU model deployed in the cluster. Override it via a `device-config.yaml`
file or by patching the scheduler ConfigMap directly:

```yaml
# Example: values override snippet
scheduler:
  # Mount a custom device-config.yaml that sets memoryFactor: 2
  extraVolumes:
    - name: device-config
      configMap:
        name: my-device-config
  extraVolumeMounts:
    - name: device-config
      mountPath: /config
```

Or, if you supply a `files/device-config.yaml` in your Helm chart overlay, the
template will use it automatically (see the comment in
`charts/hami/templates/scheduler/device-configmap.yaml`).
