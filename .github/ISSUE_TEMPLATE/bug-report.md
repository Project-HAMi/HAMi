---
name: Bug Report
about: Report a bug encountered while using HAMi across supported accelerator backends.
labels: kind/bug
---

<!-- Please use this template when reporting a bug and provide as much detail as possible.
     SECURITY REMINDER: Please ensure you redact any sensitive information
     (such as credentials, tokens, private registry secrets, passwords, or API keys)
     before posting logs, YAMLs, or configuration files.
-->

**What happened**:

**What you expected to happen**:

**How to reproduce it (as minimally and precisely as possible)**:

**Diagnostic Logs & Configuration**:
<!-- Please provide relevant logs, configurations, and outputs where applicable: -->
- Relevant Pod YAML or resource requests/limits configuration
- Pod events and status (e.g. `kubectl describe pod <pod-name>`)
- Relevant node annotations and status (e.g. `kubectl get node <node-name> -o yaml` or `kubectl describe node <node-name>`)
- `hami-scheduler` container logs
- `hami-device-plugin` container logs for the relevant backend
- Host container runtime configuration (e.g. `/etc/containerd/config.toml` or `/etc/docker/daemon.json`)
- Accelerator vendor diagnostic output, if available (e.g. `nvidia-smi`, `npu-smi`, `rocm-smi`, `cnmon`, `hy-smi`, `mx-smi`, `efsmi`, `xpu-smi`, `ixsmi`, `mthreads-gpus`, `br-smi`, `neuron-ls`, `vast-smi`)
- Host kubelet logs or kernel messages if relevant (e.g. `sudo journalctl -u kubelet`, `dmesg`)

**Environment**:
- HAMi version / Helm chart version / image tag:
- Accelerator vendor and hardware model (e.g. NVIDIA H100/A100, Ascend 910B, Cambricon MLU370, AMD MI300/MI210, Hygon DCU, MetaX, Enflame, Kunlun, Iluvatar, Moore Threads, Biren, AWS Neuron, Vastai):
- HAMi device backend in use (e.g. `nvidia`, `ascend`, `cambricon`, `amd`, `hygon`, `metax`, `enflame`, `kunlun`, `iluvatar`, `mthreads`, `biren`, `awsneuron`, `vastai`):
- Accelerator driver, firmware, and toolkit/runtime version (e.g. NVIDIA Driver / CUDA, Huawei CANN, AMD ROCm, Cambricon Neuware, etc.):
- Kubernetes version (`kubectl version`):
- Container runtime and version (e.g. containerd, Docker, CRI-O):
- Operating system and kernel version (`uname -a`):
- Others / Additional context:
