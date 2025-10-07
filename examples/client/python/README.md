# HAMi Python Client Example

This directory contains a Python client library example for interacting with HAMi (Heterogeneous AI Computing Virtualization Middleware).

## Overview

The HAMi Python client provides a high-level interface for:
- Querying GPU/NPU nodes and their device information
- Creating pods with HAMi device resource requests
- Monitoring HAMi scheduling decisions
- Managing device allocations

## Prerequisites

- Python 3.7 or higher
- Access to a Kubernetes cluster with HAMi installed
- Kubernetes Python client library

## Installation

Install the required dependencies:

```bash
pip install kubernetes
```

## Usage

### Basic Example

```python
from hami_client import HAMiClient

# Initialize the client
hami = HAMiClient()

# List all GPU nodes
nodes = hami.list_gpu_nodes()
for node in nodes:
    print(f"Node: {node['name']}")
    print(f"Devices: {node['devices']}")
```

### Creating a GPU Pod

```python
# Create a pod with GPU resources
pod_info = hami.create_gpu_pod(
    pod_name="my-gpu-pod",
    namespace="default",
    image="nvidia/cuda:11.0-base",
    gpu_count=1,           # Number of GPUs
    gpu_memory_mb=3000,    # GPU memory in MB
    gpu_cores=50,          # GPU core percentage
    command=["nvidia-smi"]
)
```

### Querying Device Information

```python
# Get devices for a specific node
devices = hami.get_node_devices("node-1")
print(devices)

# Get scheduling information for a pod
scheduling_info = hami.get_pod_scheduling_info("my-gpu-pod", "default")
print(scheduling_info)
```

## Resource Specifications

When creating pods, you can specify the following GPU resource limits:

- `gpu_count`: Number of physical GPUs (required)
- `gpu_memory_mb`: GPU memory in MB (optional)
- `gpu_cores`: GPU core percentage (0-100) (optional)

Example:
```python
hami.create_gpu_pod(
    pod_name="gpu-training-job",
    namespace="ml-team",
    image="tensorflow/tensorflow:latest-gpu",
    gpu_count=2,          # Request 2 GPUs
    gpu_memory_mb=8000,   # 8GB memory per GPU
    gpu_cores=100         # 100% cores (full GPU)
)
```

## Understanding HAMi Annotations

HAMi uses Kubernetes annotations to store device information and scheduling decisions.

### Node Annotations

Nodes with HAMi devices have annotations like:
- `hami.io/node-handshake-nvidia`: Handshake timestamp
- `hami.io/node-nvidia-register`: Device registration information

### Pod Annotations

Scheduled pods have annotations like:
- `hami.io/devices-to-allocate`: Allocated device information
- `hami.io/vgpu-node`: Scheduled node name
- `hami.io/vgpu-time`: Scheduling timestamp

## Running the Example

Run the example script:

```bash
python hami_client.py
```

This will:
1. List all GPU nodes and their devices
2. Create an example GPU pod
3. Query the scheduling information
4. Clean up the created resources

## Authentication

The client uses the Kubernetes Python client library, which supports multiple authentication methods:

1. **In-cluster configuration**: Automatically used when running inside a Kubernetes pod
2. **Kubeconfig file**: Uses `~/.kube/config` by default
3. **Custom kubeconfig**: Pass a custom path to the constructor

```python
# Use custom kubeconfig
hami = HAMiClient(kubeconfig="/path/to/kubeconfig")
```

## Supported Device Types

HAMi supports various heterogeneous devices:
- NVIDIA GPUs (`nvidia`)
- Cambricon MLU (`mlu`)
- HYGON DCU (`dcu`)
- Iluvatar CoreX GPU (`iluvatar`)
- Moore Threads GPU (`mthreads`)
- HUAWEI Ascend NPU (`ascend`)
- And more...

The client automatically detects and parses information for all supported device types.

## Error Handling

All methods raise exceptions on errors. Wrap calls in try-except blocks for proper error handling:

```python
try:
    pod_info = hami.create_gpu_pod(...)
except Exception as e:
    print(f"Failed to create pod: {e}")
```

## Advanced Usage

### Custom Pod Specifications

For more complex pod configurations, you can use the Kubernetes Python client directly:

```python
from kubernetes import client

# Create a custom pod with multiple containers
pod = client.V1Pod(
    metadata=client.V1ObjectMeta(name="complex-pod"),
    spec=client.V1PodSpec(
        containers=[
            client.V1Container(
                name="training",
                image="my-training-image",
                resources=client.V1ResourceRequirements(
                    limits={
                        "nvidia.com/gpu": "2",
                        "nvidia.com/gpumem": "8000",
                        "nvidia.com/gpucores": "100"
                    }
                )
            ),
            client.V1Container(
                name="sidecar",
                image="my-sidecar-image"
            )
        ]
    )
)

hami.core_v1.create_namespaced_pod(namespace="default", body=pod)
```

### Monitoring Multiple Pods

```python
# List all pods in a namespace
pods = hami.core_v1.list_namespaced_pod(namespace="default")

for pod in pods.items:
    scheduling_info = hami.get_pod_scheduling_info(
        pod.metadata.name, 
        pod.metadata.namespace
    )
    print(f"Pod {pod.metadata.name}: {scheduling_info}")
```

## Contributing

This is an example client implementation. For production use, consider:
- Adding comprehensive error handling
- Implementing retry logic
- Adding logging and monitoring
- Supporting additional HAMi features
- Creating a proper Python package

## References

- [HAMi Documentation](../../docs/)
- [HAMi API Reference](../../docs/develop/api-reference.md)
- [Kubernetes Python Client](https://github.com/kubernetes-client/python)
- [HAMi GitHub Repository](https://github.com/Project-HAMi/HAMi)
