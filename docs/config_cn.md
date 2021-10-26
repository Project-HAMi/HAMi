# 配置

在这个deployments文件中, 你可以在 `vgpu/values.yaml` 文件的 `devicePlugin.extraArgs` 中使用以下的客制化参数：

* `device-split-count:` 
  整数类型，预设值是10。GPU的分割数，每一张GPU都不能分配超过其配置数目的任务。若其配置为N的话，每个GPU上最多可以同时存在N个任务。
* `device-memory-scaling:` 
  浮点数类型，预设值是1。NVIDIA装置显存使用比例，可以大于1（启用虚拟显存，实验功能）。对于有*M*显存大小的NVIDIA GPU，如果我们配置`device-memory-scaling`参数为*S*，在部署了我们装置插件的Kubenetes集群中，这张GPU分出的vGPU将总共包含 `S * M` 显存。

除此之外，你可以在 `vgpu/values.yaml` 文件的 `devicePlugin.extraArgs` 中使用以下客制化参数:

* `default-mem:`
  整数类型，预设值为5000，表示不配置显存时使用的默认显存大小，单位为MB

* `default-cores:`
  整数类型(0-100)，默认为0，表示默认为每个任务预留的百分比算力。若设置为0，则代表任务可能会被分配到任一满足显存需求的GPU中，若设置为100，代表该任务独享整张显卡