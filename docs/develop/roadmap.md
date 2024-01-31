# roadmap

| feature            | description                                                                                                                            |  release | Example  | Example expected behaviour |
|--------------------|----------------------------------------------------------------------------------------------------------------------------------------|---------------|--------------|------------|
| Kubernetes schedule layer       | Support Resource Quota for vgpu-memory                                                                                                                            | v3.2.0        | "requests.nvidia.com/gpu-memory: 30000" in ResourceQuota      | Pods in this namespace can allocate up to 30G device memory in this namespace     |
|                    | Support Best-fit, idle-first, Numa-first Schedule Policy                                                                                                                     | v3.2.0        | add "scheduler policy configmap"       |  execute schedule policy according to configMap          |
|                    |  Support k8s 1.28 version with compatable to v1.16                                                                                                                   | v3.1.0        |        |            |
| Add more Heterogeneous AI computing device                    | HuaWei Ascend Support                                                                                                                 | v3.1.0        |              |            |
|                    | Iluvatar GPU support                                                                                                                     | v3.1.0        |              |            |
|                    |Teco DPU Support                                                                                                                    | v3.2.0        |              |            |
