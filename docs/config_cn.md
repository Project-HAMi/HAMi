# 全局配置

## 设备配置

**注意:**
以下所有配置均在名称为 `hami-scheduler-device` 的 ConfigMap 中进行定义，你可以通过以下的方式进行更新：

1. 直接热更新 ConfigMap：成功部署 HAMi 后，你可以通过 `kubectl edit` 指令手动热更新 hami-scheduler 中的数据，如以下指令所示：

   ```bash
   kubectl edit configmap hami-scheduler-device -n <namespace>
   ```

   更新完毕后，需要重启 hami-Scheduler 和对应节点上的 hami-device-plugin

2. 修改 Helm Chart: 在[这里](../charts/hami/templates/scheduler/device-configmap.yaml)更新对应的字段，并进行部署

* `nvidia.deviceSplitCount`：
  整数类型，预设值是 10。GPU 的分割数，每一张 GPU 都不能分配超过其配置数目的任务。若其配置为 N 的话，每个 GPU 上最多可以同时存在 N 个任务。
* `nvidia.deviceMemoryScaling`：
  浮点数类型，预设值是 1。NVIDIA 装置显存使用比例，可以大于 1（启用虚拟显存，实验功能）。对于有 *M* 显存大小的 NVIDIA GPU，
  如果我们配置`nvidia.deviceMemoryScaling`参数为 *S*，在部署了我们装置插件的 Kubernetes 集群中，这张 GPU 分出的 vGPU 将总共包含 `S * M` 显存。
* `nvidia.migStrategy`：
  字符串类型，目前支持 "none“ 与 “mixed“ 两种工作方式，前者忽略 MIG 设备，后者使用专门的资源名称指定 MIG 设备，使用详情请参考 mix_example.yaml，默认为 "none"
* `nvidia.disablecorelimit`：
  字符串类型，"true" 为关闭算力限制，"false" 为启动算力限制，默认为 "false"
* `nvidia.defaultMem`：
  整数类型，预设值为 0，表示不配置显存时使用的默认显存大小，单位为 MB。当值为 0 时，代表使用全部的显存。
* `nvidia.defaultCores`：
  整数类型 (0-100)，默认为 0，表示默认为每个任务预留的百分比算力。若设置为 0，则代表任务可能会被分配到任一满足显存需求的 GPU 中，若设置为 100，代表该任务独享整张显卡
  说明：当容器仅声明 `nvidia.com/gpu` 且显存为独占场景（例如显式设置 `nvidia.com/gpumem-percentage: 100`，或显存字段都未配置且 `nvidia.defaultMem` 保持默认 0，从而回退为 100% 显存）并且未显式设置 `nvidia.com/gpucores` 时，HAMi 会在准入阶段将该容器的 `nvidia.com/gpucores` 默认为 100。对于非独占显存（如 `gpumem-percentage: 50`）或已经声明 `nvidia.com/gpucores` 的情况，不会自动调整。
* `nvidia.defaultGPUNum`：
  整数类型，默认为 1，如果配置为 0，则配置不会生效。当用户在 Pod 资源中没有设置 nvidia.com/gpu 这个 key 时，webhook 会检查 nvidia.com/gpumem、
  resource-mem-percentage、nvidia.com/gpucores 这三个 key 中的任何一个 key 有值，webhook 都会添加 nvidia.com/gpu 键和此默认值到 resources limit 中。
* `nvidia.memoryFactor`:
  整数类型，默认为 1。在资源申请时`nvidia.com/gpumem`的真实值会放大相应的倍数。如果部署了`mock-device-plugin`, 在`node.status.capacity`的真实值也会放大对应的倍数。
* `nvidia.resourceCountName`：
  字符串类型，申请 vgpu 个数的资源名，默认："nvidia.com/gpu"
* `nvidia.resourceMemoryName`：
  字符串类型，申请 vgpu 显存大小资源名，默认："nvidia.com/gpumem"
* `nvidia.resourceMemoryPercentageName`：
  字符串类型，申请 vgpu 显存比例资源名，默认："nvidia.com/gpumem-percentage"
* `nvidia.resourceCoreName`：
  字符串类型，申请 vgpu 算力资源名，默认："nvidia.com/gpucores"
* `nvidia.resourcePriorityName`：
  字符串类型，表示申请任务的任务优先级，默认："nvidia.com/priority"

## 节点配置

HAMi 允许为每个节点的 device plugin 配置不同的行为，只需要修改 configmap，如下:
```sh
kubectl -n <namespace> edit cm hami-device-plugin
```
* `name`: 要配置的节点名称。
* `operatingmode`: 节点的运行模式，可以是 "hami-core" 或者 "mig", 默认: "hami-core"。
* `devicememoryscaling`: 节点内存的超配率。
* `devicecorescaling`: 节点算力的超配率。
* `devicesplitcount`: 每个设备允许被分配的任务数。
* `filterdevices`: 节点上不被 HAMi 管理的设备。
  * `uuid`: 所要排除设备的 UUID。
  * `index`: 所要排除设备的索引。
  * 一个设备只要在 `uuid` 或者 `index` 列表中，就不会被 HAMi 管理。

## Chart 参数

你可以在安装过程中，通过 `-set` 来修改以下的客制化参数，例如：

