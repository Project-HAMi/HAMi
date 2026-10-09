# Remote GPU examples

The workload uses an upstream CUDA or vLLM image. An init container copies
Lupine's client libraries into an `emptyDir`; no custom workload image or local
NVIDIA driver is required. Use a client library version compatible with your
Lupine server. These examples target an A100 server with two physical GPUs and
an amd64 Ubuntu 24.04 client.

## Prerequisites

Enable `devices.remotegpu.enabled` in the HAMi chart, configure GPU server nodes
with `operatingmode: remote`, and ensure a Lupine server is reachable on its
node InternalIP and configured port. Client nodes must be separate from server
nodes. HAMi injects `LUPINE_SERVER` and `LUPINE_WORKLOAD_ID` during admission and
scheduling. Keep `NVIDIA_VISIBLE_DEVICES=void` to avoid mounting a local driver.

A workload reserves the **entire server**. Set `nvidia.com/remote-gpu` to its
physical GPU count; the CUDA client can see all GPUs on that server. Concurrent
examples therefore need separate servers, or must be run sequentially.

## CUDA computation

```sh
kubectl apply -f examples/remotegpu/default_use.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Succeeded pod/remote-gpu-pod --timeout=600s
kubectl logs remote-gpu-pod -c cuda-container
kubectl delete -f examples/remotegpu/default_use.yaml
```

The official `nvidia/cuda` development image compiles an `sm_80` kernel and
checks `35 + 7 == 42` on every visible GPU. Change the architecture for other
GPU generations. It requests 3000 MiB per GPU, enforced by HAMi-core delivered
by the webhook when `devices.remotegpu.libImage` is configured. This checks
actual remote computation rather than relying on `nvidia-smi`, which is not
included in the ordinary CUDA image without driver injection.

## vLLM serving

```sh
kubectl apply -f examples/remotegpu/vllm_deployment.yaml
kubectl rollout status deployment/vllm-remote-gpu --timeout=900s
kubectl port-forward service/vllm-remote-gpu 8000:8000
```

In another terminal:

```sh
curl -fsS http://127.0.0.1:8000/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"Qwen/Qwen3-0.6B","prompt":"The capital of France is","max_tokens":16,"temperature":0}'
kubectl delete -f examples/remotegpu/vllm_deployment.yaml
```

The official `vllm/vllm-openai:v0.25.1-ubuntu2404` image runs a public small
model with tensor parallelism across two remote GPUs. Model download requires
Internet access; the temporary cache is discarded when the pod is removed.
Use a PVC for a persistent cache in a longer lived deployment.

The NCCL shim is also mounted at the absolute path used by this image's
PyTorch package. Recheck that path when changing the vLLM image. Eager mode,
TRITON attention, synchronous CUDA launches and the cuBLAS preference are
compatibility settings for this Lupine setup, rather than throughput tuning.
No HAMi memory limit is requested by this serving example; vLLM's memory
utilization setting is not a HAMi quota. `Recreate` avoids a rolling update
waiting for an extra GPU server while the old replica still holds the fleet.

`port_forward_session.yaml` separately describes manual sessions, which do not
reserve GPU capacity through the scheduler.
