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

**Add nvidia.com/gpumem-percentage resource name**
This resource indicates the device memory percentage of GPU, can not be used with "nvidia.com/gpumem". If you want an exclusive GPU, specify both the "nvidia.com/gpucores" and "nvidia.com/gpumem-percentage" to 100

**Add GPU type specification**
You can set "nvidia.com/use-gputype" annotation to specify which type of GPU to use. "nvidia.com/nouse-gputype" annotation to specify which type of GPU to avoid.

## v1.0.1.5

Fix an monitor "desc not found" error

Add "devicePlugin.sockPath" parameter to set the location of vgpu.sock

## v1.1.0.0

**Major Update: Device Memory will be counted more accurately**
several device memory usage, including cuda context, modules, parameters, reserved addresses will be counted in v1.1.0.0

**Update to be compatible with CUDA 11.6 and Driver 500+**

**Rework monitor strategy**
Monitor will mmap control file into address space instead of reading it in each query.

## v1.1.1.0

**Fix segmentation fault when invoking cuMallocAsync**

**Core Utilization Oversubscribe and priority-base scheduling**
Currently we have two priority, 0 for high and 1 for low. The core utilization of high priority task won't be limited to resourceCores unless sharing GPU node with other high priority tasks.
The core utilization of low priority task won't be limited to resourceCores if no other tasks sharing its GPU.
See examples/nvidia/example.yaml for more details

