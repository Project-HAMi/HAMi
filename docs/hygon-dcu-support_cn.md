## 简介

本组件支持复用海光DCU设备，并为此提供以下几种与vGPU类似的复用功能，包括：

<<<<<<< HEAD
***DCU 共享***: 每个任务可以只占用一部分DCU卡，多个任务可以共享一张DCU卡
=======
*** DCU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡
>>>>>>> 21785f7 (update to v2.3.2)

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）来分配DCU，本组件会确保任务使用的显存不会超过分配数值

***可限制计算单元数量***: 你现在可以指定任务使用的算力比例（例如60即代表使用60%算力）来分配DCU，本组件会确保任务使用的算力不会超过分配数值

<<<<<<< HEAD
***指定DCU型号***：当前任务可以通过设置annotation("hygon.com/use-dcutype","hygon.com/nouse-dcutype")的方式，来选择使用或者不使用某些具体型号的DCU

## 节点需求

* DCU驱动版本 >= 6.3.8

## 开启DCU复用

* 部署[DCU-Device-Plugin](https://developer.sourcefind.cn/document/87ee5c5b-c10d-11f0-b077-0242ac150003?id=8df80ff9-c10e-11f0-b077-0242ac150003)

## 运行DCU任务

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vdcu-pytorch-demo
spec:
  containers:
    - name: vdcu-pytorch-demo
      image: image.sourcefind.cn:5000/dcu/admin/base/pytorch:2.1.0-ubuntu22.04-dtk24.04.2-py3.10
      command: [ "/bin/bash", "-c", "--" ]
      args: [ "sleep infinity & wait" ]
      resources:
        limits:
          hygon.com/dcunum: 1   # requesting a vDCU
          hygon.com/dcucores: 60  # each vDCU use 60% of total compute cores
          hygon.com/dcumem: 2000  # each vDCU require 2000 MiB device memory
```

## 容器内查看虚拟DCU规格

容器内查看vDCU规格，需要配置驱动环境变量。
```
source /opt/hyhal/env.sh
```

随后，使用驱动命令查看虚拟设备信息。
```
hy-smi virtual -show-device-info
```

若输出如下，则代表虚拟设备已经成功激活。
=======
***指定MLU型号***：当前任务可以通过设置annotation("hygon.com/use-dcutype","hygon.com/nouse-dcutype")的方式，来选择使用或者不使用某些具体型号的DCU

## 节点需求

* 带有虚拟化功能的dtk驱动（例如dtk-22.10.1-vdcu）,相关组件可以在海光开发者社区获取，或联系您的设备提供商

* 在宿主机上执行hdmcli -show-device-info获取设备信息，若能成功获取，则代表配置成功。若找不到指令，说明您安装的驱动不带有虚拟化功能，请联系厂商获取代虚拟化功能的dtk驱动

* 需要将各个DCU节点上的dtk驱动路径放置在统一的绝对路径上，例如均放置在/root/dtk-driver

## 开启DCU复用

* 通过helm部署本组件, 参照[主文档中的开启vgpu支持章节](https://github.com/4paradigm/k8s-vgpu-scheduler/blob/master/README_cn.md#kubernetes开启vgpu支持)，需要注意的是，必须使用--set devicePlugin.hygondriver="/root/dcu-driver/dtk-22.10.1-vdcu" 手动指定dtk驱动的绝对路径

```
helm install vgpu vgpu-charts/vgpu --set devicePlugin.hygondriver="/root/dcu-driver/dtk-22.10.1-vdcu" --set scheduler.kubeScheduler.imageTag={your k8s server version} -n kube-system
```

* 使用以下指令，为MLU节点打上label
```
kubectl label node {dcu-node} dcu=on
```

## 运行DCU任务

```
apiVersion: v1
kind: Pod
metadata:
  name: alexnet-tf-gpu-pod-mem
  labels:
    purpose: demo-tf-amdgpu
spec:
  containers:
    - name: alexnet-tf-gpu-container
      image: pytorch:resnet50
      workingDir: /root
      command: ["sleep","infinity"]
      resources:
        limits:
          hygon.com/dcunum: 1 # requesting a GPU
          hygon.com/dcumem: 2000
          hygon.com/dcucores: 60

```

## 容器内开启虚拟DCU功能

使用vDCU首先需要激活虚拟环境
```
source /opt/hygondriver/env.sh
```

随后，使用hdmcli指令查看虚拟设备是否已经激活
```
hdmcli -show-device-info
```

若输出如下，则代表虚拟设备已经成功激活
>>>>>>> 21785f7 (update to v2.3.2)
```
Device 0:
	Actual Device: 0
	Compute units: 60
	Global memory: 2097152000 bytes
```

接下来正常启动DCU任务即可

## 注意事项

1. 在init container中无法使用DCU复用功能，否则该任务不会被调度

2. 每个容器最多只能使用一个虚拟DCU设备, 如果您希望在容器中挂载多个DCU设备，则不能使用`hygon.com/dcumem`和`hygon.com/dcucores`字段
