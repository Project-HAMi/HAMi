## Introduction

HAMi supports sharing Hygon HCU devices with the following resources:

- `hygon.com/hcunum`: number of virtual or physical HCUs
- `hygon.com/hcucores`: compute core percentage per vHCU
- `hygon.com/hcumem`: device memory in MiB per vHCU

### Deploy the HCU device plugin

Refer to [https://github.com/HYGON-AI/k8s-hcu-device-plugin](https://github.com/HYGON-AI/k8s-hcu-device-plugin).

### Run HCU jobs

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vhcu-pytorch-demo
spec:
  containers:
    - name: vhcu-pytorch-demo
      image: image.sourcefind.cn:5000/hcu/admin/base/pytorch:2.1.0-ubuntu22.04-dtk24.04.2-py3.10
      command: ["/bin/bash", "-c", "--"]
      args: ["sleep infinity & wait"]
      resources:
        limits:
          hygon.com/hcunum: 1   # requesting a vHCU
          hygon.com/hcucores: 60  # each vHCU use 60% of total compute cores
          hygon.com/hcumem: 2000  # each vHCU require 2000 MiB device memory
```

More examples are available under `examples/hygon/`.
