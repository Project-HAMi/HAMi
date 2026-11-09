English version | [中文版](README_cn.md) | [日本語版](README_ja.md)

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

**Kubernetes GPU virtualization and heterogeneous accelerator scheduling for AI infrastructure.**

![HAMi Architecture](imgs/hami-architecture.png)

HAMi stands for **Heterogeneous AI Computing Virtualization Middleware**. Formerly known as `k8s-vGPU-scheduler`, HAMi helps platform teams share expensive GPUs and other AI accelerators across Kubernetes workloads, isolate device memory and compute, and schedule pods with device-aware policies without changing application code.

HAMi is a [CNCF Incubating](https://www.cncf.io/projects/) and [CNCF Landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami) project. It is also listed in the [CNAI Landscape](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami).

![CNCF logo](imgs/cncf-logo.png)

## Documentation

- [Architecture](https://project-hami.io/docs/next/core-concepts/architecture)
- [GPU virtualization](https://project-hami.io/docs/next/core-concepts/gpu-virtualization)
- [Supported devices and vendor guides](https://project-hami.io/docs/next/userguide/device-supported)
- [Prerequisites](https://project-hami.io/docs/next/installation/prerequisites)
- [Install and run a first workload](https://project-hami.io/docs/next/get-started/deploy-with-helm)
- [Upgrade and migrate values](https://project-hami.io/docs/next/installation/upgrade)
- [Configuration and operations](https://project-hami.io/docs/next/userguide/configure)
- [NVIDIA scheduling policies](https://project-hami.io/docs/next/userguide/nvidia-device/scheduling-policy)
- [NVIDIA dynamic MIG](https://project-hami.io/docs/next/userguide/nvidia-device/dynamic-mig-support)
- [Device allocation metrics](https://project-hami.io/docs/next/userguide/monitoring/device-allocation)
- [Device usage metrics](https://project-hami.io/docs/next/userguide/monitoring/real-time-device-usage)
- [Grafana dashboards](https://project-hami.io/docs/next/userguide/monitoring/grafana-dashboard)
- [WebUI](https://project-hami.io/docs/next/userguide/hami-webui-user-guide)
- [Volcano integration](https://project-hami.io/docs/next/installation/how-to-use-volcano-vgpu)
- [Kueue integration](https://project-hami.io/docs/next/userguide/kueue/how-to-use-kueue)
- [Benchmarks](https://project-hami.io/docs/next/userguide/benchmark)
- [Troubleshooting](https://project-hami.io/docs/next/troubleshooting)
- [Uninstall](https://project-hami.io/docs/next/installation/uninstall)

## Repository resources

- [Chart values reference](charts/hami/README.md)
- [Workload examples](examples/)
- [Agent skills](skill/)
- [HAMi-WebUI repository](https://github.com/Project-HAMi/HAMi-WebUI)

## Roadmap, Governance, And Contributing

HAMi is governed by [maintainers](./MAINTAINERS.md) and [contributors](./AUTHORS.md). Governance is described in the [HAMi community repository](https://github.com/Project-HAMi/community/blob/main/governance.md).

To contribute code, documentation, tests, or device backend improvements, read [CONTRIBUTING.md](./CONTRIBUTING.md).

## Community

For chat channels, mailing lists, meeting schedules, and meeting notes, visit the [HAMi community page](https://project-hami.io/community).

[Talk materials](https://github.com/Project-HAMi/community/tree/main/talks)

## License

HAMi is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

Copyright Contributors to HAMi, established as HAMi a Series of LF Projects, LLC.
