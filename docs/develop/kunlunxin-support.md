# Kunlunxin XPU Support

HAMi supports Kunlunxin XPUs (e.g., P800) for heterogeneous AI clusters, providing both Memory Isolation and Core Isolation capabilities.

## Prerequisites

- Compatible Kunlunxin XPU (e.g., P800)
- Kunlunxin driver and runtime properly installed on the host
- Kunlunxin device plugin (if applicable) configured to expose devices to Kubernetes

## Resource Allocation

You can request Kunlunxin XPU resources in your Pod specifications using the following labels:

- `kunlunxin.com/xpu`: Request a physical Kunlunxin XPU count.
- `kunlunxin.com/vxpu`: Request virtual Kunlunxin XPU core allocation.
- `kunlunxin.com/vxpu-memory`: Request virtual Kunlunxin XPU memory allocation.

### Example

Here is an example Pod specification requesting Kunlunxin virtual XPU resources:

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

## Known Limitations

- Multi-card support for Kunlunxin is not currently available.
- Dynamic MIG or similar dynamic partitioning is not supported natively via this plugin.
