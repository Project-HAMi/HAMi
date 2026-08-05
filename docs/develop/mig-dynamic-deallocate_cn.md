# 动态 MIG 按需分配与回收

## 目标

动态 MIG 模式把硬件事实与调度策略分离：

- device plugin 通过 NVML 上报 GPU 支持的完整 profile 元数据和 placement。
- scheduler 根据显存请求、profile allowlist 和当前 placement 占用选择候选实例。
- device plugin 信任 scheduler 的 `profile + placement` 结果，在 `Allocate` 阶段精确创建 GI/CI。
- Pod 删除或结束后，根据节点上仍存活 Pod 的 allocation annotation 回收不再使用的实例。
- device plugin 重启时，通过 annotation 中的 `profile + placement + migUUID` 恢复管理状态。

该模型不再依赖静态 geometry/count 模板，也不再使用 `templateIdx/slotIdx`。

## Node 能力上报

MIG 模式下，`hami.io/node-nvidia-register` 中的每张 GPU 包含 `migProfiles`。每个 profile 上报：

- profile 名称和 NVML GI profile ID
- 实际可用显存、SM 数量和计算比例
- slice 数量和最大实例数
- copy engine、decoder、encoder、JPEG、OFA 等能力
- NVML 返回的全部合法 placement

例如 A100 40GB 的 `2g.10gb` 通常包含：

```json
{
  "name": "2g.10gb",
  "memoryMB": 9984,
  "sliceCount": 2,
  "instanceCount": 3,
  "placements": [
    {"start": 0, "size": 2},
    {"start": 2, "size": 2},
    {"start": 4, "size": 2}
  ]
}
```

scheduler 只从该上报构造候选实例，不根据配置中的 count 展开虚拟 slot。

## 配置

配置仅保留 profile allowlist：

```yaml
nvidia:
  migProfileAllowlist:
    - models: ["A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB"]
      profiles: ["1g.5gb", "2g.10gb", "3g.20gb", "7g.40gb"]
```

allowlist 决定哪些硬件 profile 可以参与调度，不定义 placement 或实例数量。

## Pod allocation annotation

scheduler 将分配结果写入 `hami.io/vgpu-mig-allocations`：

```json
[
  {
    "containerIndex": 0,
    "deviceIndex": 0,
    "gpuUUID": "GPU-xxx",
    "profile": "2g.10gb",
    "placement": {"start": 2, "size": 2}
  }
]
```

device plugin 创建实例后写回实际 `migUUID`：

```json
[
  {
    "containerIndex": 0,
    "deviceIndex": 0,
    "gpuUUID": "GPU-xxx",
    "profile": "2g.10gb",
    "placement": {"start": 2, "size": 2},
    "migUUID": "MIG-xxx"
  }
]
```

当前实现不兼容旧的 `templateIdx/slotIdx` Pod annotation。

## 调度流程

1. scheduler 读取 Node 上报的 profile 和 placement。
2. 根据 Pod 请求选择能够满足实际显存的最小允许 profile。
3. 排除与已分配实例重叠的 placement。
4. 将选中的 `profile + placement` 写入 Pod annotation。
5. 如果不存在连续且合法的 placement，Pod 保持 Pending，不进入 Bind。

placement 是否可创建以 NVML 的 `GetGpuInstancePossiblePlacements` 返回结果为准。scheduler 对多个待分配实例进行组合检查，避免局部选择造成后续实例无法放置。

## 创建流程

1. kubelet 调用 device plugin `Allocate`。
2. device plugin 按 container/device index 读取 scheduler reservation。
3. 校验 profile 和 placement 是否属于当前 GPU 的 NVML 能力集合。
4. `MigInstanceManager.EnsureAllocation` 在指定 placement 创建 GI 和 CI，不尝试其他位置。
5. 将生成的 MIG UUID 返回给容器运行时，并写回 Pod annotation。

同一 GPU 的 NVML 修改由卡级锁串行化，不同 GPU 可以并行操作。

## 回收流程

Pod annotation 是 HAMi allocation 的事实源。device plugin 每 5 秒执行一次 fail-closed reconciliation：

1. 列出调度到当前节点的未结束 Pod。
2. 从 `hami.io/vgpu-mig-allocations` 构造活跃的 `profile + placement` 集合。
3. 与 `MigInstanceManager` 中已创建的实例比较。
4. 销毁不再属于任何活跃 Pod 的 CI 和 GI。

Kubernetes API 查询失败或 annotation 无法解析时跳过本轮回收，保留现有实例，避免误删运行中任务。

动态 MIG UUID 不作为 kubelet 注册的资源 ID，因此 kubelet pod-resources API 无法可靠表达该 UUID。当前实现不再挂载或轮询 pod-resources socket。

## 重启恢复

device plugin 启动时使用两个来源保护已有任务：

- 活跃 Pod annotation：确定 Kubernetes 管理的物理 GPU、profile、placement 和 migUUID。
- NVML 进程查询：保护绕过 Kubernetes 或 annotation 尚未完成写回的活动实例。

启动过程：

1. 对没有活跃 allocation、也没有 NVML 进程的 GPU 清理残留 MIG 实例。
2. 对活跃 Pod annotation 中的实例校验实际 profile、placement 和 migUUID。
3. 将验证通过的实例采纳到 `MigInstanceManager`。
4. 后续周期 reconciler 继续清理 stale allocation。

如果 Kubernetes allocation 状态无法可靠读取，启动清理会 fail closed，保留 GPU 当前布局。

## 容器运行时

动态创建 MIG 实例后，GPU Operator 的 CDI spec 不一定在容器创建前完成刷新。当前验证环境使用 GPU Operator 提供的 `nvidia-legacy` RuntimeClass：

```yaml
devicePlugin:
  runtimeClassName: nvidia-legacy
```

若使用 CDI runtime，需要额外保证创建 MIG 实例后同步刷新 CDI spec，并解决容器创建时序问题。

## 测试覆盖

`hack/hami-mig-e2e.sh` 使用持续执行 CUDA `vectorAdd` 的 Pod 验证：

- 1g 和 2g 实例同时运行。
- 混合 profile 达到精确 placement 容量后拒绝溢出请求。
- device plugin 重启时 MIG UUID 不变，已有 CUDA 负载持续前进。
- Pod 删除后立即回收并补位，其他任务不受影响。
- 两个 3g 占满拓扑后，1g 请求在 scheduler 阶段保持 Pending。
- 七个 1g 并发、部分回收和重新填满。
- 最终所有 MIG 实例回收为零。

显存请求应使用 NVML 上报的实际容量。例如 A100 40GB 上 `1g.5gb` 实际为 4864 MiB，测试请求使用 4500 MiB；`2g.10gb` 实际为 9984 MiB，测试请求使用 9500 MiB。