1. 直接编辑 ConfigMap：如果 HAMi 已成功安装，你可以使用 `kubectl edit` 命令手动更新 hami-scheduler-device ConfigMap。

   ```bash
   kubectl edit configmap hami-scheduler-device -n <namespace>
   ```

   更改后，重启相关的 HAMi 组件以应用更新的配置。

2. 修改 Helm Chart：更新 [ConfigMap](../charts/hami/templates/scheduler/device-configmap.yaml)
   中相应的值，然后重新应用 Helm Chart 以重新生成 ConfigMap。

你可以在安装过程中，通过 `-set` 来修改以下的定制参数，例如：

```bash
helm install vgpu vgpu-charts/vgpu --set devicePlugin.deviceMemoryScaling=5 ...
```
* `devicePlugin.service.schedulerPort`: 整数类型, 预设值为31998, 调度器webhook服务的节点端口.
* `devicePlugin.deviceListStrategy`: 字符串类型, 预设值为 "envvar", 用于向容器暴露NVIDIA设备的策略。 "envvar" 表示使用 'NVIDIA_VISIBLE_DEVICES' 环境变量, "cdi-annotations" 表示使用容器设备接口 (CDI)。
* `devicePlugin.nvidiaDriverRoot`: 字符串类型。指定主机上NVIDIA驱动的根目录, 在 `deviceListStrategy` 为 "cdi-annotations" 时使用。如果未通过Helm设置, 则默认为 "/"。
* `devicePlugin.nvidiaHookPath`: 字符串类型。指定GPU节点上 `nvidia-ctk` 二进制文件的路径, 在 `deviceListStrategy` 为 "cdi-annotations" 时使用。如果未通过Helm设置, 则默认为 "/usr/bin/nvidia-ctk"。
* `scheduler.defaultSchedulerPolicy.nodeSchedulerPolicy`：字符串类型，预设值为 "binpack" 表示 GPU 节点调度策略，
  "binpack"表示尽量将任务分配到同一个 GPU 节点上，"spread"表示尽量将任务分配到不同 GPU 节点上。
* `scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy`：字符串类型，预设值为 "spread" 表示 GPU 调度策略，
  "binpack"表示尽量将任务分配到同一个 GPU 上，"spread"表示尽量将任务分配到不同 GPU 上。

**Webhook TLS 证书配置**

在 Kubernetes 中，为了让 API server 能够与 webhook 组件通信，webhook 需要一个 API server 信任的 TLS 证书。HAMi scheduler 提供了两种生成/配置所需 TLS 证书的方法。

* `scheduler.patch.enabled`：
  布尔类型，默认值为 true。如果设置为 true，helm 将使用 kube-webhook-certgen ([job-patch](../charts/hami/templates/scheduler/job-patch/job-createSecret.yaml)) 生成自签名证书并创建 secret。
* `scheduler.certManager.enabled`：
  布尔类型，默认值为 false。如果设置为 true，cert-manager 将生成自签名证书。**注意：此选项需要先在集群中安装 cert-manager。** _更多详情请参见 [cert-manager 安装说明](https://cert-manager.io/docs/installation/kubernetes/)。_


# Pod 配置（在注解中指定）

* `nvidia.com/use-gpuuuid`：

  字符串类型，如: "GPU-AAA,GPU-BBB"

  如果设置，该任务申请的设备只能是字符串中定义的设备之一。

* `nvidia.com/nouse-gpuuuid`：

  字符串类型，如: "GPU-AAA,GPU-BBB"

  如果设置，该任务不能使用字符串中定义的任何设备

* `nvidia.com/nouse-gputype`：

  字符串类型，如: "Tesla V100-PCIE-32GB，NVIDIA A10"

  如果设置，该任务不能使用字符串中定义的任何设备型号

* `nvidia.com/use-gputype`：

  字符串类型，如: "Tesla V100-PCIE-32GB，NVIDIA A10"

  如果设置，该任务申请的设备只能使用字符串中定义的设备型号。

* `hami.io/gpu-scheduler-policy`：

  字符串类型，"binpack" 或 "spread"

  - spread:，调度器会尽量将任务均匀地分配在不同 GPU 中
  - binpack: 调度器会尽量将任务分配在已分配的 GPU 中，从而减少碎片

* `hami.io/node-scheduler-policy`：

  字符串类型，"binpack" 或 "spread"

  - spread: 调度器会尽量将任务均匀地分配到不同节点上
  - binpack: 调度器会尽量将任务分配在已分配任务的节点上，从而减少碎片

* `nvidia.com/vgpu-mode`：

  字符串类型，"hami-core" 或 "mig"

  该任务希望使用的 vgpu 类型

## 容器配置（在容器的环境变量中指定）

* `GPU_CORE_UTILIZATION_POLICY` 
  > 当前这个参数可以在 `helm install` 的时候指定，然后自动注入到容器环境变量中, 通过 `--set devices.nvidia.gpuCorePolicy=force`

  字符串类型，"default"，"force"，"disable"

  - 默认为"default" 代表容器算力限制策略，"default" 为默认
  - "force" 为强制限制算力，一般用于测试算力限制的功能
  - "disable" 为忽略算力限制

* `CUDA_DISABLE_CONTROL`：

  布尔类型，"true"，"false"

  - 默认为 false
  - 若设置为 true，则代表屏蔽掉容器层的资源隔离机制，需要注意的是，这个参数只有在容器创建时指定才会生效，一般用于调试
