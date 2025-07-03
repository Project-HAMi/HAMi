# GPU Topology-Aware Scheduling Introduction

HAMi supports GPU topology-aware scheduling in vGPU environments. HAMi can optimize GPU card scheduling based on the topological relationships between GPUs, thereby improving GPU resource utilization and performance.

You can use the `nvidia-smi topo -m` command to view the topological relationships between GPUs.

## How to Enable Nvidia GPU Topology-Aware Scheduling
1. Set the environment variable `ENABLE_TOPOLOGY_SCORE: 'true'` in hami-device-plugin.
2. Restart hami-device-plugin.
3. Create a Pod requesting 2 or more vGPUs.

   3.1 Global Setting

        Add `gpu-scheduler-policy=togology` when starting hami-scheduler

   3.2 Pod-Level Setting

        Set `hami.io/gpu-scheduler-policy: topology` in the Annotations

4. Submit the Pod and check the logs of hami-scheduler. HAMi will allocate the optimal GPU combination for you.

   Please check the log level is greater than 5
```bash  
I0703 08:34:27.032644       1 device.go:708] "device allocate success" pod="default/testpod" best device combination={"NVIDIA":[{"Idx":7,"UUID":"GPU-dsaf","Type":"NVIDIA","Usedmem":1024,"Usedcores":0,"CustomInfo":null},{"Idx":5,"UUID":"GPU-gads","Type":"NVIDIA","Usedmem":1024,"Usedcores":0,"CustomInfo":null}]}  
```  

## How it works
For details, refer to gpu-topo-policy.md