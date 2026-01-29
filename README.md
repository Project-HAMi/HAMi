English version | [中文版](README_cn.md) | [日本語版](README_ja.md)

<img src="imgs/hami-horizontal-colordark.png" width="600px">

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/projecthami/hami.svg)](https://hub.docker.com/r/projecthami/hami)
[![Contact Me](https://img.shields.io/badge/Contact%20Me-blue)](https://github.com/Project-HAMi/HAMi#meeting--contact)
[![slack](https://img.shields.io/badge/slack-green?style=for-the-badge&logo=googlechat)](https://cloud-native.slack.com/archives/C07T10BU4R2)
[![discord](https://img.shields.io/badge/discord-grey?style=for-the-badge&logo=discord)](https://discord.gg/Amhy7XmbNq)
[![website](https://img.shields.io/badge/website-blue?style=for-the-badge&logo=readthedocs)](http://project-hami.io)

## Project-HAMi: Heterogeneous AI Computing Virtualization Middleware

## Introduction

HAMi, formerly known as 'k8s-vGPU-scheduler', is a Heterogeneous device management middleware for Kubernetes. It can manage different types of heterogeneous devices (like GPU, NPU, etc.), share heterogeneous devices among pods, make better scheduling decisions based on topology of devices and scheduling policies.

It aims to remove the gap between different Heterogeneous devices, and provide a unified interface for users to manage with no changes to their applications. As of December 2024, HAMi has been widely used not only in Internet, public cloud and private cloud, but also broadly adopted in various vertical industries including finance, securities, energy, telecommunications, education, and manufacturing. More than 50 companies or institutions are not only end users but also active contributors. 

![cncf_logo](imgs/cncf-logo.png)

HAMi is a sandbox and [landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami) project of  
[Cloud Native Computing Foundation](https://cncf.io/)(CNCF), 
[CNAI Landscape project](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami).


## Device virtualization

HAMi provides device virtualization for several heterogeneous devices including GPU, by supporting device sharing and device resource isolation. For the list of devices supporting device virtualization, see [supported devices](#supported-devices)

### Device sharing

- Allows partial device allocation by specifying device core usage.
- Allows partial device allocation by specifying device memory.
- Imposes a hard limit on streaming multiprocessors.
- Requires zero changes to existing programs.
- Support [dynamic-mig](docs/dynamic-mig-support.md) feature, [example](examples/nvidia/dynamic_mig_example.yaml)

<img src="./imgs/example.png" width = "500" /> 

### Device Resources Isolation

A simple demonstration of device isolation:
A task with the following resources will see 3000M device memory inside container:

```yaml
      resources:
        limits:
          nvidia.com/gpu: 1 # declare how many physical GPUs the pod needs
          nvidia.com/gpumem: 3000 # identifies 3G GPU memory each physical GPU allocates to the pod
```

![img](./imgs/hard_limit.jpg)

> Note:
1. **After installing HAMi, the value of `nvidia.com/gpu` registered on the node defaults to the number of vGPUs.**
2. **When requesting resources in a pod, `nvidia.com/gpu` refers to the number of physical GPUs required by the current pod.**

### Supported devices

[NVIDIA GPU](https://github.com/Project-HAMi/HAMi#preparing-your-gpu-nodes)   
[Cambricon MLU](docs/cambricon-mlu-support.md)   
[HYGON DCU](docs/hygon-dcu-support.md)   
[Iluvatar CoreX GPU](docs/iluvatar-gpu-support.md)   
[Moore Threads GPU](docs/mthreads-support.md)   
[HUAWEI Ascend NPU](https://github.com/Project-HAMi/ascend-device-plugin/blob/main/README.md)   
[MetaX GPU](docs/metax-support.md)   

## Architect

<img src="./imgs/hami-arch.png" width = "600" /> 

HAMi consists of several components, including a unified mutatingwebhook, a unified scheduler extender, different device-plugins and different in-container virtualization technics for each heterogeneous AI devices.

## Quick Start

### Choose your orchestrator

[![kube-scheduler](https://img.shields.io/badge/kube-scheduler-blue)](#prerequisites)
[![volcano-scheduler](https://img.shields.io/badge/volcano-scheduler-orange)](docs/how-to-use-volcano-vgpu.md)

### Prerequisites

The list of prerequisites for running the NVIDIA device plugin is described below:

- NVIDIA drivers >= 440
- nvidia-docker version > 2.0
- default runtime configured as nvidia for containerd/docker/cri-o container runtime
- Kubernetes version >= 1.18
- glibc >= 2.17 & glibc < 2.30
- kernel version >= 3.10
- helm > 3.0

### Install

First, Label your GPU nodes for scheduling with HAMi by adding the label "gpu=on". Without this label, the nodes cannot be managed by our scheduler.

```
kubectl label nodes {nodeid} gpu=on
```

Add our repo in helm

```
helm repo add hami-charts https://project-hami.github.io/HAMi/
```

Use the following command for deployment:

```
helm install hami hami-charts/hami -n kube-system
```

Customize your installation by adjusting the [configs](docs/config.md).

Verify your installation using the following command:

```
kubectl get pods -n kube-system
```

If both `hami-device-plugin` (formerly known as `vgpu-device-plugin`)  and `hami-scheduler` (formerly known as `vgpu-scheduler`)  pods are in the *Running* state, your installation is successful. You can try examples [here](examples/nvidia/default_use.yaml) 

### WebUI

[HAMi-WebUI](https://github.com/Project-HAMi/HAMi-WebUI) is available after HAMi v2.4

For installation guide, click [here](https://github.com/Project-HAMi/HAMi-WebUI/blob/main/docs/installation/helm/index.md)

### Monitor

Monitoring is automatically enabled after installation. Obtain an overview of cluster information by visiting the following URL:

```
http://{scheduler ip}:{monitorPort}/metrics
```

The default monitorPort is 31993; other values can be set using `--set devicePlugin.service.httpPort` during installation.

Grafana dashboard [example](docs/dashboard.md)

> **Note** The status of a node won't be collected before you submit a task

## Notes

- If you don't request vGPUs when using the device plugin with NVIDIA images all the GPUs on the machine may be exposed inside your container
- Currently, A100 MIG can be supported in only "none" and "mixed" modes.
- Tasks with the "nodeName" field cannot be scheduled at the moment; please use "nodeSelector" instead.

## RoadMap, Governance & Contributing

The project is governed by a group of [Maintainers](./MAINTAINERS.md) and [Contributors](./AUTHORS.md). How they are selected and govern is outlined in our [Governance Document](https://github.com/Project-HAMi/community/blob/main/governance.md).

If you're interested in being a contributor and want to get involved in developing the HAMi code, please see [CONTRIBUTING](CONTRIBUTING.md) for details on submitting patches and the contribution workflow.

See [RoadMap](docs/develop/roadmap.md) to see anything you interested.

## Meeting & Contact

The HAMi community is committed to fostering an open and welcoming environment, with several ways to engage with other users and developers.

If you have any questions, please feel free to reach out to us through the following channels:

- Regular Community Meeting: Friday at 16:00 UTC+8 (Chinese)(weekly). [Convert to your timezone](https://www.thetimezoneconverter.com/?t=14%3A30&tz=GMT%2B8&).
  - [Meeting Notes and Agenda](https://docs.google.com/document/d/1YC6hco03_oXbF9IOUPJ29VWEddmITIKIfSmBX8JtGBw/edit#heading=h.g61sgp7w0d0c)
  - [Meeting Link](https://meeting.tencent.com/dm/Ntiwq1BICD1P)
- Email: refer to the [MAINTAINERS.md](MAINTAINERS.md) to find the email addresses of all maintainers. Feel free to contact them via email to report any issues or ask questions.
- [mailing list](https://groups.google.com/forum/#!forum/hami-project)

## Talks and References

|                  | Link                                                                                                                    |
|------------------|-------------------------------------------------------------------------------------------------------------------------|
| CHINA CLOUD COMPUTING INFRASTRUCTURE DEVELOPER CONFERENCE (Beijing 2024) | [Unlocking heterogeneous AI infrastructure on k8s clusters](https://live.csdn.net/room/csdnnews/3zwDP09S) Starting from 03:06:15 |
| KubeDay(Japan 2024) | [Unlocking Heterogeneous AI Infrastructure K8s Cluster:Leveraging the Power of HAMi](https://www.youtube.com/watch?v=owoaSb4nZwg) |
| KubeCon & AI_dev Open Source GenAI & ML Summit(China 2024) | [Is Your GPU Really Working Efficiently in the Data Center?N Ways to Improve GPU Usage](https://www.youtube.com/watch?v=ApkyK3zLF5Q) |
| KubeCon & AI_dev Open Source GenAI & ML Summit(China 2024) | [Unlocking Heterogeneous AI Infrastructure K8s Cluster](https://www.youtube.com/watch?v=kcGXnp_QShs)                                     |
| KubeCon(EU 2024)| [Cloud Native Batch Computing with Volcano: Updates and Future](https://youtu.be/fVYKk6xSOsw) |

## License

HAMi is under the Apache 2.0 license. See the [LICENSE](LICENSE) file for details.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Project-HAMi/HAMi&type=Date)](https://star-history.com/#Project-HAMi/HAMi&Date)
