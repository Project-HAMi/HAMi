# HAMi MCP Server — Design

> **Bilingual document.** English section first; 中文见文末 [中文设计文档](#hami-mcp-server--设计文档中文).
>
> This is the **design** reference for the HAMi MCP Server: goals, architecture, data
> flow, security model, and roadmap — explained through diagrams. For **how to use** the
> server (install, tools, client config, troubleshooting), see [`mcp-server.md`](./mcp-server.md)
> / [`mcp-server_cn.md`](./mcp-server_cn.md).
>
> It reflects the **as-built** implementation in `pkg/mcp/` and `cmd/mcp-server/`.

---

## 1. Goals & Non-Goals

The HAMi MCP Server exposes **read-only** GPU scheduling state of a Kubernetes cluster
through the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/), so AI
assistants (Claude Desktop, Claude Code, Cursor, any streamable-HTTP MCP client) can
answer questions about GPU nodes, pod allocations, quota usage, HAMi metrics, and
scheduler configuration.

| | |
|---|---|
| **Primary audience** | Cluster operators debugging GPU scheduling with an AI assistant |
| **Secondary audience** | Developers building tooling around HAMi |
| **Out of scope** | End users *requesting* GPUs (they use `kubectl` / pod specs) |

**Non-goals (v1):** write operations of any kind, authentication beyond ServiceAccount
RBAC, per-user/multi-tenant request scoping, a caching layer, custom MCP prompts, and
streaming long-running results.

## 2. Capability Boundary — read-only, by design

Read-only is not a convention; it is enforced at **three independent layers**, so a
single mistake at one layer cannot make the server mutating.

```
                    ┌───────────────────────────────────────────┐
                    │        Read-only enforced 3 ways           │
                    ├───────────────────────────────────────────┤
   ① Code           │  no .Create/.Update/.Delete/.Patch calls   │
                    │  in pkg/mcp/ or cmd/mcp-server/            │
                    ├───────────────────────────────────────────┤
   ② RBAC           │  ClusterRole: get/list/watch only;         │
                    │  no write verbs; no `secrets` access       │
                    ├───────────────────────────────────────────┤
   ③ Naming         │  no tool named bind/allocate/mutate/       │
                    │  apply/delete/create                       │
                    └───────────────────────────────────────────┘
```

| Allowed | Forbidden |
|---|---|
| List GPU nodes / pods / quotas | Bind pods or mutate webhook decisions |
| Read Prometheus metrics | Trigger device-plugin allocation |
| Describe node & HAMi config | Modify HAMi config or any K8s resource |

## 3. Architecture

**Decision:** a **standalone binary** `cmd/mcp-server` that talks only to the **Kubernetes
API** and **Prometheus**, treating HAMi itself as a black box.

```
                Rejected alternatives                     Chosen
   ┌────────────────────────────────┐        ┌──────────────────────────────┐
   │ embed into scheduler binary     │  ✗     │ standalone cmd/mcp-server      │  ✓
   │  → couples release cadence      │        │  → independent release cadence │
   ├────────────────────────────────┤        │  → HAMi treated as black box   │
   │ scheduler sidecar               │  ✗     │  → reads K8s API + Prometheus  │
   │  → extra deployment complexity  │        │    only                        │
   └────────────────────────────────┘        └──────────────────────────────┘
```

**SDK:** the official
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
(`pkg/mcp/server.go`). Selected over `mark3labs/mcp-go` and `metoro-io/mcp-golang` for
client-go compatibility, single-SDK stdio + HTTP transport, active maintenance, and an
Apache-2.0-compatible license.

### 3.1 Component diagram

```
        ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
        │ Claude Desktop│   │  Claude Code  │   │    Cursor    │   MCP clients
        └──────┬───────┘   └──────┬───────┘   └──────┬───────┘
               │  stdio           │  HTTP            │  HTTP
               └──────────────────┼──────────────────┘
                                  ▼
              ┌──────────────────────────────────────────┐
              │            cmd/mcp-server                 │
              │  main.go  → stdio (Run) | HTTP (RunHTTP)  │
              │  ┌──────────────────────────────────────┐ │
              │  │            pkg/mcp/server.go          │ │
              │  │   register 5 tools + 1 resource       │ │
              │  └───┬───────────────────────────┬──────┘ │
              │      │                           │        │
              │  ┌───▼─────────┐          ┌──────▼──────┐ │
              │  │ tools/ (×5) │          │ resources/  │ │
              │  └───┬─────────┘          └──────┬──────┘ │
              │      │        redact.Redact()    │        │
              │  ┌───▼───────────────────────────▼──────┐ │
              │  │  client/k8s.go     client/prometheus  │ │
              │  └───┬────────────────────────┬─────────┘ │
              └──────┼────────────────────────┼───────────┘
                     ▼                         ▼
              ┌─────────────┐          ┌──────────────┐
              │ Kubernetes  │          │  Prometheus  │
              │    API      │          │   (metrics)  │
              └─────────────┘          └──────────────┘
```

