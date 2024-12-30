# 全局配置

**注意：**
以下列出的所有配置均在 hami-scheduler-device ConfigMap 中进行管理。
你可以通过以下方法之一更新这些配置：

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

- `devicePlugin.deviceSplitCount`：
  整数类型，预设值是 10。GPU 的分割数，每一张 GPU 都不能分配超过其配置数目的任务。若其配置为 N 的话，每个 GPU 上最多可以同时存在 N 个任务。
- `devicePlugin.deviceMemoryScaling`：
  浮点数类型，预设值是 1。NVIDIA 装置显存使用比例，可以大于 1（启用虚拟显存，实验功能）。对于有 *M* 显存大小的 NVIDIA GPU，
  如果我们配置 `devicePlugin.deviceMemoryScaling` 参数为 *S*，在部署了我们装置插件的 Kubenetes 集群中，
  这张 GPU 分出的 vGPU 将总共包含 `S * M` 显存。
- `devicePlugin.migStrategy`：
  字符串类型，目前支持 "none“ 与 “mixed“ 两种工作方式，前者忽略 MIG 设备，后者使用专门的资源名称指定 MIG 设备，
  使用详情请参考 mix_example.yaml，默认为"none"
- `devicePlugin.disablecorelimit`：
  字符串类型，"true" 为关闭算力限制，"false" 为启动算力限制，默认为 "false"
- `scheduler.defaultMem`：
  整数类型，预设值为 0，表示不配置显存时使用的默认显存大小，单位为 MB。当值为 0 时，代表使用全部的显存。
- `scheduler.defaultCores`：
  整数类型(0-100)，默认为 0，表示默认为每个任务预留的百分比算力。若设置为 0，
  则代表任务可能会被分配到任一满足显存需求的 GPU 中，若设置为 100，代表该任务独享整张显卡
- `scheduler.defaultGPUNum`：
  整数类型，默认为 1，如果配置为 0，则配置不会生效。当用户在 Pod 资源中没有设置 nvidia.com/gpu 这个 key 时，
  webhook 会检查 nvidia.com/gpumem、resource-mem-percentage、nvidia.com/gpucores 这三个 key 中的任何一个 key 有值，
  webhook 都会添加 nvidia.com/gpu 键和此默认值到 resources limit 中。
- `scheduler.defaultSchedulerPolicy.nodeSchedulerPolicy`：字符串类型，预设值为 "binpack"
  表示 GPU 节点调度策略，"binpack" 表示尽量将任务分配到同一个 GPU 节点上，"spread" 表示尽量将任务分配到不同 GPU 节点上。
- `scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy`：字符串类型，预设值为 "spread" 表示 GPU 调度策略，
  "binpack" 表示尽量将任务分配到同一个 GPU 上，"spread" 表示尽量将任务分配到不同 GPU 上。
- `resourceName`：
  字符串类型，申请 vgpu 个数的资源名，默认："nvidia.com/gpu"
- `resourceMem`：
  字符串类型，申请 vgpu 显存大小资源名，默认："nvidia.com/gpumem"
- `resourceMemPercentage`：
  字符串类型，申请 vgpu 显存比例资源名，默认："nvidia.com/gpumem-percentage"
- `resourceCores`：
  字符串类型，申请 vgpu 算力资源名，默认："nvidia.com/cores"
- `resourcePriority`：
  字符串类型，表示申请任务的任务优先级，默认："nvidia.com/priority"

## 容器配置（在容器的环境变量中指定）

- `GPU_CORE_UTILIZATION_POLICY`：

  字符串类型，"default"，"force"，"disable"

  - 默认为"default" 代表容器算力限制策略，"default" 为默认
  - "force" 为强制限制算力，一般用于测试算力限制的功能
  - "disable" 为忽略算力限制

- `ACTIVE_OOM_KILLER`：

  布尔类型，"true"，"false"

  - 默认为 false
  - 若设置为 true，则代表监控系统将会持续监控进程的显存使用量，并主动 kill 掉任何用超配额的进行

- `CUDA_DISABLE_CONTROL`：

  布尔类型，"true"，"false"

  - 默认为 false
  - 若设置为 true，则代表屏蔽掉容器层的资源隔离机制，需要注意的是，这个参数只有在容器创建时指定才会生效，一般用于调试
