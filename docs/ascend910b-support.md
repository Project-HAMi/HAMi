# Introduction to huawei.com/Ascend910 support

**HAMi now supports huawei.com/Ascend910 by implementing most device-sharing features as nvidia-GPU**, including:

* **_NPU sharing_**: Each task can allocate a portion of Ascend NPU instead of a whole NLU card, thus NPU can be shared among multiple tasks.

* **_Device Memory Control_**: Ascend NPUs can be allocated with certain device memory size and guarantee it that it does not exceed the boundary.

* **_Device Core Control_**: Ascend NPUs can be allocated with certain compute cores and guarantee it that it does not exceed the boundary.

## Prerequisites

* Ascend device type: 910B,910B3,310P
* driver version >= 24.1.rc1
* Ascend docker runtime

## Enabling Ascend-sharing Support

* Install the chart using helm, See 'enabling vGPU support in kubernetes' section [here](https://github.com/Project-HAMi/HAMi#enabling-vgpu-support-in-kubernetes)

* Tag Ascend-910B node with the following command

```bash
kubectl label node {ascend-node} accelerator=huawei-Ascend910
```

* Install [Ascend docker runtime](https://gitee.com/ascend/ascend-docker-runtime)

* Download yaml for Ascend-vgpu-device-plugin from HAMi Project [here](https://github.com/Project-HAMi/ascend-device-plugin/blob/master/build/ascendplugin-910-hami.yaml), and deploy

```bash
wget https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/master/build/ascendplugin-910-hami.yaml
kubectl apply -f ascendplugin-910-hami.yaml
```

## Custom ascend share configuration

HAMi currently has a [built-in share configuration](https://github.com/Project-HAMi/HAMi/blob/master/charts/hami/templates/scheduler/device-configmap.yaml) for ascend.

You can customize the ascend share configuration by following the steps below:

<details>
  <summary>customize ascend config</summary>

  ### Create a new directory in hami charts

  The directory structure is as follows:

  ```bash
  tree -L 1
  .
  ├── Chart.yaml
  ├── files
  ├── templates
  └── values.yaml
  ```

  ### Create device-config.yaml

  The content is as follows:

  ```yaml
  vnpus:
  - chipName: 910B
  commonWord: Ascend910A
  resourceName: huawei.com/Ascend910A
  resourceMemoryName: huawei.com/Ascend910A-memory
  memoryAllocatable: 32768
  memoryCapacity: 32768
  aiCore: 30
  templates:
    - name: vir02
      memory: 2184
      aiCore: 2
    - name: vir04
      memory: 4369
      aiCore: 4
    - name: vir08
      memory: 8738
      aiCore: 8
    - name: vir16
      memory: 17476
      aiCore: 16
  - chipName: 910B3
  commonWord: Ascend910B
  resourceName: huawei.com/Ascend910B
  resourceMemoryName: huawei.com/Ascend910B-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_16g
      memory: 16384
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_32g
      memory: 32768
      aiCore: 10
      aiCPU: 3
  - chipName: 310P3
  commonWord: Ascend310P
  resourceName: huawei.com/Ascend310P
  resourceMemoryName: huawei.com/Ascend310P-memory
  memoryAllocatable: 21527
  memoryCapacity: 24576
  aiCore: 8
  aiCPU: 7
  templates:
    - name: vir01
      memory: 3072
      aiCore: 1
      aiCPU: 1
    - name: vir02
      memory: 6144
      aiCore: 2
      aiCPU: 2
    - name: vir04
      memory: 12288
      aiCore: 4
      aiCPU: 4
  ```

  ### Install and update with Helm

  Helm installation and updates will be based on the configuration in this file, overwriting the built-in configuration of Helm.

</details>

## Running Ascend jobs

Ascend 910Bs can now be requested by a container
using the `huawei.com/ascend910` and `huawei.com/ascend910-memory` resource type:

```yaml
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

1. `huawei.com/Ascend910-memory` only work when `huawei.com/Ascend910=1`.
