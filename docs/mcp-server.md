# HAMi MCP Server

The HAMi MCP Server provides read-only access to GPU scheduling state in Kubernetes clusters through the Model Context Protocol (MCP). This allows AI assistants like Claude Desktop, Claude Code, and Cursor to query GPU node status, pod allocations, and metrics.

> This page is the **usage guide** (install, tools, client config, troubleshooting).
> For how the server is **designed** (architecture, data flow, diagrams), see
> [`mcp-server-design.md`](./mcp-server-design.md).

## Features

- **Read-only access**: All operations are read-only, ensuring cluster safety
- **GPU-aware queries**: Specialized tools for GPU node and pod information
- **Prometheus integration**: Query HAMi metrics directly
- **Metrics endpoint**: Exposes `/metrics` for Prometheus scraping when enabled
- **Security**: RBAC-controlled access with automatic secret redaction

## Installation

The MCP server is **disabled by default**. A single boolean `mcpServer.enabled` controls whether the MCP server is deployed, including its metrics exposure.

### Enable via Helm values

```yaml
# values.yaml
mcpServer:
  enabled: true
  prometheusUrl: "http://prometheus.monitoring:9090"
  logLevel: "info"
```

### Enable via Helm flag

```bash
helm install hami charts/hami \
  --set mcpServer.enabled=true \
  --set mcpServer.prometheusUrl="http://prometheus.monitoring:9090"
```

When `mcpServer.enabled` is `true`, the following resources are created:

| Resource | Description |
|---|---|
| Deployment | Runs the MCP server container |
| Service | ClusterIP Service exposing port 9395 (MCP + metrics) |
| ServiceAccount | Dedicated SA for the MCP server |
| ClusterRole | Read-only access to nodes, pods, namespaces, configmaps |
| ClusterRoleBinding | Binds the ClusterRole to the ServiceAccount |
| ServiceMonitor | Prometheus ServiceMonitor (requires `prometheus.enabled=true` and ServiceMonitor CRD) |

When `mcpServer.enabled` is `false` (default), **none** of these resources are created.

### Prometheus ServiceMonitor

If you also want Prometheus to auto-discover the MCP server's metrics endpoint:

```yaml
mcpServer:
  enabled: true
prometheus:
  enabled: true
```

This creates a ServiceMonitor that scrapes `/metrics` on port 9395.

### Docker Image

Build the MCP server image:

```bash
make docker-mcp
```

## Available Tools

### list_gpu_nodes

List Kubernetes nodes with GPU resources.

**Input:**
- `labelSelector` (optional): Label selector to filter nodes (e.g., `gpu=on`)

**Output:** Array of node objects with:
- `name`: Node name
- `gpuVendor`: GPU vendor (NVIDIA, Cambricon, Hygon, etc.)
- `gpuCount`: Number of GPUs
- `allocatedMemoryGB`: Allocated GPU memory in GB
- `totalMemoryGB`: Total GPU memory in GB
- `allocatedCoresPct`: Allocated GPU cores percentage

### list_gpu_pods

List pods that have GPU resource requests.

**Input:**
- `namespace` (optional): Namespace to filter pods
- `phase` (optional): Pod phase filter (Running, Pending, Succeeded, Failed, Unknown)

**Output:** Array of pod objects with:
- `namespace`: Pod namespace
- `name`: Pod name
- `node`: Node where pod is running
- `requestedGPU`: Number of requested GPUs
- `allocatedDeviceUUIDs`: Allocated GPU device UUIDs
- `status`: Pod status

### get_quota_usage

Get GPU quota usage for a namespace.

**Input:**
- `namespace` (required): Namespace to check quota usage for

**Output:** Quota usage object with:
- `namespace`: Namespace name
- `gpuMemoryUsed`: GPU memory used in GB
- `gpuMemoryQuota`: GPU memory quota in GB
- `gpuCoreUsed`: GPU cores used
- `gpuCoreQuota`: GPU cores quota

### get_gpu_metrics

Get GPU metrics from Prometheus.

**Input:**
- `metric` (required): Prometheus metric name (e.g., `hami_gpu_memory_allocated_bytes`)
- `node` (optional): Node name to filter metrics

**Output:** Array of metric objects with:
- `metric`: Metric labels
- `value`: Metric value
- `time`: Timestamp

