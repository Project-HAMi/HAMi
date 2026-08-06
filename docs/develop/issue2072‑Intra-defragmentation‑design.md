# Design Proposal for Issue #2072
Related Issue: #2072

## Background
外部碎片整理程序需要向HAMi调度器下发强制Pod‑GPU放置指令，实现Pod跨节点GPU碎片迁移能力。

## Goals
- 支持外部碎片整理组件通过CRD下发调度放置指令
- 调度器识别并强制执行CRD中的节点、GPU设备绑定要求
- CRD具备状态追踪、幂等、Pod主动删除能力
- 兼容原有HAMi调度逻辑，无指令时走原有调度流程

## Non‑Goals
- 本次不实现碎片整理算法本身，仅提供指令接收与调度执行能力
- 暂不支持多租户权限校验

## Proposal
# 调度器接收CRD调度请求初步构思
外部碎片整理程序强制指定放置

### 目标
外部碎片整理程序创建一个指令对象（HamiPodPlacement CRD），指明某个 Deployment 生成的 Pod（通过 Label Selector 匹配）必须绑定到某个节点的某张卡。调度器在调度该类 Pod 时，需识别并强制执行该指令。

HamiPodPlacement CRD 充当外部碎片整理程序与虚拟化调度器之间的指令信使。
简单来说，它就是一个“纸条”，碎片整理程序写好“把哪个 Pod 挪到哪张卡上”，然后贴在集群里，虚拟化调度器看到后就会照做。

### 创建表示调度指令的CRD
CRD定义内容如下：

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: HamiPodPlacements.scheduling.vgpu.example.com
spec:
  group: scheduling.hami.placement.com
  names:
    kind: HamiPodPlacement
    listKind: HamiPodPlacementList
    plural: HamiPodPlacements
    singular: HamiPodPlacement
    shortNames:
      - vcp
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required: ["reqID", "podSelector", "nodeName", "gpuDevices"]
              properties:
                reqID:
                  type: string
                  description: "唯一请求ID，用于幂等控制和审计"
                podSelector:
                  type: object
                  required: ["matchLabels"]
                  properties:
                    matchLabels:
                      type: object
                      additionalProperties:
                        type: string
                nodeName:
                  type: string
                  description: "目标节点"
                gpuDevices:
                  type: array
                  description: "目标GPU设备标识列表，如 ['gpu-0', 'gpu-1']"
                  items:
                    type: string
                deletePod:
                  type: string
                  description: "可选：需要主动删除的Pod名称。若为空，则仅在下一次Pod重建时应用指令；若不为空，调度器将先删除该Pod再等待新Pod被调度"
            status:
              type: object
              properties:
                phase:
                  type: string
                  enum: ["Pending", "DeletingOldPod", "PartiallyCompleted", "Completed", "Failed"]
                assignmentDetails:
                  type: array
                  description: "记录每个被调度的Pod和分配的GPU"
                  items:
                    type: object
                    properties:
                      podName:
                        type: string
                      gpuDevice:
                        type: string
                consumedAt:
                  type: string
                  format: date-time
                message:
                  type: string
      subresources:
        status: {}
```
### 使用示例

```yaml
apiVersion: scheduling.hami.placement.com/v1
kind: HamiPodPlacement
metadata:
  name: immediate-migrate-20260708-03de4
  namespace: yixin
spec:
  reqID: "defrag-20260708-03de4"
  podSelector:
    matchLabels:
      app: app-2018239454967238656-sh
  nodeName: aitem-node
  gpuDevices:
    - gpu-0/20
  deletePod: "app-2018239454967238656-sh-77cb787f6-9vdtq" #主动删除该Pod
---
apiVersion: scheduling.vgpu.example.com/v1
kind: HamiPodPlacement
metadata:
  name: batch-2-20260708-xf2de
  namespace: yixin
spec:
  reqID: "deploy-20260708-03de4"
  podSelector:
    matchLabels:
      app: app-2074316262019698688-sh
  nodeName: k8s-gpu-3080x2
  gpuDevices:
    - gpu-0/70
    - gpu-1/70
  # deletePod 留空，仅在下一次部署时生效

