# NVIDIA GB10 (Grace-Blackwell iGPU) Support

## Introduction

The NVIDIA GB10 (Grace-Blackwell "superchip", as shipped in the DGX Spark) is an
**integrated GPU** that shares unified LPDDR5X memory with the Grace CPU over a
coherent NVLink-C2C link. It differs from a discrete GPU in two ways that affect
HAMi's device plugin:

1. **No dedicated framebuffer.** `nvmlDeviceGetMemoryInfo()` returns
   `ERROR_NOT_SUPPORTED` because there is no fixed device-local memory pool to
   report — the "GPU memory" is system memory allocated dynamically.
2. **CDI-only exposure.** The device is provisioned through the NVIDIA Container
   Toolkit's [CDI](https://github.com/cncf-tags/container-device-interface)
   mechanism. It is described by an on-node CDI spec
   (`/var/run/cdi/k8s.device-plugin.nvidia.com-gpu.json`) and is **not**
   enumerable via NVML inside the device-plugin container.

Because HAMi's device plugin historically discovered devices only through NVML,
on a GB10 node `ResolvePlatform()` returns `unknown` and the plugin exits with:

```text
factory.go] Incompatible strategy detected auto
main.go] error starting plugins: ... failed to construct resource managers:
         invalid device discovery strategy
```

To support these accelerators, the NVIDIA device plugin can discover GPUs
directly from the node's CDI specs (no NVML required) using the `cdi`
device-discovery strategy.

## Prerequisites

- The **NVIDIA GPU Operator** (or NVIDIA Container Toolkit) is installed and has
  generated the CDI spec at `/var/run/cdi/k8s.device-plugin.nvidia.com-gpu.json`.
  You can verify the node's own device plugin advertises `nvidia.com/gpu` and
  that GPU Feature Discovery has labelled the node, e.g.
  `nvidia.com/gpu.product=NVIDIA-GB10`.
- The node carries the label HAMi's device-plugin DaemonSet selects on
  (`gpu=on` by default):

  ```bash
  kubectl label node <gb10-node> gpu=on
  ```

- The kernel's inotify limits are high enough for the plugin's filesystem
  watcher. Busy nodes can exhaust the default `fs.inotify.max_user_instances`
  (128), which makes the plugin fail at start-up with
  `couldn't initialize inotify: too many open files`. Raise it persistently:

  ```bash
  # /etc/sysctl.d/99-hami-inotify.conf
  fs.inotify.max_user_instances = 8192
  fs.inotify.max_user_watches   = 524288
  ```

  ```bash
  sudo sysctl --system
  ```

## Configuration

Set the following on the NVIDIA device plugin (Helm `values.yaml`):

```yaml
devicePlugin:
  # Discover devices from the on-node CDI specs instead of via NVML.
  deviceDiscoveryStrategy: "cdi"
  # Inject the device through CDI (matches the NVIDIA GPU Operator plugin).
  deviceListStrategy: "cdi-annotations,cdi-cri"
  # Unified memory (in MiB) HAMi should treat as schedulable per GPU. NVML
  # cannot report it on a unified-memory GPU, so it is a policy value — set it
  # at or below the node's total unified memory, leaving headroom for the OS.
  # See the note below for deriving it from the node (the example is not a
  # direct GiB conversion).
  preConfiguredDeviceMemory: 122566
  # Optional. Device type recorded for scheduling/`use-gputype`.
  # Defaults to "NVIDIA-GB10" when empty.
  preConfiguredDeviceType: "NVIDIA-GB10"
```

Notes:

- `deviceDiscoveryStrategy` also accepts `auto` (the default), which falls back
  to `cdi` automatically when no NVML/Tegra platform is detected **and** a CDI
  device-list strategy is active and CDI specs are present. Setting it to `cdi`
  explicitly is recommended for clarity on GB10 nodes.
- `preConfiguredDeviceMemory` is a scheduling **policy** value — how much unified
  memory HAMi treats as schedulable per GPU — interpreted as MiB (HAMi multiplies
  it by 1024×1024 to get bytes). It is **not** a hardware GPU-memory readout: on a
  unified-memory device the memory is shared with the CPU/OS. The example
  `122566` is **this DGX Spark's total system memory as reported by Kubernetes**,
  not a direct GiB conversion (120 GiB would be 122880 MiB):

  ```bash
  kubectl get node <gb10-node> -o jsonpath='{.status.capacity.memory}'
  # 125506464Ki  ->  125506464 / 1024 = 122566 MiB  (~119.7 GiB)
  ```

  Choose a value at or below this, leaving headroom for the OS. 
  It can be overridden per node via `nodeConfiguration.config` (`preconfigureddevicememory`).
- `preConfiguredDeviceType` can be overridden per node via `preconfigureddevicetype`.

## How it works

1. **Discovery** — the plugin reads the CDI specs under `/etc/cdi` and
   `/var/run/cdi`, enumerating GPU devices for vendor
   `k8s.device-plugin.nvidia.com`, class `gpu` (the `all` meta-device is
   excluded). Each CDI device becomes a schedulable GPU keyed by its CDI device
   name (a GPU UUID).
2. **Registration** — because NVML is unavailable, per-GPU memory and type come
   from `preConfiguredDeviceMemory` / `preConfiguredDeviceType` instead of being
   queried from the driver.
3. **Allocation** — on `Allocate`, the plugin emits the CDI device reference
   (`k8s.device-plugin.nvidia.com/gpu=<uuid>`) via the `cdi-annotations` /
   `cdi-cri` device-list strategy, so containerd injects the device. HAMi's
   memory/core limiting (libvgpu) is applied on top as usual.

## Limitations

- Because there is no per-device NVML data, **all** GPUs discovered in `cdi`
  mode share the same `preConfiguredDeviceMemory` and `preConfiguredDeviceType`.
  This targets homogeneous CDI-only nodes (e.g. a single-GB10 DGX Spark).
- Health checks and MIG are not available in `cdi` mode (both require NVML).
- GPU topology scoring (`ENABLE_TOPOLOGY_SCORE`) is skipped in `cdi` mode: it
  computes pairwise P2P/NVLink scores via NVML, which is unavailable and
  meaningless for a single CDI-only iGPU.
