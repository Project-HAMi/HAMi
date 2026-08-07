# Feature Request: GPU Monitor Cgroup‑Driver Fallback Mode
功能需求：GPU监控新增cgroup‑driver降级读取模式

## Problem Description 问题描述
Currently, monitoring depends solely on reading shared memory.
When using the whole card (whole‑GPU mode), users are forced to mount the interception library,
but in whole‑card mode there's actually no need to mount it — its only purpose in that scenario is to collect monitoring metrics.
Mounting the interception library inevitably introduces a performance overhead (~2%), so we'd like to report monitoring metrics without mounting the interception library.

> 目前，监控仅依赖于读取共享内存。当使用整张卡（全GPU模式）时，用户会被迫挂载拦截库，
> 但在全卡模式下，实际上不需要挂载它——在这种场景下，挂载拦截库仅仅是为了收集监控指标。
> 安装拦截库会带来约2%的性能开销，因此希望在不挂载拦截库的前提下完成监控指标上报。

## Desired Functionality 需要的功能
Add a new reporting mode that doesn't read shared memory;
instead it reads per‑pod usage via the cgroup driver.

> 添加一套不依赖共享内存的监控上报模式，通过 cgroup 驱动读取 Pod 维度的 GPU 使用情况。

## Proposed Implementation 建议实现
Add a `cgroup‑driver` read path to the GPUmonitor.
If shared‑memory read fails, fall back to the cgroup‑driver mode.

Specifically:
1. First use NVML to read all processes using the GPU.
2. Then use the cgroup driver to read all PIDs inside the given pod.
3. Cross‑reference these PIDs against utilization data collected by NVML.
4. Reconstruct per‑process and per‑pod GPU usage metrics.

> 为 GPUmonitor 新增 cgroup‑driver 读取链路。当共享内存读取失败时，自动降级到 cgroup‑driver 模式。
> 具体步骤：
> 1. 通过 NVML 获取当前占用GPU的全部进程；
> 2. 通过 cgroup 驱动获取目标 Pod 内的所有 PID；
> 3. 将 cgroup 的 PID 列表与 NVML 获取的GPU利用率数据做关联匹配；
> 4. 还原出每个进程以及每个Pod维度的GPU使用率指标。
