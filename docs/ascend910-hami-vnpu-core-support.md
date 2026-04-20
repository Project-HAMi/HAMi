# Introduction to Ascend910B Series and Ascend910C Support

Implements  a soft slicing mechanism based on `libvnpu.so` interception and `limiter` token scheduling, enabling fine-grained resource sharing.  For detailed information, check [hami-vnpu-core](https://github.com/Project-HAMi/hami-vnpu-core).

* **_NPU sharing_**: Each task can allocate a portion of Ascend NPU instead of a whole NLU card, thus NPU can be shared among multiple tasks.
* **_Device Memory Control_**: Ascend NPUs can be allocated with certain device memory size and guarantee it that it does not exceed the boundary.
* **_Device Core Control_**: Ascend NPUs can be allocated with certain compute cores and guarantee it that it does not exceed the boundary.

## Prerequisites

* Ascend docker runtime
* driver version > 25.5
* Ascend device type: 910B3、910C

## Enabling NPU Sharing

* [Ascend docker runtime](https://gitcode.com/Ascend/mind-cluster/tree/master/component/ascend-docker-runtime)

* [ascend-device-plugin](https://github.com/Project-HAMi/ascend-device-plugin)
* **Chip Mode**: enable `device-share` mode on Ascend chips for virtualization

### Host Environment Preparation

Before launching any containers, the **Global Shared Memory (SHM) Region** must be initialized on the host to allow inter-Pod coordination.

#### 1. Create the Shared Directory

```bash
sudo mkdir -p /usr/local/hami-shared-region
sudo chmod 777 /usr/local/hami-shared-region
```

#### 2. Deploy hami-vnpu-core Components

Place the following files in a fixed host path (`/usr/local/hami-vnpu-core/`) for mounting into containers:

```
/usr/local/hami-vnpu-core/
├── limiter              # Manager daemon binary (compiled from hami-vnpu-core)
├── libvnpu.so           # Interception library for LD_PRELOAD
└── ld.so.preload        # Global preload config 
```

  ### Create device-config.yaml

The content is as follows:

  ```yaml
vnpus:
- chipName: 910B3
  commonWord: Ascend910B3
  resourceName: huawei.com/Ascend910B3
  resourceMemoryName: huawei.com/Ascend910B3-memory
  resourceCoreName: huawei.com/Ascend910B3-core
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
- chipName: Ascend910
      commonWord: Ascend910C
      resourceName: huawei.com/Ascend910C
      resourceMemoryName: huawei.com/Ascend910C-memory
      resourceCoreName: huawei.com/Ascend910C-core
      memoryAllocatable: 65536
      memoryCapacity: 65536
      aiCore: 20
      aiCPU: 7
  ```

  ### Install and update with Helm

  Helm installation and updates will be based on the configuration in this file, overwriting the built-in configuration of Helm.

## Running NPU Workloads

You can request Ascend 910B series resources using the `huawei.com/ascend910Bx` ,`huawei.com/ascend910Bx-memory`  and   `huawei.com/ascend910Bx-core` resource types:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-soft-slice-pod
  annotations:
    huawei.com/vnpu-mode: 'hami-core' # Enables hami-vnpu-core soft-segmentation for this pod
spec:
  containers:
    - name: npu_pod
      ...
      resources:
        limits:
          huawei.com/Ascend910B3: "1"           # Request 1 physical NPU
          huawei.com/Ascend910B3-memory: "28672"     # Request 28Gi memory
          huawei.com/Ascend910B3-core: "40"     # Request 40% core
```

