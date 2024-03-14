# 全局配置

你可以在安装过程中，通过`-set`来修改以下的客制化参数，例如：

```
helm install vgpu vgpu-charts/vgpu --set devicePlugin.deviceMemoryScaling=5 ...
```

* `devicePlugin.deviceSplitCount:` 
  整数类型，预设值是10。GPU的分割数，每一张GPU都不能分配超过其配置数目的任务。若其配置为N的话，每个GPU上最多可以同时存在N个任务。
* `devicePlugin.deviceMemoryScaling:` 
  浮点数类型，预设值是1。NVIDIA装置显存使用比例，可以大于1（启用虚拟显存，实验功能）。对于有*M*显存大小的NVIDIA GPU，如果我们配置`devicePlugin.deviceMemoryScaling`参数为*S*，在部署了我们装置插件的Kubenetes集群中，这张GPU分出的vGPU将总共包含 `S * M` 显存。
* `devicePlugin.migStrategy:`
  字符串类型，目前支持"none“与“mixed“两种工作方式，前者忽略MIG设备，后者使用专门的资源名称指定MIG设备，使用详情请参考mix_example.yaml，默认为"none"
* `devicePlugin.disablecorelimit:`
  字符串类型，"true"为关闭算力限制，"false"为启动算力限制，默认为"false"
* `scheduler.defaultMem:`
  整数类型，预设值为5000，表示不配置显存时使用的默认显存大小，单位为MB
* `scheduler.defaultCores:`
  整数类型(0-100)，默认为0，表示默认为每个任务预留的百分比算力。若设置为0，则代表任务可能会被分配到任一满足显存需求的GPU中，若设置为100，代表该任务独享整张显卡
* `scheduler.defaultGPUNum:`
  整数类型，默认为1，如果配置为0，则配置不会生效。当用户在 pod 资源中没有设置 nvidia.com/gpu 这个 key 时，webhook 会检查 nvidia.com/gpumem、resource-mem-percentage、nvidia.com/gpucores 这三个 key 中的任何一个 key 有值，webhook 都会添加 nvidia.com/gpu 键和此默认值到 resources limit中。
* `resourceName:`
  字符串类型, 申请vgpu个数的资源名, 默认: "nvidia.com/gpu"
* `resourceMem:`
  字符串类型, 申请vgpu显存大小资源名, 默认: "nvidia.com/gpumem"
* `resourceMemPercentage:`
  字符串类型，申请vgpu显存比例资源名，默认: "nvidia.com/gpumem-percentage"
* `resourceCores:`
  字符串类型, 申请vgpu算力资源名, 默认: "nvidia.com/cores"
* `resourcePriority:`
  字符串类型，表示申请任务的任务优先级，默认: "nvidia.com/priority"

# 容器配置（在容器的环境变量中指定）

* `GPU_CORE_UTILIZATION_POLICY:`
  字符串类型，"default", "force", "disable"
  代表容器算力限制策略， "default"为默认，"force"为强制限制算力，一般用于测试算力限制的功能，"disable"为忽略算力限制
* `ACTIVE_OOM_KILLER:`
  字符串类型，"true", "false"
  代表容器是否会因为超用显存而被终止执行，"true"为会，"false"为不会