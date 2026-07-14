# AMD Instinct vGPU Support (issue:#1707)

## 1. Motivation

This proposal adds per-pod **memory limiting** and **compute-unit (CU) partitioning** for 
AMD ROCm GPUs, so multiple pods can share one Instinct GPU.

## 2. Goals

- Fractional allocation by memory (`amd.com/gpumem`, MiB) and compute (`amd.com/gpucores`, percentage).
- Exclusive, non-overlapping CU partitioning across pods.
- Following HAMi's existing architecture design to achieve the functionality.

## 3. Approach

**Why LD_AUDIT instead of LD_PRELOAD?**
In our prototype on ROCm 7.x, LD_PRELOAD broke HIP — interposing HIP symbols leads 
to recursive re-entry through HIP-internal calls. 
Switching to LD_AUDIT (`la_symbind64`), which intercepts only cross-library bindings, 
resolved it. The existing NVIDIA LD_PRELOAD path is unchanged.

**Advantages of CU masking, compared to hardware partitioning (CPX/NPS).**
Masking (`HSA_CU_MASK`) assigns an arbitrary and fine-grained per-pod CU partitioning at container start ([AMD Docs](https://rocm.docs.amd.com/en/latest/how-to/setting-cus.html)).
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

- `devmem`  = total device memory in MiB (e.g. 196608 for MI300X).
- `devcore` = total CU count (e.g. 304 for MI300X). 
- `id`      = device identifier.


### 4.2 Pod allocation (scheduler -> pod annotations -> Allocate)

The scheduler allocation result is written under the **AMD-specific** keys:

```text
hami.io/amd-devices-to-allocate: <UUID>,amd,<memMiB>,<cuCount>:;
hami.io/amd-devices-allocated:   <UUID>,amd,<memMiB>,<cuCount>:;
```

The device-plugin converts `cuCount` into a non-overlapping CU range during
`Allocate`, and stores the per-device result in a separate annotation:

```json
hami.io/amd-cu-allocated: {"<UUID1>":"0-75","<UUID2>":"0-75"}
```

Each value uses HSA's CU ID-list grammar, for example `0-3,8,10-12`. The
device-plugin translates each entry into the container-local `HSA_CU_MASK` at
container start.

Exclusivity of CU ranges across pods on a device is enforced under the AMD
node lock (`AMDDevices.LockNode` and `ReleaseNodeLock`).

## 5. Resource model and core_limit -> CU mask

Example Pod request:

```yaml
resources:
  limits:
    amd.com/gpu: 1          # number of physical AMD GPUs
    amd.com/gpumem: 16384   # MiB
    amd.com/gpucores: 25    # percentage of physical CUs
```

### 5.1 Memory limit
`amd.com/gpumem` (MiB) flows through the shared annotation and is injected by
the device plugin as `HIP_DEVICE_MEMORY_LIMIT=<MiB>m`. The limit is scoped to
the container and applies to all GPUs mounted in that container. Each
container receives an independent allocation response and environment.

### 5.2 Core limit -> CU mask

`amd.com/gpucores` is a **percentage**, not a CU count. The scheduler converts
the requested percentage into the physical CU count for the selected device.
For example, `25` on a 304-CU MI300X allocates 76 CUs.

The scheduler records the computed CU count in the standard AMD allocation
annotation. The device-plugin selects a non-overlapping CU range and injects
it as `HSA_CU_MASK`.

**Zero-interference is not guaranteed.** Even with non-overlapping masks,
residual interference remains (as reported in #1707).

## 6. Known limitations

- **No `amd-smi` / `rocm-smi` virtualization.** 
  These read sysfs/drm, not HIP, so
  LD_AUDIT cannot intercept them; in-container tools may report physical resources.
- **Not support mixed type in one node.**
  Currently, device plugin get gpu type form the label `amd.com/gpu.product-name`, This label can not describe more than one gpu types.

## 7. Discussion points

- **Device-plugin layering.** In #1707, the AMD vGPU device-plugin is proposed to be built on ROCm/k8s-device-plugin, which already advertises whole-GPU `amd.com/gpu`. Since kubelet cannot register the same resource from two plugins, the natural path is to **extend the ROCm plugin** so a single plugin owns `amd.com/gpu` and also advertises the fractional `amd.com/gpumem` / `amd.com/gpucores` (optional, is not must have for hami while can be useful for other schedulers).
