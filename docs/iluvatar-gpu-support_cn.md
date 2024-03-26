## 简介

本组件支持复用天数智芯GPU设备，并为此提供以下几种与vGPU类似的复用功能，包括：

***GPU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）来分配MLU，本组件会确保任务使用的显存不会超过分配数值，注意只有M100型号的M150支持可配显存

***可限制分配的算力核组比例***: 你现在可以用算力比例（例如60%）来分配GPU，本组件会确保任务使用的显存不会超过分配数值，注意只有M100型号的M150支持可配算力比例

***方便易用***:  部署本组件后，只需要部署厂家提供的gpu-manager即可使用


## 节点需求

* Iluvatar gpu-manager (please consult your device provider)
* driver version > 3.1.0

## 开启GPU复用

* 部署'gpu-manager'，天数智芯的GPU共享需要配合厂家提供的'gpu-manager'一起使用，请联系设备提供方获取

> **注意:** *只需要安装gpu-manager，不要安装gpu-admission.*

* 部署'gpu-manager'之后，你需要确认显存和核组对应的资源名称(例如 'iluvatar.ai/vcuda-core', 'iluvatar.ai/vcuda-memory')

* 在安装HAMi时配置'iluvatarResourceMem'和'iluvatarResourceCore'参数

```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag={your kubernetes version} --set iluvatarResourceMem=iluvatar.ai/vcuda-memory --set iluvatarResourceCore=iluvatar.ai/vcuda-core -n kube-system
```

## 运行GPU任务

```
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
spec:
  restartPolicy: Never
  containers:
  - name: poddemo
    image: harbor.4pd.io/vgpu/corex_transformers@sha256:36a01ec452e6ee63c7aa08bfa1fa16d469ad19cc1e6000cf120ada83e4ceec1e
    command: 
    - bash
    args:
    - -c
    - |
      set -ex
      echo "export LD_LIBRARY_PATH=/usr/local/corex/lib64:$LD_LIBRARY_PATH">> /root/.bashrc
      cp -f /usr/local/iluvatar/lib64/libcuda.* /usr/local/corex/lib64/
      cp -f /usr/local/iluvatar/lib64/libixml.* /usr/local/corex/lib64/
      source /root/.bashrc
      sleep 360000
    resources:
      requests:
        iluvatar.ai/vgpu: 1
        iluvatar.ai/vcuda-core: 50
        iluvatar.ai/vcuda-memory: 64
      limits:
        iluvatar.ai/vgpu: 1
        iluvatar.ai/vcuda-core: 50
        iluvatar.ai/vcuda-memory: 64
```

> **注意1:** *每一单位的vcuda-memory代表256M的显存.*

> **注意2:** *查看更多的[用例](../examples/iluvatar/).*

## 注意事项

1. 你需要在容器中进行如下的设置才能正常的使用共享功能
```
      set -ex
      echo "export LD_LIBRARY_PATH=/usr/local/corex/lib64:$LD_LIBRARY_PATH">> /root/.bashrc
      cp -f /usr/local/iluvatar/lib64/libcuda.* /usr/local/corex/lib64/
      cp -f /usr/local/iluvatar/lib64/libixml.* /usr/local/corex/lib64/
      source /root/.bashrc 
```

2. 共享模式只对申请一张GPU的容器生效（iluvatar.ai/vgpu=1）

   
