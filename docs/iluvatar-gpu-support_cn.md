## 简介

本组件支持复用天数智芯GPU设备(MR-V100、BI-V150、BI-V100)，并为此提供以下几种与vGPU类似的复用功能，包括：

**_GPU 共享_**: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

**_可限制分配的显存大小_**: 你现在可以用显存值（例如3000M）来分配GPU，本组件会确保任务使用的显存不会超过分配数值

**_可限制分配的算力核组比例_**: 你现在可以用算力比例（例如60%）来分配GPU，本组件会确保任务使用的显存不会超过分配数值

**_设备 UUID 选择_**: 你可以通过注解指定使用或排除特定的 GPU 设备

**_方便易用_**: 部署本组件后，只需要部署厂家提供的gpu-manager即可使用

## 节点需求

- Iluvatar gpu-manager (please consult your device provider)
- driver version > 3.1.0
- iluvatar container toolkit (please consult your device provider)

## 开启GPU复用

- 部署'gpu-manager'，天数智芯的GPU共享需要配合厂家提供的'gpu-manager'一起使用，请联系设备提供方获取

> **注意:** _只需要安装gpu-manager，不要安装gpu-admission._

- 部署'gpu-manager'之后，你需要确认显存和核组对应的资源名称(例如 'iluvatar.ai/vcuda-core', 'iluvatar.ai/vcuda-memory')

- 在安装HAMi时默认已配置'iluvatarResourceMem'和'iluvatarResourceCore'参数，如有新设备可按规则添加

```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag={your kubernetes version}  -n kube-system
```

> **说明:** 默认资源名称如下：

```
iluvatar:
- chipName: Iluvatar MR-V100
  commonWord: mrv100
  resourceName: iluvatar.ai/mrv100-vgpu
  resourceMemoryName: iluvatar.ai/mrv100-memory
  resourceCoreName: iluvatar.ai/mrv100-core
- chipName: Iluvatar MR-V50
  commonWord: mrv50
  resourceName: iluvatar.ai/mrv50-vgpu
  resourceMemoryName: iluvatar.ai/mrv50-memory
  resourceCoreName: iluvatar.ai/mrv50-core
- chipName: Iluvatar BI-V150
  commonWord: biv150
  resourceName: iluvatar.ai/biv150-vgpu
  resourceMemoryName: iluvatar.ai/biv150-memory
  resourceCoreName: iluvatar.ai/biv150-core
- chipName: Iluvatar BI-V100
  commonWord: biv100
  resourceName: iluvatar.ai/biv100-vgpu
  resourceMemoryName: iluvatar.ai/biv100-memory
  resourceCoreName: iluvatar.ai/biv100-core
```

> - `chipName` GPU具体型号（Model），可通过下面cmd查看

```
# iluvatar-container-cli -k info
NVRM version:   4.2.0
CUDA version:   10.2

Device Index:   0
Device Minor:   0
Model:          Iluvatar MR-V100
Brand:          Iluvatar
GPU UUID:       GPU-bad51c5a-ed4c-591d-91bf-c04a12e19eae
Bus Location:   00000000:c1:00.0
Architecture:   7.1

...

Device Index:   3
Device Minor:   3
Model:          Iluvatar MR-V50
Brand:          Iluvatar
GPU UUID:       GPU-9b900f73-728f-4404-99dc-a9ec0ae5fd2f
Bus Location:   00000000:82:00.0
Architecture:   7.1
...
```

> - `iluvatar.ai/<commonWord>-vgpu` 用于 GPU 数量
> - `iluvatar.ai/<commonWord>-memory` 用于内存分配
> - `iluvatar.ai/<commonWord>-core` 用于核心分配
>
> 你可以通过上述参数自定义这些名称。

## 设备粒度切分

HAMi 将每个天数智芯 GPU 划分为 100 个单元进行资源分配。当你请求一部分 GPU 时，实际上是在请求这些单元中的一定数量。

### 内存分配

- 每个 `iluvatar.ai/<commonWord>-memory` 单位代表 256MB 的设备内存
- 如果不指定内存请求，系统将默认使用 100% 的可用内存
- 内存分配通过硬限制强制执行，确保任务不会超过其分配的内存

### 核心分配

