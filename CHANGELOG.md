# CHANGELOG

## v1.0.1

**Add MIG support:"mixed strategy"**

**Add support for kubernetes v1.22+**

## v1.0.1.1

**Bugs fixed**

a pod can be scheduled to a node where its core usage is 100 - Fixed

cudevshr.cache can't be modified with non-root users - Fixed

## v1.0.1.2

**Add custom resource name**

A task with cores=100 will allocate all device memory(virtual device memory excluded)

## v1.0.1.3

**nvidia.com/gpucores will limit the GPU utilization inside container**
Prior than v1.0.1.3, nvidia.com/gpucores will not limit utilization inside container, we have fixed it in v1.0.1.3

## v1.0.1.4

**Add nvidia.com/gpumem-percentage resoure name**
This resource indicates the device memory percentage of GPU, can not be used with "nvidia.com/gpumem". If you want an exclusive GPU, specify both the "nvidia.com/gpucores" and "nvidia.com/gpumem-percentage" to 100

**Add GPU type specification**
You can set "nvidia.com/use-gputype" annotation to specify which type of GPU to use. "nvidia.com/nouse-gputype" annotation to specify which type of GPU to avoid.

## v1.0.1.5

Fix an monitor "desc not found" error

Add "devicePlugin.sockPath" parameter to set the location of vgpu.sock



