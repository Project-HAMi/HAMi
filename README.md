English version | [中文版](README_cn.md) | [日本語版](README_ja.md)

<<<<<<< HEAD
<<<<<<< HEAD
<img src="imgs/hami-horizontal-colordark.png" width="600px">
=======
[![build status](https://github.com/4paradigm/k8s-device-plugin/actions/workflows/build.yml/badge.svg)](https://github.com/4paradigm/k8s-vgpu-scheduler/actions/workflows/main.yml)
[![docker pulls](https://img.shields.io/docker/pulls/4pdosc/k8s-device-plugin.svg)](https://hub.docker.com/r/4pdosc/k8s-vgpu)
=======
<img src="https://github.com/Project-HAMi/HAMi/blob/libopensource/HAMi.jpg" width="200px">

# Heterogeneous AI Computing Virtualization Middleware

[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/main.yml/badge.svg)](https://github.com/4paradigm/k8s-vgpu-scheduler/actions/workflows/main.yml)
[![docker pulls](https://img.shields.io/docker/pulls/4pdosc/k8s-vgpu.svg)](https://hub.docker.com/r/4pdosc/k8s-vgpu)
>>>>>>> c7a3893 (Remake this repo to HAMi)
[![slack](https://img.shields.io/badge/Slack-Join%20Slack-blue)](https://join.slack.com/t/k8s-device-plugin/shared_invite/zt-oi9zkr5c-LsMzNmNs7UYg6usc0OiWKw)
[![discuss](https://img.shields.io/badge/Discuss-Ask%20Questions-blue)](https://github.com/4paradigm/k8s-device-plugin/discussions)
[![cambricon MLU](https://img.shields.io/badge/cambricon%20mlu-8A2BE2)](docs/cambricon-mlu-support.md)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

[![LICENSE](https://img.shields.io/github/license/Project-HAMi/HAMi.svg)](/LICENSE)
[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/ci.yaml)
[![Releases](https://img.shields.io/github/v/release/Project-HAMi/HAMi)](https://github.com/Project-HAMi/HAMi/releases/latest)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/9416/badge)](https://www.bestpractices.dev/en/projects/9416)
[![Go Report Card](https://goreportcard.com/badge/github.com/Project-HAMi/HAMi)](https://goreportcard.com/report/github.com/Project-HAMi/HAMi)
[![codecov](https://codecov.io/gh/Project-HAMi/HAMi/branch/master/graph/badge.svg?token=ROM8CMPXZ6)](https://codecov.io/gh/Project-HAMi/HAMi)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FProject-HAMi%2FHAMi?ref=badge_shield)
[![docker pulls](https://img.shields.io/docker/pulls/projecthami/hami.svg)](https://hub.docker.com/r/projecthami/hami)
[![Contact Me](https://img.shields.io/badge/Contact%20Me-blue)](https://github.com/Project-HAMi/HAMi#contact)
[![discord](https://img.shields.io/badge/discord-5865F2?style=for-the-badge&logo=discord)](https://discord.gg/Amhy7XmbNq)
[![website](https://img.shields.io/badge/website-green?style=for-the-badge&logo=readthedocs)](http://project-hami.io)

<<<<<<< HEAD
## Project-HAMi: Heterogeneous AI Computing Virtualization Middleware
=======
[![nvidia GPU](https://img.shields.io/badge/Nvidia-GPU-blue)](https://github.com/4paradigm/k8s-vgpu-scheduler#preparing-your-gpu-nodes)
[![cambricon MLU](https://img.shields.io/badge/Cambricon-Mlu-blue)](docs/cambricon-mlu-support.md)
[![hygon DCU](https://img.shields.io/badge/Hygon-DCU-blue)](docs/hygon-dcu-support.md)
>>>>>>> 21785f7 (update to v2.3.2)

## Introduction

<<<<<<< HEAD
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
=======
!<img src="./imgs/example.png" width = "600" /> 

**Heterogeneous AI Computing Virtualization Middleware (HAMi), formerly known as k8s-vGPU-scheduler, is an "all-in-one" chart designed to manage Heterogeneous AI Computing Devices in a k8s cluster.** It includes everything you would expect, such as:

***Device sharing***: Each task can allocate a portion of a device instead of the entire device, allowing a device to be shared among multiple tasks.

***Device Memory Control***: Devices can be allocated a specific device memory size (e.g., 3000M) or a percentage of the whole GPU's memory (e.g., 50%), ensuring it does not exceed the specified boundaries.

***Device Type Specification***: You can specify the type of device to use or avoid for a particular task by setting annotations, such as "nvidia.com/use-gputype" or "nvidia.com/nouse-gputype".

***Easy to use***: You don't need to modify your task YAML to use our scheduler. All your jobs will be automatically supported after installation. Additionally, you can specify a resource name other than "nvidia.com/gpu" if you prefer.

## Major Features

- Hard Limit on Device Memory.

A simple demostration for Hard Limit:
A task with the following resources.

```
      resources:
        limits:
          nvidia.com/gpu: 1 # requesting 1 vGPU
          nvidia.com/gpumem: 3000 # Each vGPU contains 3000m device memory
```

will see 3G device memory inside container

![img](./imgs/hard_limit.jpg)

- Allows partial device allocation by specifying device memory.
- Imposes a hard limit on streaming multiprocessors.
- Permits partial device allocation by specifying device core usage.
- Requires zero changes to existing programs.

## Architect

!<img src="./imgs/arch.png" width = "600" /> 

HAMi consists of several components, including a unified mutatingwebhook, a unified scheduler extender, different device-plugins and different in-container virtualization technics for each heterogeneous AI devices.

## Application Scenarios

1. Device sharing (or device virtualization) on Kubernetes.
2. Scenarios where pods need to be allocated with specific device memory 3. usage or device cores.
3. Need to balance GPU usage in a cluster with multiple GPU nodes.
4. Low utilization of device memory and computing units, such as running 10 TensorFlow servings on one GPU.
5. Situations that require a large number of small GPUs, such as teaching scenarios where one GPU is provided for multiple students to use, and cloud platforms that offer small GPU instances.

## Quick Start

### Prerequisites

The list of prerequisites for running the NVIDIA device plugin is described below:

- NVIDIA drivers >= 440
- CUDA Version > 10.2
- nvidia-docker version > 2.0
- Kubernetes version >= 1.16
- glibc >= 2.17 & glibc < 2.3.0
- kernel version >= 3.10
- helm > 3.0

### Preparing your GPU Nodes

<details> <summary> Configure nvidia-container-toolkit </summary>

Execute the following steps on all your GPU nodes.

This README assumes pre-installation of NVIDIA drivers and the `nvidia-container-toolkit`. Additionally, it assumes configuration of the `nvidia-container-runtime` as the default low-level runtime.

Please see: <https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html>

#### Example for debian-based systems with `Docker` and `containerd`

##### Install the `nvidia-container-toolkit`

```bash
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -s -L https://nvidia.github.io/libnvidia-container/gpgkey | sudo apt-key add -
curl -s -L https://nvidia.github.io/libnvidia-container/$distribution/libnvidia-container.list | sudo tee /etc/apt/sources.list.d/libnvidia-container.list
>>>>>>> c7a3893 (Remake this repo to HAMi)

- NVIDIA drivers >= 440
- nvidia-docker version > 2.0
- default runtime configured as nvidia for containerd/docker/cri-o container runtime
- Kubernetes version >= 1.18
- glibc >= 2.17 & glibc < 2.30
- kernel version >= 3.10
- helm > 3.0

<<<<<<< HEAD
### Install

First, Label your GPU nodes for scheduling with HAMi by adding the label "gpu=on". Without this label, the nodes cannot be managed by our scheduler.
=======
##### Configure `Docker`

When running `Kubernetes` with `Docker`, edit the configuration file, typically located at `/etc/docker/daemon.json`, to set up `nvidia-container-runtime` as the default low-level runtime:

```json
{
    "default-runtime": "nvidia",
    "runtimes": {
        "nvidia": {
            "path": "/usr/bin/nvidia-container-runtime",
            "runtimeArgs": []
        }
    }
}
```

And then restart `Docker`:

```
sudo systemctl daemon-reload && systemctl restart docker
```

##### Configure `containerd`

When running `Kubernetes` with `containerd`, modify the configuration file typically located at `/etc/containerd/config.toml`, to set up
`nvidia-container-runtime` as the default low-level runtime:

```
version = 2
[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    [plugins."io.containerd.grpc.v1.cri".containerd]
      default_runtime_name = "nvidia"

      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia]
          privileged_without_host_devices = false
          runtime_engine = ""
          runtime_root = ""
          runtime_type = "io.containerd.runc.v2"
          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia.options]
            BinaryName = "/usr/bin/nvidia-container-runtime"
```

And then restart `containerd`:

```
sudo systemctl daemon-reload && systemctl restart containerd
```

</details>

<details> <summary> Label your nodes </summary>

Label your GPU nodes for scheduling with HAMi by adding the label "gpu=on". Without this label, the nodes cannot be managed by our scheduler.
>>>>>>> c7a3893 (Remake this repo to HAMi)

```
kubectl label nodes {nodeid} gpu=on
```

<<<<<<< HEAD
Add our repo in helm
=======
</details>

### Install and Uninstall

<details> <summary> Installation </summary>

First, you need to check your Kubernetes version by using the following command:
>>>>>>> c7a3893 (Remake this repo to HAMi)

```
helm repo add hami-charts https://project-hami.github.io/HAMi/
```

Use the following command for deployment:

```
helm install hami hami-charts/hami -n kube-system
```

<<<<<<< HEAD
Customize your installation by adjusting the [configs](docs/config.md).

Verify your installation using the following command:
=======
During installation, set the Kubernetes scheduler image version to match your Kubernetes server version. For instance, if your cluster server version is 1.16.8, use the following command for deployment:
>>>>>>> c7a3893 (Remake this repo to HAMi)

```
kubectl get pods -n kube-system
```

<<<<<<< HEAD
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
=======
Customize your installation by adjusting the [configs](docs/config.md).

Verify your installation using the following command:

```
kubectl get pods -n kube-system
```

If both `vgpu-device-plugin` and `vgpu-scheduler` pods are in the *Running* state, your installation is successful.

</details>

<details> <summary> Upgrade </summary>

Upgrading HAMi to the latest version is a simple process, update the repository and restart the chart:

```
helm uninstall vgpu -n kube-system
helm repo update
helm install vgpu vgpu -n kube-system
```

> **WARNING:** *If you upgrade HAMi without clearing your submitted tasks, it may result in segmentation fault.*

</details>

<details> <summary> Uninstall </summary>

```
helm uninstall vgpu -n kube-system
```

> **NOTICE:** *Uninstallation won't kill running tasks.*

</details>

### Submit Task

<details> <summary> Task example </summary>

Containers can now request NVIDIA vGPUs using the `nvidia.com/gpu`` resource type.
>>>>>>> c7a3893 (Remake this repo to HAMi)

## Notes

<<<<<<< HEAD
- If you don't request vGPUs when using the device plugin with NVIDIA images all the GPUs on the machine may be exposed inside your container
- Currently, A100 MIG can be supported in only "none" and "mixed" modes.
- Tasks with the "nodeName" field cannot be scheduled at the moment; please use "nodeSelector" instead.

## RoadMap, Governance & Contributing

The project is governed by a group of [Maintainers](./MAINTAINERS.md) and [Contributors](./AUTHORS.md). How they are selected and govern is outlined in our [Governance Document](https://github.com/Project-HAMi/community/blob/main/governance.md).

If you're interested in being a contributor and want to get involved in developing the HAMi code, please see [CONTRIBUTING](CONTRIBUTING.md) for details on submitting patches and the contribution workflow.
=======
Exercise caution; if a task cannot fit into any GPU node (i.e., the requested number of `nvidia.com/gpu` exceeds the available GPUs in any node), the task will remain in a `pending` state.

You can now execute the `nvidia-smi` command in the container to observe the difference in GPU memory between vGPU and physical GPU.

> **WARNING:**
>
> *1. if you don't request vGPUs when using the device plugin with NVIDIA images all
> the vGPUs on the machine will be exposed inside your container.*
>
> *2. Do not set "nodeName" field, use "nodeSelector" instead.*

#### More examples
>>>>>>> c7a3893 (Remake this repo to HAMi)

See [RoadMap](docs/develop/roadmap.md) to see anything you interested.

<<<<<<< HEAD
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
=======
</details>

### Monitor

<details> <summary> Get cluster overview </summary>

Monitoring is automatically enabled after installation. Obtain an overview of cluster information by visiting the following URL:

```
http://{scheduler ip}:{monitorPort}/metrics
```

The default monitorPort is 31993; other values can be set using `--set devicePlugin.service.httpPort` during installation.

Grafana dashboard [example](docs/dashboard.md)

> **Note** The status of a node won't be collected before you submit a task

</details>

## [Benchmarks](docs/benchmark.md)

## Known Issues

- Currently, A100 MIG can be supported in only "none" and "mixed" modes.
- Tasks with the "nodeName" field cannot be scheduled at the moment; please use "nodeSelector" instead.
- Only computing tasks are currently supported; video codec processing is not supported.

## Roadmap

Heterogeneous AI Computing device to support

| Production  | manufactor | MemoryIsolation | CoreIsolation | MultiCard support |
|-------------|------------|-----------------|---------------|-------------------|
| GPU         | NVIDIA     | ✅              | ✅            | ✅                |
| MLU         | Cambricon  | ✅              | ❌            | ❌                |
| DCU         | Hygon      | ✅              | ✅            | ❌                |
| Ascend      | Huawei     | In progress     | In progress   | ❌                |
| GPU         | iluvatar   | In progress     | In progress   | ❌                |
| DPU         | Teco       | In progress     | In progress   | ❌                |

- Support video codec processing
- Support Multi-Instance GPUs (MIG)

## Issues and Contributing

- Report bugs, ask questions, or suggest modifications by [filing a new issue](https://github.com/4paradigm/k8s-vgpu-scheduler/issues/new)
- For more information or to share your ideas, you can participate in the [Discussions](https://github.com/4paradigm/k8s-device-plugin/discussions) and the [slack](https://join.slack.com/t/k8s-device-plugin/shared_invite/zt-oi9zkr5c-LsMzNmNs7UYg6usc0OiWKw) exchanges

## Contact

Owner & Maintainer: Limengxuan

Feel free to reach me by

```
email: <limengxuan@4paradigm.com>
phone: +86 18810644493
WeChat: xuanzong4493
```
>>>>>>> c7a3893 (Remake this repo to HAMi)
