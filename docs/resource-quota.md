# ResourceQuota Support

HAMi supports Kubernetes [ResourceQuota](https://kubernetes.io/docs/concepts/policy/resource-quotas/)
to limit GPU memory usage per namespace.

## Why This Is Needed

The number of virtual devices in a namespace does not directly represent
actual GPU memory consumption. HAMi solves this by supporting memory-based
quota limits, so cluster administrators can cap how much device memory a
namespace is allowed to use.

## How It Works

HAMi exposes `requests.nvidia.com/gpu-memory` as a quota resource.
When a ResourceQuota is applied to a namespace, HAMi enforces that the
total device memory requested by all pods in that namespace does not exceed
the configured limit.

## Example

**Step 1:** Create the ResourceQuota definition:

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: compute-resources
spec:
  hard:
    requests.cpu: "1"
    requests.memory: 1Gi
    limits.cpu: "2"
    limits.memory: 2Gi
    limits.nvidia.com/gpumem: 30000
```

**Step 2:** Apply it to your namespace:

```bash
kubectl create -f ./compute-resources.yaml --namespace=myspace
```

This limits the total GPU memory that can be requested across all pods
in namespace `myspace` to 30,000 MiB (approximately 30GB).

## Verify the Quota

```bash
kubectl describe resourcequota compute-resources --namespace=myspace
```

## Notes

- The quota unit for `limits.nvidia.com/gpumem` is MiB.
- This quota applies to the sum of all pod requests in the namespace,
  not per individual pod.
- Pods that would exceed the quota will be rejected at admission time.