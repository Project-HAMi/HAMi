# vLLM Benchmark for HAMi vGPU Scheduler

Compare vLLM inference performance on HAMi vGPU vs native NVIDIA device plugin.

## Architecture

Each benchmark deploys a **sidecar pod** with two containers:

- **vllm-server** — runs `vllm/vllm-openai` OpenAI-compatible API server (GPU required)
- **benchmark-client** — sends streaming requests to the server and records TTFT + per-token latency

## Build Images

```bash
cd ai-benchmark

# Build and push both server and client images
REGISTRY=docker.io/your-org TAG=v0.1.0 sh build.sh
```

Override the default model at build time:

```bash
docker buildx build \
  --build-arg MODEL_NAME=Qwen/Qwen3-8B \
  --build-arg DTYPE=bfloat16 \
  -t your-registry/vllm-bench-server:latest \
  -f Dockerfile .
```

## Run Locally (Docker)

```bash
cd ai-benchmark
sh run_bench.sh
```

This starts a vLLM container, waits for it to be ready, runs `benchmark.py`, then stops the container.

Pass extra arguments to `benchmark.py` via `run_bench.sh`:

```bash
sh run_bench.sh --model Qwen3-8B --warmup 10 --runs 100
```

## Run on Kubernetes

Benchmark on HAMi vGPU:

```bash
kubectl apply -f deployments/job-on-hami.yml
```

Benchmark on native NVIDIA device plugin (baseline):

```bash
kubectl apply -f deployments/job-on-nvidia-device-plugin.yml
```

### Retrieve Results

Results are written to a shared `emptyDir` volume. After the pod completes:

```bash
# Find the pod
kubectl get pods -l job-name=ai-benchmark-on-hami

# Copy results
kubectl cp <pod-name>:/results ./results-hami
```

## Generate Comparison Report

After collecting both HAMi and native benchmark results:

```bash
cd ai-benchmark
python gen_report.py \
  --dataset native ../results-native/bench_native.jsonl \
  --dataset hami ../results-hami/bench_hami.jsonl \
  --output-dir ../report
```

## benchmark.py CLI

```
--vllm-url   vLLM endpoint (default: http://127.0.0.1:8000/v1/chat/completions)
--model      Model name (default: Qwen3-8B)
--prompt     Prompt text
--warmup     Warmup requests (default: 30)
--runs       Benchmark requests (default: 200)
--output     Output JSONL path (auto-generated if omitted)
```
