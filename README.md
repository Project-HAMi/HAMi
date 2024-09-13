English version|[中文版](README_cn.md)

<img src="imgs/hami-horizontal-colordark.png" width="600px">

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/build-image-release.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/build-image-release.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/4pdosc/k8s-vgpu.svg)](https://hub.docker.com/r/4pdosc/k8s-vgpu)
[![slack](https://img.shields.io/badge/Slack-Join%20Slack-blue)](https://join.slack.com/t/hami-hsf3791/shared_invite/zt-2gcteqiph-Ls8Atnpky6clrspCAQ_eGQ)
[![discuss](https://img.shields.io/badge/Discuss-Ask%20Questions-blue)](https://github.com/Project-HAMi/HAMi/discussions)
[![website](https://img.shields.io/badge/website-blue)](http://project-hami.io)
[![Contact Me](https://img.shields.io/badge/Contact%20Me-blue)](https://github.com/Project-HAMi/HAMi#contact)

## Project-HAMi: Heterogeneous AI Computing Virtualization Middleware

## Introduction

HAMi, formerly known as 'k8s-vGPU-scheduler', is a Heterogeneous device management middleware for Kubernetes. It can manage different types of heterogeneous devices(like GPU,NPU,etc...), share heterogeneous devices among pods, make better scheduling decision based on topology of devices and schedule policies.

It aims to remove the gap between different Heterogeneous devices, and provide a unified interface for user to manage with no change to your application. Until June 2024, HAMi has been widely used around the world at a variety of industries such as Internet/Cloud/Finance/ Manufacturing. More than 40 companies or institutions are not only end users but also active contributors. 

![cncf_logo](imgs/cncf-logo.png)

HAMi is a sandbox and [landscape](https://landscape.cncf.io/?item=orchestration-management--scheduling-orchestration--hami) project of  
[Cloud Native Computing Foundation](https://cncf.io/)(CNCF), 
[CNAI Landscape project](https://landscape.cncf.io/?group=cnai&item=cnai--general-orchestration--hami).


## Device virtualization

HAMi provides device virtualization for several heterogeneous devices including GPU, by supporting device sharing and device resource isolation. For the list of devices supporting device virtualization, see [supported devices](#supported-devices)

### Device sharing

- Allows partial device allocation by specifying device memory.
- Imposes a hard limit on streaming multiprocessors.
- Permits partial device allocation by specifying device core usage.
- Requires zero changes to existing programs.

<img src="./imgs/example.png" width = "500" /> 

### Device Resources Isolation

A simple demostration for device isolation:
A task with the following resources.

```
      resources:
        limits:
          nvidia.com/gpu: 1 # requesting 1 vGPU
          nvidia.com/gpumem: 3000 # Each vGPU contains 3000m device memory
```

will see 3G device memory inside container

![img](./imgs/hard_limit.jpg)

### Supported devices

[![nvidia GPU](https://img.shields.io/badge/Nvidia-GPU-blue)](https://github.com/Project-HAMi/HAMi#preparing-your-gpu-nodes)
[![cambricon MLU](https://img.shields.io/badge/Cambricon-Mlu-blue)](docs/cambricon-mlu-support.md)
[![hygon DCU](https://img.shields.io/badge/Hygon-DCU-blue)](docs/hygon-dcu-support.md)
[![iluvatar GPU](https://img.shields.io/badge/Iluvatar-GPU-blue)](docs/iluvatar-gpu-support.md)

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
- config default runtime is nvidia for containerd/docker/cri-o container runtime.
- Kubernetes version >= 1.16
- glibc >= 2.17 & glibc < 2.3.0
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

Check your Kubernetes version by using the following command:

```
kubectl version
```

During installation, set the Kubernetes scheduler image version to match your Kubernetes server version. For instance, if your cluster server version is 1.16.8, use the following command for deployment:

```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag=v1.16.8 -n kube-system
```

Customize your installation by adjusting the [configs](docs/config.md).

Verify your installation using the following command:

```
kubectl get pods -n kube-system
```

If both `vgpu-device-plugin` and `vgpu-scheduler` pods are in the *Running* state, your installation is successful. You can try examples [here](https://github.com/Project-HAMi/HAMi/blob/newprofile/examples/nvidia/default_use.yaml) 

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

The project is governed by a group of [Maintainers and Committers](https://github.com/Project-HAMi/HAMi/blob/master/AUTHORS). How they are selected and govern is outlined in our [Governance Document](https://github.com/Project-HAMi/community/blob/main/governance.md).

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
- [slack](https://join.slack.com/t/hami-hsf3791/shared_invite/zt-2gcteqiph-Ls8Atnpky6clrspCAQ_eGQ)

## License

HAMi is under the Apache 2.0 license. See the [LICENSE](LICENSE) file for details.
