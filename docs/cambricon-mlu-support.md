## Introduction

**We now support cambricon.com/mlu by implementing most device-sharing features as nvidia-GPU**, including:

***MLU sharing***: Each task can allocate a portion of MLU instead of a whole MLU card, thus MLU can be shared among multiple tasks.

***Device Memory Control***: MLUs can be allocated with certain device memory size and guarantee it that it does not exceed the boundary.

***Device Core Control***: MLUs can be allocated with certain compute cores and guarantee it that it does not exceed the boundary.

***MLU Type Specification***: You can specify which type of MLU to use or to avoid for a certain task, by setting "cambricon.com/use-mlutype" or "cambricon.com/nouse-mlutype" annotations. 


## Prerequisites

* neuware-mlu370-driver > 5.10
* cntoolkit > 2.5.3

## Enabling MLU-sharing Support

* Install the chart using helm, See 'enabling vGPU support in kubernetes' section [here](https://github.com/Project-HAMi/HAMi#enabling-vgpu-support-in-kubernetes)

* Tag MLU node with the following command
```
kubectl label node {mlu-node} mlu=on
```

* Get cambricon-device-plugin from your device provider and specify the following parameters during deployment:

`mode=dynamic-smlu`, `min-dsmlu-unit=256`

These two parameters represent enabling the dynamic smlu function and setting the minimum allocable memory unit to 256 MB, respectively. You can refer to the document from device provider for more details

* Deploy the cambricon-device-plugin you just specified

```
kubectl apply -f cambricon-device-plugin-daemonset.yaml
```

## Running MLU jobs

Cambricon MLUs can now be requested by a container
using the `cambricon.com/vmlu` ,`cambricon.com/mlu.smlu.vmemory` and `cambricon.com/mlu.smlu.vcore` resource type:

```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: binpack-1
  labels:
    app: binpack-1
spec:
  replicas: 1
  selector:
    matchLabels:
      app: binpack-1
  template:
    metadata:
      labels:
        app: binpack-1
    spec:
      containers:
        - name: c-1
          image: ubuntu:18.04
          command: ["sleep"]
          args: ["100000"]
          resources:
            limits:
              cambricon.com/vmlu: "1"
              cambricon.com/mlu.smlu.vmemory: "20"
              cambricon.com/mlu.smlu.vcore: "10"
```

## Notes

1. Mlu-sharing in init container is not supported, pods with "combricon.com/mlumem" in init container will never be scheduled.

2. `cambricon.com/mlu.smlu.vmemory`, `cambricon.com/mlu.smlu.vcore` only work when `cambricon.com/vmlu=1`, otherwise, whole MLUs are allocated when `cambricon.com/vmlu>1` regardless of `cambricon.com/mlu.smlu.vmemory` and `cambricon.com/mlu.smlu.vcore`.