### 3.2 Package layout (`pkg/mcp/`)

```
cmd/mcp-server/
  main.go          # entrypoint; picks stdio vs HTTP by --listen-addr
  flags.go         # CLI flags (kubeconfig, prometheus-url, log-level, listen-addr, metrics-*)
pkg/mcp/
  server.go        # server constructor, tool/resource registration, Run / RunHTTP
  client/
    k8s.go         # client-go wrapper: nodes, pods, namespaces, configmaps (read-only)
    prometheus.go  # Prometheus HTTP query client (read-only)
  tools/           # the 5 tools (one file each) + unit tests
  resources/
    config.go      # exposes HAMi scheduler ConfigMap as an MCP resource
  redact/
    redact.go      # strips secrets/tokens from all responses
```

### 3.3 Transport selection

The same binary serves both transports; `main.go` picks one based on `--listen-addr`.

```
              --listen-addr == ""                 --listen-addr == ":9395"
        ┌───────────────────────────┐       ┌────────────────────────────────┐
        │        Server.Run          │       │        Server.RunHTTP          │
        │   stdio transport          │       │   streamable HTTP handler       │
        │   (local Claude Desktop /  │       │   serves:  /mcp                 │
        │    Claude Code)            │       │            /healthz             │
        │                            │       │            /metrics (optional)  │
        └───────────────────────────┘       └────────────────────────────────┘
             local / dev usage                    in-cluster Deployment
```

## 4. Request flow

Every tool/resource result passes through `redact.Redact()` before it leaves the process.
Errors are returned as MCP `content` with `isError: true` — the server never panics on
bad input.

```
 MCP client                    mcp-server                  K8s API / Prometheus
     │                              │                              │
     │──── initialize ─────────────▶│                              │
     │◀─── capabilities ────────────│  (tools + resources)         │
     │                              │                              │
     │──── notifications/           │                              │
     │       initialized ──────────▶│                              │
     │                              │                              │
     │──── tools/call ─────────────▶│                              │
     │                              │──── get/list (read-only) ───▶│
     │                              │◀─── raw objects ─────────────│
     │                              │                              │
     │                       redact.Redact()                       │
     │                              │                              │
     │◀─── content ─────────────────│  (or isError:true, clean msg)│
     │                              │                              │
```

> **Streamable HTTP session note:** an HTTP client must `initialize`, capture the
> `Mcp-Session-Id` response header, send `notifications/initialized`, and reuse that
> session id on every subsequent `tools/call`. Single-shot calls are rejected with
> *"invalid during session initialization"*.

## 5. Capabilities

### 5.1 Tools (5)

| Tool | Input | Output |
|---|---|---|
| `list_gpu_nodes` | `{ labelSelector?: string }` | Array of `{ name, gpuVendor, gpuCount, allocatedMemoryGB, totalMemoryGB, allocatedCoresPct }` |
| `list_gpu_pods` | `{ namespace?: string, phase?: Running\|Pending\|Succeeded\|Failed\|Unknown }` | Array of `{ namespace, name, node, requestedGPU, allocatedDeviceUUIDs, status }` |
| `get_quota_usage` | `{ namespace: string }` (required) | `{ namespace, gpuMemoryUsed, gpuMemoryQuota, gpuCoreUsed, gpuCoreQuota }` |
| `get_gpu_metrics` | `{ metric: string, node?: string }` | Array of `{ metric, value, time }` (last value from Prometheus) |
| `describe_node` | `{ node: string }` (required) | `{ name, labels, annotations (redacted), gpuDevices, capacity, allocatable }` |

**Implementation rules (enforced in review / CI):**
- Tools call only `client/k8s.go` or `client/prometheus.go` — never `pkg/scheduler/*` directly.
- Every response runs through `redact.Redact()`.
- Invalid input → `isError: true`, never a panic or stack trace.

### 5.2 Resources (1)

