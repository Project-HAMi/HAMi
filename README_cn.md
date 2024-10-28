<img src="imgs/hami-horizontal-colordark.png" width="600px">

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/build-image-release.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/build-image-release.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/4pdosc/k8s-vgpu.svg)](https://hub.docker.com/r/4pdosc/k8s-vgpu)
[![slack](https://img.shields.io/badge/Slack-Join%20Slack-blue)](https://cloud-native.slack.com/archives/C07T10BU4R2)
[![discuss](https://img.shields.io/badge/Discuss-Ask%20Questions-blue)](https://github.com/Project-HAMi/HAMi/discussions)
[![website](https://img.shields.io/badge/website-blue)](http://project-hami.io)
[![Contact Me](https://img.shields.io/badge/Contact%20Me-blue)](https://github.com/Project-HAMi/HAMi#contact)


# Project-HAMi；异构算力虚拟化中间件

## 支持设备：

[![英伟达 GPU](https://img.shields.io/badge/Nvidia-GPU-blue)](https://github.com/Project-HAMi/HAMi#preparing-your-gpu-nodes)
[![寒武纪 MLU](https://img.shields.io/badge/寒武纪-Mlu-blue)](docs/cambricon-mlu-support_cn.md)
[![海光 DCU](https://img.shields.io/badge/海光-DCU-blue)](docs/hygon-dcu-support.md)
[![天数智芯 GPU](https://img.shields.io/badge/天数智芯-GPU-blue)](docs/iluvatar-gpu-support_cn.md)
[![摩尔线程 GPU](https://img.shields.io/badge/摩尔线程-GPU-blue)](docs/mthreads-support_cn.md)
[![华为昇腾 NPU](https://img.shields.io/badge/华为昇腾-NPU-blue)](https://github.com/Project-HAMi/ascend-device-plugin/blob/main/README_cn.md)
[![沐曦 GPU](https://img.shields.io/badge/metax-GPU-blue)](docs/metax-support_cn.md)


## 简介

HAMi，原名“k8s-vGPU-scheduler”，是管理Kubernetes中异构设备的中间件。它可以管理不同类型的异构设备（如GPU、NPU等），在Pod之间共享异构设备，根据设备的拓扑信息和调度策略做出更好的调度决策。

它旨在消除不同异构设备之间的差距，为用户提供一个统一的管理接口，且无需更改应用程序。截至 2024年6月，HAMi已广泛应用于全球互联网/云/金融/制造等多个行业，被超过40多家公司或机构采纳。他们中的许多不仅是最终用户，也是项目积极的贡献者。

![cncf_logo](imgs/cncf-logo.png)

HAMi 是[Cloud Native Computing Foundation](https://cncf.io/)(CNCF)基金会的sandbox项目和[landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami)项目，并且是
[CNAI Landscape project](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami).

## 虚拟化能力

HAMi通过支持设备共享和设备资源隔离，为包括GPU在内的多个异构设备提供设备虚拟化。有关支持设备虚拟化的设备列表，请参阅 [支持的设备]（#支持设备）

### 设备复用能力

- 允许通过指定显存来申请算力设备
- 算力资源的硬隔离
- 允许通过指定算力使用比例来申请算力设备
- 对已有程序零改动

<img src="./imgs/example.png" width = "500" /> 

### 设备资源隔离能力

HAMi支持设备资源的硬隔离
一个以NVIDIA GPU为例硬隔离的简单展示：
一个使用以下方式定义的任务提交后
```yaml
      resources:
        limits:
          nvidia.com/gpu: 1 # requesting 1 vGPU
          nvidia.com/gpumem: 3000 # Each vGPU contains 3000m device memory
```
会只有3G可见显存

![img](./imgs/hard_limit.jpg)

## 项目架构图

<img src="./imgs/hami-arch.png" width = "600" />

HAMi 包含以下几个组件，一个统一的mutatingwebhook，一个统一的调度器，以及针对各种不同的异构算力设备对应的设备插件和容器内的控制组件，整体的架构特性如上图所示。


## 快速入门

### 选择你的集群调度器

[![kube-scheduler](https://img.shields.io/badge/kube-scheduler-blue)](https://github.com/Project-HAMi/HAMi#quick-start)
[![volcano-scheduler](https://img.shields.io/badge/volcano-scheduler-orange)](docs/how-to-use-volcano-vgpu.md)

### 安装要求

* NVIDIA drivers >= 440
* nvidia-docker version > 2.0 
* docker/containerd/cri-o已配置nvidia作为默认runtime
* Kubernetes version >= 1.16
* glibc >= 2.17 & glibc < 2.3.0
* kernel version >= 3.10
* helm > 3.0 

### 安装

首先使用helm添加我们的 repo

```bash
helm repo add hami-charts https://project-hami.github.io/HAMi/
```

随后，你需要将所有要使用到的GPU节点打上gpu=on标签，否则该节点不会被调度到

```bash
kubectl label nodes {nodeid} gpu=on
```

使用下列指令获取集群服务端版本

```bash
kubectl version
```

在安装过程中须根据集群服务端版本（上一条指令的结果）指定调度器镜像版本，例如集群服务端版本为1.16.8，则可以使用如下指令进行安装

```bash
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag=v1.16.8 -n kube-system
```

你可以修改这里的[配置](docs/config_cn.md)来定制安装

通过kubectl get pods指令看到 `vgpu-device-plugin` 与 `vgpu-scheduler` 两个pod 状态为*Running*  即为安装成功

```bash
kubectl get pods -n kube-system
```

### 提交任务

<details> <summary> 任务样例 </summary>

NVIDIA vGPUs 现在能透过资源类型`nvidia.com/gpu`被容器请求：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  containers:
    - name: ubuntu-container
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          nvidia.com/gpu: 2 # 请求2个vGPUs
          nvidia.com/gpumem: 3000 # 每个vGPU申请3000m显存 （可选，整数类型）
          nvidia.com/gpucores: 30 # 每个vGPU的算力为30%实际显卡的算力 （可选，整数类型）
```

如果你的任务无法运行在任何一个节点上（例如任务的`nvidia.com/gpu`大于集群中任意一个GPU节点的实际GPU数量）,那么任务会卡在`pending`状态

现在你可以在容器执行`nvidia-smi`命令，然后比较vGPU和实际GPU显存大小的不同。

> **注意:** *1. 如果你使用privileged字段的话，本任务将不会被调度，因为它可见所有的GPU，会对其它任务造成影响.*
> 
> *2. 不要设置nodeName字段，类似需求请使用nodeSelector.* 

#### 更多范例

点击 [范例](examples/nvidia)

</details>

### 监控：

<details> <summary> 访问集群算力视图 </summary>

调度器部署成功后，监控默认自动开启，你可以通过

```http
http://{nodeip}:{monitorPort}/metrics
```

来获取监控数据，其中monitorPort可以在Values中进行配置，默认为31992

grafana dashboard [示例](docs/dashboard_cn.md)

> **注意** 节点上的vGPU状态只有在其使用vGPU后才会被统计

</details>

## 注意事项

- 目前仅支持计算任务，不支持视频编解码处理。
- 暂时仅支持MIG的"none"和"mixed"模式，暂时不支持single模式
- 当任务有字段“nodeName“时会出现无法调度的情况，有类似需求的请使用"nodeSelector"代替
- 我们修改了 `device-plugin` 组件的环境变量，从 `NodeName` 改为 `NODE_NAME`, 如果使用的是镜像版本是 `v2.3.9`, 则可能会出现 `device-plugin` 无法启动的情况，目前有两种修复建议：
  - 手动执行`kubectl edit daemonset` 修改 `device-plugin` 的环境变量从`NodeName` 改为 `NODE_NAME`。
  - 使用helm升级到最新版本，最新版`device-plugin`的镜像版本是`v2.3.10`，执行 `helm upgrade hami hami/hami -n kube-system`, 会自动修复。

## 社区治理

该项目由一组 [Maintainers and Committers]（https://github.com/Project-HAMi/HAMi/blob/master/AUTHORS） 管理。我们的 [治理文件]（https://github.com/Project-HAMi/community/blob/main/governance.md） 中概述了如何选择和管理它们。

如果你想成为 HAMi 的贡献者，请参[考贡献者指南](CONTRIBUTING.md),里面有详细的贡献流程。

请参阅 [RoadMap]（docs/develop/roadmap.md） 查看您感兴趣的任何内容。

## 项目周会和联系方式

HAMi 社区致力于营造一个开放和友好的环境，并通过多种方式与其他用户和开发人员互动。

如果您有任何问题，请随时通过以下渠道与我们联系：

- 定期社区会议：周五 16：00 UTC+8（中文）(每周). [转换为您的时区](https://www.thetimezoneconverter.com/?t=14%3A30&tz=GMT%2B8&)。
  - [会议纪要及议程](https://docs.google.com/document/d/1YC6hco03_oXbF9IOUPJ29VWEddmITIKIfSmBX8JtGBw/edit#heading=h.g61sgp7w0d0c)
  - [会议链接](https://meeting.tencent.com/dm/Ntiwq1BICD1P)
- 电子邮件：请参阅[MAINTAINERS.md](MAINTAINERS.md)以查找所有维护者的电子邮件地址。请随时通过电子邮件与他们联系以报告任何问题或提出问题。
- [邮件列表](https://groups.google.com/forum/#!forum/hami-project)
- [slack]( https://cloud-native.slack.com/archives/C07T10BU4R2) | [Join](https://slack.cncf.io/)