- 每个 `iluvatar.ai/<commonWord>-core` 单位代表 1% 的可用计算核心
- 核心分配通过硬限制强制执行，确保任务不会超过其分配的核心
- 当请求多个 GPU 时，系统会根据请求的 GPU 数量自动设置核心资源
- 不设置，默认core为0，即：用满物理GPU卡sm

## Iluvatar GPU 资源调度说明

以commonWord mrv100为例子

### `iluvatar.ai/mrv100-vgpu` (虚拟 GPU)

- **调度必要性**：Webhook 需要查看 `iluvatar.ai/*-vgpu` 这个 key，才能转发给 hami-scheduler。
- **限制**：Pod 中 vGPU 数目不能大于物理设备数目。
- **默认分配**：一张物理卡分为两份 vGPU。即：`iluvatar.ai/mrv100-vgpu` 总数是 2。
- **资源分配规则**：
  - `iluvatar.ai/mrv100-vgpu > 1`：
    - 分配一整张物理卡。
    - 节点上 vCore 的计算方式：`vgpu * 100`。
  - `iluvatar.ai/mrv100-vgpu = 1`：
    - 无操作，透传 vCore 和 vMem。

### `iluvatar.ai/mrv100-memory` (虚拟显存)

- **单位**：一整张物理卡的显存按照 256MB 拆分。
- **默认行为**：不设置时，默认占满整张物理卡对应的 vMem。
  - 例如：32G 的 MR100，对应 128 份（32G / 256MB = 128），不设置 vMemory 的话，当前 Pod 会占满 128 份（32G）。

### `iluvatar.ai/mrv100-core` (虚拟计算核心)

- **调度必要性**：非必要。
- **单位**：一整张物理卡对应 100 core。
- **默认行为**：不设置时，默认 vCore = 0。
  - 对 SM（Streaming Multiprocessor）没有限制。
  - `CUDA_DEVICE_SM_LIMIT=0`，即在 劫持库 中的 `cuKernelLauncher` 中不进行 sleep。
- **vCore 设置说明**：
  - `vCore = 0`（默认）：
    - 只要 vMem 合法，则能一直创建 Pod。
  - `vCore = 100`：
    - 表示本 vGPU 占满整张物理卡的 SM，此时本物理卡不能再分配给其他 Pod。
  - `vCore = 50`：
    - 指定为 50，表示本 vGPU 只使用一半的 core。
- **设置含义**：
  - 若设置为 0，则代表任务可能会被分配到任一满足显存需求的 GPU 中。
  - 若设置为 100，代表该任务独享整张显卡。

## 运行GPU任务

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ubuntu-ix183
  annotations:
#    hami.io/use-mrv100-uuid: "GPU-bad51c5a-ed4c-591d-91bf-c04a12e19eae"
spec:
  runtimeClassName: iluvatar
  containers:
    - name: ubuntu
      image: corex:4.2.0
      command:
        - sleep
        - "infinity"
      env:
        - name: NVIDIA_REQUIRE_CUDA
          value: "cuda=10.2"
      resources:
        limits:
          iluvatar.ai/mrv100-vgpu: 1
```

```yaml
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

> **注意1:** _每一单位的vcuda-memory代表256M的显存._

> **注意2:** _查看更多的[用例](../examples/iluvatar/)._

## 设备 UUID 选择

你可以通过 Pod 注解来指定要使用或排除特定的 GPU 设备：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
  annotations:
    # 使用特定的 GPU 设备（逗号分隔的列表）
    iluvatar.ai/use-gpuuuid: "node1-iluvatar-0,node1-iluvatar-1"
    # 或者排除特定的 GPU 设备（逗号分隔的列表）
    iluvatar.ai/nouse-gpuuuid: "node1-iluvatar-2,node1-iluvatar-3"
spec:
  # ... 其余 Pod 配置
```

> **说明:** 设备 ID 格式为 `{节点名称}-iluvatar-{索引}`。你可以在节点状态中找到可用的设备 ID。

### 查找设备 UUID

你可以使用以下命令查找节点上的天数智芯 GPU 设备 UUID：

```bash
kubectl get pod <pod-name> -o yaml | grep -A 10 "hami.io/<card-type>-devices-allocated"
```

或者通过检查节点注解：

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "hami.io/node-register-<card-type>"
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

3. `iluvatar.ai/vcuda-memory` 资源仅在 `iluvatar.ai/vgpu=1` 时有效。

4. 多设备请求（`iluvatar.ai/vgpu > 1`）不支持 vGPU 模式。
