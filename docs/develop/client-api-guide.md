# Client API Guide

This guide provides an overview of how to interact with HAMi programmatically using client libraries in various programming languages.

## Overview

HAMi (Heterogeneous AI Computing Virtualization Middleware) integrates deeply with Kubernetes to manage heterogeneous devices. While HAMi doesn't expose a standalone REST API separate from Kubernetes, you can interact with HAMi-managed resources through the Kubernetes API using standard Kubernetes client libraries.

## How HAMi Works

HAMi extends Kubernetes through:

1. **Device Plugins**: Register devices with Kubernetes nodes
2. **Scheduler Extender**: Filter and bind pods based on device availability
3. **Mutating Webhook**: Validate and modify pod specifications
4. **Annotations**: Store device information and scheduling decisions

## Client Libraries

We provide example client implementations in multiple languages:

### Python

The Python client provides a high-level interface for common HAMi operations.

**Location**: `examples/client/python/`

**Key Features**:
- Query GPU/NPU nodes and device information
- Create pods with device resource requests
- Monitor scheduling decisions
- Parse HAMi annotations

**Quick Start**:
```bash
pip install kubernetes
python examples/client/python/hami_client.py
```

[Python Client Documentation →](../../examples/client/python/README.md)

### Java

The Java client provides a type-safe interface for HAMi operations.

**Location**: `examples/client/java/`

**Key Features**:
- Type-safe API using Kubernetes Java client
- Maven/Gradle integration
- Comprehensive examples

**Quick Start**:
```bash
cd examples/client/java
mvn clean package
mvn exec:java
```

[Java Client Documentation →](../../examples/client/java/README.md)

## Common Operations

### 1. Listing GPU Nodes

Query all nodes with HAMi-managed devices:

**Python**:
```python
from hami_client import HAMiClient

hami = HAMiClient()
nodes = hami.list_gpu_nodes()
for node in nodes:
    print(f"{node['name']}: {node['devices']}")
```

**Java**:
```java
HAMiClient hami = new HAMiClient();
List<NodeInfo> nodes = hami.listGPUNodes("gpu=on");
for (NodeInfo node : nodes) {
    System.out.println(node.name + ": " + node.devices);
}
```

### 2. Creating GPU Workloads

Create a pod with specific device requirements:

**Python**:
```python
pod = hami.create_gpu_pod(
    pod_name="training-job",
    namespace="default",
    image="tensorflow/tensorflow:latest-gpu",
    gpu_count=2,
    gpu_memory_mb=8000,
    gpu_cores=100
)
```

**Java**:
```java
PodInfo pod = hami.createGPUPod(
    "training-job",
    "default",
    "tensorflow/tensorflow:latest-gpu",
    2,      // GPU count
    8000,   // Memory MB
    100,    // Cores %
    null    // Command
);
```

### 3. Monitoring Scheduling

Check where devices were allocated:

**Python**:
```python
info = hami.get_pod_scheduling_info("training-job", "default")
print(f"Node: {info['node']}")
print(f"Devices: {info['allocated_devices']}")
```

**Java**:
```java
SchedulingInfo info = hami.getPodSchedulingInfo("training-job", "default");
System.out.println("Node: " + info.node);
System.out.println("Devices: " + info.allocatedDevices);
```

## Resource Specifications

When creating pods, specify device requirements using Kubernetes resource limits:

| Resource Key | Description | Example Values |
|--------------|-------------|----------------|
| `nvidia.com/gpu` | Number of GPUs | `1`, `2`, `4` |
| `nvidia.com/gpumem` | GPU memory (MB) | `3000`, `8000`, `16000` |
| `nvidia.com/gpucores` | GPU cores (%) | `25`, `50`, `100` |

For other device types, replace `nvidia.com` with the appropriate prefix (e.g., `cambricon.com/mlu`, `hygon.com/dcu`).

## Understanding HAMi Annotations

HAMi uses Kubernetes annotations to store metadata:

### Node Annotations

```yaml
metadata:
  annotations:
    hami.io/node-handshake-nvidia: "Reported 2024-01-23 04:30:04"
    hami.io/node-nvidia-register: "GPU-uuid,split,memory,cores,type,numa,healthy:..."
```

- **Handshake**: Timestamp showing when device plugin last reported
- **Register**: Device specifications in a colon-separated list

### Pod Annotations

