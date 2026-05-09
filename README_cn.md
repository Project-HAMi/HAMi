[English version](README.md) | 中文版 | [日本語版](README_ja.md)

<img src="imgs/hami-horizontal-colordark.png" width="600px" alt="HAMi logo">

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/projecthami/hami.svg)](https://hub.docker.com/r/projecthami/hami)
[![slack](https://img.shields.io/badge/slack-green?style=for-the-badge&logo=slack)](https://cloud-native.slack.com/archives/C07T10BU4R2)
[![discord](https://img.shields.io/badge/discord-grey?style=for-the-badge&logo=discord)](https://discord.gg/Amhy7XmbNq)
[![website](https://img.shields.io/badge/website-blue?style=for-the-badge&logo=readthedocs)](https://project-hami.io)

# HAMi

**Kubernetes GPU 虚拟化与异构加速器调度，面向 AI 基础设施。**

![HAMi Architecture](imgs/hami-architecture.png)

HAMi 全称**异构 AI 计算虚拟化中间件**（Heterogeneous AI Computing Virtualization Middleware）。前身为 `k8s-vGPU-scheduler`，HAMi 帮助平台团队在 Kubernetes 工作负载间共享昂贵的 GPU 及其他 AI 加速器，隔离设备显存和算力，并通过设备感知的调度策略调度 Pod，无需修改应用代码。

HAMi 是 [CNCF Sandbox](https://www.cncf.io/sandbox-projects/) 和 [CNCF Landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami) 项目，同时也被列入 [CNAI Landscape](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami)。

![CNCF logo](imgs/cncf-logo.png)

## 为什么选择 HAMi？

AI 基础设施团队常常面临同样的 Kubernetes 加速器问题：整张 GPU 被分配给小型任务，团队争抢稀缺设备，不同加速器厂商暴露不同的操作模型，调度器缺乏足够的设备上下文来高效放置工作负载。

HAMi 提供了一个 Kubernetes 原生层：

- **设备共享**：按显存、核心或设备数量分配物理加速器的部分资源。
- **资源隔离**：在设备后端支持的情况下，强制执行每个工作负载的加速器显存和算力限制。
- **设备感知调度**：通过拓扑感知、binpack、spread 和设备特定的调度策略放置 Pod。
- **异构 AI 集群**：通过统一的调度和分配工作流管理 NVIDIA GPU、NPU、DCU、MLU 等多种加速器类型。
- **零应用改动**：继续使用标准 Kubernetes 资源请求和限制。
- **生产运维**：暴露指标、仪表盘、WebUI、Helm 安装和社区支持的部署指南。

## 使用场景

- 提升共享 Kubernetes AI 集群中的 GPU 利用率。
- 在同一加速器资源池上运行多租户 Notebook、训练和推理工作负载。
- 构建具有公平设备分配和配额控制的私有云 AI 平台。
- 运营跨 NVIDIA、昇腾、寒武纪、海光、天数智芯、沐曦、摩尔线程等厂商的异构加速器集群。
- 将 HAMi 与 kube-scheduler、Volcano 等 Kubernetes 调度器结合用于批量 AI 工作负载。

## 工作原理

HAMi 由 Mutating Webhook、调度器扩展器、设备插件和设备特定的容器内虚拟化组件组成。

```text
Pod 提交
  -> HAMi Mutating Webhook
  -> HAMi 调度器 filter / score / bind
  -> 设备分配写入 Pod 注解
  -> 设备插件 Allocate()
  -> 容器运行时环境
  -> HAMi 监控和指标
```

## 设备虚拟化

HAMi 让工作负载只申请所需的加速器资源。例如，以下 Pod 请求一张具有 3 GiB 显存的物理 NVIDIA GPU：

```yaml
resources:
  limits:
    nvidia.com/gpu: 1
    nvidia.com/gpumem: 3000
```

工作负载在容器内看到已分配的设备资源，HAMi 负责协调调度、分配和隔离。

![HAMi Example](imgs/example.png)

> 注意：
>
> 1. 安装 HAMi 后，节点上注册的 `nvidia.com/gpu` 值默认为 vGPU 数量。
> 2. 在 Pod 中申请资源时，`nvidia.com/gpu` 指的是当前 Pod 所需的物理 GPU 数量。

## 支持的设备

HAMi 支持多种异构加速器后端，包括 GPU、NPU、DCU、MLU、GCU、XPU 等。设备能力因厂商、型号、驱动和硬件代次而异。

请参阅 [HAMi 支持的设备](https://project-hami.io/docs/userguide/device-supported)页面获取最新的支持矩阵。

## 快速开始

### 前置条件

使用 NVIDIA 设备插件路径，需准备：

- NVIDIA 驱动 >= 440
- `nvidia-docker` 版本 > 2.0
- NVIDIA 配置为 containerd、Docker 或 CRI-O 的默认运行时
- Kubernetes >= 1.23
- glibc >= 2.17 且 < 2.30
- Linux 内核 >= 3.10
- Helm > 3.0

### 使用 Helm 安装

标记 GPU 节点以便 HAMi 管理：

```bash
kubectl label nodes <node-name> gpu=on
```

添加 HAMi Helm 仓库：

```bash
helm repo add hami-charts https://project-hami.github.io/HAMi/
helm repo update
```

安装 HAMi：

```bash
helm install hami hami-charts/hami -n kube-system
```

验证调度器和设备插件是否正在运行：

```bash
kubectl get pods -n kube-system
```

当 `hami-device-plugin` 和 `hami-scheduler` 都为 `Running` 状态时，提交示例工作负载：

```bash
kubectl apply -f examples/nvidia/default_use.yaml
```

完整的安装指南和配置选项，请参阅 [HAMi 文档](https://project-hami.io/docs/get-started/deploy-with-helm/)。

## 调度策略

HAMi 支持多种 AI 工作负载调度模式：

- **binpack**：将工作负载打包到更少的节点或设备上以提高资源利用率。
- **spread**：将工作负载分布到不同节点或设备上以减少争用。
- **拓扑感知调度**：在支持的情况下，基于 GPU 拓扑选择设备组合。
- **动态 MIG**：为支持的显卡和模式动态创建和分配 NVIDIA MIG 实例。

HAMi 兼容默认 Kubernetes 调度路径，也可与 Volcano 配合用于批量 AI 工作负载。请参阅 [HAMi 网站](https://project-hami.io/docs/)获取当前的调度器集成指南。

## 可观测性与 WebUI

HAMi 暴露用于监控集群加速器使用情况的指标。安装后，指标可通过调度器监控端点获取：

```text
http://<scheduler-ip>:<monitor-port>/metrics
```

默认监控端口为 `31993`。可通过 Helm 参数修改，如 `--set scheduler.service.monitorPort=<port>`。

HAMi 还提供：

- [HAMi-WebUI](https://github.com/Project-HAMi/HAMi-WebUI) 用于可视化集群和设备管理。
- Grafana 仪表盘示例用于加速器监控。
- 基准测试材料用于评估工作负载行为和调度效果。

![HAMi WebUI](imgs/hami-webui-overview.png)

## 路线图、治理与贡献

HAMi 由[维护者](./MAINTAINERS.md)和[贡献者](./AUTHORS.md)共同治理。治理规则详见 [HAMi 社区仓库](https://github.com/Project-HAMi/community/blob/main/governance.md)。

如需贡献代码、文档、测试或设备后端改进，请阅读 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 社区

HAMi 社区欢迎用户、贡献者、硬件厂商和构建 Kubernetes AI 基础设施的平台团队。

- 官网：[project-hami.io](https://project-hami.io)
- Discord：[加入 HAMi Discord](https://discord.gg/Amhy7XmbNq)（推荐）
- Slack：[CNCF Slack #hami-dev](https://cloud-native.slack.com/archives/C07T10BU4R2)
- 邮件列表：[hami-project](https://groups.google.com/forum/#!forum/hami-project)
- [会议记录和议程](https://docs.google.com/document/d/1YC6hco03_oXbF9IOUPJ29VWEddmITIKIfSmBX8JtGBw/edit#heading=h.g61sgp7w0d0c)
- 中文社区周会：每周五 16:00 UTC+8 — [会议链接](https://meeting.tencent.com/dm/Ntiwq1BICD1P)
- 英文社区双周会：隔周三 16:00 UTC+8 — [会议链接](https://zoom-lfx.platform.linuxfoundation.org/meeting/95994137931?password=55b961b5-3e8e-4040-8657-0f2d26511f1d)

## 演讲和参考资料

| 活动 | 演讲 |
| --- | --- |
| 中国云计算基础架构开发者大会，北京 2024 | [在 Kubernetes 集群上解锁异构 AI 基础设施](https://live.csdn.net/room/csdnnews/3zwDP09S) |
| KubeDay Japan 2024 | [Unlocking Heterogeneous AI Infrastructure K8s Cluster: Leveraging the Power of HAMi](https://www.youtube.com/watch?v=owoaSb4nZwg) |
| KubeCon + AI_dev Open Source GenAI & ML Summit China 2024 | [Is Your GPU Really Working Efficiently in the Data Center? N Ways to Improve GPU Usage](https://www.youtube.com/watch?v=ApkyK3zLF5Q) |
| KubeCon + AI_dev Open Source GenAI & ML Summit China 2024 | [Unlocking Heterogeneous AI Infrastructure K8s Cluster](https://www.youtube.com/watch?v=kcGXnp_QShs) |
| KubeCon Europe 2024 | [Cloud Native Batch Computing with Volcano: Updates and Future](https://youtu.be/fVYKk6xSOsw) |

## 许可证

HAMi 采用 Apache License 2.0 许可证。详见 [LICENSE](LICENSE)。

Copyright Contributors to HAMi, established as HAMi a Series of LF Projects, LLC.
