# vGPU scheduler for Kubernetes

[![build status](https://github.com/4paradigm/k8s-device-plugin/actions/workflows/build.yml/badge.svg)](https://github.com/4paradigm/k8s-vgpu-scheduler/actions/workflows/main.yml)
[![docker pulls](https://img.shields.io/docker/pulls/4pdosc/k8s-device-plugin.svg)](https://hub.docker.com/r/4pdosc/k8s-vgpu)
[![slack](https://img.shields.io/badge/Slack-Join%20Slack-blue)](https://join.slack.com/t/k8s-device-plugin/shared_invite/zt-oi9zkr5c-LsMzNmNs7UYg6usc0OiWKw)
[![discuss](https://img.shields.io/badge/Discuss-Ask%20Questions-blue)](https://github.com/4paradigm/k8s-device-plugin/discussions)

English version|[中文版](README_cn.md)

## Table of Contents

- [About](#about)
- [When to use](#when-to-use)
- [Stategy](#strategy)
- [Benchmarks](#Benchmarks)
- [Features](#Features)
- [Experimental Features](#Experimental-Features)
- [Known Issues](#Known-Issues)
- [TODO](#TODO)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
  - [Preparing your GPU Nodes](#preparing-your-gpu-nodes)
  - [Enabling vGPU Support in Kubernetes](#enabling-vGPU-support-in-kubernetes)
  - [Running GPU Jobs](#running-gpu-jobs)
- [Tests](#Tests)
- [Issues and Contributing](#issues-and-contributing)

## About

The **k8s vGPU scheduler** is based on 4pd-k8s-device-plugin ([4paradigm/k8s-device-plugin](https://github.com/4paradigm/k8s-device-plugin)), and on the basis of retaining features of 4pd-device-plugin, such as splits the physical GPU, limits the memory and computing unit. It adds the scheduling module to balance GPU usage across GPU nodes. In addition, it allow users to allocate GPU by specifying the device memory and device core usage. Thus provide great flexibility and more GPU utilization. Furthermore, the vGPU scheduler can virtualize the device memory (the used device memory can exceed the physical device memory), run some tasks with large device memory requirements, or increase the number of shared tasks. You can refer to [the benchmarks report](#benchmarks).

## When to use

1. Needs pods with certain device memory usage or device core percentage.
2. Needs to balance GPU usage in cluster with mutiple GPU node
3. Low utilization of device memory and computing units, such as running 10 tf-servings on one GPU.
4. Situations that require a large number of small GPUs, such as teaching scenarios where one GPU is provided for multiple students to use, and the cloud platform provides small GPU instances.
5. In the case of insufficient physical device memory, virtual device memory can be turned on, such as training of large batches and large models.

## Scheduling

Current schedule strategy is to select GPU with lowest task, thus balance the loads across mutiple GPUs

## Benchmarks

Three instances from ai-benchmark have been used to evaluate vGPU-device-plugin performance as follows

| Test Environment | description                                              |
| ---------------- | :------------------------------------------------------: |
| Kubernetes version | v1.12.9                                                |
| Docker  version    | 18.09.1                                                |
| GPU Type           | Tesla V100                                             |
| GPU Num            | 2                                                      |

| Test instance |                         description                         |
| ------------- | :---------------------------------------------------------: |
| nvidia-device-plugin      |               k8s + nvidia k8s-device-plugin                |
| vGPU-device-plugin        | k8s + VGPU k8s-device-plugin，without virtual device memory |
| vGPU-device-plugin(virtual device memory) |  k8s + VGPU k8s-device-plugin，with virtual device memory   |

Test Cases:

| test id |     case      |   type    |         params          |
| ------- | :-----------: | :-------: | :---------------------: |
| 1.1     | Resnet-V2-50  | inference |  batch=50,size=346*346  |
| 1.2     | Resnet-V2-50  | training  |  batch=20,size=346*346  |
| 2.1     | Resnet-V2-152 | inference |  batch=10,size=256*256  |
| 2.2     | Resnet-V2-152 | training  |  batch=10,size=256*256  |
| 3.1     |    VGG-16     | inference |  batch=20,size=224*224  |
| 3.2     |    VGG-16     | training  |  batch=2,size=224*224   |
| 4.1     |    DeepLab    | inference |  batch=2,size=512*512   |
| 4.2     |    DeepLab    | training  |  batch=1,size=384*384   |
| 5.1     |     LSTM      | inference | batch=100,size=1024*300 |
| 5.2     |     LSTM      | training  | batch=10,size=1024*300  |

Test Result: ![img](./imgs/benchmark_inf.png)

![img](./imgs/benchmark_train.png)

To reproduce:

1. install vGPU-nvidia-device-plugin，and configure properly
2. run benchmark job

```
$ kubectl apply -f benchmarks/ai-benchmark/ai-benchmark.yml
```

3. View the result by using kubctl logs

```
$ kubectl logs [pod id]
```

## Features

- Specify the number of vGPUs divided by each physical GPU.
- Limit vGPU's Device Memory.
- Allows vGPU allocation by specifying device memory 
- Limit vGPU's Streaming Multiprocessor.
- Allows vGPU allocation by specifying device core usage
- Zero changes to existing programs

## Experimental Features

- Virtual Device Memory

  The device memory of the vGPU can exceed the physical device memory of the GPU. At this time, the excess part will be put in the RAM, which will have a certain impact on the performance.

## Known Issues

- Currently, A100 MIG not supported 
- Currently, only computing tasks are supported, and video codec processing is not supported.

## TODO

- Support video codec processing
- Support Multi-Instance GPUs (MIG)

## Prerequisites

The list of prerequisites for running the NVIDIA device plugin is described below:
* NVIDIA drivers ~= 384.81
* nvidia-docker version > 2.0
* Kubernetes version >= 1.10
* glibc >= 2.17
* kernel version >= 3.10

## Quick Start

### Preparing your GPU Nodes


The following steps need to be executed on all your GPU nodes.
This README assumes that the NVIDIA drivers and `nvidia-docker` have been installed.

Note that you need to install the `nvidia-docker2` package and not the `nvidia-container-toolkit`.
This is because the new `--gpus` options hasn't reached kubernetes yet. Example:

```
# Add the package repositories
$ distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
$ curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | sudo apt-key add -
$ curl -s -L https://nvidia.github.io/nvidia-docker/$distribution/nvidia-docker.list | sudo tee /etc/apt/sources.list.d/nvidia-docker.list

$ sudo apt-get update && sudo apt-get install -y nvidia-docker2
$ sudo systemctl restart docker
```

You will need to enable the nvidia runtime as your default runtime on your node.
We will be editing the docker daemon config file which is usually present at `/etc/docker/daemon.json`:

```
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

> *if `runtimes` is not already present, head to the install page of [nvidia-docker](https://github.com/NVIDIA/nvidia-docker)*

Then, you need to label your GPU nodes which can be scheduled by 4pd-k8s-scheduler by adding "gpu=on", otherwise, it can not be managed by our scheduler.

```
kubectl label nodes {nodeid} gpu=on
```

### Enabling vGPU Support in Kubernetes

Once you have configured the options above on all the GPU nodes in your
cluster, remove existing NVIDIA device plugin for Kubernetes if it already exists. Then, you need to clone our project, and enter deployments folder

```
$ git clone https://gitlab.4pd.io/vgpu/k8s-vgpu.git
$ cd k8s-vgpu/deployments
```

In the deployments folder, you can customize your vGPU support by modifying following values in `values.yaml/devicePlugin/extraArgs` :

* `device-memory-scaling:` 
  Float type, by default: 1. The ratio for NVIDIA device memory scaling, can be greater than 1 (enable virtual device memory, experimental feature). For NVIDIA GPU with *M* memory, if we set `device-memory-scaling` argument to *S*, vGPUs splitted by this GPU will totaly get *S \* M* memory in Kubernetes with our device plugin.
* `device-split-count:` 
  Integer type, by default: equals 10. Maxinum tasks assigned to a simple GPU device.

Besides, you can customize the follwing values in `values.yaml/scheduler/extender/extraArgs`:

* `default-mem:` 
  Integer type, by default: 5000. The default device memory of the current task, in MB
* `default-cores:` 
  Integer type, by default: equals 0. Default GPU core usage of the current task. If assigned to 0, it may fit in any GPU with enough device memory. If assigned to 100, it will use an entire GPU card exclusively.

After configure those optional arguments, you can enable the vGPU support by following command:

```
$ helm install vgpu 4pd-vgpu
```

You can verify your install by following command:

```
$ kubectl get pods
```

If the following two pods `vgpu-4pd-vgpu-device-plugin` and `vgpu-4pd-vgpu-scheduler` are in running state, then your installation is successful.

### Running GPU Jobs

NVIDIA vGPUs can now be requested by a container
using the `nvidia.com/gpu` resource type:

```
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
          nvidia.com/gpu: 2 # requesting 2 vGPUs
	  nvidia.com/gpumem: 3000 # Each vGPU contains 3000m device memory （Optional）
	  nvidia.com/gpucores: 30 # Each vGPU uses 30% of the entire GPU （Optional)
```

You can now execute `nvidia-smi` command in the container and see the difference of GPU memory between vGPU and real GPU.

> **WARNING:** *if you don't request vGPUs when using the device plugin with NVIDIA images all
> the vGPUs on the machine will be exposed inside your container.*

## Tests

- TensorFlow 1.14.0/2.4.1
- torch 1.1.0
- mxnet 1.4.0
- mindspore 1.1.1

The above frameworks have passed the test.

## Issues and Contributing

* You can report a bug, a doubt or modify by [filing a new issue](https://github.com/4paradigm/k8s-vgpu-scheduler/issues/new)
* If you want to know more or have ideas, you can participate in [Discussions](https://github.com/4paradigm/k8s-device-plugin/discussions) and [slack](https://join.slack.com/t/k8s-device-plugin/shared_invite/zt-oi9zkr5c-LsMzNmNs7UYg6usc0OiWKw) exchanges


