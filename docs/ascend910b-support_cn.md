# huawei.com/Ascend910A、Ascend910B系列 和 Ascend310P 支持简介

HAMi 支持复用华为昇腾 910A、910B 系列设备（910B、910B2、910B3、910B4）和 310P 设备，并为此提供以下几种与 vGPU 类似的复用功能，包括：

* **_NPU 共享_**: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡；

* **_可限制分配的显存大小_**: 你可以用显存值（例如 3000M）来分配 NPU，本组件会确保任务使用的显存不会超过分配数值；

* **_可限制分配的算力大小_**: 你可以用固定数量来分配 NPU 的 AI 核心和 AI CPU 核心，本组件会确保任务使用的算力不会超过分配数值。

## 节点需求

* Ascend docker runtime
* driver version > 24.1.rc1
* Ascend device type: 910B,910B2,910B3,910B4,310P

## 开启 NPU 复用

* 通过 helm 部署本组件, 参照[主文档中的开启 vGPU 支持章节](https://github.com/Project-HAMi/HAMi/blob/master/README_cn.md#kubernetes开启vgpu支持)。

* 使用以下指令，为 Ascend 910B 所在节点打上 label：

```bash
kubectl label node {ascend-node} accelerator=huawei-Ascend910
```

* 部署[Ascend docker runtime](https://gitcode.com/Ascend/mind-cluster/tree/master/component/ascend-docker-runtime)

* 部署 [ascend-device-plugin](https://github.com/Project-HAMi/ascend-device-plugin)

## 自定义 NPU 虚拟化参数

HAMi 目前有一个 NPU 内置[虚拟化配置文件](https://github.com/Project-HAMi/HAMi/blob/master/charts/hami/templates/scheduler/device-configmap.yaml).

当然 HAMi 也支持通过以下方式自定义虚拟化参数:

<details>
  <summary>自定义配置</summary>

  ### 在 HAMi charts 创建 files 的目录

  创建后的目录架构应为如下所示：

  ```bash
  tree -L 1
  .
  ├── Chart.yaml
  ├── files
  ├── templates
  └── values.yaml
  ```

  ### 在 files 目录下创建 device-config.yaml

  配置文件如下所示，可以按需调整：

  ```yaml
  vnpus:
  - chipName: 910A
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
  - chipName: 910B2
    commonWord: Ascend910B2
    resourceName: huawei.com/Ascend910B2
    resourceMemoryName: huawei.com/Ascend910B2-memory
    memoryAllocatable: 65536
    memoryCapacity: 65536
    aiCore: 24
    aiCPU: 6
    templates:
      - name: vir03_1c_8g
        memory: 8192
        aiCore: 3
        aiCPU: 1
      - name: vir06_1c_16g
        memory: 16384
        aiCore: 6
        aiCPU: 1
      - name: vir12_3c_32g
        memory: 32768
        aiCore: 12
        aiCPU: 3  
  - chipName: 910B3
    commonWord: Ascend910B3
    resourceName: huawei.com/Ascend910B3
    resourceMemoryName: huawei.com/Ascend910B3-memory
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
  - chipName: 910B4
    commonWord: Ascend910B4
    resourceName: huawei.com/Ascend910B4
    resourceMemoryName: huawei.com/Ascend910B4-memory
    memoryAllocatable: 32768
    memoryCapacity: 32768
    aiCore: 20
    aiCPU: 7
    templates:
      - name: vir05_1c_8g
        memory: 8192
        aiCore: 5
        aiCPU: 1
      - name: vir10_3c_16g
        memory: 16384
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

  ### Helm 安装和更新

  Helm 安装、更新将基于该配置文件，覆盖默认的配置文件

</details>

## 虚拟化模板说明

HAMi 支持通过预定义的设备模板来配置 NPU 资源分配。每个模板包含以下内容：

- 模板名称（name）：模板的唯一标识符；
- 内存大小（memory）：分配给该模板的设备内存大小（单位：MB）；
- AI 核心数量（aiCore）：分配给该模板的 AI 核心数量；
- AI CPU 核心数量（aiCPU）：分配给该模板的 AI CPU 核心数量（部分型号支持）。

当用户请求特定大小的内存时，系统会自动将请求的内存大小对齐到最接近的模板大小。例如，如果用户请求 2000MB 内存，系统会选择内存大小大于或等于 2000MB 的最小模板。

具体配置，请参考：[昇腾官方的虚拟化模板](https://www.hiascend.com/document/detail/zh/computepoweralloca/300/cpaug/cpaug/cpaug_00005.html)

## 设备粒度切分

具体参考每个类型配置（chipName）中的aiCore 和 template 下的 aiCore 配比。

### Ascend910 系列设备粒度切分

- Ascend910A 设备支持 4 种粒度的切分，分别是 1/15、2/15 卡、 4/15 卡和 8/15 卡，分配的显存会自动对齐到在分配额之上最近的粒度上。
- Ascend910B2 设备支持 3 种粒度的切分，分别是 1/8、1/4 卡和 1/2 卡，分配的显存会自动对齐到在分配额之上最近的粒度上。
- Ascend910B3 和 Ascend910B4 设备支持 2 种粒度的切分，分别是 1/4 卡和 1/2 卡，分配的显存会自动对齐到在分配额之上最近的粒度上。

### Ascend310P 设备粒度切分

Ascend310P 设备（Atlas 推理系列产品）支持多种粒度的切分，包括 1/8 卡、1/4 卡和 1/2 卡，分配的显存会自动对齐到在分配额之上最近的粒度上。

## 运行 NPU 任务

可通过使用 `huawei.com/ascend910Bx` 和 `huawei.com/ascend910Bx-memory` 资源类型，来请求 Ascend 910B 系列设备：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend910A-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910A: 1 # requesting 1 vGPUs
          huawei.com/Ascend910A-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend910B2-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B2: 1 # requesting 1 vGPUs
          huawei.com/Ascend910B2-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend910B3-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B3: 1 # requesting 1 vGPUs
          huawei.com/Ascend910B3-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend910B4-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B4: 1 # requesting 1 vGPUs
          huawei.com/Ascend910B4-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend310P-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend310P: 1 # requesting 1 vGPUs
          huawei.com/Ascend310P-memory: 2000 # requesting 2000m device memory
```

## 设备健康检查

HAMi 支持对 Ascend NPU 设备进行健康检查，确保只有健康的设备被分配给 Pod。健康检查包括以下内容：

- 设备状态检查；
- 设备资源可用性检查；
- 设备驱动状态检查。

## 资源使用统计

HAMi 支持对 Ascend NPU 设备的资源使用情况进行统计，包括：

- 设备内存使用情况；
- AI 核心使用情况；
- AI CPU 核心使用情况；
- 设备利用率。

这些统计信息可以用于资源调度决策和性能优化。

## 节点锁定机制

HAMi 实现了节点锁定机制，确保在分配设备资源时不会发生冲突。当 Pod 请求 Ascend NPU 资源时，系统会锁定相应的节点，防止其他 Pod 同时使用相同的设备资源。


## 设备 UUID 选择

你可以通过 Pod 注解来指定要使用或排除特定的 Ascend 设备：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-pod
  annotations:
    # 使用特定的 Ascend 设备（逗号分隔的列表）
    hami.io/use-Ascend910B-uuid: "device-uuid-1,device-uuid-2"
    # 或者排除特定的 Ascend 设备（逗号分隔的列表）
    hami.io/no-use-Ascend910B-uuid: "device-uuid-3,device-uuid-4"
spec:
  # ... 其余 Pod 配置
```

> **注意**：设备 UUID 格式取决于设备类型，例如 `Ascend910B`、`Ascend910B2`、`Ascend910B3`、`Ascend910B4` 等。你可以在节点状态中找到可用的设备 UUID。



### 使用示例

以下是一个完整的示例，展示如何使用 UUID 选择功能：

<details>
  <summary>自定义配置</summary>

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-pod
  annotations:
    hami.io/use-Ascend910B-uuid: "device-uuid-1,device-uuid-2"
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B3: 1
          huawei.com/Ascend910B3-memory: 2000
```

在这个示例中，Pod 将只在 UUID 为 `device-uuid-1` 或 `device-uuid-2` 的 Ascend910B3 设备上运行。

#### 查找设备 UUID

你可以通过以下命令查找节点上的 Ascend 设备 UUID：

```bash
kubectl describe node <node-name> | grep -A 10 "Allocated resources"
```

或者使用以下命令查看节点的注解：

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "annotations:"
```

在节点注解中，查找 `hami.io/node-register-Ascend910B` 或类似的注解，其中包含设备 UUID 信息。

</details>

## 注意事项

- 在 init container 中无法使用 NPU 复用功能；
- `huawei.com/Ascend910Bx-memory` 仅在 `huawei.com/Ascend910Bx=1` 时有效；
- 多设备请求（`huawei.com/Ascend910Bx > 1`）不支持 vNPU 模式。