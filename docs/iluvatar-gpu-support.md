## Introduction

**We now support iluvatar.ai/gpu(i.e MR-V100、BI-V150、BI-V100) by implementing most device-sharing features as nvidia-GPU**, including:

***GPU sharing***: Each task can allocate a portion of GPU instead of a whole GPU card, thus GPU can be shared among multiple tasks.

***Device Memory Control***: GPUs can be allocated with certain device memory size and have made it that it does not exceed the boundary.

***Device Core Control***: GPUs can be allocated with limited compute cores and have made it that it does not exceed the boundary.

***Device UUID Selection***: You can specify which GPU devices to use or exclude using annotations.

***Very Easy to use***: You don't need to modify your task yaml to use our scheduler. All your GPU jobs will be automatically supported after installation.

## Prerequisites

* Iluvatar gpu-manager (please consult your device provider)
* driver version > 3.1.0

## Enabling GPU-sharing Support

* Deploy gpu-manager on iluvatar nodes (Please consult your device provider to aquire its package and document)

> **NOTICE:** *Install only gpu-manager, don't install gpu-admission package.*

* set the devices.iluvatar.enabled=true when install hami
```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag={your kubernetes version} --set devices.iluvatar.enabled=true
```

**Note:** The currently supported GPU models and resource names are defined in (https://github.com/Project-HAMi/HAMi/blob/master/charts/hami/templates/scheduler/device-configmap.yaml):
```yaml
    iluvatars:
    - chipName: MR-V100
      commonWord: MR-V100
      resourceCountName: iluvatar.ai/MR-V100-vgpu
      resourceMemoryName: iluvatar.ai/MR-V100.vMem
      resourceCoreName: iluvatar.ai/MR-V100.vCore
    - chipName: MR-V50
      commonWord: MR-V50
      resourceCountName: iluvatar.ai/MR-V50-vgpu
      resourceMemoryName: iluvatar.ai/MR-V50.vMem
      resourceCoreName: iluvatar.ai/MR-V50.vCore
    - chipName: BI-V150
      commonWord: BI-V150
      resourceCountName: iluvatar.ai/BI-V150-vgpu
      resourceMemoryName: iluvatar.ai/BI-V150.vMem
      resourceCoreName: iluvatar.ai/BI-V150.vCore
    - chipName: BI-V100
      commonWord: BI-V100
      resourceCountName: iluvatar.ai/BI-V100-vgpu
      resourceMemoryName: iluvatar.ai/BI-V100.vMem
      resourceCoreName: iluvatar.ai/BI-V100.vCore
```

## Device Granularity

HAMi divides each Iluvatar GPU into 100 units for resource allocation. When you request a portion of a GPU, you're actually requesting a certain number of these units.

### Memory Allocation

- Each unit of `iluvatar.ai/<card-type>.vMem` represents 256MB of device memory
- If you don't specify a memory request, the system will default to using 100% of the available memory
- Memory allocation is enforced with hard limits to ensure tasks don't exceed their allocated memory

### Core Allocation

- Each unit of `iluvatar.ai/<card-type>.vCore` represents 1% of the available compute cores
- Core allocation is enforced with hard limits to ensure tasks don't exceed their allocated cores
- When requesting multiple GPUs, the system will automatically set the core resources based on the number of GPUs requested

## Running Iluvatar jobs

Iluvatar GPUs can now be requested by a container
using the `iluvatar.ai/BI-V150-vgpu`, `iluvatar.ai/BI-V150.vMem` and `iluvatar.ai/BI-V150.vCore`  resource type:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: BI-V150-poddemo
spec:
  restartPolicy: Never
  containers:
  - name: BI-V150-poddemo
    image: registry.iluvatar.com.cn:10443/saas/mr-bi150-4.3.0-x86-ubuntu22.04-py3.10-base-base:v1.0
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
        iluvatar.ai/BI-V150-vgpu: 1
        iluvatar.ai/BI-V150.vCore: 50
        iluvatar.ai/BI-V150.vMem: 64
      limits:
        iluvatar.ai/BI-V150-vgpu: 1
        iluvatar.ai/BI-V150.vCore: 50
        iluvatar.ai/BI-V150.vMem: 64
```

> **NOTICE1:** *Each unit of vcuda-memory indicates 256M device memory*

> **NOTICE2:** *You can find more examples in [examples/iluvatar folder](../examples/iluvatar/)*

## Device UUID Selection

You can specify which GPU devices to use or exclude using annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
  annotations:
    # Use specific GPU devices (comma-separated list)
    hami.io/use-<card-type>-uuid: "device-uuid-1,device-uuid-2"
    # Or exclude specific GPU devices (comma-separated list)
    hami.io/no-use-<card-type>-uuid: "device-uuid-1,device-uuid-2"
spec:
  # ... rest of pod spec
```

### Finding Device UUIDs

You can find the UUIDs of Iluvatar GPUs on a node using the following command:

```bash
kubectl get pod <pod-name> -o yaml | grep -A 10 "hami.io/<card-type>-devices-allocated"
```

Or by examining the node annotations:

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "hami.io/node-<card-type>-register"
```

Look for annotations containing device information in the node status.

## Notes

1. You need to set the following prestart command in order for the device-share to work properly
```sh
      set -ex
      echo "export LD_LIBRARY_PATH=/usr/local/corex/lib64:$LD_LIBRARY_PATH">> /root/.bashrc
      cp -f /usr/local/iluvatar/lib64/libcuda.* /usr/local/corex/lib64/
      cp -f /usr/local/iluvatar/lib64/libixml.* /usr/local/corex/lib64/
      source /root/.bashrc
```

2. Virtualization takes effect only for containers that apply for one GPU(i.e iluvatar.ai/vgpu=1 ). When requesting multiple GPUs, the system will automatically set the core resources based on the number of GPUs requested.

3. The `iluvatar.ai/<card-type>.vMem` resource is only effective when `iluvatar.ai/<card-type>-vgpu=1`.

4. Multi-device requests (`iluvatar.ai/<card-type>-vgpu= > 1`) do not support vGPU mode.