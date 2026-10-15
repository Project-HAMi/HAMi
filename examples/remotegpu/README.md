# Remote GPU examples

The workload uses an upstream CUDA or vLLM image. HAMi automatically injects
an init container, an `emptyDir`, library mounts and environment variables to
deliver Lupine client libraries. No custom workload image or local NVIDIA
driver is required. These examples target two A100 GPUs and amd64 Ubuntu 24.04
workload images.

## Prerequisites

Enable `devices.remotegpu.enabled` in the HAMi chart, configure GPU server nodes
with `operatingmode: remote`, and ensure a Lupine server is reachable on its
node InternalIP and configured port. Client nodes must be separate from server
nodes. HAMi injects `LUPINE_SERVER` and `LUPINE_WORKLOAD_ID` during admission and
scheduling. Set `devices.remotegpu.clientImage` to a Lupine client image
compatible with the server and the workload ABI. The chart defaults to
`ghcr.io/lupinemachines/lupine-client:cuda-13.3.1-ubuntu24.04`. To test a modified
Lupine build, override this chart setting rather than editing the examples.
HAMi sets `NVIDIA_VISIBLE_DEVICES=void` to avoid mounting a local driver.

Automatic library delivery currently targets the CUDA and vLLM example images,
which already include `/usr/local/nvidia/lib64` in their library search paths.
HAMi mounts the Lupine CUDA and NVML shims and `libnghttp2.so.14` as individual
files there, preserving other files in that directory. The complete client
library directory is also available at `/opt/lupine/lib` for explicit mounts,
such as the vLLM NCCL mount below. `LD_LIBRARY_PATH` is not injected or changed.

The client image must contain `/opt/lupine/lib` and provide `sh` and `cp`.
Delivery copies only the known missing dependency `libnghttp2.so.14`, searching
Debian/Ubuntu multiarch directories and the common `/usr/lib64`, `/lib64`,
`/usr/lib`, and `/lib` directories in the client image. A missing library fails
delivery with a diagnostic. Other dependencies, including OpenSSL, libstdc++,
and glibc, are provided by the workload image and must have compatible versions.
This is a limited compatibility measure, not a complete dependency bundler;
arbitrary workload images may need explicit library mounts or manual delivery.
Only HAMi-core is added to `LD_PRELOAD` when memory enforcement is requested;
Lupine shims are loaded on demand.

For images that already deliver their own libraries, set the Pod annotation
`hami.io/remote-gpu-client-injection: "disabled"`. Setting the chart's
`devices.remotegpu.clientImage: ""` disables automatic delivery cluster-wide;
server endpoint, workload identity and configured memory enforcement still apply.

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
GPU generations. The 3000 MiB per GPU limit is enforced by HAMi-core delivered
by the webhook when `devices.remotegpu.libImage` is configured. Ensure that
HAMi-core and its dependencies are compatible with the workload image. This checks
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
  -d '{"model":"Qwen/Qwen3-8B","prompt":"The capital of France is","max_tokens":16,"temperature":0}'
kubectl delete -f examples/remotegpu/vllm_deployment.yaml
```

The official `vllm/vllm-openai:v0.25.1-ubuntu2404` image runs `Qwen/Qwen3-8B`
in float16 with tensor parallelism across two remote GPUs. Model download requires
Internet access; the temporary cache is discarded when the pod is removed.
Use a PVC for a persistent cache in a longer lived deployment.

HAMi populates the `hami-lupine-client` volume with client libraries.
The vLLM example uses a standard `volumeMounts` entry with
`subPath: libnccl.so.2` to replace NCCL at the absolute path used by this
image's PyTorch package. Recheck that path when changing the vLLM image.
Other libraries loaded by absolute path can be handled the same way; HAMi
needs no library-specific annotations. The Deployment also declares this
volume as `emptyDir`: controllers validate mount references before the Pod
admission webhook runs. HAMi reuses that volume for client library delivery.
This mount depends on automatic client
injection being enabled, unless the workload supplies the volume itself. Eager mode,
TRITON attention, synchronous CUDA launches and the cuBLAS preference are
compatibility settings for this Lupine setup, rather than throughput tuning.
This example requests two remote GPUs with a HAMi quota of 20480 MiB
(20 GiB) per GPU through `nvidia.com/remote-gpu-memory`. vLLM uses
`--gpu-memory-utilization=0.8` to plan its model and KV cache budget, with a
4096-token maximum context and at most four sequences. This vLLM budget
is planned within the HAMi memory view. No explicit KV cache byte budget
is set, so vLLM sizes the cache through profiling. The container requests 16 GiB of host RAM
and has a 32 GiB host RAM limit, separate from GPU memory. `Recreate` avoids a rolling update
waiting for an extra GPU server while the old replica still holds the fleet.

`port_forward_session.yaml` separately describes manual sessions, which do not
reserve GPU capacity through the scheduler.

## Compatibility

Use compatible Lupine client and server builds. GPU memory quotas require
server support for the client's NVML RPCs, including
`nvmlDeviceGetProcessUtilization`, and HAMi-core with missing NVML symbol
guards (upstream commit `a3bae57` or later).

Lupine may not provide `nvmlDeviceGetP2PStatus`. vLLM can log an NVLink
detection error and continue when this function is unavailable.

Run the examples sequentially and stop any existing workload reserving the
same server first. Restore it after deleting the test workload.

Upstream image references: [NVIDIA CUDA](https://hub.docker.com/r/nvidia/cuda/)
and [vLLM Docker deployment](https://docs.vllm.ai/en/stable/deployment/docker/).
