---
name: hami-nvml-mock
description: Use kind and nvml-mock for local integration tests of the HAMi scheduler or NVIDIA device plugin, including reproducing bugs and testing fixes without physical GPUs.
---

# Test HAMi with nvml-mock

## When to use this setup

Use this setup when you need to exercise the HAMi scheduler or NVIDIA device plugin in Kubernetes and the test can use simulated GPUs. Run both components in a local kind cluster, with nvml-mock supplying devices to the plugin through NVML.

## What you can test

- **NVIDIA device plugin.** Test device discovery through NVML and registration of vGPU resources with the kubelet. When the kubelet prepares a new container that requests a vGPU on the mock node, it calls the plugin's `Allocate` RPC for the simulated devices.
- **HAMi scheduler.** Test GPU-aware Pod placement and allocation. Use the allocation annotations to check whether Pods share a GPU, whether a Pod gets multiple GPUs, and how much GPU memory or compute the scheduler assigns.

## When you need a physical GPU

Use a physical NVIDIA GPU to run CUDA workloads, verify runtime enforcement of GPU memory or compute limits, or measure actual GPU utilization and performance.

## Set up the test environment

Follow Lab 5 for prerequisites, cluster setup, test workloads, and cleanup. The lab installs a published HAMi release. [Lab 5 in English](https://project-hami.io/tutorials/labs/nvml-mock) · [中文实验](https://project-hami.io/zh/tutorials/labs/nvml-mock)

### Lab 5 known issue

If `kubectl version --client --short` reports `unknown flag: --short` during the prerequisite check, run `kubectl version --client`.
