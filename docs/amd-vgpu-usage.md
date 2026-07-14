# AMD vGPU 使用指南

本指南面向在 Kubernetes 集群中使用 HAMi 共享 AMD Instinct/ROCm GPU 的用户。工作负载通过标准 Kubernetes 资源请求显存和算力份额，无需修改应用代码。

> 请使用与 HAMi 版本匹配的 AMD vGPU device-plugin 发布镜像和部署清单；本文不包含源码构建流程。

## 需要部署的组件

| 组件 | 作用 | 关键要求 |
| --- | --- | --- |
| HAMi | 调度、设备分配和准入 | scheduler 正常运行，并管理三个 AMD 资源。 |
| AMD GPU Operator | 驱动、ROCm 环境和 node-labeller | 启用 node-labeller；禁用原生 device-plugin。 |
| AMD vGPU device-plugin | 注册 AMD 资源、分配 CU、注入容器运行时限制 | 使用与当前 HAMi 版本匹配的发布镜像和清单。 |

节点还必须具备可用的 AMD 驱动和 ROCm。可用以下命令确认：

```bash
amd-smi static --gpu 0
```

输出应包含设备型号、VRAM 和 `NUM_COMPUTE_UNITS`。

## 1. 安装或配置 HAMi

安装 HAMi 后，确认 scheduler 管理所有 AMD vGPU 资源。values 文件中应包含：

```yaml
devices:
  amd:
    customresources:
      - amd.com/gpu
      - amd.com/gpumem
      - amd.com/gpucores
```

安装或升级后确认 scheduler 正常运行：

```bash
kubectl -n kube-system get pods | grep hami-scheduler
```

## 2. 启用 AMD GPU Operator node-labeller

node-labeller 将显存、CU 数量和产品名称写入 Node 标签，供 AMD vGPU device-plugin 注册给 HAMi。

为避免两个 device-plugin 竞争同一 `amd.com/gpu` 资源名，保持 Operator 的普通 device-plugin 关闭，只开启 node-labeller：

```bash
kubectl -n kube-amd-gpu patch deviceconfig default --type=merge -p \
  '{"spec":{"devicePlugin":{"enableDevicePlugin":false,"enableNodeLabeller":true}}}'
```

确认 labeller 已就绪：

```bash
kubectl -n kube-amd-gpu get ds default-node-labeller
kubectl get node <node-name> -o json | \
  jq '.metadata.labels | with_entries(select(.key | contains("amd.com/gpu")))'
```

典型输出如下：

```text
amd.com/gpu.vram=192G
amd.com/gpu.cu-count=304
amd.com/gpu.product-name=AMD_Instinct_MI300X_VF
```

## 3. 部署 AMD vGPU device-plugin

使用该版本发布的 AMD vGPU device-plugin Helm chart 或 YAML 清单部署到所有 AMD GPU 节点。部署清单必须具备以下配置：

- 使用发布的 AMD vGPU device-plugin 镜像；
- 挂载 `/var/lib/kubelet/device-plugins`、`/sys` 和发布清单指定的 vGPU hook 路径；
- 设置 `NODE_NAME` 为 `spec.nodeName`；
- 仅选择 AMD GPU 节点，例如 `feature.node.kubernetes.io/amd-vgpu=true`；
- 使用有权限读取 Node 和 Pod 的 ServiceAccount。

等待 DaemonSet 就绪：

```bash
kubectl -n kube-system rollout status ds/amdgpu-device-plugin-daemonset
```

确认 device-plugin 已将完整设备信息注册给 HAMi：

```bash
kubectl get node <node-name> -o json | \
  jq -r '.metadata.annotations["hami.io/node-amd-register"]'
```

结果必须包含 `devmem` 和 `devcore`。例如 MI300X VF：

```json
[{"count":10,"devmem":196608,"devcore":304,"type":"AMD_Instinct_MI300X_VF"}]
```

若刚启用 node-labeller，可重启 device-plugin 使其立即读取标签：

```bash
kubectl -n kube-system rollout restart ds/amdgpu-device-plugin-daemonset
kubectl -n kube-system rollout status ds/amdgpu-device-plugin-daemonset
```

## 4. 提交 AMD vGPU 工作负载

资源含义如下：

- `amd.com/gpu`：Pod 需要的 AMD GPU 数量。
- `amd.com/gpumem`：每张 GPU 的显存配额，单位为 MiB。
- `amd.com/gpucores`：每张 GPU 的 CU 配额百分比，范围为 0--100；例如 `25` 在 304 CU 的设备上约分配 76 CU。

以下示例请求一张 GPU 的 48 GiB 显存和 25% CU：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: amd-vgpu-example
spec:
  schedulerName: hami-scheduler
  restartPolicy: Never
  containers:
    - name: pytorch
      image: rocm/pytorch:latest
      command: ["bash", "-c"]
      args:
        - |
          env | grep -E 'LD_AUDIT|HIP_DEVICE_MEMORY_LIMIT'
          python3 -c 'import torch; print(torch.cuda.mem_get_info(0)); print(torch.cuda.get_device_name(0))'
          sleep 300
      resources:
        requests:
          amd.com/gpu: 1
          amd.com/gpumem: 49152
          amd.com/gpucores: 25
        limits:
          amd.com/gpu: 1
          amd.com/gpumem: 49152
          amd.com/gpucores: 25
```

```bash
kubectl apply -f amd-vgpu-example.yaml
kubectl get pod amd-vgpu-example -o wide
kubectl logs amd-vgpu-example
```

成功时，日志应包含类似内容：

```text
LD_AUDIT=/usr/local/vgpu/libamvgpu.so
HIP_DEVICE_MEMORY_LIMIT=49152m
(51539607552, 51539607552)
AMD Instinct MI300X VF
```

`51539607552` 为字节，约等于 48 GiB。可同时提交两个相同的 48 GiB / 25% CU 工作负载，验证共享和并发。

## 常见问题

| 症状 | 处理 |
| --- | --- |
| `node unregistered` | 检查 node-labeller 是否运行，以及 `hami.io/node-amd-register` 是否包含 `devmem`、`devcore`。必要时重启 vGPU device-plugin。 |
| `CardInsufficientMemory` | Pod 请求的显存超过设备剩余显存；降低 `amd.com/gpumem` 或等待其他工作负载结束。 |
| `insufficient free CUs` | 删除已完成的 AMD vGPU 测试 Pod，并重启 vGPU device-plugin 清理过期分配。 |
| 容器内显存仍为物理卡大小 | 检查 Pod 环境是否有 `LD_AUDIT` 和 `HIP_DEVICE_MEMORY_LIMIT`。 |

测试完成后删除工作负载：

```bash
kubectl delete pod amd-vgpu-example
```
