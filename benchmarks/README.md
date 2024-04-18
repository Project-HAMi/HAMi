# Benchmarking the vGPU scheduler

## Prerequisites

### how to build the benchmark image

```bash
cd k8s-vgpu-scheduler/benchmarks/ai-benchmark

sh build.sh
```

## How to install the official nvidia device plugin

Please refer to  [Quick Start](https://github.com/NVIDIA/k8s-device-plugin?tab=readme-ov-file#quick-start) in the official nvidia device plugin repository.

## Run the benchmark

```bash
cd k8s-vgpu-scheduler/deployments

k apply -f job-on-hami.yml

k apply -f job-on-nvidia-device-plugin.yml
```