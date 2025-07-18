# GPU Topology-Aware Scheduling Introduction

HAMi supports GPU topology-aware scheduling in vGPU environments. HAMi can optimize GPU card scheduling based on the topological relationships between GPUs, thereby improving GPU resource utilization and performance.

You can use the `nvidia-smi topo -m` command to view the topological relationships between GPUs.

## How to Enable Nvidia GPU Topology-Aware Scheduling
When installing HAMi, set `scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy` to `topology-aware`.
```bash  
helm install hami hami-charts/hami --set scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy=topology-aware -n kube-system  
```  
If HAMi is already installed, you can enable it through the following methods:

1. Device-plugin configuration

   Set the environment variable `ENABLE_TOPOLOGY_SCORE: 'true'` in the DaemonSet `hami-device-plugin`.

2. Global scheduler settings

   Add `gpu-scheduler-policy=topology-aware` when starting `hami-scheduler`.

3. Pod-level individual settings

   Set `hami.io/gpu-scheduler-policy: topology-aware` in the Pod annotations.

4. Submit the Pod and check the logs of `hami-scheduler`. HAMi will allocate the optimal GPU combination for you.

   The log level must be greater than 5.
```bash  
I0703 08:34:27.032644       1 device.go:708] "device allocate success" pod="default/testpod" best device combination={"NVIDIA":[{"Idx":7,"UUID":"GPU-dsaf","Type":"NVIDIA","Usedmem":1024,"Usedcores":0,"CustomInfo":null},{"Idx":5,"UUID":"GPU-gads","Type":"NVIDIA","Usedmem":1024,"Usedcores":0,"CustomInfo":null}]}  
```  

## How It Works
For details, refer to the translated document `gpu-topo-policy.md`.