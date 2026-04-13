## 性能测试

> **⚠️ 注意**：以下基准测试数据来自项目的早期版本（当时称为 vGPU-device-plugin），使用的测试环境已经过时。虽然这些结果被保留作为历史参考，但可能无法反映 HAMi 当前的性能表现。
>
> 如需进行最新的性能测试,请参考下方的[运行基准测试](#运行基准测试)部分。

### 历史基准测试结果（旧版）

在测试报告中，我们在以下场景中执行了 ai-benchmark 测试脚本，并汇总了最终结果：

| 测试环境 | 环境描述                                              |
| ---------------- | :------------------------------------------------------: |
| Kubernetes version | v1.12.9                                                |
| Docker version     | 18.09.1                                                |
| GPU Type           | Tesla V100                                             |
| GPU Num            | 2                                                      |

| 测试名称 |                      测试用例                      |
| -------- | :------------------------------------------------: |
| Nvidia-device-plugin        |         k8s + nvidia 官方 k8s-device-plugin          |
| vGPU-device-plugin        |      k8s + vGPU k8s-device-plugin，无虚拟显存      |
| vGPU-device-plugin (virtual device memory)  | k8s + vGPU k8s-device-plugin，高负载，开启虚拟显存 |

测试内容：

| Test ID |     名称      |   类型    |          参数           |
| ------- | :-----------: | :-------: | :---------------------: |
| 1.1     | Resnet-V2-50  | inference |  batch=50,size=346*346  |
| 1.2     | Resnet-V2-50  | training  |  batch=20,size=346*346  |
| 2.1     | Resnet-V2-152 | inference |  batch=10,size=256*256  |
| 2.2     | Resnet-V2-152 | training  |  batch=10,size=256*256  |
| 3.1     |    VGG-16     | inference |  batch=20,size=224*224  |
| 3.2     |    VGG-16     | training  |  batch=2,size=224*224   |
| 4.1     |    DeepLab    | inference |  batch=2,size=512*512   |
| 4.2     |    DeepLab    | training  |  batch=1,size=384*384   |
| 5.1     |     LSTM      | inference | batch=100,size=1024*300 |
| 5.2     |     LSTM      | training  | batch=10,size=1024*300  |

历史测试结果：

![img](../imgs/benchmark_inf.png)

![img](../imgs/benchmark_train.png)

---

## 运行基准测试

要在您的环境中测试 HAMi 的性能，请按照以下步骤操作：

### 前置条件

- HAMi 已安装并在 Kubernetes 集群中配置完成（参见[快速开始](../README_cn.md#快速开始)）
- GPU 节点已标记 `gpu=on` 标签
- Kubernetes 版本 >= 1.18
- Docker 或 containerd 运行时，支持 NVIDIA

### 构建基准测试镜像（可选）

如果您想自己构建基准测试镜像：

```bash
cd benchmarks/ai-benchmark
sh build.sh
```

### 运行基准测试任务

HAMi 提供了两个基准测试任务配置来比较性能：

**1. 在 HAMi 上运行基准测试：**

```bash
cd benchmarks/deployments
kubectl apply -f job-on-hami.yml
```

这将部署一个使用 HAMi GPU 共享和显存隔离功能的任务（请求 50% 的 GPU 显存）。

**2. 在 NVIDIA device plugin 上运行基准测试（用于对比）：**

```bash
kubectl apply -f job-on-nvidia-device-plugin.yml
```

要安装官方 NVIDIA device plugin，请参考 [NVIDIA k8s-device-plugin 快速开始](https://github.com/NVIDIA/k8s-device-plugin?tab=readme-ov-file#quick-start)。

### 查看结果

任务完成后，查看基准测试结果：

```bash
# 检查任务状态
kubectl get jobs

# 查看 HAMi 基准测试结果
kubectl logs job/ai-benchmark-on-hami

# 查看 NVIDIA device plugin 基准测试结果
kubectl logs job/ai-benchmark-on-official
```

### 自定义基准测试

您可以修改 `benchmarks/deployments/` 中的基准测试任务来测试不同的配置：

- 调整 GPU 显存分配（例如：`nvidia.com/gpumem-percentage: 50`）
- 测试不同的 GPU 数量
- 比较启用和不启用 HAMi 显存隔离功能的性能差异

更多详细信息，请参阅[基准测试 README](../benchmarks/README.md)。
