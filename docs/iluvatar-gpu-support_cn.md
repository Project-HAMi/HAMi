## 简介

本组件支持复用天数智芯GPU设备(MR-V100、BI-V150、BI-V100)，并为此提供以下几种与vGPU类似的复用功能，包括：

***GPU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如3000M）来分配GPU，本组件会确保任务使用的显存不会超过分配数值

***可限制分配的算力核组比例***: 你现在可以用算力比例（例如60%）来分配GPU，本组件会确保任务使用的显存不会超过分配数值

***设备 UUID 选择***: 你可以通过注解指定使用或排除特定的 GPU 设备

***方便易用***:  部署本组件后，只需要部署厂家提供的gpu-manager即可使用


## 节点需求

* Iluvatar gpu-manager (please consult your device provider)
* driver version > 3.1.0

## 开启GPU复用

* 部署'gpu-manager'，天数智芯的GPU共享需要配合厂家提供的'gpu-manager'一起使用，请联系设备提供方获取

> **注意:** *只需要安装gpu-manager，不要安装gpu-admission.*

* 在安装HAMi时配置设置devices.iluvatar.enabled=true
```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag={your kubernetes version} --set devices.iluvatar.enabled=true
```

* 部署'gpu-manager'之后，会根据GPU设备型号上报资源名称


> **说明:** 目前默认支持的GPU型号和资源名称在(https://github.com/Project-HAMi/HAMi/blob/master/charts/hami/templates/scheduler/device-configmap.yaml)定义：
```yaml
    iluvatars:
    - chipName: MR-V100
      commonWord: MR-V100
      resourceCountName: iluvatar.ai/MR-V100-vgpu
      resourceMemoryName: iluvatar.ai/MR-V100.vMem
      resourceCoreName: iluvatar.ai/MR-V100.vCore
    - chipName: MR-V50
      commonWord: MR-V50
      resourceCountName: iluvatar.ai/MR-V50-vgpu
      resourceMemoryName: iluvatar.ai/MR-V50.vMem
      resourceCoreName: iluvatar.ai/MR-V50.vCore
    - chipName: BI-V150
      commonWord: BI-V150
      resourceCountName: iluvatar.ai/BI-V150-vgpu
      resourceMemoryName: iluvatar.ai/BI-V150.vMem
      resourceCoreName: iluvatar.ai/BI-V150.vCore
    - chipName: BI-V100
      commonWord: BI-V100
      resourceCountName: iluvatar.ai/BI-V100-vgpu
      resourceMemoryName: iluvatar.ai/BI-V100.vMem
      resourceCoreName: iluvatar.ai/BI-V100.vCore
```

## 设备粒度切分

HAMi 将每个天数智芯 GPU 划分为 100 个单元进行资源分配。当你请求一部分 GPU 时，实际上是在请求这些单元中的一定数量。

### 内存分配

- 每个 `iluvatar.ai/<card-type>.vMem` 单位代表 256MB 的设备内存
- 如果不指定内存请求，系统将默认使用 100% 的可用内存
- 内存分配通过硬限制强制执行，确保任务不会超过其分配的内存

### 核心分配

- 每个 `iluvatar.ai/<card-type>.vCore` 单位代表 1% 的可用计算核心
- 核心分配通过硬限制强制执行，确保任务不会超过其分配的核心
- 当请求多个 GPU 时，系统会根据请求的 GPU 数量自动设置核心资源

## 运行GPU任务

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: BI-V150-poddemo
spec:
  restartPolicy: Never
  containers:
  - name: BI-V150-poddemo
    image: registry.iluvatar.com.cn:10443/saas/mr-bi150-4.3.0-x86-ubuntu22.04-py3.10-base-base:v1.0
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
        iluvatar.ai/BI-V150-vgpu: 1
        iluvatar.ai/BI-V150.vCore: 50
        iluvatar.ai/BI-V150.vMem: 64
      limits:
        iluvatar.ai/BI-V150-vgpu: 1
        iluvatar.ai/BI-V150.vCore: 50
        iluvatar.ai/BI-V150.vMem: 64
```

> **注意1:** *每一单位的vMem代表256M的显存.*

> **注意2:** *查看更多的[用例](../examples/iluvatar/).*

## 设备 UUID 选择

你可以通过 Pod 注解来指定要使用或排除特定的 GPU 设备：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
  annotations:
    # 使用特定的 GPU 设备（逗号分隔的列表）
    hami.io/use-<card-type>-uuid: "device-uuid-1,device-uuid-2"
    # 或者排除特定的 GPU 设备（逗号分隔的列表）
    hami.io/no-use-<card-type>-uuid: "device-uuid-1,device-uuid-2"
spec:
  # ... 其余 Pod 配置
```

### 查找设备 UUID

你可以使用以下命令查找节点上的天数智芯 GPU 设备 UUID：

```bash
kubectl get pod <pod-name> -o yaml | grep -A 10 "hami.io/<card-type>-devices-allocated"
```

或者通过检查节点注解：

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "hami.io/node-<card-type>-register"
```

在节点注解中查找包含设备信息的注解。


## 注意事项

1. 你需要在容器中进行如下的设置才能正常的使用共享功能
```sh
      set -ex
      echo "export LD_LIBRARY_PATH=/usr/local/corex/lib64:$LD_LIBRARY_PATH">> /root/.bashrc
      cp -f /usr/local/iluvatar/lib64/libcuda.* /usr/local/corex/lib64/
      cp -f /usr/local/iluvatar/lib64/libixml.* /usr/local/corex/lib64/
      source /root/.bashrc
```

2. 共享模式只对申请一张GPU的容器生效（iluvatar.ai/vgpu=1）。当请求多个 GPU 时，系统会根据请求的 GPU 数量自动设置核心资源。

3. `iluvatar.ai/<card-type>.vMem` 资源仅在 `iluvatar.ai/<card-type>-vgpu=1` 时有效。

4. 多设备请求（`iluvatar.ai/<card-type>-vgpu > 1`）不支持 vGPU 模式。