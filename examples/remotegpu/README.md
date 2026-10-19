# Remote GPU examples

Remote GPU lets a Pod on a node without a local GPU use NVIDIA GPUs on another node. The GPU node runs a [Lupine](https://github.com/lupinemachines) server. HAMi places the Pod on a separate client node, injects the Lupine client libraries, and sets `LUPINE_SERVER` to that server. The workload keeps an upstream CUDA or vLLM image, and the client node does not need an NVIDIA driver. 

Please note that:
1. Client nodes must be separate from server nodes. 
2. We only support limited image for this feature(CUDA amd vllm 0.19-0.26) 
3. A workload reserves the **entire server**. Set `nvidia.com/remote-gpu` to its
physical GPU count; the CUDA client can see all GPUs on that server. Concurrent
examples therefore need separate servers, or must be run sequentially.

## Configuration

1. Enable `devices.remotegpu.enabled` in the HAMi chart
2. Configure GPU server nodes with `operatingmode: remote`
3. submit an example shown below

## CUDA computation

```sh
kubectl apply -f examples/remotegpu/default_use.yaml
kubectl logs remote-gpu-pod -c cuda-container
```


## vLLM serving

```sh
kubectl apply -f examples/remotegpu/vllm_deployment.yaml
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
Internet access; 

