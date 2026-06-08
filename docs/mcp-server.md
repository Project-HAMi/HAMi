# HAMi MCP Server

The HAMi MCP Server provides read-only access to GPU scheduling state in Kubernetes clusters through the Model Context Protocol (MCP). This allows AI assistants like Claude Desktop, Claude Code, and Cursor to query GPU node status, pod allocations, and metrics.

## Features

- **Read-only access**: All operations are read-only, ensuring cluster safety
- **GPU-aware queries**: Specialized tools for GPU node and pod information
- **Prometheus integration**: Query HAMi metrics directly
- **Security**: RBAC-controlled access with automatic secret redaction

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

## Installation

### Helm Chart

The MCP server is disabled by default. Enable it in your Helm values:

```yaml
mcpServer:
  enabled: true
  image:
    repository: "hami-mcp"
    tag: "v0.1.0"
  prometheusUrl: "http://prometheus:9090"
  logLevel: "info"
```

Install with Helm:

```bash
helm install hami charts/hami --set mcpServer.enabled=true
```

### Docker Image

Build the MCP server image:

```bash
make docker-mcp
```

## Usage

### Claude Desktop Configuration

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

### Claude Code Configuration

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

### In-Cluster Usage

Exec into the MCP server pod:

```bash
kubectl exec -it deploy/hami-mcp-server -- /mcp-server
```

### Command Line Flags

- `--kubeconfig`: Path to kubeconfig file (default: in-cluster config)
- `--prometheus-url`: URL of Prometheus server (default: `http://localhost:9090`)
- `--log-level`: Log level (debug, info, warn, error) (default: `info`)
- `--metrics-port`: Port for HAMi scheduler metrics (default: `9395`)
- `--version`: Print version and exit

## Limitations

- **stdio transport only**: v1 supports stdio transport for local usage
- **Read-only**: No write operations are supported
- **No authentication**: Uses ServiceAccount RBAC only
- **No caching**: Each call queries live K8s/Prometheus

## Troubleshooting

### Server won't start

1. Check kubeconfig is valid: `kubectl cluster-info`
2. Verify RBAC permissions: `kubectl auth can-i list nodes --as=system:serviceaccount:hami:hami-mcp-server`
3. Check Prometheus URL: `curl http://prometheus:9090/api/v1/status/config`

### Tools return errors

1. Check server logs: `kubectl logs deploy/hami-mcp-server`
2. Verify HAMi is installed: `kubectl get pods -n hami-system`
3. Check node GPU resources: `kubectl get nodes -o json | jq '.items[].status.capacity'`

### No GPU nodes found

1. Verify GPU drivers are installed: `nvidia-smi`
2. Check node labels: `kubectl get nodes --show-labels`
3. Verify HAMi device plugin is running: `kubectl get pods -n hami-system | grep device-plugin`

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

### Linting

```bash
golangci-lint run ./cmd/mcp-server/... ./pkg/mcp/...
```