### describe_node

Describe a Kubernetes node with GPU details.

**Input:**
- `node` (required): Node name to describe

**Output:** Node description with:
- `name`: Node name
- `labels`: Node labels
- `annotations`: Node annotations (redacted)
- `gpuDevices`: List of GPU devices
- `capacity`: Node capacity
- `allocatable`: Node allocatable resources

## Usage

### Claude Desktop (stdio)

Add to your Claude Desktop configuration (`~/.claude/claude_desktop_config.json`):

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

### Claude Code (stdio)

Add to your Claude Code settings:

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

### In-Cluster HTTP Mode

When deployed via Helm, the MCP server runs in HTTP mode by default. The MCP endpoint is available at:

```
http://<service-name>:9395/mcp
```

You can connect to it from any MCP-compatible client that supports streamable HTTP transport.

### Health Check

The server exposes a health check endpoint:

```
http://<service-name>:9395/healthz
```

### Metrics

When `mcpServer.enabled` is `true`, the server exposes Prometheus metrics at:

```
http://<service-name>:9395/metrics
```

## Helm Values Reference

| Parameter | Description | Default |
|---|---|---|
| `mcpServer.enabled` | Enable MCP server deployment and metrics | `false` |
| `mcpServer.image.repository` | MCP server image repository | `hami-mcp` |
| `mcpServer.image.tag` | MCP server image tag (defaults to `global.imageTag`) | `""` |
| `mcpServer.image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `mcpServer.prometheusUrl` | Prometheus URL for metrics queries | `http://localhost:9090` |
| `mcpServer.logLevel` | Log level (debug, info, warn, error) | `info` |
| `mcpServer.resources.limits.cpu` | CPU limit | `100m` |
| `mcpServer.resources.limits.memory` | Memory limit | `128Mi` |
| `mcpServer.resources.requests.cpu` | CPU request | `50m` |
| `mcpServer.resources.requests.memory` | Memory request | `64Mi` |

## Command Line Flags

| Flag | Description | Default |
|---|---|---|
| `--kubeconfig` | Path to kubeconfig file (in-cluster if empty) | `""` |
| `--prometheus-url` | Prometheus server URL | `http://localhost:9090` |
| `--log-level` | Log level (debug, info, warn, error) | `info` |
| `--listen-addr` | HTTP listen address (e.g. `:9395`). If empty, runs over stdio | `""` |
| `--metrics-port` | Port for scheduler metrics | `9395` |
| `--metrics-enabled` | Enable `/metrics` endpoint | `false` |
| `--version` | Print version and exit | |

## Security Model

### Read-only Access

The MCP server only performs read operations:
- `get`, `list`, `watch` on nodes, pods, namespaces, configmaps
- No write operations (create, update, delete, patch)

### RBAC

The server uses a dedicated ServiceAccount with minimal permissions:
- ClusterRole with read-only access to specific resources
- No access to secrets
- No write verbs

### Output Redaction

Sensitive information is automatically redacted:
- Environment variables containing tokens, secrets, passwords, keys
- Annotation keys matching sensitive patterns
- `imagePullSecrets`
- Secret volumes

## Limitations

- **Read-only**: No write operations are supported
- **No authentication**: Uses ServiceAccount RBAC only
- **No caching**: Each call queries live K8s/Prometheus

## Troubleshooting

### Server won't start

1. Check kubeconfig is valid: `kubectl cluster-info`
2. Verify RBAC permissions: `kubectl auth can-i list nodes --as=system:serviceaccount:<namespace>:<release>-hami-mcp-server`
3. Check Prometheus URL: `curl http://prometheus:9090/api/v1/status/config`

### Tools return errors

1. Check server logs: `kubectl logs deploy/<release>-hami-mcp-server`
2. Verify HAMi is installed: `kubectl get pods -n <namespace>`
3. Check node GPU resources: `kubectl get nodes -o json | jq '.items[].status.capacity'`

### No GPU nodes found

1. Verify GPU drivers are installed: `nvidia-smi`
2. Check node labels: `kubectl get nodes --show-labels`
3. Verify HAMi device plugin is running: `kubectl get pods -n <namespace> | grep device-plugin`

## Development

### Building

```bash
make mcp-server
make docker-mcp
```

### Testing

```bash
go test ./pkg/mcp/...
```
