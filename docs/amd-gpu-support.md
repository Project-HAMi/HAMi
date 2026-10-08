## Introduction

HAMi shares AMD GPUs (Radeon and Instinct) between pods with the following resources:

- `amd.com/gpu`: number of GPUs
- `amd.com/gpumem`: device memory in MiB per GPU
- `amd.com/gpumem-percentage`: device memory per GPU as a percentage of its VRAM (1-100); `amd.com/gpumem` wins if both are set
- `amd.com/gpucores`: compute units per GPU as a percentage (1-100)

A pod that asks for no memory or cores gets the whole GPU.

### Deploy

1. Install the HAMi chart as usual. It registers the AMD resources with the scheduler (`devices.amd.*` in `values.yaml`).
2. Install [amd-device-plugin](https://github.com/Project-HAMi/amd-device-plugin) on the AMD nodes. It publishes each GPU in the node annotation `hami.io/node-amd-register`, hands out CU slices and memory limits to the pods, and marks a GPU unhealthy when its DRM device cannot be opened or the AMD Device Metrics Exporter reports it unhealthy.

Memory limits are enforced by [amd-hami-core](https://github.com/Project-HAMi/amd-hami-core) (`libamvgpu`, injected by the plugin) and, where the node has the `dmem` cgroup controller and the systemd cgroup driver, by the kernel as well.

### Run AMD jobs

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: amd-gpu-cores
spec:
  containers:
    - name: rocm
      image: rocm/dev-ubuntu-24.04:7.2.4
      command: ["bash", "-c", "sleep infinity"]
      resources:
        limits:
          amd.com/gpu: 1
          amd.com/gpumem: 4096     # MiB
          amd.com/gpucores: 25     # percent of the GPU's compute units
```

More examples are available under `examples/amd/`, including how to pick or avoid a GPU type or UUID with `amd.com/use-gputype`, `amd.com/nouse-gputype`, `amd.com/use-gpu-uuid` and `amd.com/nouse-gpu-uuid`.

### Things to know

- **Cores are CU masks.** `amd.com/gpucores` becomes an `HSA_CU_MASK`. On RDNA GPUs a request is rounded up to whole WGPs (2 CUs), and the plugin publishes the granularity as `cuPerWGP`. The mask is enforced by the ROCm runtime and pinned by `libamvgpu`; a child process started without the `LD_AUDIT` hook is not covered.
- **Share count.** The plugin publishes each GPU several times: 2 on RDNA4 (gfx12), whose throughput falls when more processes share it, 10 elsewhere. `dp.splitCount` in the plugin chart overrides it.
- **Whole-GPU pods** get neither a CU mask nor a memory limit and do not load `libamvgpu`.
- **Monitoring.** `monitor.enabled=true` in the plugin chart runs `k8s-vgpu-monitor`, which serves `hami_vgpu_memory_used_bytes` and `hami_vgpu_memory_limit_bytes` per container and `hami_host_gpu_memory_used_bytes`, `hami_host_gpu_utilization_ratio`, `hami_host_gpu_temperature_celsius` and `hami_host_gpu_power_usage_watts` per GPU on port 9394, under the metric names of the NVIDIA monitor. Per-container utilization is not reported: the kernel does not account compute time per process.
- **Metrics from the scheduler** report AMD cores as a percentage of the GPU.
