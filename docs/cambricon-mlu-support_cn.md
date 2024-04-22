## 简介

本组件支持复用寒武纪MLU设备，并为此提供以下几种与vGPU类似的复用功能，包括：

***MLU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）来分配MLU，本组件会确保任务使用的显存不会超过分配数值

***可限制分配的算力大小***: 你现在可以用百分比来分配MLU的算力，本组件会确保任务使用的算力不会超过分配数值

***指定MLU型号***：当前任务可以通过设置annotation("cambricon.com/use-mlutype","cambricon.com/nouse-mlutype")的方式，来选择使用或者不使用某些具体型号的MLU

## 节点需求

* neuware-mlu370-driver > 5.10
* cntoolkit > 2.5.3

## 开启MLU复用

* 通过helm部署本组件, 参照[主文档中的开启vgpu支持章节](https://github.com/Project-HAMi/HAMi/blob/master/README_cn.md#kubernetes开启vgpu支持)

* 使用以下指令，为MLU节点打上label
```
kubectl label node {mlu-node} mlu=on
```

* 从您的设备提供商处获取cambricon-device-plugin，并配置以下两个参数：

`mode=dynamic-smlu`, `min-dsmlu-unit=256`

它们分别代表开启MLU复用功能，与设置最小可分配的内存单元为256M，您可以参考设备提供方的文档来获取更多的配置信息。

* 部署配置后的`cambricon-device-plugin`

```
kubectl apply -f cambricon-device-plugin-daemonset.yaml
```


## 运行MLU任务

```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: binpack-1
  labels:
    app: binpack-1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: binpack-1
  template:
    metadata:
      labels:
        app: binpack-1
    spec:
      containers:
        - name: c-1
          image: ubuntu:18.04
          command: ["sleep"]
          args: ["100000"]
          resources:
            limits:
              cambricon.com/vmlu: "1"
              cambricon.com/mlu.smlu.vmemory: "20"
              cambricon.com/mlu.smlu.vcore: "10"
```

## 注意事项

1. 在init container中无法使用MLU复用功能，否则该任务不会被调度

2. 只有申请单MLU的任务可以指定显存`mlu.smlu.vmemory`和算力`mlu.smlu.vcore`的数值，若申请的MLU数量大于1，则所有申请的MLU都会被整卡分配 
