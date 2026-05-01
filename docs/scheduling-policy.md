# Scheduling Policies

HAMi supports multiple GPU scheduling policies to handle complex workload
scenarios. A pod can select a scheduling policy using pod annotations.

## Available Policies

| Policy      | Scope | Effect |
| ---------   | ----- | ------ |
| `binpack`   | Node  | Tries to allocate tasks to the **same GPU node** as much as possible |
| `spread`    | Node  | Tries to allocate tasks to **different GPU nodes** as much as possible |
| `numa-first`| GPU   | For multi-GPU allocations, prefers GPUs on the **same NUMA node** |

## Default Policy

The default node scheduling policy is `binpack` and the default GPU
scheduling policy is `spread`. These can be changed globally via Helm:

```bash
helm install hami hami-charts/hami \
  --set scheduler.defaultSchedulerPolicy.nodeSchedulerPolicy=binpack \
  --set scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy=spread
```

## Per-Pod Policy via Annotations

Individual pods can override the default by specifying a scheduling policy
in `.metadata.annotations`.

### Example: binpack policy

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  annotations:
    hami.io/node-scheduler-policy: "binpack"
spec:
  containers:
    - name: ubuntu-container
      image: ubuntu:22.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          nvidia.com/gpu: 2
          nvidia.com/gpumem: 4000
```

### Example: spread policy

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  annotations:
    hami.io/node-scheduler-policy: "spread"
spec:
  containers:
    - name: ubuntu-container
      image: ubuntu:22.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          nvidia.com/gpu: 2
          nvidia.com/gpumem: 4000
```

## Notes

- The annotation `hami.io/node-scheduler-policy` controls **which node**
  the pod is placed on.
- Only node-level policies (`binpack`, `spread`) can be overridden per-pod
  via annotations. GPU-level policy within a node is configured globally
  via `scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy`.
- `numa-first` (NUMA affinity) is not yet implemented and is tracked
  as an open item in [roadmap.md](./develop/roadmap.md).
- See [config.md](./config.md) for the full list of scheduler configuration options.