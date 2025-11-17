# Volcano 中 Ascend 设备使用指南

## 介绍

Volcano 通过 `ascend-device-plugin` 支持 Ascend 310 和 Ascend 910 的 vNPU 功能。同时支持管理异构 Ascend 集群（包含多种 Ascend 类型的集群，例如 910A、910B2、910B3、310p）。

**使用场景**：

- Ascend 910 系列的 NPU 和 vNPU 集群
- Ascend 310 系列的 NPU 和 vNPU 集群
- 异构 Ascend 集群

此功能仅在Volcano 1.14及以上版本中可用。

## 快速开始

### 环境要求

[ascend-docker-runtime](https://gitcode.com/Ascend/mind-cluster/tree/master/component/ascend-docker-runtime)

### 安装Volcano

```shell
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm install volcano volcano-sh/volcano -n volcano-system --create-namespace
```

更多安装方式请参考[这里](https://github.com/volcano-sh/volcano?tab=readme-ov-file#quick-start-guide)。

### 给 Ascend 设备打上 ascend=on 标签

```shell
kubectl label node {ascend-node} ascend=on
``` 

### 部署 hami-scheduler-device ConfigMap

```shell
kubectl apply -f https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/refs/heads/main/ascend-device-configmap.yaml
```

### 部署 ascend-device-plugin

```shell
kubectl apply -f https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/refs/heads/main/ascend-device-plugin.yaml
```
更多信息请参考 [ascend-device-plugin 文档](https://github.com/Project-HAMi/ascend-device-plugin)。

### 更新调度器配置

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

**注意：** 您可能会注意到 `volcano-vgpu` 有自己的 `GeometriesCMName` 和 `KnownGeometriesCMNamespace`，这意味着如果要在同一个 Volcano 集群中同时使用 vNPU 和 vGPU，您需要合并两边的 configMap。

## 使用方法

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
支持的 Ascend 芯片及其对应的资源名称如下表所示:
| ChipName | ResourceName | ResourceMemoryName |
|-------|-------|-------|
| 910A | huawei.com/Ascend910A | huawei.com/Ascend910A-memory |
| 910B2 | huawei.com/Ascend910B2 | huawei.com/Ascend910B2-memory |
| 910B3 | huawei.com/Ascend910B3 | huawei.com/Ascend910B3-memory |
| 910B4 | huawei.com/Ascend910B4 | huawei.com/Ascend910B4-memory |
| 910B4-1 | huawei.com/Ascend910B4-1 | huawei.com/Ascend910B4-1-memory |
| 310P3 | huawei.com/Ascend310P | huawei.com/Ascend310P-memory |