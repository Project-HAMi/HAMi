## Introduction

**We now support hygon.com/dcu by implementing most device-sharing features as nvidia-GPU**, including:

***DCU sharing***: Each task can allocate a portion of DCU instead of a whole DCU card, thus DCU can be shared among multiple tasks.

***Device Memory Control***: DCUs can be allocated with certain device memory size on certain type(i.e Z100) and have made it that it does not exceed the boundary.

***Device compute core limitation***: DCUs can be allocated with certain percentage of device core(i.e hygon.com/dcucores:60 indicate this container uses 60% compute cores of this device)

***DCU Type Specification***: You can specify which type of DCU to use or to avoid for a certain task, by setting "hygon.com/use-dcutype" or "hygon.com/nouse-dcutype" annotations. 

## Prerequisites

* DCU device driver >= 6.3.8

## Enabling DCU-sharing Support

* [DCU-Device-Plugin](https://developer.sourcefind.cn/document/87ee5c5b-c10d-11f0-b077-0242ac150003?id=8df80ff9-c10e-11f0-b077-0242ac150003)


## Running DCU jobs

Hygon DCUs can now be requested by a container
using the `hygon.com/dcunum` , `hygon.com/dcumem` and `hygon.com/dcucores` resource type:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vdcu-pytorch-demo
spec:
  containers:
    - name: vdcu-pytorch-demo
      image: image.sourcefind.cn:5000/dcu/admin/base/pytorch:2.1.0-ubuntu22.04-dtk24.04.2-py3.10
      command: [ "/bin/bash", "-c", "--" ]
      args: [ "sleep infinity & wait" ]
      resources:
        limits:
          hygon.com/dcunum: 1   # requesting a vDCU
          hygon.com/dcucores: 60  # each vDCU use 60% of total compute cores
          hygon.com/dcumem: 2000  # each vDCU require 2000 MiB device memory
```

## View the virtual DCU specifications within the container

To view the vDCU specifications within the container, it is necessary to configure the driver environment variables.
```
source /opt/hyhal/env.sh
```

Then, use the driver command to view the information of the virtual device.

```
hy-smi virtual -show-device-info
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
