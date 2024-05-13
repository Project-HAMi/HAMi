## Introduction

**We now support huawei.com/Ascend910 by implementing most device-sharing features as nvidia-GPU**, including:

***NPU sharing***: Each task can allocate a portion of Ascend NPU instead of a whole NLU card, thus NPU can be shared among multiple tasks.

***Device Memory Control***: Ascend NPUs can be allocated with certain device memory size and guarantee it that it does not exceed the boundary.

***Device Core Control***: Ascend NPUs can be allocated with certain compute cores and guarantee it that it does not exceed the boundary.


## Prerequisites

* Ascend device type: 910B(300T A2)
* driver version >= 24.1.rc1
* Ascend docker runtime

## Enabling Ascend-sharing Support

* Install the chart using helm, See 'enabling vGPU support in kubernetes' section [here](https://github.com/Project-HAMi/HAMi#enabling-vgpu-support-in-kubernetes)

* Tag Ascend-910B node with the following command
```
kubectl label node {ascend-node} accelerator=huawei-Ascend910
```

* Install [Ascend docker runtime](https://gitee.com/ascend/ascend-docker-runtime)

* Install Ascend-device-plugin from HAMi Project [here](https://github.com/Project-HAMi/ascend-device-plugin/blob/master/build/ascendplugin-910-hami.yaml)

* Deploy the ascend-device-plugin you just specified

```
kubectl apply -f ascendplugin-910-hami.yaml
```

## Running Ascend jobs

Ascend 910Bs can now be requested by a container
using the `huawei.com/ascend910` and `huawei.com/ascend910-memory` resource type:

```
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910: 1 # requesting 1 vGPUs
          huawei.com/Ascend910-memory: 2000 # requesting 2000m device memory
```

## Notes

1. Currently, the Ascend 910b supports only two sharding strategies, which are 1/4 and 1/2. The memory request of the job will automatically align with the most close sharding strategy. In this example, the task will allocate 16384M device memory.

1. Ascend-910B-sharing in init container is not supported.

2. `huawei.com/Ascend910-memory` only work when `huawei.com/Ascend910=1`. 