# Volcano 中 vGPU 使用指南

**注意**:

在 Volcano 中使用 vgpu 功能时只需要安装 [volcano vgpu device-plugin](https://github.com/Project-HAMi/volcano-vgpu-device-plugin)，不需要安装HAMi。安装后支持为 Volcano 管理的 NVIDIA 设备提供设备共享机制

该功能基于 [Nvidia Device Plugin](https://github.com/NVIDIA/k8s-device-plugin)，使用 [HAMi-core](https://github.com/Project-HAMi/HAMi-core) 支持GPU卡的硬隔离。

Volcano vGPU功能仅在volcano 1.9以上版本中可用。

## 快速开始

### 安装Volcano

```
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm install volcano volcano-sh/volcano -n volcano-system --create-namespace
```

更多安装方式请参考[这里](https://github.com/volcano-sh/volcano?tab=readme-ov-file#quick-start-guide)

### 配置调度器

更新调度器配置：

```shell script
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
      - name: priority
      - name: gang
      - name: conformance
    - plugins:
      - name: drf
      - name: deviceshare
        arguments:
          deviceshare.VGPUEnable: true # enable vgpu
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
```

### 在 Kubernetes 中启用GPU

如果希望在**所有** GPU 节点上启用此功能，可以在集群中部署如下 Daemonset

```
$ kubectl create -f https://raw.githubusercontent.com/Project-HAMi/volcano-vgpu-device-plugin/main/volcano-vgpu-device-plugin.yml
```

### 验证环境

如果可分配资源中包含 `volcano.sh/vgpu-number` 则表示环境正常。

```shell script
$ kubectl get node {node name} -oyaml
...
status:
  addresses:
  - address: 172.17.0.3
    type: InternalIP
  - address: volcano-control-plane
    type: Hostname
  allocatable:
    cpu: "4"
    ephemeral-storage: 123722704Ki
    hugepages-1Gi: "0"
    hugepages-2Mi: "0"
    memory: 8174332Ki
    pods: "110"
    volcano.sh/gpu-number: "10"    # vGPU resource
  capacity:
    cpu: "4"
    ephemeral-storage: 123722704Ki
    hugepages-1Gi: "0"
    hugepages-2Mi: "0"
    memory: 8174332Ki
    pods: "110"
    volcano.sh/gpu-memory: "89424"
    volcano.sh/gpu-number: "10"   # vGPU resource
```

### 运行 vGPU 任务

可以通过在resource.limits中设置 `volcano.sh/vgpu-number`、`volcano.sh/vgpu-cores` 和 `volcano.sh/vgpu-memory` 来请求vGPU资源

```shell script
$ cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod1
spec:
  containers:
    - name: cuda-container
      image: nvidia/cuda:9.0-devel
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          volcano.sh/vgpu-number: 2 # requesting 2 gpu cards
          volcano.sh/vgpu-memory: 3000 # (optional)each vGPU uses 3G device memory
          volcano.sh/vgpu-cores: 50 # (optional)each vGPU uses 50% core  
EOF
```

可以在容器内使用nvidia-smi验证设备内存：

> **警告:** *如果您在使用NVIDIA镜像时没有声明请求GPU资源，机器上的所有GPU都将在容器内暴露。一个容器使用的vGPU数量不能超过该节点上的GPU数量。*

### 监控

有关监控信息，请参考 [volcano-vgpu-device-plugin](https://github.com/Project-HAMi/volcano-vgpu-device-plugin#monitor)。
