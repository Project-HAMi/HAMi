# Global Config

## Device Configs: ConfigMap

**Note:**
All the configurations listed below are managed within the hami-scheduler-device ConfigMap.
You can update these configurations using one of the following methods:

1. Directly edit the ConfigMap: If HAMi has already been successfully installed, you can manually update the hami-scheduler-device ConfigMap using the `kubectl edit` command to manually update the hami-scheduler-device ConfigMap.

    ```bash
    kubectl edit configmap hami-scheduler-device -n <namespace>
    ```

    After making changes, restart the related HAMi components to apply the updated configurations.

2. Modify Helm Chart: Update the corresponding values in the [ConfigMap](../charts/hami/templates/scheduler/device-configmap.yaml), then reapply the Helm Chart to regenerate the ConfigMap.

* `nvidia.deviceMemoryScaling`: 
  Float type, by default: 1. The ratio for NVIDIA device memory scaling, can be greater than 1 (enable virtual device memory, experimental feature). For NVIDIA GPU with *M* memory, if we set `nvidia.deviceMemoryScaling` argument to *S*, vGPUs splitted by this GPU will totally get `S * M` memory in Kubernetes with our device plugin.
* `nvidia.deviceSplitCount`: 
  Integer type, by default: equals 10. Maximum tasks assigned to a simple GPU device.
* `nvidia.migstrategy`: 
  String type, "none" for ignoring MIG features or "mixed" for allocating MIG device by seperate resources. Default "none"
* `nvidia.disablecorelimit`: 
  String type, "true" for disable core limit, "false" for enable core limit, default: false
* `nvidia.defaultMem`: 
  Integer type, by default: 0. The default device memory of the current task, in MB.'0' means use 100% device memory
* `nvidia.defaultCores`: 
  Integer type, by default: equals 0. Percentage of GPU cores reserved for the current task. If assigned to 0, it may fit in any GPU with enough device memory. If assigned to 100, it will use an entire GPU card exclusively.
* `nvidia.defaultGPUNum`: 
  Integer type, by default: equals 1, if configuration value is 0, then the configuration value will not take effect and will be filtered. when a user does not set nvidia.com/gpu this key in pod resource, webhook should check nvidia.com/gpumem、resource-mem-percentage、nvidia.com/gpucores this three key, anyone a key having value, webhook should add nvidia.com/gpu key and this default value to resources limits map.
* `nvidia.resourceCountName`: 
  String type, vgpu number resource name, default: "nvidia.com/gpu"
* `nvidia.resourceMemoryName`: 
  String type, vgpu memory size resource name, default: "nvidia.com/gpumem"
* `nvidia.resourceMemoryPercentageName`: 
  String type, vgpu memory fraction resource name, default: "nvidia.com/gpumem-percentage" 
* `nvidia.resourceCoreName`: 
  String type, vgpu cores resource name, default: "nvidia.com/gpucores"
* `nvidia.resourcePriorityName`: 
  String type, vgpu task priority name, default: "nvidia.com/priority"

## Chart Configs: parameters

you can customize your vGPU support by setting the following parameters using `-set`, for example

```bash
helm install hami hami-charts/hami --set devicePlugin.deviceMemoryScaling=5 ...
```

* `devicePlugin.service.schedulerPort`:
  Integer type, by default: 31998, scheduler webhook service nodePort.
* `scheduler.defaultSchedulerPolicy.nodeSchedulerPolicy`: String type, default value is "binpack", representing the GPU node scheduling policy. "binpack" means trying to allocate tasks to the same GPU node as much as possible, while "spread" means trying to allocate tasks to different GPU nodes as much as possible.
* `scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy`: String type, default value is "spread", representing the GPU scheduling policy. "binpack" means trying to allocate tasks to the same GPU as much as possible, while "spread" means trying to allocate tasks to different GPUs as much as possible.

## Pod configs: annotations

* `nvidia.com/use-gpuuuid`:

  String type, ie: "GPU-AAA,GPU-BBB"

  If set, devices allocated by this pod must be one of UUIDs defined in this string.

* `nvidia.com/nouse-gpuuuid`:

  String type, ie: "GPU-AAA,GPU-BBB"

  If set, devices allocated by this pod will NOT in UUIDs defined in this string.

* `nvidia.com/nouse-gputype`:

  String type, ie: "Tesla V100-PCIE-32GB, NVIDIA A10"

  If set, devices allocated by this pod will NOT in types defined in this string.

* `nvidia.com/use-gputype`:

  String type, ie: "Tesla V100-PCIE-32GB, NVIDIA A10"

  If set, devices allocated by this pod MUST be one of types defined in this string.

* `hami.io/node-scheduler-policy`:

  String type, "binpack" or "spread"

  - binpack: the scheduler will try to allocate the pod to used GPU nodes for execution. 
  - spread: the scheduler will try to allocate the pod to different GPU nodes for execution.

* `hami.io/gpu-scheduler-policy`:

  String type, "binpack" or "spread"

  - binpack: the scheduler will try to allocate the pod to the same GPU card for execution.
  - spread:the scheduler will try to allocate the pod to different GPU card for execution. 

* `nvidia.com/vgpu-mode`:

  String type, "hami-core" or "mig"

  Which type of vgpu instance this pod wish to use

## Container configs: env

* `GPU_CORE_UTILIZATION_POLICY`:
> Currently this parameter can be specified during `helm install` and then automatically injected into the container environment variables, through `--set devices.nvidia.gpuCorePolicy=force`

  String type, "default", "force", "disable"

  - default: "default"
  - "default" means the dafault utilization policy
  - "force" means the container will always limit the core utilization below "nvidia.com/gpucores"
  - "disable" means the container will ignore the utilization limitation set by "nvidia.com/gpucores" during task execution

* `CUDA_DISABLE_CONTROL`:

  Bool type, "true", "false"

  - default: false
  - "true" means the HAMi-core will not be used inside container, as a result, there will be no resource isolation and limitation in that container, only for debug. 

* `ACTIVE_OOM_KILLER`:
  
  Bool type, "true", "false"

  - default: false
  - "true" means there will be a daemon process which monitors all running tasks inside this container, and instantly kill any process which exceeds the limitation set by "nvidia.com/gpumem" or "nvidia.com/gpumemory"

* `CUDA_DISABLE_CONTROL`:

  Bool type, "true", "false"
  
  - default: false
  - "true" means the HAMi-core will not be used inside container, as a result, there will be no resource isolation and limitation in that container, only for debug.
