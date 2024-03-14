## Introduction

**We now support iluvatar.ai/gpu by implementing most device-sharing features as nvidia-GPU**, including:

***GPU sharing***: Each task can allocate a portion of GPU instead of a whole GPU card, thus GPU can be shared among multiple tasks.

***Device Memory Control***: GPUs can be allocated with certain device memory size on certain type(i.e m100) and have made it that it does not exceed the boundary.

***Device Core Control***: GPUs can be allocated with limited compute cores on certain type(i.e m100) and have made it that it does not exceed the boundary.

***Very Easy to use***: You don't need to modify your task yaml to use our scheduler. All your GPU jobs will be automatically supported after installation.

## Prerequisites

* Iluvatar gpu-manager (please consult your device provider)
* driver version > 3.1.0

## Enabling GPU-sharing Support

* Deploy gpu-manager on iluvatar nodes (Please consult your device provider to aquire its package and document)

> **NOTICE:** *Install only gpu-manager, don't install gpu-admission package.*

* Identify the resource name about core and memory usage(i.e 'iluvatar.ai/vcuda-core', 'iluvatar.ai/vcuda-memory')

* set the 'iluvatarResourceMem' and 'iluvatarResourceCore' parameters when install hami

```
helm install hami hami-charts/hami --set scheduler.kubeScheduler.imageTag={your kubernetes version} --set iluvatarResourceMem=iluvatar.ai/vcuda-memory --set iluvatarResourceCore=iluvatar.ai/vcuda-core -n kube-system
```

## Running Iluvatar jobs

Iluvatar GPUs can now be requested by a container
using the `iluvatar.ai/vgpu`, `iluvatar.ai/vcuda-memory` and `iluvatar.ai/vcuda-core`  resource type:

```
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
spec:
  restartPolicy: Never
  containers:
  - name: poddemo
    image: harbor.4pd.io/vgpu/corex_transformers@sha256:36a01ec452e6ee63c7aa08bfa1fa16d469ad19cc1e6000cf120ada83e4ceec1e
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
        iluvatar.ai/vgpu: 1
        iluvatar.ai/vcuda-core: 50
        iluvatar.ai/vcuda-memory: 64
      limits:
        iluvatar.ai/vgpu: 1
        iluvatar.ai/vcuda-core: 50
        iluvatar.ai/vcuda-memory: 64
```

> **NOTICE1:** *Each unit of vcuda-memory indicates 256M device memory*

> **NOTICE2:** *You can find more examples in [examples/iluvatar folder](../examples/iluvatar/)*

## Notes

1. You need to set the following prestart command in order for the device-share to work properly
```
      set -ex
      echo "export LD_LIBRARY_PATH=/usr/local/corex/lib64:$LD_LIBRARY_PATH">> /root/.bashrc
      cp -f /usr/local/iluvatar/lib64/libcuda.* /usr/local/corex/lib64/
      cp -f /usr/local/iluvatar/lib64/libixml.* /usr/local/corex/lib64/
      source /root/.bashrc 
```

2. Virtualization takes effect only for containers that apply for one GPU(i.e iluvatar.ai/vgpu=1 )

   