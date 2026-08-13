# 昆仑芯 XPU 支持

HAMi 支持在异构 AI 集群中使用昆仑芯 XPU（例如 P800），并提供显存隔离和算力隔离能力。

## 前提条件

- 兼容的昆仑芯 XPU 设备（如 P800）
- 主机上已正确安装昆仑芯驱动和运行环境
- 配置了昆仑芯设备插件（如适用），将设备暴露给 Kubernetes

## 资源分配

您可以在 Pod 规范中使用以下标签来请求昆仑芯 XPU 资源：

- `kunlunxin.com/xpu`：请求物理昆仑芯 XPU 数量。
- `kunlunxin.com/vxpu`：请求虚拟昆仑芯 XPU 算力（核心）分配。
- `kunlunxin.com/vxpu-memory`：请求虚拟昆仑芯 XPU 显存分配。

### 示例

以下是请求昆仑芯虚拟 XPU 资源的 Pod 规范示例：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kunlunxin-test-pod
spec:
  containers:
  - name: test-container
    image: your-kunlunxin-image:latest
    resources:
      limits:
        kunlunxin.com/xpu: 1
        kunlunxin.com/vxpu-memory: 4000
```

## 已知限制

- 目前尚不支持昆仑芯的多卡模式。
- 不支持动态 MIG 或类似的原生动态分区。
