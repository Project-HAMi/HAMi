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

Please include the following information:

- The Pod manifest, or at least its accelerator resource requests and limits
- Pod events and relevant accelerator-related node annotations
- Output from the vendor's diagnostic tool, when available (for example, `nvidia-smi -a` for NVIDIA devices)
- Your container runtime configuration (for example, `/etc/docker/daemon.json` or the relevant containerd configuration)
- Logs from the device plugin for the affected accelerator
- The hami-scheduler logs
- The kubelet logs from the affected node (for example, `sudo journalctl -r -u kubelet`)
- Relevant kernel output from `dmesg`

> **Security note:** Remove credentials, tokens, registry secrets, private image names, and other sensitive information before posting manifests, configuration files, or logs.

**Environment**:
- HAMi version:
- Kubernetes version:
- Accelerator vendor and model:
- HAMi device backend and requested resource names (for example, `nvidia.com/gpu`):
- Accelerator driver and runtime version:
- Container runtime and version:
- Workload image and tag (redact private image names and state when redacted):
- Operating system and kernel version (for example, the output of `uname -a`):
- Others:
