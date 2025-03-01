## Introduction

**We now support mthreads.com/vgpu by implementing most device-sharing features as nvidia-GPU**, including:

***GPU sharing***: Each task can allocate a portion of GPU instead of a whole GPU card, thus GPU can be shared among multiple tasks.

***Device Memory Control***: GPUs can be allocated with certain device memory size on certain type(i.e MTT S4000) and have made it that it does not exceed the boundary.

***Device Core Control***: GPUs can be allocated with limited compute cores on certain type(i.e MTT S4000) and have made it that it does not exceed the boundary.

## Important Notes

1. Device sharing for multi-cards is not supported.

2. Only one mthreads device can be shared in a pod(even there are multiple containers).

3. Support allocating exclusive mthreads GPU by specifying mthreads.com/vgpu only.

4. These features are tested on MTT S4000

## Prerequisites

* [MT CloudNative Toolkits > 1.9.0](https://docs.mthreads.com/cloud-native/cloud-native-doc-online/)
* driver version >= 1.2.0

## Enabling GPU-sharing Support

* Deploy MT-CloudNative Toolkit on mthreads nodes (Please consult your device provider to aquire its package and document)

> **NOTICE:** *You can remove mt-mutating-webhook and mt-gpu-scheduler after installation(optional).*

* set the 'devices.mthreads.enabled = true' when installing hami

```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag={your kubernetes version} --set device.mthreads.enabled=true -n kube-system
```

## Running Mthreads jobs

Mthreads GPUs can now be requested by a container
using the `mthreads.com/vgpu`, `mthreads.com/sgpu-memory` and `mthreads.com/sgpu-core`  resource type:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpushare-pod-default
spec:
  restartPolicy: OnFailure
  containers:
    - image: core.harbor.zlidc.mthreads.com:30003/mt-ai/lm-qy2:v17-mpc
      imagePullPolicy: IfNotPresent
      name: gpushare-pod-1
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          mthreads.com/vgpu: 1
          mthreads.com/sgpu-memory: 32
          mthreads.com/sgpu-core: 8
```

> **NOTICE1:** *Each unit of sgpu-memory indicates 512M device memory*

> **NOTICE2:** *You can find more examples in [examples/mthreads folder](../examples/mthreads/)*
