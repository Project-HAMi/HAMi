# Global Config

you can customize your vGPU support by setting the following parameters using `-set`, for example

```
helm install vgpu-charts/vgpu vgpu --set devicePlugin.deviceMemoryScaling=5 ...
```

* `devicePlugin.service.schedulerPort:`
  Integer type, by default: 31998, scheduler webhook service nodePort.
* `devicePlugin.deviceMemoryScaling:` 
  Float type, by default: 1. The ratio for NVIDIA device memory scaling, can be greater than 1 (enable virtual device memory, experimental feature). For NVIDIA GPU with *M* memory, if we set `devicePlugin.deviceMemoryScaling` argument to *S*, vGPUs splitted by this GPU will totally get `S * M` memory in Kubernetes with our device plugin.
* `devicePlugin.deviceSplitCount:` 
  Integer type, by default: equals 10. Maximum tasks assigned to a simple GPU device.
* `devicePlugin.migstrategy:`
  String type, "none" for ignoring MIG features or "mixed" for allocating MIG device by seperate resources. Default "none"
* `devicePlugin.disablecorelimit:`
  String type, "true" for disable core limit, "false" for enable core limit, default: false
* `scheduler.defaultMem:` 
  Integer type, by default: 5000. The default device memory of the current task, in MB
* `scheduler.defaultCores:` 
  Integer type, by default: equals 0. Percentage of GPU cores reserved for the current task. If assigned to 0, it may fit in any GPU with enough device memory. If assigned to 100, it will use an entire GPU card exclusively.
* `scheduler.defaultGPUNum:`
  Integer type, by default: equals 1, if configuration value is 0, then the configuration value will not take effect and will be filtered. when a user does not set nvidia.com/gpu this key in pod resource, webhook should check nvidia.com/gpumem、resource-mem-percentage、nvidia.com/gpucores this three key, anyone a key having value, webhook should add nvidia.com/gpu key and this default value to resources limits map.
* `resourceName:`
  String type, vgpu number resource name, default: "nvidia.com/gpu"
* `resourceMem:`
  String type, vgpu memory size resource name, default: "nvidia.com/gpumem"
* `resourceMemPercentage:`
  String type, vgpu memory fraction resource name, default: "nvidia.com/gpumem-percentage" 
* `resourceCores:`
  String type, vgpu cores resource name, default: "nvidia.com/cores"
* `resourcePriority:`
  String type, vgpu task priority name, default: "nvidia.com/priority"

# Container config envs

* `GPU_CORE_UTILIZATION_POLICY:`
  String type, "default", "force", "disable"
  "default" means the dafault utilization policy
  "force" means the container will always limit the core utilization below "nvidia.com/gpucores"
  "disable" means the container will ignore the utilization limitation set by "nvidia.com/gpucores" during task execution

* `ACTIVE_OOM_KILLER:`
  String type, "true","false"
  "true" means the task may be killed if exceeds the limitation set by "nvidia.com/gpumem" or "nvidia.com/gpumemory"
  "false" means the task will not be killed even it exceeds the limitation.

  
