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

- The output of `nvidia-smi -a` on your host
- Your docker or containerd configuration file (e.g: `/etc/docker/daemon.json`)
- The hami-device-plugin container logs
- The hami-scheduler container logs
- The kubelet logs on the node (e.g: `sudo journalctl -r -u kubelet`)
- Any relevant kernel output lines from `dmesg`

**Environment**:
- HAMi version:
- nvidia driver or other AI device driver version:
- Docker version from `docker version`
- Docker command, image and tag used
- Kernel version from `uname -a`
- Others:

