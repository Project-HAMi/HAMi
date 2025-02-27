## 简介

我们对沐曦设备做如下支持：

- 复用沐曦GPU设备，提供与vGPU类似的复用功能
- 基于拓扑结构，对沐曦设备进行优化调度

## 复用沐曦GPU设备，提供与vGPU类似的复用功能

复用功能包括以下：

***GPU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如4G）来分配GPU，本组件会确保任务使用的显存不会超过分配数值

***可限制计算单元数量***: 你现在可以指定任务使用的算力比例（例如60即代表使用60%算力）来分配GPU，本组件会确保任务使用的算力不会超过分配数值

### 需求

* Metax Driver >= 2.31.0
* Metax GPU Operator >= 0.10.1
* Kubernetes >= 1.23

### 开启复用沐曦设备

* 部署Metax GPU Operator (请联系您的设备提供方获取)
* 根据readme.md部署HAMi

### 运行沐曦任务

一个典型的沐曦任务如下所示：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod1
spec:
  containers:
    - name: ubuntu-container
      image: cr.metax-tech.com/public-ai-release/c500/colossalai:2.24.0.5-py38-ubuntu20.04-amd64 
      imagePullPolicy: IfNotPresent
      command: ["sleep","infinity"]
      resources:
        limits:
          metax-tech.com/sgpu: 1 # requesting 1 GPU 
          metax-tech.com/vcore: 60 # each GPU use 60% of total compute cores
          metax-tech.com/vmemory: 4 # each GPU require 4 GiB device memory
```

> **NOTICE1:** *你可以在这里找到更多样例 [examples/metax folder](../examples/metax/sgpu)*

## 基于拓扑结构，对沐曦设备进行优化调度

在单台服务器上配置多张 GPU 时，GPU 卡间根据双方是否连接在相同的 PCIe Switch 或 MetaXLink
下，存在近远（带宽高低）关系。服务器上所有卡间据此形成一张拓扑，如下图所示。

![img](../imgs/metax_topo.png)

用户作业请求一定数量的 metax-tech.com/gpu 资源，Kubernetes 选择剩余资源数量满足要求的
节点，并将 Pod 调度到相应节点。gpu‑device 进一步处理资源节点上剩余资源的分配逻辑，并按照以
下优先级逻辑为作业容器分配 GPU 设备：
1. MetaXLink 优先级高于 PCIe Switch，包含两层含义：
– 两卡之间同时存在 MetaXLink 连接以及 PCIe Switch 连接时，认定为 MetaXLink 连接。
– 服务器剩余 GPU 资源中 MetaXLink 互联资源与 PCIe Switch 互联资源均能满足作业请求时，分
配 MetaXLink 互联资源。

2. 当任务使用 `node-scheduler-policy=spread` ,分配GPU资源尽可能位于相同 MetaXLink或PCIe Switch下，如下图所示:

![img](../imgs/metax_spread.png)

3. 当使用 `node-scheduler-policy=binpack`,分配GPU资源后，剩余资源尽可能完整，如下图所示：

![img](../imgs/metax_binpack.png)

### 注意：

1. 暂时不支持沐曦设备的切片，只能申请整卡

2. 本功能基于MXC500进行测试

### 需求

* Metax GPU extensions >= 0.8.0
* Kubernetes >= 1.23

### 开启针对沐曦设备的拓扑调度优化

* 部署Metax GPU extensions (请联系您的设备提供方获取)

* 根据readme.md部署HAMi

### 运行沐曦任务

一个典型的沐曦任务如下所示：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod1
  annotations: hami.io/node-scheduler-policy: "spread" # when this parameter is set to spread, the scheduler will try to find the best topology for this task.
spec:
  containers:
    - name: ubuntu-container
      image: cr.metax-tech.com/public-ai-release/c500/colossalai:2.24.0.5-py38-ubuntu20.04-amd64 
      imagePullPolicy: IfNotPresent
      command: ["sleep","infinity"]
      resources:
        limits:
          metax-tech.com/gpu: 1 # requesting 1 vGPUs
```

> **NOTICE2:** *你可以在这里找到更多样例 [examples/metax folder](../examples/metax/gpu)*
