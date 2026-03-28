# Quick Start: Verifying HAMi in a Real Kubernetes Environment

This guide verifies that GPU workloads run correctly in a Kubernetes cluster with HAMi installed, focusing on actual runtime behavior rather than deployment success.

It explains what is happening at each step, why it matters, and how to interpret the results.

The goal is simple but strict: prove that HAMi is correctly installed and usable, not just deployed.

---

## What "working" actually means

Before starting, clarify expectations.

A working HAMi setup does not mean:

* Pods are running
* Helm install succeeded
* Scheduler pod is up

A working HAMi setup means:

* GPU is accessible inside a container
* Kubernetes correctly advertises GPU resources
* Scheduler does not block GPU workloads
* Workloads behave predictably

Everything in this guide builds toward that proof.

---

## Environment (real test reference)

This guide was validated on a clean system:

* Kubernetes: k3s
* Version: k3s (tested on a recent stable release)
* GPU: NVIDIA L40S
* Driver: 580.126.09
* CUDA: 13.0

Different Kubernetes distributions behave differently (especially k3s vs kubeadm).
This guide confirms that HAMi works in a lightweight Kubernetes environment such as k3s.

---

## Prerequisites

This guide assumes that a working GPU stack is already available.

GPU components may be installed via:

* NVIDIA GPU Operator
* standalone NVIDIA device plugin

The installation method does not matter, as long as GPU workloads run successfully.

---

## Step 1: Validate the GPU stack first (non-negotiable)

Before even thinking about HAMi, you must prove that Kubernetes can use the GPU.

Create this pod:

```yaml
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

```bash
kubectl delete pod cuda-test --ignore-not-found
kubectl apply -f cuda-test.yaml
kubectl wait --for=condition=Ready pod/cuda-test --timeout=60s || true
kubectl wait --for=condition=Succeeded pod/cuda-test --timeout=60s || true
kubectl logs cuda-test
```

---

### What happens here (important)

When you apply this pod:

1. Kubernetes checks for nodes advertising `nvidia.com/gpu`
2. The NVIDIA device plugin reports available GPUs
3. The container runtime injects GPU access into the container
4. `nvidia-smi` executes inside the container

---

### Expected behavior

Initially:

```
ContainerCreating
```

This is normal. The runtime is preparing GPU access.

Then:

```
Completed
```

---

### Logs

```bash
kubectl wait --for=condition=Ready pod/cuda-test --timeout=60s || true
kubectl wait --for=condition=Succeeded pod/cuda-test --timeout=60s || true
kubectl logs cuda-test
```

Expected:

* GPU name (e.g. L40S)
* Driver version
* CUDA version

---

### What this proves

This step proves:

* NVIDIA drivers are installed correctly
* Container runtime supports GPUs
* Device plugin is functioning
* Kubernetes scheduling works with GPUs

---

### Critical insight

If this step fails, HAMi is irrelevant.

Most "HAMi issues" are actually GPU runtime or driver problems.

---

## Step 2: Verify GPU resources on the node

```bash
kubectl get nodes -o jsonpath='{.items[*].status.allocatable}' | grep -i nvidia
```

Expected:

```
nvidia.com/gpu: 1
```

---

### Important detail

At this stage, you will likely NOT see:

```
nvidia.com/gpucores
nvidia.com/gpumem
```

---

### Why this matters

This means:

* GPU exists and is schedulable
* HAMi GPU sharing is NOT enabled

Therefore:

* Full GPU workloads work
* Fractional GPU workloads will not work yet

---

## Step 3: Install HAMi

HAMi extends Kubernetes GPU handling by introducing:

* scheduler extensions
* GPU resource abstraction
* optional GPU sharing mechanisms (`gpucores`, `gpumem`)

Install HAMi using Helm:

```bash
helm install hami hami-charts/hami \
  -n kube-system \
  --set scheduler.kubeScheduler.imageTag=<your-k8s-version>
```

---

### Why version matters

The scheduler image must match the Kubernetes version.

Mismatch can lead to:

* pods stuck in `Pending`
* silent scheduling issues
* hard-to-debug behavior

---

## Step 4: Verify HAMi components

```bash
kubectl get pods -n kube-system | grep hami
```

Expected:

```
hami-scheduler → Running
```

---

### What this actually proves

This confirms:

* HAMi scheduler is deployed
* it is not crashing

It does NOT guarantee:

* that it is actively influencing scheduling
* that GPU sharing is enabled

---

## Step 5: Re-run GPU workload under HAMi

```bash
kubectl delete pod cuda-test --ignore-not-found
kubectl apply -f cuda-test.yaml
kubectl wait --for=condition=Ready pod/cuda-test --timeout=60s || true
kubectl wait --for=condition=Succeeded pod/cuda-test --timeout=60s || true
kubectl logs cuda-test
```

---

### Why repeat this step?

You are verifying that:

* HAMi does not break existing GPU workloads
* the scheduling chain remains intact

---

### Expected result

You still see valid `nvidia-smi` output.

---

### Interpretation

* HAMi is compatible with your cluster
* no regression introduced
* baseline GPU functionality is preserved

---

## Step 6: Attempt fractional GPU (expected to fail without additional configuration)

Try using:

```yaml
resources:
  limits:
    nvidia.com/gpucores: 50
    nvidia.com/gpumem: 2000
```

Result:

```
Pod → Pending
```

---

### Why this happens

This is expected behavior.

Because:

* default NVIDIA device plugin is still active
* HAMi GPU sharing is not enabled
* fractional resources are not registered in the cluster

---

### Key insight

Installing HAMi does NOT automatically enable:

* `nvidia.com/gpucores`
* `nvidia.com/gpumem`

Additional configuration is required for GPU sharing.

---

## What you actually verified

From start to finish, this test proves:

1. GPU stack is working
2. Kubernetes advertises GPU resources
3. GPU workloads run successfully
4. HAMi scheduler is deployed
5. HAMi does not break execution

---

## What is NOT verified

This guide does NOT verify:

* GPU sharing (`gpucores`, `gpumem`)
* fairness scheduling
* multi-tenant GPU isolation

---

## Common misconceptions

### "HAMi is not working"

If `nvidia-smi` works, the system is correctly configured at the base level.

---

### "Fractional GPU failed → HAMi broken"

Incorrect.

Fractional GPU requires additional configuration.

---

### "Scheduler is running → everything works"

Incorrect.

Scheduler presence does not guarantee scheduling behavior.

---

## Troubleshooting order

Always debug in this order:

1. GPU hardware and drivers
2. Container runtime
3. Device plugin
4. Node resources
5. Scheduler layer (HAMi)

---

## Cleanup

```bash
kubectl delete pod cuda-test
```

---

## Next steps

To use HAMi fully:

* enable GPU sharing
* configure `gpucores` and `gpumem`
* test multiple concurrent workloads

---

This guide separates two concepts:

* "system works"
* "advanced features enabled"

Confusing these is the most common mistake in GPU Kubernetes setups.
