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

**Anything else we need to know?**:

- Accelerator diagnostic command output on host (e.g., `nvidia-smi -a`, `npu-smi info`, `cnmon`, `rocm-smi`, or vendor equivalent)
- Your docker or containerd configuration file (e.g: `/etc/docker/daemon.json` or `/etc/containerd/config.toml`)
- The hami-device-plugin container logs
- The hami-scheduler container logs
- The kubelet logs on the node (e.g: `sudo journalctl -r -u kubelet`)
- Any relevant kernel output lines from `dmesg`

**Environment**:
- HAMi version:
- Accelerator vendor & architecture (e.g., NVIDIA, Huawei Ascend, Cambricon, Hygon, AMD, MetaX, Moore Threads, Iluvatar, AWS Neuron, etc.):
- Driver and runtime toolkit version (e.g., NVIDIA Driver/CUDA, Ascend CANN, Cambricon Neuware, ROCm, etc.):
- Container runtime & version (`docker version` or `crictl version`)
- Kernel version from `uname -a`
- Others: