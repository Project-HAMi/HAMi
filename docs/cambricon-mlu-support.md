## Introduction

**We now support cambricon.com/mlu by implementing most device-sharing features as nvidia-GPU**, including:

***MLU sharing***: Each task can allocate a portion of MLU instead of a whole MLU card, thus MLU can be shared among multiple tasks.

***Device Memory Control***: MLUs can be allocated with certain device memory size on certain type(i.e 370) and have made it that it does not exceed the boundary.

***MLU Type Specification***: You can specify which type of MLU to use or to avoid for a certain task, by setting "cambricon.com/use-mlutype" or "cambricon.com/nouse-mlutype" annotations. 

***Very Easy to use***: You don't need to modify your task yaml to use our scheduler. All your MLU jobs will be automatically supported after installation. The only thing you need to do is tag the MLU node.

## Prerequisites

* neuware-mlu370-driver > 4.15.10
* cntoolkit > 2.5.3

## Enabling MLU-sharing Support

* Install the chart using helm, See 'enabling vGPU support in kubernetes' section [here](https://github.com/Project-HAMi/HAMi#enabling-vgpu-support-in-kubernetes)

* Tag MLU node with the following command
```
kubectl label node {mlu-node} mlu=on
```

## Running MLU jobs

Cambricon MMLUs can now be requested by a container
using the `cambricon.com/mlunum` and `cambricon.com/mlumem` resource type:

```
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  containers:
    - name: ubuntu-container
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          cambricon.com/mlunum: 1 # requesting 1 MLU
          cambricon.com/mlumem: 10240 # requesting 10G MLU device memory
    - name: ubuntu-container1
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          cambricon.com/mlunum: 1 # requesting 1 MLU
          cambricon.com/mlumem: 10240 # requesting 10G MLU device memory
```

## Notes

1. Mlu-sharing in init container is not supported, pods with "combricon.com/mlumem" in init container will never be scheduled.

2. Mlu-sharing with containerd is not supported, the container may not start successfully.

3. Mlu-sharing can only be applied on MLU-370
   