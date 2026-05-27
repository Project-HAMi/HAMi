## Introduction

HAMi now supports Enflame **DRS** hard partition scheduling, aligned with Enflame native scheduler behavior.

DRS is a hard-slice mode similar to NVIDIA MIG and Ascend VNPU templates.

## Prerequisites

* Enflame gcushare-device-plugin >= 2.2.14
* driver version >= 1.8.7
* kubernetes >= 1.24
* enflame-container-toolkit >= 2.0.50

## Enable Enflame DRS scheduling

* Deploy `gcushare-device-plugin` on Enflame nodes.
* Enable Enflame support in HAMi:

```bash
helm install hami hami-charts/hami --set devices.enflame.enabled=true -n kube-system
```

Default DRS resource:

* `enflame.com/drs-gcu`
* `enflame.com/gcu-memory`
* `enflame.com/gcu-core`

## Run DRS workloads

HAMi supports two request styles:

1. Direct DRS slice request (`enflame.com/drs-gcu`)
2. Unified memory/core request (`enflame.com/gcu-memory` + `enflame.com/gcu-core`), HAMi converts it to DRS profile internally.

### 1) Direct DRS slice request

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gcushare-pod-drs
  namespace: kube-system
spec:
  terminationGracePeriodSeconds: 0
  containers:
    - name: pod-gcu-example1
      image: ubuntu:18.04
      imagePullPolicy: IfNotPresent
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          enflame.com/drs-gcu: 3
        requests:
          enflame.com/drs-gcu: 3
```

### 2) Request by memory/core (recommended unified API)

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gcushare-pod-by-spec
  namespace: kube-system
spec:
  terminationGracePeriodSeconds: 0
  containers:
    - name: pod-gcu-example1
      image: ubuntu:18.04
      imagePullPolicy: IfNotPresent
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          enflame.com/gcu-memory: 20480 # MiB
          enflame.com/gcu-core: 40      # percent
        requests:
          enflame.com/gcu-memory: 20480
          enflame.com/gcu-core: 40
```

During scheduling HAMi writes DRS-compatible annotations such as:

* `assigned-containers`
* `enflame.com/gcu-assigned`
* `enflame.com/gcu-assigned-index`
* `enflame.com/gcu-assigned-minor`
* `enflame.com/gcu-request-size`

These annotations are then consumed by Enflame device-plugin allocate flow.
