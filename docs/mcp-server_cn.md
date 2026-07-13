# HAMi MCP Server

HAMi MCP Server 通过模型上下文协议（MCP）提供对 Kubernetes 集群中 GPU 调度状态的只读访问。支持 Claude Desktop、Claude Code、Cursor 等 AI 助手查询 GPU 节点状态、Pod 分配情况和监控指标。

## 功能特性

- **只读访问**：所有操作均为只读，确保集群安全
- **GPU 感知查询**：专门用于查询 GPU 节点和 Pod 信息的工具
- **Prometheus 集成**：直接查询 HAMi 指标
- **Metrics 端点**：启用后暴露 `/metrics` 供 Prometheus 抓取
- **安全性**：基于 RBAC 的访问控制，自动脱敏敏感信息

## 安装部署

MCP Server **默认关闭**。通过一个布尔值 `mcpServer.enabled` 统一控制 MCP Server 的部署及其 Metrics 暴露。

### 通过 Helm values 启用

```yaml
# values.yaml
mcpServer:
  enabled: true
  prometheusUrl: "http://prometheus.monitoring:9090"
  logLevel: "info"
```

### 通过 Helm 参数启用

```bash
helm install hami charts/hami \
  --set mcpServer.enabled=true \
  --set mcpServer.prometheusUrl="http://prometheus.monitoring:9090"
```

当 `mcpServer.enabled` 为 `true` 时，将创建以下资源：

| 资源 | 说明 |
|---|---|
| Deployment | 运行 MCP Server 容器 |
| Service | ClusterIP 类型 Service，暴露 9395 端口（MCP + Metrics） |
| ServiceAccount | MCP Server 专用 ServiceAccount |
| ClusterRole | 只读权限，可访问 nodes、pods、namespaces、configmaps |
| ClusterRoleBinding | 将 ClusterRole 绑定到 ServiceAccount |
| ServiceMonitor | Prometheus ServiceMonitor（需要同时开启 `prometheus.enabled` 且集群安装了 ServiceMonitor CRD） |

当 `mcpServer.enabled` 为 `false`（默认值）时，**不会创建任何上述资源**。

### Prometheus ServiceMonitor

如果需要 Prometheus 自动发现 MCP Server 的 Metrics 端点：

```yaml
mcpServer:
  enabled: true
prometheus:
  enabled: true
```

这会创建一个 ServiceMonitor，抓取 9395 端口上的 `/metrics` 路径。

### 构建镜像

```bash
make docker-mcp
```

## 可用工具

### list_gpu_nodes

列出拥有 GPU 资源的 Kubernetes 节点。

**输入参数：**
- `labelSelector`（可选）：用于过滤节点的标签选择器（如 `gpu=on`）

**输出：** 节点对象数组，包含：
- `name`：节点名称
- `gpuVendor`：GPU 厂商（NVIDIA、Cambricon、Hygon 等）
- `gpuCount`：GPU 数量
- `allocatedMemoryGB`：已分配 GPU 显存（GB）
- `totalMemoryGB`：GPU 总显存（GB）
- `allocatedCoresPct`：已分配 GPU 算力百分比

### list_gpu_pods

列出有 GPU 资源请求的 Pod。

**输入参数：**
- `namespace`（可选）：按命名空间过滤 Pod
- `phase`（可选）：按 Pod 阶段过滤（Running、Pending、Succeeded、Failed、Unknown）

**输出：** Pod 对象数组，包含：
- `namespace`：Pod 命名空间
- `name`：Pod 名称
- `node`：Pod 运行的节点
- `requestedGPU`：请求的 GPU 数量
- `allocatedDeviceUUIDs`：已分配的 GPU 设备 UUID
- `status`：Pod 状态

### get_quota_usage

获取命名空间的 GPU 配额使用情况。

**输入参数：**
- `namespace`（必填）：要查询配额使用情况的命名空间

**输出：** 配额使用对象，包含：
- `namespace`：命名空间名称
- `gpuMemoryUsed`：已使用 GPU 显存（GB）
- `gpuMemoryQuota`：GPU 显存配额（GB）
- `gpuCoreUsed`：已使用 GPU 算力
- `gpuCoreQuota`：GPU 算力配额

### get_gpu_metrics

从 Prometheus 获取 GPU 指标。

**输入参数：**
- `metric`（必填）：Prometheus 指标名称（如 `hami_gpu_memory_allocated_bytes`）
- `node`（可选）：按节点名称过滤指标

**输出：** 指标对象数组，包含：
- `metric`：指标标签
- `value`：指标值
- `time`：时间戳

### describe_node

查看 Kubernetes 节点的 GPU 详细信息。

**输入参数：**
- `node`（必填）：要查看的节点名称

**输出：** 节点描述，包含：
- `name`：节点名称
- `labels`：节点标签
- `annotations`：节点注解（已脱敏）
- `gpuDevices`：GPU 设备列表
- `capacity`：节点容量
- `allocatable`：节点可分配资源

