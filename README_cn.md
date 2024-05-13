<img src="./HAMi.jpg" width="200px">

# HAMi--异构算力虚拟化中间件

[![build status](https://github.com/Project-HAMi/HAMi/actions/workflows/main.yml/badge.svg)](https://github.com/Project-HAMi/HAMi/actions/workflows/build.yml)
[![docker pulls](https://img.shields.io/docker/pulls/4pdosc/k8s-vgpu.svg)](https://hub.docker.com/r/4pdosc/k8s-vgpu)
[![slack](https://img.shields.io/badge/Slack-Join%20Slack-blue)](https://join.slack.com/t/hami-hsf3791/shared_invite/zt-2gcteqiph-Ls8Atnpky6clrspCAQ_eGQ)
[![discuss](https://img.shields.io/badge/Discuss-Ask%20Questions-blue)](https://github.com/Project-HAMi/HAMi/discussions)
[![Contact Me](https://img.shields.io/badge/Contact%20Me-blue)](https://github.com/Project-HAMi/HAMi#contact)

---
<p>
<img src="https://github.com/cncf/artwork/blob/main/other/illustrations/ashley-mcnamara/transparent/cncf-cloud-gophers-transparent.png" style="width:700px;" />
</p>

**HAMi is a [Cloud Native Computing Foundation](https://cncf.io/) Landscape project.**

## 支持设备：

[![英伟达 GPU](https://img.shields.io/badge/Nvidia-GPU-blue)](https://github.com/Project-HAMi/HAMi#preparing-your-gpu-nodes)
[![寒武纪 MLU](https://img.shields.io/badge/寒武纪-Mlu-blue)](docs/cambricon-mlu-support_cn.md)
[![海光 DCU](https://img.shields.io/badge/海光-DCU-blue)](docs/hygon-dcu-support.md)
[![天数智芯 GPU](https://img.shields.io/badge/天数智芯-GPU-blue)](docs/iluvatar-gpu-support_cn.md)
[![华为升腾 NPU](https://img.shields.io/badge/华为升腾-NPU-blue)](docs/ascend910b-support_cn.md)

## 简介

!<img src="./imgs/example.png" width = "600" /> 

异构算力虚拟化中间件HAMi满足了所有你对于管理异构算力集群所需要的能力，包括：

***设备复用***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）或者显存比例（例如50%）来分配GPU，vGPU调度器会确保任务使用的显存不会超过分配数值

***指定设备型号***：当前任务可以通过设置annotation的方式，来选择使用或者不使用某些具体型号的设备

***设备指定UUID***：当前任务可以通过设置`annotation`的方式，来选择使用或者不使用指定的设备，比如："nvidia.com/use-gpuuuid" or "nvidia.com/nouse-gpuuuid"

***无侵入***:  vGPU调度器兼容nvidia官方插件的显卡分配方式，所以安装完毕后，你不需要修改原有的任务文件就可以使用vGPU的功能。当然，你也可以自定义的资源名称

## 使用场景

1. 云原生场景下需要复用算力设备的场合
2. 需要定制异构算力申请的场合，如申请特定显存大小的虚拟GPU，每个虚拟GPU使用特定比例的算力。
3. 在多个异构算力节点组成的集群中，任务需要根据自身的显卡需求分配到合适的节点执行。
4. 显存、计算单元利用率低的情况，如在一张GPU卡上运行10个tf-serving。
5. 需要大量小显卡的情况，如教学场景把一张GPU提供给多个学生使用、云平台提供小GPU实例。

## 产品设计

!<img src="./imgs/arch.png" width = "600" /> 

HAMi 包含以下几个组件，一个统一的mutatingwebhook，一个统一的调度器，以及针对各种不同的异构算力设备对应的设备插件和容器内的控制组件，整体的架构特性如上图所示。

## 产品特性

- 显存资源的硬隔离

一个硬隔离的简单展示：
一个使用以下方式定义的任务提交后
```
      resources:
        limits:
          nvidia.com/gpu: 1 # requesting 1 vGPU
          nvidia.com/gpumem: 3000 # Each vGPU contains 3000m device memory
```
会只有3G可见显存

![img](./imgs/hard_limit.jpg)

- 允许通过指定显存来申请算力设备
- 算力资源的硬隔离
- 允许通过指定算力使用比例来申请算力设备
- 对已有程序零改动

## 安装要求

* NVIDIA drivers >= 440
* nvidia-docker version > 2.0 
* docker已配置nvidia作为默认runtime
* Kubernetes version >= 1.16
* glibc >= 2.17 & glibc < 2.3.0
* kernel version >= 3.10
* helm > 3.0 

## 快速入门

### 准备节点

<details> <summary> 配置 nvidia-container-toolkit </summary>

### GPU节点准备

以下步骤要在所有GPU节点执行,这份README文档假定GPU节点已经安装NVIDIA驱动。它还假设您已经安装docker或container并且需要将nvidia-container-runtime配置为要使用的默认低级运行时。

安装步骤举例：

####
```
# 加入套件仓库
distribution=$(. /etc/os-release;echo $ID$VERSION_ID)
curl -s -L https://nvidia.github.io/libnvidia-container/gpgkey | sudo apt-key add -
curl -s -L https://nvidia.github.io/libnvidia-container/$distribution/libnvidia-container.list | sudo tee /etc/apt/sources.list.d/libnvidia-container.list

sudo apt-get update && sudo apt-get install -y nvidia-container-toolkit
```

##### 配置docker
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
```
systemctl daemon-reload && systemctl restart docker
```
##### 配置containerd
你需要在节点上将nvidia runtime做为你的containerd runtime预设值。我们将编辑containerd daemon的配置文件，此文件通常在`/etc/containerd/config.toml`路径
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
```
systemctl daemon-reload && systemctl restart containerd
```

</details>

<details> <summary> 为GPU节点打上标签 </summary>

最后，你需要将所有要使用到的GPU节点打上gpu=on标签，否则该节点不会被调度到

```
$ kubectl label nodes {nodeid} gpu=on
```

</details>

### 安装，更新与卸载

<details> <summary> 安装 </summary>

首先使用helm添加我们的 repo

```
helm repo add hami-charts https://project-hami.github.io/HAMi/
```

随后，使用下列指令获取集群服务端版本

```
kubectl version
```

在安装过程中须根据集群服务端版本（上一条指令的结果）指定调度器镜像版本，例如集群服务端版本为1.16.8，则可以使用如下指令进行安装

```
$ helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag=v1.16.8 -n kube-system
```

你可以修改这里的[配置](docs/config_cn.md)来定制安装

通过kubectl get pods指令看到 `vgpu-device-plugin` 与 `vgpu-scheduler` 两个pod 状态为*Running*  即为安装成功

```
$ kubectl get pods -n kube-system
```

</details>

<details> <summary> 更新 </summary>

只需要更新helm repo，并重新启动整个Chart即可自动完成更新，最新的镜像会被自动下载

```
$ helm uninstall hami -n kube-system
$ helm repo update
$ helm install hami hami-charts/hami -n kube-system
```

> **注意:** *如果你没有清理完任务就进行热更新的话，正在运行的任务可能会出现段错误等报错.*

</details>

<details> <summary> 卸载 </summary>

```
$ helm uninstall hami -n kube-system
```

> **注意:** *卸载组件并不会使正在运行的任务失败.*

</details>

### 提交任务

<details> <summary> 任务样例 </summary>

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
	  nvidia.com/gpumem: 3000 # 每个vGPU申请3000m显存 （可选，整数类型）
	  nvidia.com/gpucores: 30 # 每个vGPU的算力为30%实际显卡的算力 （可选，整数类型）
```

如果你的任务无法运行在任何一个节点上（例如任务的`nvidia.com/gpu`大于集群中任意一个GPU节点的实际GPU数量）,那么任务会卡在`pending`状态

现在你可以在容器执行`nvidia-smi`命令，然后比较vGPU和实际GPU显存大小的不同。

> **注意:** *1. 如果你使用privileged字段的话，本任务将不会被调度，因为它可见所有的GPU，会对其它任务造成影响.*
> 
> *2. 不要设置nodeName字段，类似需求请使用nodeSelector.* 

</details>

#### 更多范例

点击 [范例](examples/nvidia)


### 监控：

<details> <summary> 访问集群算力视图 </summary>

调度器部署成功后，监控默认自动开启，你可以通过

```
http://{nodeip}:{monitorPort}/metrics
```

来获取监控数据，其中monitorPort可以在Values中进行配置，默认为31992

grafana dashboard [示例](docs/dashboard_cn.md)

> **注意** 节点上的vGPU状态只有在其使用vGPU后才会被统计

</details>

## [性能测试](docs/benchmark_cn.md)

## 已知问题

- 目前仅支持计算任务，不支持视频编解码处理。
- 暂时仅支持MIG的"none"和"mixed"模式，暂时不支持single模式
- 当任务有字段“nodeName“时会出现无法调度的情况，有类似需求的请使用"nodeSelector"代替
- 我们修改了 `device-plugin` 组件的环境变量，从 `NodeName` 改为 `NODE_NAME`, 如果使用的是镜像版本是 `v2.3.9`, 则可能会出现 `device-plugin` 无法启动的情况，目前有两种修复建议：
  - 手动执行`kubectl edit daemonset` 修改 `device-plugin` 的环境变量从`NodeName` 改为 `NODE_NAME`。
  - 使用helm升级到最新版本，最新版`device-plugin`的镜像版本是`v2.3.10`，执行 `helm upgrade hami hami/hami -n kube-system`, 会自动修复。

## 开发计划

- 目前支持的异构算力设备及其对应的复用特性如下表所示

| 产品  | 制造商 | 显存隔离 | 算力隔离 | 多卡支持 |
|-------------|------------|-----------------|---------------|-------------------|
| GPU         | NVIDIA     | ✅              | ✅            | ✅                |
| MLU         | 寒武纪  | ✅              | ❌            | ❌                |
| DCU         | 海光      | ✅              | ✅            | ❌                |
| Ascend      | 华为     | 开发中     | 开发中   | ❌                |
| GPU         | 天数智芯   | 开发中     | 开发中   | ❌                |
| DPU         | 太初       | 开发中     | 开发中   | ❌                | 
- 支持视频编解码处理。
- 支持Multi-Instance GPUs (MIG)。


## 参与贡献

如果你想成为 HAMi 的贡献者，请参[考贡献者指南](CONTRIBUTING.md),里面有详细的贡献流程。
