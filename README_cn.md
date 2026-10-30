[English version](README.md) | 中文版 | [日本語版](README_ja.md)

<img src="imgs/hami-horizontal-colordark.png" width="600px" alt="HAMi logo">

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Project-HAMi/HAMi/badge)](https://scorecard.dev/viewer/?uri=github.com/Project-HAMi/HAMi)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://app.codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://app.codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/projecthami/hami.svg)](https://hub.docker.com/r/projecthami/hami)
[![slack](https://img.shields.io/badge/slack-green?style=for-the-badge&logo=slack)](https://cloud-native.slack.com/archives/C07T10BU4R2)
[![discord](https://img.shields.io/badge/discord-grey?style=for-the-badge&logo=discord)](https://discord.com/invite/Amhy7XmbNq)
[![website](https://img.shields.io/badge/website-blue?style=for-the-badge&logo=readthedocs)](https://project-hami.io)

# HAMi

**Kubernetes GPU 虚拟化与异构加速器调度，面向 AI 基础设施。**

![HAMi Architecture](imgs/hami-architecture.png)

HAMi 全称**异构 AI 计算虚拟化中间件**（Heterogeneous AI Computing Virtualization Middleware）。前身为 `k8s-vGPU-scheduler`，HAMi 帮助平台团队在 Kubernetes 工作负载间共享昂贵的 GPU 及其他 AI 加速器，隔离设备显存和算力，并通过设备感知的调度策略调度 Pod，无需修改应用代码。

HAMi 是 [CNCF Incubating](https://www.cncf.io/projects/) 和 [CNCF Landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami) 项目，同时也被列入 [CNAI Landscape](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami)。

![CNCF logo](imgs/cncf-logo.png)

## 文档

- [架构](https://project-hami.io/zh/docs/next/core-concepts/architecture)
- [GPU 虚拟化](https://project-hami.io/zh/docs/next/core-concepts/gpu-virtualization)
- [支持的设备与厂商指南](https://project-hami.io/zh/docs/next/userguide/device-supported)
- [前置条件](https://project-hami.io/zh/docs/next/installation/prerequisites)
- [安装并运行首个工作负载](https://project-hami.io/zh/docs/next/get-started/deploy-with-helm)
- [升级与参数迁移](https://project-hami.io/zh/docs/next/installation/upgrade)
- [配置与运维](https://project-hami.io/zh/docs/next/userguide/configure)
- [NVIDIA 调度策略](https://project-hami.io/zh/docs/next/userguide/nvidia-device/scheduling-policy)
- [NVIDIA 动态 MIG](https://project-hami.io/zh/docs/next/userguide/nvidia-device/dynamic-mig-support)
- [设备分配指标](https://project-hami.io/zh/docs/next/userguide/monitoring/device-allocation)
- [设备使用指标](https://project-hami.io/zh/docs/next/userguide/monitoring/real-time-device-usage)
- [Grafana 仪表盘](https://project-hami.io/zh/docs/next/userguide/monitoring/grafana-dashboard)
- [WebUI](https://project-hami.io/zh/docs/next/userguide/hami-webui-user-guide)
- [Volcano 集成](https://project-hami.io/zh/docs/next/installation/how-to-use-volcano-vgpu)
- [Kueue 集成](https://project-hami.io/zh/docs/next/userguide/kueue/how-to-use-kueue)
- [性能测试](https://project-hami.io/zh/docs/next/userguide/benchmark)
- [故障排查](https://project-hami.io/zh/docs/next/troubleshooting)
- [卸载](https://project-hami.io/zh/docs/next/installation/uninstall)

## 仓库资源

- [Chart 完整参数参考](charts/hami/README.md)
- [工作负载示例](examples/)
- [Agent skills](skill/)
- [HAMi-WebUI 仓库](https://github.com/Project-HAMi/HAMi-WebUI)

## 路线图、治理与贡献

HAMi 由[维护者](./MAINTAINERS.md)和[贡献者](./AUTHORS.md)共同治理。治理规则详见 [HAMi 社区仓库](https://github.com/Project-HAMi/community/blob/main/governance.md)。

如需贡献代码、文档、测试或设备后端改进，请阅读 [CONTRIBUTING.md](./CONTRIBUTING.md)。

## 社区

交流渠道、邮件列表、会议安排及纪要见 [HAMi 社区页面](https://project-hami.io/zh/community)。

[演讲资料](https://github.com/Project-HAMi/community/tree/main/talks)

## 许可证

HAMi 采用 Apache License 2.0 许可证。详见 [LICENSE](LICENSE)。

Copyright Contributors to HAMi, established as HAMi a Series of LF Projects, LLC.
