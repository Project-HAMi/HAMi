# vGPU device plugin for Kubernetes


## 目录

- [关于](#关于)
- [使用场景](#使用场景)
- [调度策略](#调度策略)
- [性能测试](#性能测试)
- [功能](#功能)
- [实验性功能](#实验性功能)
- [已知问题](#已知问题)
- [开发计划](#开发计划)
- [安装要求](#安装要求)
- [快速入门](#快速入门)
  - [GPU节点准备](#GPU节点准备)
  - [Kubernetes开启vGPU支持](#Kubernetes开启vGPU支持)
  - [运行GPU任务](#运行GPU任务)
- [测试](#测试)
- [问题反馈及代码贡献](#问题反馈及代码贡献)

## 关于

**k8s vGPU scheduler** 基于NVIDIA官方插件([NVIDIA/k8s-device-plugin](https://github.com/NVIDIA/k8s-device-plugin))，在保留官方功能的基础上，实现了对物理GPU进行切分，并对显存和计算单元进行限制，从而模拟出多张小的vGPU卡。k8s vGPU scheduler在原有显存分配方式的基础上，可以通过设置显存和算力更准确的分配到任务所需要的vGPU卡。在k8s集群中，基于这些切分后的vGPU进行调度，使不同的容器可以安全的共享同一张物理GPU，提高GPU的利用率。此外，插件还可以对显存做虚拟化处理（使用到的显存可以超过物理上的显存），运行一些超大显存需求的任务，或提高共享的任务数，可参考[性能测试报告](#性能测试)。

## 使用场景

1. 需要定制GPU申请的场合，如申请特定大小的vGPU，每个vGPU使用特定比例的算力。
2. 在多个GPU节点组成的集群中，任务需要根据自身的显卡需求分配到合适的节点执行。
3. 显存、计算单元利用率低的情况，如在一张GPU卡上运行10个tf-serving。
4. 需要大量小显卡的情况，如教学场景把一张GPU提供给多个学生使用、云平台提供小GPU实例。
5. 物理显存不足的情况，可以开启虚拟显存，如大batch、大模型的训练。

## 调度策略

调度策略为，在保证显存和算力满足需求的GPU中，优先选择任务数最少的GPU执行任务，这样做可以使任务均匀分配到所有的GPU中

## 性能测试

见[k8s-device-plugin的性能测试部分](https://github.com/4paradigm/k8s-device-plugin/blob/master/README_cn.md#性能测试)

## 功能

- 指定每张物理GPU切分的最大vGPU的数量
- 限制vGPU的显存
- 限制vGPU的计算单元
- 对已有程序零改动

## 实验性功能

- 虚拟显存

  vGPU的显存总和可以超过GPU实际的显存，这时候超过的部分会放到内存里，对性能有一定的影响。

## 已知问题

- 目前仅支持计算任务，不支持视频编解码处理。
- 暂时不支持MIG

## 开发计划

- 支持视频编解码处理
- 支持Multi-Instance GPUs (MIG) 

## 安装要求

* NVIDIA drivers >= 384.81
* nvidia-docker version > 2.0 
* docker已配置nvidia作为默认runtime
* Kubernetes version >= 1.10

## 快速入门

### GPU节点准备

以下步骤要在所有GPU节点执行。这份README文档假定GPU节点已经安装NVIDIA驱动和`nvidia-docker`套件。

注意你需要安装的是`nvidia-docker2`而非`nvidia-container-toolkit`。因为新的`--gpus`选项kubernetes尚不支持。安装步骤举例：

```
# 加入套件仓库
$ distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
$ curl -s -L https://nvidia.github.io/nvidia-docker/gpgkey | sudo apt-key add -
$ curl -s -L https://nvidia.github.io/nvidia-docker/$distribution/nvidia-docker.list | sudo tee /etc/apt/sources.list.d/nvidia-docker.list

$ sudo apt-get update && sudo apt-get install -y nvidia-docker2
$ sudo systemctl restart docker
```

你需要在节点上将nvidia runtime做为你的docker runtime预设值。我们将编辑docker daemon的配置文件，此文件通常在`/etc/docker/daemon.json`路径：

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

> *如果 `runtimes` 字段没有出现, 前往的安装页面执行安装操作 [nvidia-docker](https://github.com/NVIDIA/nvidia-docker)*

### Kubernetes开启vGPU支持

当你在所有GPU节点完成前面提到的准备动作，如果Kubernetes有已经存在的NVIDIA装置插件，需要先将它移除。然后，你需要下载整个项目，并进入deployments文件夹

```
$ git clone https://gitlab.4pd.io/vgpu/k8s-vgpu.git
$ cd k8s-vgpu/deployments
```

在这个deployments文件中, 你可以在 `values.yaml/devicePlugin/extraArgs` 中使用以下的客制化参数：

* `device-split-count:` 
  整数类型，预设值是10。GPU的分割数，每一张GPU都不能分配超过其配置数目的任务。若其配置为N的话，每个GPU上最多可以同时存在N个任务。
* `device-memory-scaling:` 
  浮点数类型，预设值是1。NVIDIA装置显存使用比例，可以大于1（启用虚拟显存，实验功能）。对于有*M​*显存大小的NVIDIA GPU，如果我们配置`device-memory-scaling`参数为*S*，在部署了我们装置插件的Kubenetes集群中，这张GPU分出的vGPU将总共包含 *S \* M*显存。每张vGPU的显存大小也受`device-split-count`参数影响。在先前的例子中，如果`device-split-count`参数配置为*K*，那每一张vGPU最后会取得 *S \* M / K* 大小的显存。

配置完成后，随后使用helm安装整个chart

```
$ helm install vgpu 4pd-vgpu
```

通过kubectl get pods指令看到vgpu-4pd-vgpu-device-plugin与vgpu-4pd-vgpu-scheduler两个pod即为安装成功

```
$ kubectl get pods
```

### 运行GPU任务

NVIDIA vGPUs 现在能透过资源类型`nvidia.com/gpu`被容器请求：

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
          nvidia.com/gpu: 2 # 请求2个vGPUs
	  nvidia.com/gpumem: 3000 # 每个vGPU申请3000m显存 （可选）
	  nvidia.com/gpucores: 30 # 每个vGPU的算力为30%实际显卡的算力 （可选）
```

现在你可以在容器执行`nvidia-smi`命令，然后比较vGPU和实际GPU显存大小的不同。

## 测试

- TensorFlow 1.14.0/2.4.1
- torch1.1.0
- mxnet 1.4.0
- mindspore 1.1.1

以上框架均通过测试。

## 反馈和参与

* bug、疑惑、修改欢迎提在 [Github Issues](https://github.com/4paradigm/k8s-device-plugin/issues/new)
* 想了解更多或者有想法可以参与到[Discussions](https://github.com/4paradigm/k8s-device-plugin/discussions)和[slack](https://join.slack.com/t/k8s-device-plugin/shared_invite/zt-oi9zkr5c-LsMzNmNs7UYg6usc0OiWKw)交流

