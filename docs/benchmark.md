## Benchmarks

Three instances from vLLM benchmark have been used to evaluate vGPU-device-plugin performance as follows:

| Test Environment | Description |
| ---------------- | :----------: |
| Kubernetes version | v1.35.4 |
| Docker version | 29.4.0 |
| GPU Type | A100-SXM4-40GB |
| GPU Num | 2 |

| Test Instance | Description |
| ------------- | :---------------------------------------------------------: |
| Native | k8s + nvidia k8s-device-plugin |
| Opensource_v280 | k8s + VGPU k8s-device-plugin, opensource version v280 |
| Opensource_v290 | k8s + VGPU k8s-device-plugin, opensource version v290 |

Test Cases:

| test id |      case      |   type    |                params                |
| ------- | :------------: | :-------: | :----------------------------------: |
| 6.1     |  Qwen3-8B (vLLM)  | inference | batch=1, stream=True, max_model_len=8192 |

Test Result:

| Metric | Native | Opensource_v280 | Opensource_v290 |
| ------ | :----: | :-------------: | :-------------: |
| TTFT p50 (s) | 0.0621 | 0.0670 | 0.0629 |
| TTFT p95 (s) | 0.0642 | 0.0713 | 0.0650 |
| TTFT p99 (s) | 0.0652 | 0.0735 | 0.0674 |
| Per-token latency (clean mean, s) | 0.0285 | 0.0310 | 0.0291 |

![per-token latency histogram](../imgs/benchmark_vllm_pt_hist.png)

![per-token latency violin](../imgs/benchmark_vllm_pt_violin.png)

![per-token latency CDF](../imgs/benchmark_vllm_pt_cdf.png)

![TTFT histogram](../imgs/benchmark_vllm_ttft_hist.png)

![TTFT violin](../imgs/benchmark_vllm_ttft_violin.png)

![TTFT CDF](../imgs/benchmark_vllm_ttft_cdf.png)

To reproduce:

1. install k8s-vGPU-scheduler, and configure properly
2. build benchmark images

```
$ cd benchmarks/ai-benchmark
$ sh build.sh
```

3. run benchmark job

```
$ kubectl apply -f benchmarks/deployments/job-on-nvidia-device-plugin.yml
$ kubectl apply -f benchmarks/deployments/job-on-hami.yml
```

4. View the result

```
$ kubectl cp <pod-name>:/results ./results
$ python3 benchmarks/ai-benchmark/gen_report.py \
    --dataset native ./results/bench_native.jsonl \
    --dataset hami ./results/bench_hami.jsonl
```
