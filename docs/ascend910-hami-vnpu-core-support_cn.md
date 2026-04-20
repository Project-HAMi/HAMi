# huawei.com/Ascend910B、huawei.com/Ascend910C系列软切分支持简介

实现了基于 `libvnpu.so` 拦截和limiter令牌调度的软切分机制，能够实现精细化的资源共享。详细信息请参阅 [hami-vnpu-core](https://www.google.com/search?q=链接)。

* **_NPU 共享_**: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡；

* **_可限制分配的显存大小_**: 你可以用显存值（例如 3000M）来分配 NPU，本组件会确保任务使用的显存不会超过分配数值；

* **_可限制分配的算力大小_**: 你可以用固定数量来分配 NPU 的 AI 核心和 AI CPU 核心，本组件会确保任务使用的算力不会超过分配数值。

## 节点需求

* Ascend docker runtime
* driver version > 25.5
* Ascend device type: 910B3、910C

## 开启 NPU 复用

* 部署 [Ascend docker runtime](https://gitcode.com/Ascend/mind-cluster/tree/master/component/ascend-docker-runtime)

* 部署 [ascend-device-plugin](https://github.com/Project-HAMi/ascend-device-plugin)
* 芯片模式：在昇腾芯片上开启 `device-share` 模式以支持虚拟化。

## 宿主机环境准备

在启动任何容器之前，必须在宿主机上初始化 **全局共享内存 (SHM) 区域**，以便进行 Pod 间的协同。

1. **创建共享目录**

   ```
   sudo mkdir -p /usr/local/hami-shared-region
   sudo chmod 777 /usr/local/hami-shared-region
   ```

2. **部署 hami-vnpu-core 组件** 

   将以下文件放置在固定的宿主机路径（`/usr/local/hami-vnpu-core/`）中，以便挂载到容器内： 

   ```
   /usr/local/hami-vnpu-core/
   ├── limiter              # Manager daemon binary (compiled from hami-vnpu-core)
   ├── libvnpu.so           # Interception library for LD_PRELOAD
   └── ld.so.preload        # Global preload config 
   ```

  ### 在 files 目录下创建 device-config.yaml

  配置文件如下所示，可以按需调整：

  ```yaml
  vnpus:
  - chipName: 910B3
    commonWord: Ascend910B3
    resourceName: huawei.com/Ascend910B3
    resourceMemoryName: huawei.com/Ascend910B3-memory
    resourceCoreName: huawei.com/Ascend910B3-core
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
  - chipName: Ascend910
        commonWord: Ascend910C
        resourceName: huawei.com/Ascend910C
        resourceMemoryName: huawei.com/Ascend910C-memory
        resourceCoreName: huawei.com/Ascend910C-core
        memoryAllocatable: 65536
        memoryCapacity: 65536
        aiCore: 20
        aiCPU: 7
  ```

  ### Helm 安装和更新

  Helm 安装、更新将基于该配置文件，覆盖默认的配置文件

</details>

## 运行 NPU 任务

可通过使用 `huawei.com/ascend910Bx` 、 `huawei.com/ascend910Bx-memory` 和 `huawei.com/ascend910Bx-core`资源类型，来请求 Ascend 910B 系列设备：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-soft-slice-pod
  annotations:
    huawei.com/vnpu-mode: 'hami-core' # 添加该注解的走hami-vnpu-core软切分
spec:
  containers:
    - name: npu_pod
      ...
      resources:
        limits:
          huawei.com/Ascend910B3: "1"          # 请求 1 块物理 NPU
          huawei.com/Ascend910B3-memory: "28672" # 请求 28Gi 显存
          huawei.com/Ascend910B3-core: "40"      # 请求 40% 的算力
```

