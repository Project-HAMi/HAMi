## 简介

本组件支持复用摩尔线程GPU设备，并为此提供以下几种与vGPU类似的复用功能，包括：

***GPU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）来分配MLU，本组件会确保任务使用的显存不会超过分配数值、

***可限制分配的算力核组比例***: 你现在可以用算力核组数量（例如8个）来分配GPU，本组件会确保任务使用的显存不会超过分配数值

## 注意事项

1. 暂时不支持多卡切片，多卡任务只能分配整卡

2. 一个pod只能使用一个GPU生成的切片，即使该pod中有多个容器

3. 支持独占模式，只指定`mthreads.com/vgpu`即为独占申请

4. 本特性目前只支持MTT S4000设备

## 节点需求

* [MT CloudNative Toolkits > 1.9.0](https://docs.mthreads.com/cloud-native/cloud-native-doc-online/)
* 驱动版本 >= 1.2.0

## 开启GPU复用

* 部署'MT-CloudNative Toolkit'，摩尔线程的GPU共享需要配合厂家提供的'MT-CloudNative Toolkit'一起使用，请联系设备提供方获取

> **注意:** *（可选），部署完之后，卸载掉mt-mutating-webhook与mt-scheduler组件，因为这部分功能将由HAMi调度器提供*

* 在安装HAMi时配置'devices.mthreads.enabled = true'参数

```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag={your kubernetes version} --set device.mthreads.enabled=true -n kube-system
```

## 运行GPU任务

通过指定`mthreads.com/vgpu`, `mthreads.com/sgpu-memory` and `mthreads.com/sgpu-core`这3个参数，可以确定容器申请的切片个数，对应的显存和算力核组

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpushare-pod-default
spec:
  restartPolicy: OnFailure
  containers:
    - image: core.harbor.zlidc.mthreads.com:30003/mt-ai/lm-qy2:v17-mpc
      imagePullPolicy: IfNotPresent
      name: gpushare-pod-1
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          mthreads.com/vgpu: 1
          mthreads.com/sgpu-memory: 32
          mthreads.com/sgpu-core: 8
```

> **注意1:** *每一单位的sgpu-memory代表512M的显存.*

> **注意2:** *查看更多的[用例](../examples/mthreads/).*