```yaml
metadata:
  annotations:
    hami.io/devices-to-allocate: "GPU-uuid,type,memory,cores:..."
    hami.io/vgpu-node: "node-1"
    hami.io/vgpu-time: "1705054796"
```

- **devices-to-allocate**: Which devices are allocated to the pod
- **vgpu-node**: Node where pod is scheduled
- **vgpu-time**: Unix timestamp of scheduling decision

## Authentication

Client libraries use standard Kubernetes authentication:

1. **In-Cluster**: Automatic when running inside Kubernetes
2. **Kubeconfig**: Uses `~/.kube/config` by default
3. **Custom**: Provide credentials programmatically

## Supported Languages

Currently, we provide examples for:
- ✅ Python
- ✅ Java

The same patterns can be applied to other languages with Kubernetes client libraries:
- Go (use `pkg/util/client` directly)
- JavaScript/TypeScript
- .NET/C#
- Ruby
- Rust

## Developing Custom Clients

To create a client in another language:

1. **Use Kubernetes Client**: Use the official Kubernetes client library for your language
2. **Query Nodes**: List nodes with label `gpu=on` and parse `hami.io/*` annotations
3. **Create Pods**: Create pods with appropriate resource limits
4. **Monitor**: Watch pod annotations for scheduling decisions

See the [API Reference](api-reference.md) for detailed annotation formats.

## REST API Endpoints

HAMi scheduler exposes these endpoints for Kubernetes integration:

- `POST /filter` - Filter nodes for pod placement
- `POST /bind` - Bind pod to node
- `POST /webhook` - Admission webhook
- `GET /healthz` - Health check

**Note**: These endpoints are designed for Kubernetes internal use. For application integration, use Kubernetes client libraries as shown in the examples.

## Best Practices

1. **Use Labels**: Ensure GPU nodes have the `gpu=on` label
2. **Set Resource Limits**: Always specify resource requirements
3. **Monitor Annotations**: Check pod annotations after scheduling
4. **Handle Errors**: Implement retry logic for transient failures
5. **Use Namespaces**: Organize workloads by team or project

## Troubleshooting

### Pods Not Scheduling

**Symptoms**: Pods remain in `Pending` state

**Solutions**:
1. Check node labels: `kubectl get nodes --show-labels`
2. Verify device registration: `kubectl get node <name> -o yaml | grep hami.io`
3. Check resource availability: Ensure requested resources are available
4. Review scheduler logs: `kubectl logs -n kube-system deployment/hami-scheduler`

### Missing Device Information

**Symptoms**: Node annotations don't show device information

**Solutions**:
1. Verify device plugin is running: `kubectl get pods -n kube-system | grep device-plugin`
2. Check device plugin logs: `kubectl logs -n kube-system <device-plugin-pod>`
3. Ensure devices are properly installed on nodes
4. Check handshake timestamps are recent (within 5 minutes)

### Authentication Errors

**Symptoms**: Client fails to connect to Kubernetes

**Solutions**:
1. Verify kubeconfig is valid: `kubectl cluster-info`
2. Check permissions: Ensure service account has appropriate RBAC
3. Test with kubectl: Verify basic Kubernetes operations work
4. Review client configuration: Ensure correct kubeconfig path

## Examples Repository

All examples are available in the `examples/client/` directory:

```
examples/client/
├── README.md           # Overview and common use cases
├── python/            # Python client example
│   ├── README.md
│   ├── hami_client.py
│   └── requirements.txt
└── java/              # Java client example
    ├── README.md
    ├── HAMiClient.java
    └── pom.xml
```

## Further Reading

- [API Reference](api-reference.md) - Detailed API documentation
- [Protocol Documentation](protocol.md) - HAMi protocol specifications
- [Scheduler Policy](scheduler-policy.md) - Scheduling algorithms
- [Configuration Guide](../config.md) - HAMi configuration options

## Contributing

We welcome contributions of client examples in other languages! See [CONTRIBUTING.md](../../CONTRIBUTING.md) for guidelines.

To contribute a new client example:

1. Create a directory under `examples/client/<language>/`
2. Implement the core client functionality
3. Add comprehensive documentation
4. Provide runnable examples
5. Submit a pull request

## Support

For questions or issues:
- [GitHub Discussions](https://github.com/Project-HAMi/HAMi/discussions)
- [Slack Channel](https://cloud-native.slack.com/archives/C07T10BU4R2)
- [Mailing List](https://groups.google.com/forum/#!forum/hami-project)