## 使用方式

### Claude Desktop（stdio 模式）

在 Claude Desktop 配置文件（`~/.claude/claude_desktop_config.json`）中添加：

```json
{
  "mcpServers": {
    "hami": {
      "command": "/path/to/mcp-server",
      "args": ["--kubeconfig=/path/to/kubeconfig"]
    }
  }
}
```

### Claude Code（stdio 模式）

在 Claude Code 设置中添加：

```json
{
  "mcp": {
    "servers": {
      "hami": {
        "command": "/path/to/mcp-server",
        "args": ["--kubeconfig=/path/to/kubeconfig"]
      }
    }
  }
}
```

### 集群内 HTTP 模式

通过 Helm 部署时，MCP Server 默认以 HTTP 模式运行。MCP 端点地址为：

```
http://<service-name>:9395/mcp
```

任何支持 Streamable HTTP 传输的 MCP 兼容客户端均可连接。

### 健康检查

Server 暴露健康检查端点：

```
http://<service-name>:9395/healthz
```

### Metrics 端点

当 `mcpServer.enabled` 为 `true` 时，Server 在以下地址暴露 Prometheus 指标：

```
http://<service-name>:9395/metrics
```

## Helm 参数说明

| 参数 | 说明 | 默认值 |
|---|---|---|
| `mcpServer.enabled` | 启用 MCP Server 部署和 Metrics 暴露 | `false` |
| `mcpServer.image.repository` | MCP Server 镜像仓库 | `hami-mcp` |
| `mcpServer.image.tag` | MCP Server 镜像标签（默认使用 `global.imageTag`） | `""` |
| `mcpServer.image.pullPolicy` | 镜像拉取策略 | `IfNotPresent` |
| `mcpServer.prometheusUrl` | 用于查询指标的 Prometheus 地址 | `http://localhost:9090` |
| `mcpServer.logLevel` | 日志级别（debug、info、warn、error） | `info` |
| `mcpServer.resources.limits.cpu` | CPU 限制 | `100m` |
| `mcpServer.resources.limits.memory` | 内存限制 | `128Mi` |
| `mcpServer.resources.requests.cpu` | CPU 请求 | `50m` |
| `mcpServer.resources.requests.memory` | 内存请求 | `64Mi` |

## 命令行参数

| 参数 | 说明 | 默认值 |
|---|---|---|
| `--kubeconfig` | kubeconfig 文件路径（为空时使用集群内配置） | `""` |
| `--prometheus-url` | Prometheus 服务器地址 | `http://localhost:9090` |
| `--log-level` | 日志级别（debug、info、warn、error） | `info` |
| `--listen-addr` | HTTP 监听地址（如 `:9395`），为空时使用 stdio 模式 | `""` |
| `--metrics-port` | Scheduler 指标端口 | `9395` |
| `--metrics-enabled` | 启用 `/metrics` 端点 | `false` |
| `--version` | 打印版本信息并退出 | |

## 安全模型

### 只读访问

MCP Server 仅执行读操作：
- 对 nodes、pods、namespaces、configmaps 执行 `get`、`list`、`watch`
- 无写操作（create、update、delete、patch）

### RBAC

Server 使用专用 ServiceAccount，权限最小化：
- ClusterRole 仅包含特定资源的只读权限
- 无 secrets 访问权限
- 无写操作权限

### 输出脱敏

敏感信息会被自动脱敏：
- 包含 token、secret、password、key 的环境变量
- 匹配敏感模式的注解键
- `imagePullSecrets`
- Secret 类型的 Volume

## 使用限制

- **只读**：不支持任何写操作
- **无认证**：仅使用 ServiceAccount RBAC
- **无缓存**：每次调用直接查询 K8s API / Prometheus

## 故障排查

### Server 无法启动

1. 检查 kubeconfig 是否有效：`kubectl cluster-info`
2. 验证 RBAC 权限：`kubectl auth can-i list nodes --as=system:serviceaccount:<namespace>:<release>-hami-mcp-server`
3. 检查 Prometheus 地址：`curl http://prometheus:9090/api/v1/status/config`

### 工具返回错误

1. 查看 Server 日志：`kubectl logs deploy/<release>-hami-mcp-server`
2. 验证 HAMi 已安装：`kubectl get pods -n <namespace>`
3. 检查节点 GPU 资源：`kubectl get nodes -o json | jq '.items[].status.capacity'`

### 未找到 GPU 节点

1. 验证 GPU 驱动已安装：`nvidia-smi`
2. 检查节点标签：`kubectl get nodes --show-labels`
3. 验证 HAMi Device Plugin 正在运行：`kubectl get pods -n <namespace> | grep device-plugin`

## 开发指南

### 构建

```bash
make mcp-server
make docker-mcp
```

### 测试

```bash
go test ./pkg/mcp/...
```
