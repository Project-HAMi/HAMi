## Introduction

**We now support sharing on enflame.com/gcu(i.e S60) by implementing most device-sharing features as nvidia-GPU**, including:

***GCU sharing***: Each task can allocate a portion of GCU instead of a whole GCU card, thus GCU can be shared among multiple tasks.

***Device Memory and Core Control***: GCUs can be allocated with certain percentage of device memory and core, we make sure that it does not exceed the boundary.

***Device UUID Selection***: You can specify which GCU devices to use or exclude using annotations.

***Very Easy to use***: You don't need to modify your task yaml to use our scheduler. All your GPU jobs will be automatically supported after installation.

## Prerequisites

* Enflame gcushare-device-plugin >= 2.1.6 (please consult your device provider, gcushare has two components: gcushare-scheduler-plugin and gcushare-device-plugin, we only need gcushare-device-plugin here )
* driver version >= 1.2.3.14
* kubernetes >= 1.24
* enflame-container-toolkit >=2.0.50

## Enabling GCU-sharing Support

* Deploy gcushare-device-plugin on enflame nodes (Please consult your device provider to acquire its package and document)

> **NOTICE:** *Install only gpushare-device-plugin, don't install gpu-scheduler-plugin package.*

> **NOTE:** The default resource names are:
> - `enflame.com/vgcu` for GCU count, only support 1 now.
> - `enflame.com/vgcu-percentage` for the percentage of memory and cores in a gcu slice.
>
> You can customize these names by modifying `hami-scheduler-device` configMap above.

* Set 'devices.enflame.enabled=true' when deploy HAMi

```
helm install hami hami-charts/hami --set devices.enflame.enabled=true -n kube-system
```

## Device Granularity

HAMi divides each Enflame GCU into 100 units for resource allocation. When you request a portion of a GPU, you're actually requesting a certain number of these units.

### GCU Slice Allocation

- Each unit of `enflame.com/vgcu-percentage` represents 1% device memory and 1% core
- If you don't specify a memory request, the system will default to using 100% of the available memory
- Memory allocation is enforced with hard limits to ensure tasks don't exceed their allocated memory
- Core allocation is enforced with hard limits to ensure tasks don't exceed their allocated cores

## Running Enflame jobs

Enflame GCUs can now be requested by a container
using the `enflame.com/vgcu` and `enflame.com/vgcu-percentage`  resource type:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gcushare-pod-2
  namespace: kube-system
spec:
  terminationGracePeriodSeconds: 0
  containers:
    - name: pod-gcu-example1
      image: ubuntu:18.04
      imagePullPolicy: IfNotPresent
      command:
        - sleep
      args:
        - '100000'
      resources:
        limits:
          enflame.com/vgcu: 1
          enflame.com/vgcu-percentage: 22
```

> **NOTICE:** *You can find more examples in [examples/enflame folder](../examples/enflame/)*

## Device UUID Selection

You can specify which GPU devices to use or exclude using annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
  annotations:
    # Use specific GPU devices (comma-separated list)
    enflame.com/use-gpuuuid: "node1-enflame-0,node1-enflame-1"
    # Or exclude specific GPU devices (comma-separated list)
    enflame.com/nouse-gpuuuid: "node1-enflame-2,node1-enflame-3"
spec:
  # ... rest of pod spec
```

> **NOTE:** The device ID format is `{node-name}-enflame-{index}`. You can find the available device IDs in the node status.

### Finding Device UUIDs

You can find the UUIDs of Enflame GCUs on a node using the following command:

```bash
kubectl get pod <pod-name> -o yaml | grep -A 10 "hami.io/<card-type>-devices-allocated"
```

Or by examining the node annotations:

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "hami.io/node-register-<card-type>"
```

Look for annotations containing device information in the node status.

## Notes

1. GCUshare takes effect only for containers that apply for one GCU(i.e enflame.com/vgcu=1 ).

2. Multiple GCU allocation in one container is not supported yet

3. `efsmi` inside container shows the total device memory, which is NOT a bug, device memory will be properly limited when running tasks.
