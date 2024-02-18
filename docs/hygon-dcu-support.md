## Introduction

**We now support hygon.com/dcu by implementing most device-sharing features as nvidia-GPU**, including:

***DCU sharing***: Each task can allocate a portion of DCU instead of a whole DCU card, thus DCU can be shared among multiple tasks.

***Device Memory Control***: DCUs can be allocated with certain device memory size on certain type(i.e Z100) and have made it that it does not exceed the boundary.

***Device compute core limitation***: DCUs can be allocated with certain percentage of device core(i.e hygon.com/dcucores:60 indicate this container uses 60% compute cores of this device)

***DCU Type Specification***: You can specify which type of DCU to use or to avoid for a certain task, by setting "hygon.com/use-dcutype" or "hygon.com/nouse-dcutype" annotations. 

## Prerequisites

* dtk driver with virtualization enabled(i.e dtk-22.10.1-vdcu), try the following command to see if your driver has virtualization ability

```
hdmcli -show-device-info
```

If this command can't be found, then you should contact your device provider to aquire a vdcu version of dtk driver.

* The absolute path of dtk driver on each dcu node must be the same(i.e placed in /root/dtk-driver)

## Enabling DCU-sharing Support

* Install the chart using helm, See 'enabling vGPU support in kubernetes' section [here](https://github.com/Project-HAMi/HAMi#enabling-vgpu-support-in-kubernetes), please be note that, you should set your dtk driver directory using --set devicePlugin.hygondriver={your dtk driver path on each nodes}, for example:

```
helm install vgpu vgpu-charts/vgpu --set devicePlugin.hygondriver="/root/dcu-driver/dtk-22.10.1-vdcu" --set scheduler.kubeScheduler.imageTag={your k8s server version} -n kube-system
```

* Tag DCU node with the following command
```
kubectl label node {dcu-node} dcu=on
```

## Running DCU jobs

Hygon DCUs can now be requested by a container
using the `hygon.com/dcunum` , `hygon.com/dcumem` and `hygon.com/dcucores` resource type:

```
apiVersion: v1
kind: Pod
metadata:
  name: alexnet-tf-gpu-pod-mem
  labels:
    purpose: demo-tf-amdgpu
spec:
  containers:
    - name: alexnet-tf-gpu-container
      image: pytorch:resnet50
      workingDir: /root
      command: ["sleep","infinity"]
      resources:
        limits:
          hygon.com/dcunum: 1 # requesting a GPU
          hygon.com/dcumem: 2000 # each dcu require 2000 MiB device memory
          hygon.com/dcucores: 60 # each dcu use 60% of total compute cores

```

## Enable vDCU inside container

You need to enable vDCU inside container in order to use it.
```
source /opt/hygondriver/env.sh
```

check if you have successfully enabled vDCU by using following command

```
hdmcli -show-device-info
```

If you have an output like this, then you have successfully enabled vDCU inside container.

```
Device 0:
	Actual Device: 0
	Compute units: 60
	Global memory: 2097152000 bytes
```

Launch your DCU tasks like you usually do

## Notes

1. DCU-sharing in init container is not supported, pods with "hygon.com/dcumem" in init container will never be scheduled.

2. Only one vdcu can be aquired per container. If you want to mount multiple dcu devices, then you shouldn't set `hygon.com/dcumem` or `hygon.com/dcucores`

   