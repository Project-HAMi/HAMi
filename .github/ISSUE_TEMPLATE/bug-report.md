---
name: Bug Report
about: Report a bug encountered while using HAMi.
labels: kind/bug

---

<!-- Please use this template while reporting a bug and provide as much info as possible. Not doing so may result in your bug not being addressed in a timely manner. Thanks!
-->

**What happened**:

**What you expected to happen**:

**How to reproduce it (as minimally and precisely as possible)**:

**Environment**:

- HAMi version:
- Accelerator vendor and model (e.g., NVIDIA A100, Ascend 910B, AMD MI250X, Hygon, MetaX, Enflame, Kunlun, Iluvatar, MThreads, Biren, AWS Neuron):
- HAMi device backend (e.g., nvidia-device-plugin, ascend-device-plugin, cambricon-device-plugin, etc.):
- Vendor driver version:
- Vendor runtime version (e.g., CUDA, CANN, ROCm, etc.):
- Container runtime and version (e.g., containerd 1.7, Docker 24.0):
- Kubernetes version:
- Kernel version (`uname -a`):

**Logs and diagnostics**:

> Remove all credentials, tokens, registry secrets, and other private information before posting.

- Pod YAML or the relevant resource section:
- Pod events (`kubectl describe pod <pod-name>`):
- Node annotations (`kubectl get node <node-name> -o yaml`):
- HAMi scheduler logs:
- HAMi device-plugin logs:
- Kubelet logs (`sudo journalctl -r -u kubelet`):
- Relevant `dmesg` output:
- Vendor-specific diagnostic output (e.g., `nvidia-smi -a` for NVIDIA, `npu-smi info` for Ascend, `rocm-smi` for AMD — include if available):

**Additional context**:
