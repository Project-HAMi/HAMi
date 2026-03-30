# Quick Start: Verifying HAMi in a Real Kubernetes Environment

This guide verifies that GPU workloads run correctly in a Kubernetes cluster with HAMi installed, focusing on actual runtime behavior rather than deployment success.

It explains what is happening at each step, why it matters, and how to interpret the results.

The goal is simple but strict: prove that HAMi is correctly installed and usable, not just deployed.

## What "working" actually means

Before starting, clarify expectations. A working HAMi setup does not mean:

* Pods are running
* Helm install succeeded
* Scheduler pod is up

A working HAMi setup means:

* GPU is accessible inside a container
* Kubernetes correctly advertises GPU resources
* Scheduler does not block GPU workloads
* Workloads behave predictably

Everything in this guide builds toward that proof.

## Environment (real test reference)

This guide was validated on a clean system:

* Kubernetes: k3s
* Version: k3s (tested on a recent stable release)
* GPU: NVIDIA L40S
* Driver: 580.126.09
* CUDA: 13.0

Different Kubernetes distributions behave differently (especially k3s vs kubeadm).

This guide confirms that HAMi works in a lightweight Kubernetes environment such as k3s.

## Prerequisites

This guide assumes that a working GPU stack is already available.

GPU components may be installed via:

* NVIDIA GPU Operator
* standalone NVIDIA device plugin

The installation method does not matter, as long as GPU workloads run successfully.

## Step 1: Validate the GPU stack first (non-negotiable)

Before even thinking about HAMi, you must prove that Kubernetes can use the GPU.

Create this pod:

```
apiVersion: v1
kind: Pod
metadata:
  name: cuda-test
spec:
  restartPolicy: Never
  containers:
    - name: cuda
      image: nvcr.io/nvidia/cuda:12.2.0-base-ubuntu22.04
      command: ["nvidia-smi"]
      resources:
        limits:
          nvidia.com/gpu: 1
```

Apply it:

```
kubectl delete pod cuda-test --ignore-not-found
kubectl apply -f cuda-test.yaml
kubectl wait --for=condition=Ready pod/cuda-test --timeout=60s || true
kubectl wait --for=condition=Succeeded pod/cuda-test --timeout=60s || true
kubectl logs cuda-test
```

### What happens here

When you apply this pod:

1. Kubernetes checks for nodes advertising `nvidia.com/gpu`
2. The NVIDIA device plugin reports available GPUs
3. The container runtime injects GPU access into the container
4. `nvidia-smi` executes inside the container

### What this proves

This step proves:

* NVIDIA drivers are installed correctly
* Container runtime supports GPUs
* Device plugin is functioning
* Kubernetes scheduling works with GPUs

### Critical insight

If this step fails, HAMi is irrelevant.

Most "HAMi issues" are actually GPU runtime or driver problems.

### Troubleshooting: Getting `nvidia-smi` to work

If the previous step fails, the issue is not related to HAMi.

It is almost always one of the following:

- conflicting device plugin
- missing node labels
- misconfigured container runtime

Follow these steps carefully.

### i. Remove conflicting NVIDIA device plugins

If multiple GPU setups were attempted before, stale device plugins may exist.

Remove them:

```
kubectl delete daemonset nvidia-device-plugin-daemonset -n kube-system --ignore-not-found
```

### ii. Ensure the node is labeled correctly

Some setups require explicit GPU labeling.

```
kubectl label node $(hostname) nvidia.com/gpu.present=true --overwrite
```

### iii. Verify container runtime GPU support

Test GPU access outside Kubernetes:

```
sudo ctr run --rm --gpus 0 docker.io/nvidia/cuda:12.2.0-base-ubuntu22.04 test nvidia-smi
```

Expected:

- GPU name appears
- no runtime errors

### iv. Re-run the Kubernetes test

```
kubectl delete pod cuda-test --ignore-not-found
kubectl apply -f cuda-test.yaml
kubectl logs cuda-test
```

### Important

Do not continue until this step works.

If `nvidia-smi` fails here: HAMi will not work.


## Step 2: Verify GPU resources on the node

```
kubectl get nodes -o jsonpath='{.items[*].status.allocatable}' | grep -i nvidia
```

Expected:

```
nvidia.com/gpu: 1
```

### Important detail

At this stage, you will likely NOT see:

```
nvidia.com/gpucores
nvidia.com/gpumem
```
### Verify GPU resource availability

```
kubectl get nodes -o jsonpath='{.items[*].status.allocatable}'
```
Expected:
```
nvidia.com/gpu: <number>
```

This confirms that the GPU is visible to Kubernetes and can be scheduled.

## Step 3: Install HAMi

HAMi extends Kubernetes GPU handling by introducing:

* scheduler extensions
* GPU resource abstraction
* optional GPU sharing mechanisms (`gpucores`, `gpumem`)

Install HAMi using Helm:

```
helm install hami hami-charts/hami -n kube-system
```
> Note: The scheduler image tag is automatically resolved in recent versions.
> Manual configuration is typically not required.
---

### Scheduler version handling

In most setups, the scheduler image version is resolved automatically.

Manual configuration is only required in advanced or custom environments where version mismatches must be explicitly controlled.

## Step 4: Verify HAMi components

```
kubectl get pods -n kube-system | grep hami
```

Expected:

```
hami-scheduler → Running
```

### What this actually proves

This confirms:

* HAMi scheduler is deployed
* it is not crashing

It does NOT guarantee:

* that it is actively influencing scheduling
* that GPU sharing is enabled

## Inspecting scheduler behavior

To verify that HAMi is actively participating in scheduling:

```
kubectl logs -n kube-system -l app=hami-scheduler
```

### What to look for

You may see:

- scheduling decisions
- resource evaluation logs
- GPU allocation attempts

### Important note

The presence of logs confirms:

- scheduler is running
- scheduler is processing workload decisions

It does NOT guarantee:

- GPU sharing is enabled
- fairness policies are active

### Optional: cluster resource view

```
kubectl get nodes -o wide
kubectl describe node $(hostname)
```

This helps verify:

- GPU visibility
- node-level resource state

## Step 5: Re-run GPU workload under HAMi

```
kubectl delete pod cuda-test --ignore-not-found
kubectl apply -f cuda-test.yaml
kubectl wait --for=condition=Ready pod/cuda-test --timeout=60s || true
kubectl wait --for=condition=Succeeded pod/cuda-test --timeout=60s || true
kubectl logs cuda-test
```

### Why repeat this step?

You are verifying that:

* HAMi does not break existing GPU workloads
* the scheduling chain remains intact

### Expected result

You still see valid `nvidia-smi` output.

### Interpretation

* HAMi is compatible with your cluster
* no regression introduced
* baseline GPU functionality is preserved

## Common failure scenarios

### GPU not visible in Kubernetes

Check:

```
kubectl get nodes -o jsonpath='{.items[*].status.allocatable}'
```

If `nvidia.com/gpu` is missing:

- device plugin is not working
- runtime is misconfigured

### Pod stuck in ContainerCreating

This usually indicates:

- runtime hook issues
- missing NVIDIA libraries

### `nvidia-smi` works outside but not in Kubernetes

This means:

- container runtime is not wired correctly into Kubernetes
- device plugin cannot access GPU

## Mental model (final state)

### Before:

```
GPU test
→ HAMi install
→ done
```

### After:

```
GPU test
→ troubleshooting path
→ GPU verified
→ HAMi install
→ scheduler check
→ advanced validation
```

## Step 6: Attempt fractional GPU (expected to fail without additional configuration)

Try using:

```
resources:
  limits:
    nvidia.com/gpucores: 50
    nvidia.com/gpumem: 2000
```

Result:

```
Pod → Pending
```

### Why this happens

This is expected behavior.

Because:

* default NVIDIA device plugin is still active
* HAMi GPU sharing is not enabled
* fractional resources are not registered in the cluster

### Key insight

Installing HAMi does NOT automatically enable:

* `nvidia.com/gpucores`
* `nvidia.com/gpumem`

Additional configuration is required for GPU sharing.

## What you actually verified

From start to finish, this test proves:

1. GPU stack is working
2. Kubernetes advertises GPU resources
3. GPU workloads run successfully
4. HAMi scheduler is deployed
5. HAMi does not break execution

## What is NOT verified

This guide does NOT verify:

* GPU sharing (`gpucores`, `gpumem`)
* fairness scheduling
* multi-tenant GPU isolation

## Common misconceptions

### "HAMi is not working"

If `nvidia-smi` works, the system is correctly configured at the base level.

### "Fractional GPU failed → HAMi broken"

Incorrect. Fractional GPU requires additional configuration.

### "Scheduler is running → everything works"

Incorrect. Scheduler presence does not guarantee scheduling behavior.

## Troubleshooting order

Always debug in this order:

1. GPU hardware and drivers
2. Container runtime
3. Device plugin
4. Node resources
5. Scheduler layer (HAMi)

## Cleanup

```
kubectl delete pod cuda-test
```