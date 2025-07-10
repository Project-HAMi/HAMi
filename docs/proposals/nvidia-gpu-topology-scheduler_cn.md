# GPU 拓扑感知介绍

HAMi 支持 vGPU 环境下的 GPU 拓扑感知调度。HAMi 可以根据 GPU 之间的拓扑关系优化 GPU 卡的调度，从而提高 GPU 资源的利用率和性能。

您可以使用 `nvidia-smi topo -m` 命令查看 GPU 之间的拓扑关系。

## 如何启用 Nvidia GPU 拓扑感知调度
1. 在 hami-device-plugin 中设置环境变量 `ENABLE_TOPOLOGY_SCORE: 'true'`。
2. 重启 hami-device-plugin。
3. 创建一个 Pod，申请 2 个或更多 vGPU。

    3.1 全局设置

        hami-scheduler 启动时新增 `gpu-scheduler-policy=togology`

    3.2 Pod 级别设置

        在 Annotations 中设置 `hami.io/gpu-scheduler-policy: topology`
  
4. 提交Pod，检查 hami-scheduler 的日志，HAMi 已为您分配最优的 GPU 组合。
   
   需要日志级别大于 5
```bash  
I0703 08:34:27.032644       1 device.go:708] "device allocate success" pod="default/testpod" best device combination={"NVIDIA":[{"Idx":7,"UUID":"GPU-dsaf","Type":"NVIDIA","Usedmem":1024,"Usedcores":0,"CustomInfo":null},{"Idx":5,"UUID":"GPU-gads","Type":"NVIDIA","Usedmem":1024,"Usedcores":0,"CustomInfo":null}]}  
```  

## 工作原理
详见 gpu-topo-policy.md