**Add Container Core Utilization policy**
See details in [Configure HAMi](https://project-hami.io/docs/userguide/configure)

## v2.2

**Update device memory counting mechanism to compat with CUDA 11.3+ task**
sometimes vgpu-scheduler won't able to collect device memory usage when running cuda11.3+ compiled tasks in v1.x version of vgpu-scheduler. We solve this problem by reworking device memory counting mechanism.

**Use node annotation instead of grpc to communicate between scheduler and device-plugin**
In v1.x version of vgpu-scheduler, we use grpc to communicate between scheduler and device-plugin, but we reimplement this communication in v2.x by using node annotation, to make it more stable and readable.

**modified nvidia-container-runtime is no longer needed**
We remove self-modified nvidia-container-runtime in v1.x, because we now use node lock to track pod and container information. so this nvidia-container-runtime is no longer needed.

## v2.2.7

**BUG fix**

fix tasks with "gpumem-percentage" not working properly

fix dead lock when a process die with its lock not released

**Adjust certain logs**

**update go modules to more recent version in order to support k8s v1.25**

## v2.2.8

**BUG fix**

fix vGPUmonitor not working properly with containerd

fix installation error on k8s v1.25+

## v2.2.9

**BUG fix**

fix non-root user in container can't access /tmp/vgpulock, result in "unified lock error"

**Rework device registration**

device registration used to be done in gRpc between device-plugin and scheduler. However, in some cluster, this communication may be blocked by firewall or selinux configuration. So, we reimplement device registration mechanism by using node annotations:
A-device-plugin will put its usable device and its status in "Node-A-device-register" annotation
scheduler will read from this annotation and acknowledge this registration. So, gRpc will no longer be used.

**Optimization in code**

Put nvidia-device-plugin related code in a separate directory "nvidiadevice"

**Libvgpu log adjusting**

Downgrade the following API from LOG:WARN to LOG:INFO
cuFuncSetCacheConfig, cuFuncSetCacheConfig ,cuModuleGetTexRef, cuModuleGetSurfRef

## v2.2.10

**BUG fix**

fix process can't initialize properly in driver 440

fix cuCtxCreate failed in some tensorRT task

fix env CUDA_VISIBLE_DEVICES not working properly sometimes.

## v2.2.11

**BUG fix**

fix initialization failed with non-root users

fix core limitation not working properly on A30

## v2.2.12

Downgrade core control output from LOG:WARN to LOG:DEBUG

## v2.2.13

Adjust default memory to 0, which means use 100% device memory

Move cache file directory from /tmp/vgpu/containers to /usr/local/vgpu/containers

## v2.2.14

Fix device memory calculation error after container crashloop

Fix env cuda_oversubscribe not set properly when MemoryScaling < 1

Fix MemoryScaling not working when set < 1

## v2.2.15

Move shared-memory from from /tmp/xxx.cache to /usr/local/vgpu/xxx.cache inside container

Add Deviceidx to scheduler monitor apis(31993)

## v2.2.16

Fix crash during initlization in vGPUmonitor

# v2.3

## v2.3.0

Fix oom can't be triggered when loading module

Update device-plugin version to v0.14.0

## v2.3.1

Fix a bug where a cuda process can't be launched properly

## v2.3.2

Remove node selector for scheduler

Fix an issue where mlu device-plugin can't be launched properly

Major rework on devices-related code

Add support for hygon DCU device

## v2.3.3

Fix an issue where pod pending on nodes with multi-architect devices.

## v2.3.4

Fix an issue where 31993 port can't list all GPU nodes

Add a switch on cuda_control by set env "CUDA_DISABLE_ENV=true" in container

## v2.3.6

Fix initialization error when using ray

## v2.3.7

Fix error when "N/A" is shown in command "nvidia-smi topo -m"
Fix core utilization not working on some cases
Adjust some documents

## v2.3.8

Fix device-plugin launch error on driver version < 500

support manual config MutatingWebhookConfiguration failurePolicy

add metrics bind address flag for scheduler

Improved log messages

fix: loss of metrics after vdeivce restart

bugfix: device-plugin monitor serves too slowly in big cluster

## v2.3.9

Add support for iluvatar GPU devices

Fix issue on "get_host_pid" func in HAMi-core

Regular devices API, make it easier to add new devices

## v2.3.10

Fix issue where device-plugin failed to start

## v2.3.11

Add support for Ascend910B3 device

Add "NVIDIA_VISIBLE_DEVICES=none" to none-gpu tasks

## v2.4.0 - 2024-09-29

**New features**
- Add support for Ascend 910P device
- Support multiple cudevshr versions in vGPUmonitor
- Filter devices by UUID or index when registering nodes
- Support Ascend custom config for NPU virtualization
- Add event handler registration
- Support arm architecture

**Bug fixes**
- Fix duplicate resource keys in ConfigMap
- Fix data race when reading pod info
- Fix device ConfigMap errors

## v2.4.1 - 2024-11-15

**New features**
- Support MetaX scheduling optimization and topology awareness
- Support Moore Threads sGPU
- Add unified ConfigMap (hami-scheduler-device) for all HAMi configuration
- Refactor Helm admission webhook config

**Bug fixes**
- Fix error when allocating Iluvatar device
- Fix pod assignment when pod already has a node assigned
- Fix array out-of-bounds when GPU containers are placed between non-GPU containers
- Fix wrong device assignment when one pod has multiple containers requesting GPU

## v2.5.0 - 2025-02-06

**New features**
- Support dynamic MIG partitioning
- HAMi reinstall no longer crashes running GPU tasks
- All configuration moved to a single ConfigMap
- Add device plugin DaemonSet update strategy
- Add informer-based pod cache to reduce API server load

**Bug fixes**
- Fix HAMi-core stuck on tasks using cuMallocAsync
- Fix HAMi-core stuck on high glibc images
- Fix device filter registry to node
- Fix vGPUmonitor deviceIdx always reporting 0
- Fix Kubernetes version string handling with metadata

## v2.5.1 - 2025-05-06

**Bug fixes**
- Fix passDeviceSpecsEnabled defaulting to wrong value
- Fix scheduler ignoring KUBECONFIG env variable
- Fix device filter initialization order
- Fix parseNvidiaNumaInfo index out of range
- Fix Cambricon pods not recognized by HAMi scheduler
- Fix device memory count error on cuMallocAsync
- Fix error handling for nvml.Init in device plugin

## v2.5.2 - 2025-05-26

**Bug fixes**
- Fix device usage metrics endpoint (port 31992) not accessible

## v2.5.3 - 2025-08-05

**Bug fixes**
- Fix multiple scheduler and vGPUmonitor stability issues

## v2.6.0 - 2025-06-07

**New features**
- Support Enflame GCU sharing
- Support MetaX GPU and MetaX sGPU
- Support RuntimeClass with NVIDIA devices
- Add NVIDIA GPU topology score registry to node
- Add MIG info metrics to vGPUmonitor
- Add profiling support via net/http/pprof
- Add Helm chart checksum annotation for ConfigMap-triggered restarts

**Bug fixes**
- Fix stuck in NVIDIA driver 570+
- Fix device memory not counted properly in ComfyUI tasks
- Fix Cambricon devices not allocated properly
- Fix vgpu-devices-allocated annotation inconsistency
- Fix dynamic GPU partitioning lacking single-GPU granularity
- Fix device memory count error on cuMallocAsync
- Fix scheduler crash when MIG task runs on HAMi-core GPU
- Fix multi-process device memory count

## v2.6.1 - 2025-08-04

**Bug fixes**
- Fix multiple scheduler and node lock stability issues

## v2.7.0 - 2025-09-26

**New features**
- Add NVIDIA resource quota enforcement in webhook
- Support Kunlunxin topology-aware scheduling and vXPU
- Support Enflame GCU topology awareness
- Support AWS Neuron device and core allocation
- Support MetaX sGPU topology awareness
- Add aggregated scheduling failure events
- Make node lock timeout configurable
- Support MIG mode change
- Add option to disable admission webhook via Helm
- Add option to disable device plugin per device type via Helm
- Use scoped-down RBAC role for scheduler

**Bug fixes**
- Fix MIG partitioning NVML suppression before execution
- Fix multi-node scoring inaccuracy
- Fix error when creating Iluvatar pod
- Fix scheduler name overwrite option

## v2.7.1 - 2026-01-23

**Bug fixes**
- Update HAMi-core to fix vLLM-related issues
- Fix quota calculation error
- Fix ClusterRoleBinding failure when changing release or chart name
- Fix nil pod check in ReleaseNodeLock
- Fix concurrent map read/write fatal errors
- Fix device plugin still active after removal from GPU node
- Upgrade nvidia-mig-parted to v0.12.2 for security fix

## v2.8.0 - 2026-01-20

**New features**
- Support DRA (Dynamic Resource Allocation) via HAMi-DRA
- Enable leader election among multiple schedulers
- Support CDI mode on NVIDIA devices
- Sync NVIDIA device plugin with upstream v0.18.0
- Add hami_build_info metrics and version output
- Watch and hot-reload updated TLS certificates

**Bug fixes**
- Fix vXPU feature not working properly on P800 nodes
- Fix scheduler allocating incorrect MIG instance
- Fix concurrent map iteration and write fatal errors

## v2.8.1 - 2026-04-17

**Bug fixes**
- Fix vLLM with version above 0.18 failing to launch with multiple GPUs

## v2.8.2 - 2026-04-28

**Bug fixes**
- Fix device monitor not working properly

## v2.8.3 - 2026-05-19

**Bug fixes**
- Fix HAMi-core monitoring not working properly
- Fix device utilization watcher not launching when gpucores not specified
- Fix GetMemoryInfo error on unified memory GPUs

## v2.9.0 - 2026-05-19

**New features**
- Add HAMi-core mode for Ascend devices
- HAMi-DRA for NVIDIA is ready for use
- Sync Volcano vGPU device plugin with v0.19, add CDI support
- Add Prometheus ServiceMonitor to Helm charts
- Add resource quota check in webhook
- Support module-pair allocation for Ascend 910C in SuperPod environments
- Add support for VastAI devices
- Add namespaceSelector and objectSelector config for webhook
- Add Ascend core resource for HAMi-vNPU-core virtualization
- Add enableGetPreferredAllocation flag
- Add local-deploy target for minikube/kind clusters

**Bug fixes**
- Fix initialization error when using tensor parallelism on vLLM above 0.18
- Fix multiple device typos

