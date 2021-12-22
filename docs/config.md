# Config

you can customize your vGPU support by setting the following parameters using `-set`, for example

```
helm install vgpu-charts/vgpu vgpu --set devicePlugin.deviceMemoryScaling=5 ...
```

* `devicePlugin.deviceMemoryScaling:` 
  Float type, by default: 1. The ratio for NVIDIA device memory scaling, can be greater than 1 (enable virtual device memory, experimental feature). For NVIDIA GPU with *M* memory, if we set `devicePlugin.deviceMemoryScaling` argument to *S*, vGPUs splitted by this GPU will totally get `S * M` memory in Kubernetes with our device plugin.
* `devicePlugin.deviceSplitCount:` 
  Integer type, by default: equals 10. Maximum tasks assigned to a simple GPU device.
* `scheduler.defaultMem:` 
  Integer type, by default: 5000. The default device memory of the current task, in MB
* `scheduler.defaultCores:` 
  Integer type, by default: equals 0. Percentage of GPU cores reserved for the current task. If assigned to 0, it may fit in any GPU with enough device memory. If assigned to 100, it will use an entire GPU card exclusively.