| URI | Name | Content |
|---|---|---|
| `hami://config/scheduler` | HAMi Scheduler Configuration | The HAMi scheduler ConfigMap, JSON, redacted |

## 6. Security model

```
                       redact.Redact() strips before every response
   ┌───────────────────────────────────────────────────────────────┐
   │  container env vars matching  (?i)(token|secret|password|key)  │
   │  annotation keys matching     the same pattern                 │
   │  imagePullSecrets                                              │
   │  pod spec.volumes[*].secret                                    │
   └───────────────────────────────────────────────────────────────┘
```

- **Read-only K8s access:** `get`/`list`/`watch` on `nodes`, `pods`, `namespaces`, `configmaps`. No `create`/`update`/`delete`/`patch` anywhere.
- **RBAC:** dedicated ServiceAccount + ClusterRole; **no** access to `secrets`; **no** write verbs. Verifiable with `kubectl auth can-i`:
  - `create pods` → **no**  ·  `get secrets` → **no**  ·  `list nodes` → **yes**
- **No authentication beyond ServiceAccount RBAC** (no OIDC / per-user scoping in v1).
- **No caching** — every call hits live K8s / Prometheus.
- **CI guards:** a lint check forbids mutating verbs (`grep -rE '\.(Update|Create|Delete|Patch)\('`) in `pkg/mcp/` and `cmd/mcp-server/`.

## 7. Deployment topology

**Disabled by default** (`mcpServer.enabled: false`). When enabled, the Helm chart creates
Deployment + Service (ClusterIP :9395) + ServiceAccount + ClusterRole + ClusterRoleBinding,
and — if `prometheus.enabled=true` and the CRD exists — a ServiceMonitor.

```
   ┌─────────────────────────────── hami-system ───────────────────────────────┐
   │                                                                            │
   │   Deployment/hami-mcp-server ──uses──▶ ServiceAccount ──bound──▶ ClusterRole
   │        │  container: /k8s-vgpu/bin/mcp-server                    (read-only)│
   │        │  --listen-addr=:9395 --metrics-enabled=true                        │
   │        ▼                                                                    │
   │   Service/hami-mcp-server (ClusterIP :9395)                                 │
   │        │        ▲                                                           │
   │        │        └───── ServiceMonitor (optional) ──scrapes /metrics         │
   │   /mcp /healthz /metrics                                                    │
   └────────────────────────────────────────────────────────────────────────────┘
```

> **Runtime image note:** the Helm Deployment runs `/k8s-vgpu/bin/mcp-server`, i.e. it uses
> the **main HAMi image** (which now bundles the `mcp-server` binary), *not* the standalone
> `docker/Dockerfile.mcp` distroless image. Build the standalone image with `make docker-mcp`;
> build the all-in-one image with `make docker`.

## 8. Known limitations & open issues

- **Config resource assumes fixed names.** `resources/config.go` hardcodes namespace
  `hami-system` and ConfigMap `hami-scheduler-config`. Clusters whose scheduler ConfigMap
  has a different name (e.g. `hami-scheduler`) get `configmap not found` on `resources/read`.
  Candidate fix: derive the name from Helm values / a flag.
- **`describe_node.gpuDevices` may be `null`** if the node's HAMi register annotation
  key/shape doesn't match what the tool parses; capacity/allocatable still populate correctly.
- **`get_gpu_metrics` depends on a reachable Prometheus** at `prometheusUrl`; otherwise the
  tool returns `isError: true` (DNS/connection error), which is correct behavior, not a fault.

## 9. Roadmap

- Promote from `experimental` to GA after operator feedback (feature stays opt-in via `mcpServer.enabled`).
- Full E2E suite under `test/e2e/mcp/` (initialize, per-tool, RBAC, redaction, error-handling, concurrency).
- CI: PR-scoped e2e workflow triggered by changes under `cmd/mcp-server/**`, `pkg/mcp/**`, `test/e2e/mcp/**`.
- Fix the config-resource hardcoded name and `describe_node.gpuDevices` parsing.

---
---

# HAMi MCP Server — 设计文档（中文）

