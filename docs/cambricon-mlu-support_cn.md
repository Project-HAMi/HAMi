## 简介

本组件支持复用寒武纪MLU设备，并为此提供以下几种与vGPU类似的复用功能，包括：

***MLU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）来分配MLU，本组件会确保任务使用的显存不会超过分配数值，注意只有MLU-370型号的MLU支持可配显存

***指定MLU型号***：当前任务可以通过设置annotation("cambricon.com/use-mlutype","cambricon.com/nouse-mlutype")的方式，来选择使用或者不使用某些具体型号的MLU

***方便易用***:  部署本组件后，你只需要给MLU节点打上tag即可使用MLU复用功能


## 节点需求

* neuware-mlu370-driver > 4.15.10
* cntoolkit > 2.5.3

## 开启MLU复用

* 通过helm部署本组件, 参照[主文档中的开启vgpu支持章节](https://github.com/Project-HAMi/HAMi/blob/master/README_cn.md#kubernetes开启vgpu支持)

* 使用以下指令，为MLU节点打上label
```
kubectl label node {mlu-node} mlu=on
```

## 运行MLU任务

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
          cambricon.com/mlunum: 1 # requesting 1 MLU
          cambricon.com/mlumem: 10240 # requesting 10G MLU device memory
    - name: ubuntu-container1
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          cambricon.com/mlunum: 1 # requesting 1 MLU
          cambricon.com/mlumem: 10240 # requesting 10G MLU device memory
```

## 注意事项

1. 在init container中无法使用MLU复用功能，否则该任务不会被调度

2. MLU复用功能目前不支持containerd，在containerd中使用会导致任务失败

3. 只有MLU-370可以使用MLU复用功能
