# AMD Instinct vGPU Support (issue:#1707)

## 1. Motivation

This proposal adds per-pod **memory limiting** and **compute-unit (CU) partitioning** for 
AMD ROCm GPUs, so multiple pods can share one Instinct GPU.

## 2. Goals

- Fractional allocation by memory (`amd.com/gpumem`, MB) and compute (`amd.com/gpucores`, CU count).
- Exclusive, non-overlapping CU partitioning across pods.
- Following HAMi's existing architecture design to achieve the functionality.

## 3. Approach

**Why LD_AUDIT instead of LD_PRELOAD?**
In our prototype on ROCm 7.x, LD_PRELOAD broke HIP — interposing HIP symbols leads 
to recursive re-entry through HIP-internal calls. 
Switching to LD_AUDIT (`la_symbind64`), which intercepts only cross-library bindings, 
resolved it. The existing NVIDIA LD_PRELOAD path is unchanged.

**Advantages of CU masking, compared to hardware partitioning (CPX/NPS).**
Masking (`ROC_GLOBAL_CU_MASK`) assigns an arbitrary and fine-grained per-pod CU partitioning at container start ([AMD Docs](https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html)). 
On the other hand, hardware partitioning (CPX/NPS) slices only at fixed XCD granularity and is set per physical GPU.

## 4. Protocol (scheduler <-> device-plugin)

For AMD, it adds one AMD-specific annotation for the CU bitmap.

### 4.1 Node registration (device-plugin -> node annotations)

Registered under `hami.io/node-amd-register`, in JSON format — an array of `DeviceInfo` as follows for example:

```json
[
  {
    "id": "<device-id>",
    "index": 0,
    "count": 1,
    "devmem": 196608,
    "devcore": 304,
    "type": "AMDGPU",
    "numa": 0,
    "mode": "hami-core",
    "health": true
  }
]
```

- `devmem`  = total device memory in MB (e.g. 196608 for MI300X).
- `devcore` = total CU count (e.g. 304 for MI300X). 
- `id`      = device identifier.


### 4.2 Pod allocation (scheduler -> pod annotations -> Allocate)

The allocation result is written under the **AMD-specific** key (each vendor has its own):

```text
hami.io/amd-devices-allocated: <UUID>,AMDGPU,<memMB>,<cuCount>:;
```

Plus, a dedicated AMD annotation carrying the per-device CU bitmap. Following the
convention other vendors use for allocation data that does not fit the standard
`UUID,Type,mem,cores` encoding (e.g. Ascend's `huawei.com/<model>`), 
this is a separate annotation under the **`amd.com/`** namespace with a JSON value, for example:

```json
amd.com/cu-mask: [{"uuid":"<UUID1>","cu_mask":"0x337f"},{"uuid":"<UUID2>","cu_mask":"0x00ff"}]
```

`cu_mask` uses ROCm's hex-bitmask form of `CU_list` (`0x[0-F]*`, e.g. `0x337f`; see
<https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html>). 
A JSON value avoids inventing a delimiter scheme (and the :/; collision with 
`ROC_GLOBAL_CU_MASK`'s own grammar).

The device-plugin translates each entry into the per-GPU `ROC_GLOBAL_CU_MASK` 
at container start.

Exclusivity of CU bitmaps across pods on a device must be enforced 
under a node lock (`AMDDevices.LockNode` and `ReleaseNodeLock`) so multiple pods never receive overlapping masks.

Optional handshake:
```text
amd.com/cu-mask-assigned: "false" -> "true"  (set by device-plugin)
```

## 5. Resource model and core_limit -> CU mask

Example Pod request:

```yaml
resources:
  limits:
    amd.com/gpu: 1          # number of physical AMD GPUs
    amd.com/gpumem: 16384   # MB
    amd.com/gpucores: 152   # CU count
```

### 5.1 Memory limit
`amd.com/gpumem` (MB) flows through the shared annotation, is 
injected by the device plugin as `HIP_DEVICE_MEMORY_LIMIT_<i>` (value `<MB>m`).

### 5.2 Core limit -> CU mask (Discussion needed)

`amd.com/gpucores` is a **CU count**, not a percentage (contrast NVIDIA's
SM-utilization %). 
And converting a count into a usable `ROC_GLOBAL_CU_MASK` is not a
simple "set N bits" operation:

1. **The hard guarantee is non-overlap.** The scheduler
   assigns each pod a CU bitmap that does **not overlap** any other pod's bitmap on the same device. 
   We enforce it via the bitmap allocator under a node lock.

2. **Zero-interference is not guaranteed.** Even with non-overlapping 
   masks, residual interference remains (as reported in #1707).

The count -> bitmap conversion is a **scheduler's** responsibility, because non-overlap requires knowledge of the device's current
allocation state. 

The device-plugin simply passes the scheduler-decided mask through verbatim as `ROC_GLOBAL_CU_MASK`.

## 6. Known limitations

- **No `amd-smi` / `rocm-smi` virtualization.** 
  These read sysfs/drm, not HIP, so
  LD_AUDIT cannot intercept them; in-container tools may report physical resources.

## 7. Discussion points

- **Device-plugin layering.** In #1707, the AMD vGPU device-plugin is proposed to be built on ROCm/k8s-device-plugin, which already advertises whole-GPU `amd.com/gpu`. Since kubelet cannot register the same resource from two plugins, the natural path is to **extend the ROCm plugin** so a single plugin owns `amd.com/gpu` and also advertises the fractional `amd.com/gpumem` / `amd.com/gpucores` (to be confirmed).
