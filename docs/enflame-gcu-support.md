## Introduction

This component supports scheduling Enflame GCU devices (S60) by whole card.

## Node Requirements

* Enflame gcu-k8s-device-plugin >= 2.0
* driver version >= 1.2.3.14
* kubernetes >= 1.24
* enflame-container-toolkit >=2.0.50

## Enable GCU Scheduling

* Deploy 'gcu-k8s-device-plugin'. Enflame's GCU whole card scheduling requires the use of the manufacturer-provided 'gcu-k8s-device-plugin'. Please contact the device provider to obtain it.

* Configure the parameter 'devices.enflame.enabled=true' when installing HAMi.

```
helm install hami hami-charts/hami --set devices.enflame.enabled=true -n kube-system
```

> **Note:** Default resource names are as follows:
> - `enflame.com/gcu` used for requesting GCU count, can not be modified

## Running GCU Tasks

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gcu-pod-1
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
          enflame.com/gcu: 1
```
> **Note:** *See more [examples](../examples/enflame/).*
