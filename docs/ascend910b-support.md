# Introduction to huawei.com/Ascend910A, Ascend910B Series, and Ascend310P Support

HAMi supports virtualization of Huawei Ascend 910A, 910B series devices (910B2, 910B3, 910B4), and 310P devices, providing several features similar to vGPU, including:

* **_NPU sharing_**: Each task can allocate a portion of Ascend NPU instead of a whole NLU card, thus NPU can be shared among multiple tasks.

* **_Device Memory Control_**: Ascend NPUs can be allocated with certain device memory size and guarantee it that it does not exceed the boundary.

* **_Device Core Control_**: Ascend NPUs can be allocated with certain compute cores and guarantee it that it does not exceed the boundary.

## Prerequisites

* Ascend docker runtime
* Driver version > 24.1.rc1
* Ascend device type: 910A, 910B2, 910B3, 910B4, 310P

## Enabling NPU Sharing

* Install the chart using helm, See 'enabling vGPU support in kubernetes' section [here](https://github.com/Project-HAMi/HAMi#enabling-vgpu-support-in-kubernetes)

* Label the Ascend 910 node with the following command:

```bash
kubectl label node {ascend-node} accelerator=huawei-Ascend910
```

* Deploy [Ascend docker runtime](https://gitcode.com/Ascend/mind-cluster/tree/master/component/ascend-docker-runtime)

* Deploy [ascend-device-plugin](https://github.com/Project-HAMi/ascend-device-plugin)

## Customizing NPU Virtualization Parameters

HAMi includes a built-in [virtualization configuration file](https://github.com/Project-HAMi/HAMi/blob/master/charts/hami/templates/scheduler/device-configmap.yaml) for NPUs.

HAMi also supports customizing virtualization parameters through the following method:

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
  - chipName: 910A
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
  - chipName: 910B2
    commonWord: Ascend910B2
    resourceName: huawei.com/Ascend910B2
    resourceMemoryName: huawei.com/Ascend910B2-memory
    memoryAllocatable: 65536
    memoryCapacity: 65536
    aiCore: 24
    aiCPU: 6
    templates:
      - name: vir03_1c_8g
        memory: 8192
        aiCore: 3
        aiCPU: 1
      - name: vir06_1c_16g
        memory: 16384
        aiCore: 6
        aiCPU: 1
      - name: vir12_3c_32g
        memory: 32768
        aiCore: 12
        aiCPU: 3  
  - chipName: 910B3
    commonWord: Ascend910B3
    resourceName: huawei.com/Ascend910B3
    resourceMemoryName: huawei.com/Ascend910B3-memory
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
  - chipName: 910B4
    commonWord: Ascend910B4
    resourceName: huawei.com/Ascend910B4
    resourceMemoryName: huawei.com/Ascend910B4-memory
    memoryAllocatable: 32768
    memoryCapacity: 32768
    aiCore: 20
    aiCPU: 7
    templates:
      - name: vir05_1c_8g
        memory: 8192
        aiCore: 5
        aiCPU: 1
      - name: vir10_3c_16g
        memory: 16384
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

## Virtualization Template Overview

HAMi supports configuring NPU resource allocation through predefined device templates. Each template includes the following:

- Template name (name): Unique identifier for the template.
- Memory size (memory): Device memory allocated to the template (in MB).
- AI core count (aiCore): Number of AI cores allocated to the template.
- AI CPU core count (aiCPU): Number of AI CPU cores allocated to the template (supported by some models).

When a user requests a specific memory size, the system automatically aligns the requested memory to the nearest template size. For example, if a user requests 2000MB of memory, the system will select the smallest template with memory size greater than or equal to 2000MB.

For specific configurations, refer to the [official Ascend virtualization templates](https://www.hiascend.com/document/detail/zh/computepoweralloca/300/cpaug/cpaug/cpaug_00005.html).

## Device Granularity Partitioning

Refer to the aiCore ratio in each type configuration (chipName) and the aiCore under the template.

### Ascend910 Series Device Granularity Partitioning

- Ascend910A devices support 4 granularity partitions: 1/15, 2/15, 4/15, and 8/15 of a card. Allocated memory automatically aligns to the nearest granularity above the requested amount.
- Ascend910B2 devices support 3 granularity partitions: 1/8, 1/4, and 1/2 of a card. Allocated memory automatically aligns to the nearest granularity above the requested amount.
- Ascend910B3 and Ascend910B4 devices support 2 granularity partitions: 1/4 and 1/2 of a card. Allocated memory automatically aligns to the nearest granularity above the requested amount.

### Ascend310P Device Granularity Partitioning

Ascend310P devices (Atlas inference series products) support multiple granularity partitions, including 1/8, 1/4, and 1/2 of a card. Allocated memory automatically aligns to the nearest granularity above the requested amount.

## Running NPU Workloads

You can request Ascend 910B series resources using the `huawei.com/ascend910Bx` and `huawei.com/ascend910Bx-memory` resource types:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend910A-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910A: 1 # requesting 1 vGPUs
          huawei.com/Ascend910A-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend910B2-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B2: 1 # requesting 1 vGPUs
          huawei.com/Ascend910B2-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend910B3-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B3: 1 # requesting 1 vGPUs
          huawei.com/Ascend910B3-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend910B4-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B4: 1 # requesting 1 vGPUs
          huawei.com/Ascend910B4-memory: 2000 # requesting 2000m device memory
---
apiVersion: v1
kind: Pod
metadata:
  name: ascend310P-pod
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend310P: 1 # requesting 1 vGPUs
          huawei.com/Ascend310P-memory: 2000 # requesting 2000m device memory
```

## Device Health Monitoring

HAMi supports health monitoring for Ascend NPU devices, ensuring only healthy devices are allocated to Pods. Health monitoring includes:

- Device status verification
- Device resource availability verification
- Device driver status verification

## Resource Usage Statistics

HAMi supports statistics collection for Ascend NPU device resource usage, including:

- Device memory usage
- AI core usage
- AI CPU core usage
- Device utilization

These statistics can be used for resource scheduling decisions and performance optimization.

## Node Locking Mechanism

HAMi implements a node locking mechanism to prevent resource allocation conflicts. When a Pod requests Ascend NPU resources, the system locks the corresponding node to prevent other Pods from using the same device resources simultaneously.


## Device UUID Selection

You can specify which Ascend devices to use or exclude using Pod annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-pod
  annotations:
    # Use specific Ascend devices (comma-separated list)
    hami.io/use-Ascend910B-uuid: "device-uuid-1,device-uuid-2"
    # Or exclude specific Ascend devices (comma-separated list)
    hami.io/no-use-Ascend910B-uuid: "device-uuid-3,device-uuid-4"
spec:
  # ... rest of pod spec
```

> **NOTE:** The device UUID format depends on the device type, such as `Ascend910B`, `Ascend910B2`, `Ascend910B3`, `Ascend910B4`, etc. You can find the available device UUIDs in the node status.

### Usage Example

Here is a complete example demonstrating how to use the UUID selection feature:

<details>
  <summary>Custom Configuration</summary>

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-pod
  annotations:
    hami.io/use-Ascend910B-uuid: "device-uuid-1,device-uuid-2"
spec:
  containers:
    - name: ubuntu-container
      image: ascendhub.huawei.com/public-ascendhub/ascend-mindspore:23.0.RC3-centos7
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          huawei.com/Ascend910B3: 1
          huawei.com/Ascend910B3-memory: 2000
```

In this example, the Pod will only run on Ascend910B3 devices with UUIDs `device-uuid-1` or `device-uuid-2`.

#### Finding Device UUIDs

You can find the Ascend device UUIDs on a node using the following command:

```bash
kubectl describe node <node-name> | grep -A 10 "Allocated resources"
```

Or by viewing the node annotations:

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "annotations:"
```

In the node annotations, look for `hami.io/node-register-Ascend910B` or similar annotations, which contain the device UUID information.

</details>

## Notes

- NPU sharing is not supported in init containers.
- `huawei.com/Ascend910Bx-memory` is only effective when `huawei.com/Ascend910Bx=1`.
- Multi-device requests (`huawei.com/Ascend910Bx > 1`) do not support vNPU mode.