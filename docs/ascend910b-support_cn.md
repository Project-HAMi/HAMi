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

* 从HAMi项目中获取并安装[ascend-device-plugin](https://github.com/Project-HAMi/ascend-device-plugin/blob/master/build/ascendplugin-910-hami.yaml)，并进行部署

```
wget https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/master/build/ascendplugin-910-hami.yaml
kubectl apply -f ascendplugin-910-hami.yaml
```

## 自定义 NPU 虚拟化参数
HAMi 目前有一个 NPU 内置[虚拟化配置文件](https://github.com/Project-HAMi/HAMi/blob/master/charts/hami/templates/scheduler/device-configmap.yaml).

当然 HAMi 也支持通过以下方式自定义虚拟化参数:
<details>
  <summary>自定义配置</summary>

  ### 在 HAMi charts 创建 files 的目录，创建后的目录架构应为如下所示
  
  ```bash
  tree -L 1
  .
  ├── Chart.yaml
  ├── files
  ├── templates
  └── values.yaml
  ```

  ### 在 files 目录下创建 Create the ascend-config.yaml 文件，配置文件如下所示, 可以按需调整

  ```yaml
  vnpus:
- chipName: 910B
  commonWord: Ascend910A
  resourceName: huawei.com/Ascend910A
  resourceMemoryName: huawei.com/Ascend910A-memory
  memoryAllocatable: 32768
  memoryCapacity: 32768
  aiCore: 30
  templates:
    - name: vir02
      memory: 2184
      aiCore: 2
    - name: vir04
      memory: 4369
      aiCore: 4
    - name: vir08
      memory: 8738
      aiCore: 8
    - name: vir16
      memory: 17476
      aiCore: 16
- chipName: 910B3
  commonWord: Ascend910B
  resourceName: huawei.com/Ascend910B
  resourceMemoryName: huawei.com/Ascend910B-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_16g
      memory: 16384
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_32g
      memory: 32768
      aiCore: 10
      aiCPU: 3
- chipName: 310P3
  commonWord: Ascend310P
  resourceName: huawei.com/Ascend310P
  resourceMemoryName: huawei.com/Ascend310P-memory
  memoryAllocatable: 21527
  memoryCapacity: 24576
  aiCore: 8
  aiCPU: 7
  templates:
    - name: vir01
      memory: 3072
      aiCore: 1
      aiCPU: 1
    - name: vir02
      memory: 6144
      aiCore: 2
      aiCPU: 2
    - name: vir04
      memory: 12288
      aiCore: 4
      aiCPU: 4
  ```
  ### Helm 安装、更新将基于该配置文件，覆盖默认的配置文件
</details>


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
