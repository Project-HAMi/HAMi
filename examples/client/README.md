# HAMi Client API Examples

This directory contains example client implementations for interacting with HAMi (Heterogeneous AI Computing Virtualization Middleware) in different programming languages.

## Overview

HAMi integrates with Kubernetes to provide device virtualization and management for heterogeneous AI accelerators (GPUs, NPUs, etc.). While HAMi primarily works through Kubernetes' scheduler extender and webhook mechanisms, you can interact with HAMi-managed resources using standard Kubernetes APIs.

These examples demonstrate how to:
- Query device information from HAMi-enabled nodes
- Create pods with device resource requests
- Monitor HAMi scheduling decisions
- Manage device allocations

## Available Clients

### [Python Client](python/)

A Python client library using the official Kubernetes Python client.

**Features:**
- Simple, Pythonic API
- Comprehensive examples
- Good for scripting and automation

**Quick Start:**
```bash
cd python
pip install -r requirements.txt
python hami_client.py
```

[Full Python Documentation →](python/README.md)

### [Java Client](java/)

A Java client library using the official Kubernetes Java client.

**Features:**
- Type-safe API
- Enterprise-ready
- Maven/Gradle support

**Quick Start:**
```bash
cd java
mvn clean package
mvn exec:java -Dexec.mainClass="io.hami.client.HAMiClient"
```

[Full Java Documentation →](java/README.md)

## How HAMi Works with Kubernetes

HAMi extends Kubernetes to provide:

1. **Device Registration**: Device plugins register heterogeneous devices with node annotations
2. **Scheduling**: The HAMi scheduler extender filters and prioritizes nodes based on device availability
3. **Binding**: The scheduler binds pods to nodes and records device allocations in pod annotations
4. **Webhook**: A mutating webhook validates and potentially modifies pod specifications

## Common Use Cases

### 1. Query Available Devices

Check which nodes have GPUs and their availability:

**Python:**
```python
from hami_client import HAMiClient
hami = HAMiClient()
nodes = hami.list_gpu_nodes()
```

**Java:**
```java
HAMiClient hami = new HAMiClient();
List<NodeInfo> nodes = hami.listGPUNodes("gpu=on");
```

### 2. Create GPU Workloads

Launch a pod with specific GPU requirements:

**Python:**
```python
pod = hami.create_gpu_pod(
    pod_name="training-job",
    namespace="ml-team",
    image="tensorflow/tensorflow:latest-gpu",
    gpu_count=2,
    gpu_memory_mb=8000,
    gpu_cores=100
)
```

**Java:**
```java
PodInfo pod = hami.createGPUPod(
    "training-job", "ml-team",
    "tensorflow/tensorflow:latest-gpu",
    2, 8000, 100, null
);
```

### 3. Monitor Scheduling Decisions

Track where and how devices are allocated:

**Python:**
```python
scheduling_info = hami.get_pod_scheduling_info("training-job", "ml-team")
print(f"Allocated to: {scheduling_info['node']}")
print(f"Devices: {scheduling_info['allocated_devices']}")
```

**Java:**
```java
SchedulingInfo info = hami.getPodSchedulingInfo("training-job", "ml-team");
System.out.println("Allocated to: " + info.node);
System.out.println("Devices: " + info.allocatedDevices);
```

## Resource Specifications

When creating pods, specify device requirements using Kubernetes resource limits:

| Resource | Description | Example |
|----------|-------------|---------|
| `nvidia.com/gpu` | Number of physical GPUs | `1`, `2`, `4` |
| `nvidia.com/gpumem` | GPU memory in MB | `3000`, `8000`, `16000` |
| `nvidia.com/gpucores` | GPU core percentage (0-100) | `25`, `50`, `100` |

Similar patterns apply to other device types (MLU, DCU, etc.).

## HAMi Annotations

### Node Annotations

HAMi stores device information in node annotations:

```yaml
annotations:
  hami.io/node-handshake-nvidia: "Reported 2024-01-23 04:30:04"
  hami.io/node-nvidia-register: "GPU-00552014...,10,32768,100,NVIDIA-Tesla V100-PCIE-32GB,0,true:..."
```

### Pod Annotations

Scheduled pods receive allocation information:

```yaml
annotations:
  hami.io/devices-to-allocate: "GPU-0fc3eda5...,NVIDIA,3000,0:..."
  hami.io/vgpu-node: "node67-4v100"
  hami.io/vgpu-time: "1705054796"
```

## Supported Devices

HAMi supports multiple heterogeneous device types:

- **NVIDIA GPU** - GPU virtualization with memory and core isolation
- **Cambricon MLU** - Machine Learning Unit support
- **HYGON DCU** - Data Computing Unit support
- **Iluvatar CoreX** - GPU support
- **Moore Threads GPU** - GPU support
- **HUAWEI Ascend NPU** - Neural Processing Unit support
- **MetaX GPU** - GPU support

Each device type has specific resource names and capabilities.

## Alternative Approaches

While these examples use Kubernetes client libraries, you can also:

1. **Use kubectl**: Interact with HAMi through `kubectl` commands
2. **REST API**: Make direct HTTP calls to the Kubernetes API server
3. **Other Languages**: Use Kubernetes client libraries for Go, JavaScript, .NET, etc.

## Authentication

All examples require authentication to the Kubernetes cluster:

- **In-cluster**: Automatically configured when running inside a Kubernetes pod
- **Kubeconfig**: Uses `~/.kube/config` by default
- **Custom**: Provide custom credentials programmatically

## Development

To create a client in another language:

1. Use the official Kubernetes client for that language
2. Query node annotations for device information (pattern: `hami.io/node-{type}-register`)
3. Create pods with appropriate resource limits
4. Query pod annotations for scheduling decisions (pattern: `hami.io/devices-to-allocate`)

See the [API Reference](../../docs/develop/api-reference.md) for detailed protocol documentation.

## Best Practices

1. **Label Nodes**: Ensure GPU nodes are labeled with `gpu=on`
2. **Set Resource Limits**: Always specify resource limits for device requests
3. **Monitor Annotations**: Check pod annotations to verify successful scheduling
4. **Handle Errors**: Implement proper error handling and retry logic
5. **Use Namespaces**: Organize workloads in separate namespaces

## Troubleshooting

### Pods Not Scheduled

If pods remain in Pending state:
1. Check node labels (`kubectl get nodes --show-labels`)
2. Verify device registration (`kubectl get node <name> -o yaml | grep hami.io`)
3. Check scheduler logs
4. Verify resource requests are within available capacity

### No Device Information

If node annotations are missing:
1. Verify HAMi device plugin is running
2. Check device plugin logs
3. Ensure nodes have supported devices
4. Verify node handshake timestamps are recent

## Contributing

These are example implementations. Contributions welcome:
- Additional language examples
- Enhanced error handling
- More comprehensive features
- Better documentation

## References

- [HAMi Documentation](../../docs/)
- [HAMi API Reference](../../docs/develop/api-reference.md)
- [HAMi Protocol Documentation](../../docs/develop/protocol.md)
- [HAMi GitHub Repository](https://github.com/Project-HAMi/HAMi)
- [Kubernetes Client Libraries](https://kubernetes.io/docs/reference/using-api/client-libraries/)

## License

This example code is provided under the Apache 2.0 License. See the [LICENSE](../../LICENSE) file for details.
