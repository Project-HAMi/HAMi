# User Guide for Ascend Devices in Volcano

## Introduction

 Volcano supports vNPU feature for both Ascend 310 and Ascend 910 using the `ascend-device-plugin`. It also supports managing heterogeneous Ascend cluster(Cluster with multiple Ascend types, i.e. 910A,910B2,910B3,310p)

**Use case**:

- NPU and vNPU cluster for Ascend 910 series  
- NPU and vNPU cluster for Ascend 310 series  
- Heterogeneous Ascend cluster

This feature is only available in volcano >= 1.14.

## Quick Start

### Prerequisites

[ascend-docker-runtime](https://gitcode.com/Ascend/mind-cluster/tree/master/component/ascend-docker-runtime)

### Install Volcano

```shell
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm install volcano volcano-sh/volcano -n volcano-system --create-namespace
```

Additional installation methods can be found [here](https://github.com/volcano-sh/volcano?tab=readme-ov-file#quick-start-guide).

### Label the Node with ascend=on

```shell
kubectl label node {ascend-node} ascend=on
``` 

### Deploy `hami-scheduler-device` config map

```shell
kubectl apply -f https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/refs/heads/main/ascend-device-configmap.yaml
```

### Deploy ascend-device-plugin

```shell
kubectl apply -f https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/refs/heads/main/ascend-device-plugin.yaml
```

For more information, refer to the [ascend-device-plugin documentation](https://github.com/Project-HAMi/ascend-device-plugin).

### Scheduler Config Update

Update the scheduler configuration:

```shell
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: predicates
      - name: deviceshare
        arguments:
          deviceshare.AscendHAMiVNPUEnable: true   # enable ascend vnpu
          deviceshare.SchedulePolicy: binpack  # scheduling policy. binpack / spread
          deviceshare.KnownGeometriesCMNamespace: kube-system
          deviceshare.KnownGeometriesCMName: hami-scheduler-device
```

  **Note:** You may notice that, `volcano-vgpu` has its own `KnownGeometriesCMName` and `KnownGeometriesCMNamespace`, which means if you want to use both vNPU and vGPU in a same volcano cluster, you need to merge the configMap from both sides and set it here.

## Usage

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-pod
spec:
  schedulerName: volcano
  containers:
    - name: ubuntu-container
      image: swr.cn-south-1.myhuaweicloud.com/ascendhub/ascend-pytorch:24.0.RC1-A2-1.11.0-ubuntu20.04
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          huawei.com/Ascend310P: "1"
          huawei.com/Ascend310P-memory: "4096"

```

The supported Ascend chips and their `ResourceNames` are shown in the following table:

| ChipName | ResourceName | ResourceMemoryName |
|-------|-------|-------|
| 910A | huawei.com/Ascend910A | huawei.com/Ascend910A-memory |
| 910B2 | huawei.com/Ascend910B2 | huawei.com/Ascend910B2-memory |
| 910B3 | huawei.com/Ascend910B3 | huawei.com/Ascend910B3-memory |
| 910B4 | huawei.com/Ascend910B4 | huawei.com/Ascend910B4-memory |
| 910B4-1 | huawei.com/Ascend910B4-1 | huawei.com/Ascend910B4-1-memory |
| 310P3 | huawei.com/Ascend310P | huawei.com/Ascend310P-memory |