```
碎片整理程序可以按照以上示例发送 CRD 给 k8s，虚拟化调度器会自动识别请求并处理。
虚拟化调度器启动后，会启动有关 CRD 的 Informer，监听到部署信息后会放置在缓存中。
reqID作为请求唯一 ID，保证幂等性，同一 ID 的请求只执行一次。

### 5.1 实现方式选择
外部碎片整理指令的接收与处理通过在 HAMi 调度器内部增加 CRD 监听与调度决策拦截完成。由于 HAMi 调度器基于 Scheduler Extender，可利用其已有的 HTTP 回调入口（Filter 和 Priorize 阶段）进行扩展，同时借助 HAMi 已有的 Kubernetes client‑go 组件（Informer、Pod 注解 Patch）实现指令消费与状态回写。

### 5.2 各组件功能

#### HamiPodPlacement CRD 监听器
- **职责**：实时监听 HamiPodPlacement CRD 的创建与变更，维护本地指令缓存供调度决策使用。
- **实现逻辑**：
    - 在 HAMi 调度器启动时，创建 HamiPodPlacement 的 Informer，持续将 CRD 对象同步至本地缓存。
    - 若 CRD 中包含 deletePod 字段，由独立的 worker 协程执行指定 Pod 的删除操作，并更新 CRD 状态为 DeletingOldPod。
    - 缓存数据供 Filter 阶段快速查询，避免每次调度请求直接访问 API Server。

#### Filter‑回调增强（节点筛选）
- **职责**：在 HAMi 已有的 GPU 资源筛选逻辑基础上，增加外部指令的强制节点绑定。
- **实现逻辑**：
    - 在接收到调度器发来的 Filter 请求时，根据 Pod 的 Label 查询本地 HamiPodPlacement 缓存。
    - 若存在匹配且状态为待消费的外部指令：
        - 验证指令指定的 targetNode 是否在候选节点列表中，且具备足够的物理 GPU 资源（特别是单卡容量）。
        - 仅当候选节点名完全等于 targetNode 时放行该节点，其余节点全部过滤，或者照常放行所有可行节点，指定的targetNode获得显著高分。
    - 若无匹配指令：走 HAMi 原有的 GPU 资源过滤和打分逻辑。
    - 过滤结果返回默认调度器。

#### Bind‑资源预留与注解写入（调度后处理）
- **职责**：在 Pod 被成功调度后，将外部指令指定的 GPU 卡信息写入 Pod 注解，并更新 HamiPodPlacement 状态。
- **实现逻辑**：
    - 若分配成功且 Pod 注解写入成功，则更新 HamiPodPlacement 的 status.assignmentDetails，标记该设备已分配；若所有 gpuDevices 均已分配，将 status.phase 更新为 Completed。
    - 若写入失败或分配失败，将 status.phase 置为 Failed，便于碎片整理程序感知。
    - 此过程通过 HAMi 调度器内部的 client‑go 直接操作 API Server，无需等待下一个调度周期。

#### 资源释放与回滚（Unreserve 补偿）
- **职责**：当 Pod 调度失败或被删除时，及时释放已被预占的 GPU 资源，并回滚指令状态。
- **实现逻辑**：
    - 与原有逻辑相同。

### 流程总结
HamiPodPlacement Informer 监听指令 → Filter 回调强制过滤目标节点 → 调度器绑定 Pod → 异步 Patch Pod 注解并更新 CRD 状态 → Pod 删除时回收资源回滚指令

## 2026/07/26 会议总结
关于接收碎片整理的 CRD：放在独立的 controller+webhook，前置 annotation 设定好指定 GPU 的 UUID 列表，后面无缝接入到 hami 的 scheduler。hami‑scheduler 保持功能简洁单纯减少直接接入其他业务逻辑或耦合。具体方案后续由实际实现效果是否达到目标确定。

关于触发碎片整理的发起者：可以考虑由 koordinator 发起。

> 问题 1：独立 webhook 的话，集群各节点获取 GPU 信息单独获取？能否复用调度器维护的状态机。或者单纯复用代码，依然使用独立的 webhook？
>
> 调度器有同步机制

> 问题 2：处理好 crd 后，能否修改状态，在 Bind 中更新 CRD 状态。
>
> controller 通过 informer 机制。