> **双语文档。** 英文见文首 [English design](#hami-mcp-server--design)。
>
> 本文是 HAMi MCP Server 的**设计**参考：目标、架构、数据流、安全模型与规划，通过图示讲解。
> **如何使用**（安装、工具、客户端配置、故障排查）请见
> [`mcp-server.md`](./mcp-server.md) / [`mcp-server_cn.md`](./mcp-server_cn.md)。
>
> 内容以 `pkg/mcp/` 与 `cmd/mcp-server/` 的**实际实现**为准。

---

## 1. 目标与非目标

HAMi MCP Server 通过 [模型上下文协议（MCP）](https://modelcontextprotocol.io/) 提供对
Kubernetes 集群 GPU 调度状态的**只读**访问，让 AI 助手（Claude Desktop、Claude Code、Cursor
及任何支持 streamable HTTP 的 MCP 客户端）回答关于 GPU 节点、Pod 分配、配额使用、HAMi 指标与
调度器配置的问题。

| | |
|---|---|
| **主要用户** | 借助 AI 助手排查 GPU 调度问题的集群运维人员 |
| **次要用户** | 围绕 HAMi 构建工具的开发者 |
| **不在范围内** | *申请* GPU 的最终用户（他们使用 `kubectl` / Pod spec） |

**非目标（v1）：** 任何写操作、除 ServiceAccount RBAC 外的认证、按用户/多租户的请求隔离、缓存层、
自定义 MCP prompt、以及长任务的流式返回。

## 2. 能力边界 —— 设计上即只读

只读不是约定，而是在**三个相互独立的层面**强制保证，任何单一层面的失误都无法让服务器变成可写。

```
                    ┌───────────────────────────────────────────┐
                    │           三重只读保证                      │
                    ├───────────────────────────────────────────┤
   ① 代码层         │  pkg/mcp/ 与 cmd/mcp-server/ 中不存在        │
                    │  .Create/.Update/.Delete/.Patch 调用        │
                    ├───────────────────────────────────────────┤
   ② RBAC 层        │  ClusterRole 仅 get/list/watch；            │
                    │  无写动词；无 `secrets` 访问                 │
                    ├───────────────────────────────────────────┤
   ③ 命名层         │  不存在名为 bind/allocate/mutate/           │
                    │  apply/delete/create 的工具                 │
                    └───────────────────────────────────────────┘
```

| 允许 | 禁止 |
|---|---|
| 列出 GPU 节点 / Pod / 配额 | 绑定 Pod 或修改 webhook 决策 |
| 读取 Prometheus 指标 | 触发 device-plugin 分配 |
| 查看节点与 HAMi 配置 | 修改 HAMi 配置或任何 K8s 资源 |

## 3. 架构

**决策：** 独立二进制 `cmd/mcp-server`，仅与 **Kubernetes API** 和 **Prometheus** 通信，把
HAMi 本身当作黑盒。

```
                被否决的方案                              选定方案
   ┌────────────────────────────────┐        ┌──────────────────────────────┐
   │ 内嵌进 scheduler 二进制          │  ✗     │ 独立的 cmd/mcp-server          │  ✓
   │  → 耦合发布节奏                  │        │  → 独立的发布节奏               │
   ├────────────────────────────────┤        │  → 把 HAMi 当作黑盒             │
   │ scheduler sidecar               │  ✗     │  → 仅读取 K8s API + Prometheus │
   │  → 增加部署复杂度                │        │                                │
   └────────────────────────────────┘        └──────────────────────────────┘
```

**SDK：** 官方
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
（见 `pkg/mcp/server.go`）。相较 `mark3labs/mcp-go` 与 `metoro-io/mcp-golang`，其胜在
client-go 兼容性、单一 SDK 同时支持 stdio + HTTP 传输、活跃维护以及 Apache-2.0 兼容许可证。

### 3.1 组件图

```
        ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
        │ Claude Desktop│   │  Claude Code  │   │    Cursor    │   MCP 客户端
        └──────┬───────┘   └──────┬───────┘   └──────┬───────┘
               │  stdio           │  HTTP            │  HTTP
               └──────────────────┼──────────────────┘
                                  ▼
              ┌──────────────────────────────────────────┐
              │            cmd/mcp-server                 │
              │  main.go  → stdio (Run) | HTTP (RunHTTP)  │
              │  ┌──────────────────────────────────────┐ │
              │  │            pkg/mcp/server.go          │ │
              │  │   注册 5 个工具 + 1 个资源             │ │
              │  └───┬───────────────────────────┬──────┘ │
              │      │                           │        │
              │  ┌───▼─────────┐          ┌──────▼──────┐ │
              │  │ tools/ (×5) │          │ resources/  │ │
              │  └───┬─────────┘          └──────┬──────┘ │
              │      │        redact.Redact()    │        │
              │  ┌───▼───────────────────────────▼──────┐ │
              │  │  client/k8s.go     client/prometheus  │ │
              │  └───┬────────────────────────┬─────────┘ │
              └──────┼────────────────────────┼───────────┘
                     ▼                         ▼
              ┌─────────────┐          ┌──────────────┐
              │ Kubernetes  │          │  Prometheus  │
              │    API      │          │   （指标）    │
              └─────────────┘          └──────────────┘
```

### 3.2 包结构（`pkg/mcp/`）

```
cmd/mcp-server/
  main.go          # 入口；根据 --listen-addr 选择 stdio 或 HTTP
  flags.go         # 命令行参数（kubeconfig、prometheus-url、log-level、listen-addr、metrics-*）
pkg/mcp/
  server.go        # 服务器构造、工具/资源注册、Run / RunHTTP
  client/
    k8s.go         # client-go 封装：nodes、pods、namespaces、configmaps（只读）
    prometheus.go  # Prometheus HTTP 查询客户端（只读）
  tools/           # 5 个工具（每个一个文件）+ 单元测试
  resources/
    config.go      # 将 HAMi scheduler ConfigMap 暴露为 MCP 资源
  redact/
    redact.go      # 对所有响应做敏感信息脱敏
```

### 3.3 传输选择

同一个二进制同时支持两种传输，`main.go` 根据 `--listen-addr` 选择其一。

```
              --listen-addr == ""                 --listen-addr == ":9395"
        ┌───────────────────────────┐       ┌────────────────────────────────┐
        │        Server.Run          │       │        Server.RunHTTP          │
        │   stdio 传输               │       │   streamable HTTP handler       │
        │   （本地 Claude Desktop /  │       │   提供：  /mcp                  │
        │     Claude Code）          │       │           /healthz              │
        │                            │       │           /metrics（可选）      │
        └───────────────────────────┘       └────────────────────────────────┘
              本地 / 开发用途                       集群内 Deployment
```

## 4. 请求流程

每个工具/资源的结果在离开进程前都会经过 `redact.Redact()`。错误以 MCP `content` 加
`isError: true` 的形式返回——服务器不会因非法输入而 panic。

```
 MCP 客户端                     mcp-server                  K8s API / Prometheus
     │                              │                              │
     │──── initialize ─────────────▶│                              │
     │◀─── capabilities ────────────│  （工具 + 资源）              │
     │                              │                              │
     │──── notifications/           │                              │
     │       initialized ──────────▶│                              │
     │                              │                              │
     │──── tools/call ─────────────▶│                              │
     │                              │──── get/list（只读）────────▶│
     │                              │◀─── 原始对象 ────────────────│
     │                              │                              │
     │                       redact.Redact()                       │
     │                              │                              │
     │◀─── content ─────────────────│  （或 isError:true + 清晰信息）│
     │                              │                              │
```

> **Streamable HTTP 会话说明：** HTTP 客户端必须先 `initialize`，获取 `Mcp-Session-Id`
> 响应头，发送 `notifications/initialized`，之后每次 `tools/call` 都带上该 session id。
> 单发调用会被拒绝，报 *"invalid during session initialization"*。

## 5. 能力

### 5.1 工具（5 个）

| 工具 | 输入 | 输出 |
|---|---|---|
| `list_gpu_nodes` | `{ labelSelector?: string }` | `{ name, gpuVendor, gpuCount, allocatedMemoryGB, totalMemoryGB, allocatedCoresPct }` 数组 |
| `list_gpu_pods` | `{ namespace?: string, phase?: Running\|Pending\|Succeeded\|Failed\|Unknown }` | `{ namespace, name, node, requestedGPU, allocatedDeviceUUIDs, status }` 数组 |
| `get_quota_usage` | `{ namespace: string }`（必填） | `{ namespace, gpuMemoryUsed, gpuMemoryQuota, gpuCoreUsed, gpuCoreQuota }` |
| `get_gpu_metrics` | `{ metric: string, node?: string }` | `{ metric, value, time }` 数组（Prometheus 最新值） |
| `describe_node` | `{ node: string }`（必填） | `{ name, labels, annotations（已脱敏）, gpuDevices, capacity, allocatable }` |

**实现规则（评审 / CI 中强制）：**
- 工具只调用 `client/k8s.go` 或 `client/prometheus.go`，绝不直接调用 `pkg/scheduler/*`。
- 每个响应都经过 `redact.Redact()`。
- 非法输入 → `isError: true`，绝不 panic 或返回堆栈。

### 5.2 资源（1 个）

| URI | 名称 | 内容 |
|---|---|---|
| `hami://config/scheduler` | HAMi Scheduler Configuration | HAMi scheduler ConfigMap，JSON，已脱敏 |

## 6. 安全模型

```
                       redact.Redact() 在每次响应前脱敏
   ┌───────────────────────────────────────────────────────────────┐
   │  匹配 (?i)(token|secret|password|key) 的容器环境变量           │
   │  匹配同样模式的注解键                                          │
   │  imagePullSecrets                                              │
   │  pod spec.volumes[*].secret                                    │
   └───────────────────────────────────────────────────────────────┘
```

- **K8s 只读访问：** 对 `nodes`、`pods`、`namespaces`、`configmaps` 执行 `get`/`list`/`watch`；任何地方都无 `create`/`update`/`delete`/`patch`。
- **RBAC：** 专用 ServiceAccount + ClusterRole；**无** `secrets` 访问；**无**写动词。可用 `kubectl auth can-i` 验证：
  - `create pods` → **no**  ·  `get secrets` → **no**  ·  `list nodes` → **yes**
- **除 ServiceAccount RBAC 外无额外认证**（v1 无 OIDC / 按用户隔离）。
- **无缓存** —— 每次调用直接查询实时 K8s / Prometheus。
- **CI 守卫：** lint 检查禁止 `pkg/mcp/` 与 `cmd/mcp-server/` 出现写动词（`grep -rE '\.(Update|Create|Delete|Patch)\('`）。

## 7. 部署拓扑

**默认关闭**（`mcpServer.enabled: false`）。启用后，Helm chart 创建 Deployment + Service
（ClusterIP :9395）+ ServiceAccount + ClusterRole + ClusterRoleBinding；若
`prometheus.enabled=true` 且集群装有对应 CRD，还会创建 ServiceMonitor。

```
   ┌─────────────────────────────── hami-system ───────────────────────────────┐
   │                                                                            │
   │   Deployment/hami-mcp-server ──使用──▶ ServiceAccount ──绑定──▶ ClusterRole
   │        │  容器: /k8s-vgpu/bin/mcp-server                        （只读）    │
   │        │  --listen-addr=:9395 --metrics-enabled=true                        │
   │        ▼                                                                    │
   │   Service/hami-mcp-server (ClusterIP :9395)                                 │
   │        │        ▲                                                           │
   │        │        └───── ServiceMonitor（可选）──抓取 /metrics                │
   │   /mcp /healthz /metrics                                                    │
   └────────────────────────────────────────────────────────────────────────────┘
```

> **关于运行镜像：** Helm Deployment 运行的是 `/k8s-vgpu/bin/mcp-server`，即使用**主 HAMi 镜像**
> （现已内置 `mcp-server` 二进制），而**不是**独立的 `docker/Dockerfile.mcp` distroless 镜像。
> 用 `make docker-mcp` 构建独立镜像；用 `make docker` 构建包含全部组件的主镜像。

## 8. 已知限制与待办

- **配置资源假定了固定名称。** `resources/config.go` 硬编码了命名空间 `hami-system` 与 ConfigMap
  `hami-scheduler-config`。若集群的调度器 ConfigMap 名称不同（例如 `hami-scheduler`），`resources/read`
  会返回 `configmap not found`。建议修复：从 Helm values / 参数推导该名称。
- **`describe_node.gpuDevices` 可能为 `null`**，当节点的 HAMi 注册注解 key/结构与工具解析逻辑不匹配时；
  但 capacity/allocatable 仍能正确填充。
- **`get_gpu_metrics` 依赖可达的 Prometheus**（`prometheusUrl`）；否则工具返回 `isError: true`
  （DNS/连接错误），这是正确行为，而非服务器故障。

## 9. 规划

- 收集运维反馈后从 `experimental` 提升到 GA（该特性始终通过 `mcpServer.enabled` 显式开关）。
- 在 `test/e2e/mcp/` 下建立完整 E2E 套件（初始化、逐工具、RBAC、脱敏、错误处理、并发）。
- CI：由 `cmd/mcp-server/**`、`pkg/mcp/**`、`test/e2e/mcp/**` 变更触发的 PR 级 e2e 工作流。
- 修复配置资源硬编码名称与 `describe_node.gpuDevices` 解析问题。
