## 简介

本组件支持复用华为升腾910B设备，并为此提供以下几种与vGPU类似的复用功能，包括：

*** NPU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）来分配NPU，本组件会确保任务使用的显存不会超过分配数值

***可限制分配的算力大小***: 你现在可以用百分比来分配 NPU的算力，本组件会确保任务使用的算力不会超过分配数值

## 节点需求

* Ascend docker runtime
* driver version > 24.1.rc1
* Ascend device type: 910B(300T A2)

## 开启NPU复用

* 通过helm部署本组件, 参照[主文档中的开启vgpu支持章节](https://github.com/Project-HAMi/HAMi/blob/master/README_cn.md#kubernetes开启vgpu支持)

* 使用以下指令，为Ascend 910B所在节点打上label
```
kubectl label node {ascend-node} accelerator=huawei-Ascend910
```

* 部署[Ascend docker runtime](https://gitee.com/ascend/ascend-docker-runtime)

* 从HAMi项目中获取并安装[ascend-device-plugin](https://github.com/Project-HAMi/ascend-device-plugin/blob/master/build/ascendplugin-910-hami.yaml)

* 部署`ascend-device-plugin`

```
kubectl apply -f ascendplugin-910-hami.yaml
```


## 运行NPU任务

```
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910: 1 # requesting 1 vGPUs
          huawei.com/Ascend910-memory: 2000 # requesting 2000m device memory
```

## 注意事项

1. 目前Ascend910B设备，只支持2种粒度的切分，分别是1/4卡和1/2卡，分配的显存会自动对齐到在分配额之上最近的粒度上

2. 在init container中无法使用NPU复用功能

3. 只有申请单MLU的任务可以指定显存`Ascend910-memory`的数值，若申请的NPU数量大于1，则所有申请的NPU都会被整卡分配 
