## Introduction

This component supports multiplexing Kunlunxin XPU devices (P800-OAM) and provides the following vGPU-like multiplexing capabilities:

***XPU Sharing***: Each task can occupy only a portion of the device, allowing multiple tasks to share a single XPU

***Memory Allocation Limits***: You can now allocate XPUs using memory values (e.g., 24576M), and the component ensures that tasks do not exceed the allocated memory limit

***Device UUID Selection***: You can specify to use or exclude specific XPU devices through annotations


## Prerequisites
* driver version >= 5.0.21.16
* xpu-container-toolkit >= xpu_container_1.0.2-1
* XPU device type: P800-OAM

## Enable XPU-sharing Support

* Obtain [vxpu-device-plugin](https://hub.docker.com/r/riseunion/vxpu-device-plugin)

* Deploy [vxpu-device-plugin]
```
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: vxpu-device-plugin
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "update", "watch", "patch"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: vxpu-device-plugin
subjects:
  - kind: ServiceAccount
    name: vxpu-device-plugin
    namespace: kube-system
roleRef:
  kind: ClusterRole
  name: vxpu-device-plugin
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: vxpu-device-plugin
  namespace: kube-system
  labels:
    app.kubernetes.io/component: vxpu-device-plugin
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: vxpu-device-plugin
  namespace: kube-system
  labels:
    app.kubernetes.io/component: vxpu-device-plugin
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: vxpu-device-plugin
  template:
    metadata:
      labels:
        app.kubernetes.io/component: vxpu-device-plugin
        hami.io/webhook: ignore
    spec:
      priorityClassName: "system-node-critical"
      serviceAccountName: vxpu-device-plugin
      containers:
        - image: riseunion/vxpu-device-plugin:v1.0.0
          name: device-plugin
          resources:
            requests:
              memory: 500Mi
              cpu: 500m
            limits:
              memory: 500Mi
              cpu: 500m
          args:
            - xpu-device-plugin
            - --memory-unit=MiB
            - --resource-name=kunlunxin.com/vxpu
            - -logtostderr
          securityContext:
            privileged: true
            capabilities:
              add: [ "ALL" ]
          volumeMounts:
            - name: device-plugin
              mountPath: /var/lib/kubelet/device-plugins
            - name: xre
              mountPath: /usr/local/xpu
            - name: dev
              mountPath: /dev
          env:
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: KUBECONFIG
              value: /etc/kubernetes/kubelet.conf
      volumes:
        - name: device-plugin
          hostPath:
            path: /var/lib/kubelet/device-plugins
        - name: xre
          hostPath:
            path: /usr/local/xpu
        - name: dev
          hostPath:
            path: /dev
      nodeSelector:
        xpu: "on"
```


> **Note:** Default resource names are as follows:
> - `kunlunxin.com/vxpu` for VXPU count
> - `kunlunxin.com/vxpu-memory` for memory allocation
>
> You can customize these names using the parameters above.

## Device Granularity Partitioning

XPU P800-OAM supports 2 levels of partitioning granularity: 1/4 card and 1/2 card, with memory allocation automatically aligned. The rules are as follows:
> - Requested memory ≤ 24576M (24G) will be automatically aligned to 24576M (24G)
> - Requested memory > 24576M (24G) and ≤ 49152M (48G) will be automatically aligned to 49152M (48G)
> - Requested memory > 49152M (48G) will be allocated as full cards

## Running XPU Tasks

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: vxpu-pod-demo
spec:
  containers:
    - name: vxpu-pod-demo
      image: pytorch:resnet50
      workingDir: /root
      command: ["sleep","infinity"]
      resources:
        limits:
          kunlunxin.com/vxpu: 1 # requesting a VXPU
          kunlunxin.com/vxpu-memory: 24576 # requesting a virtual XPU that requires 24576 MiB of device memorymemory
```

> **Note:** *See more [examples](../examples/kunlun/).*

## Device UUID Selection

You can specify to use or exclude specific XPU devices through Pod annotations:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
  annotations:
    # Use specific XPU devices (comma-separated list)
    hami.io/use-xpu-uuid: ""
    # Or exclude specific XPU devices (comma-separated list)
    hami.io/no-use-xpu-uuid: ""
spec:
  # ... rest of Pod configuration
```

> **Note:** Device ID format is `{BusID}`. You can find available device IDs in the node status.

### Finding Device UUIDs

You can use the following commands to find Kunlunxin P800-OAM XPU device UUIDs on nodes:

```bash
kubectl get pod <pod-name> -o yaml | grep -A 10 "hami.io/xpu-devices-allocated"
```

Or by checking node annotations:

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "hami.io/node-register-xpu"
```

Look for annotations containing device information in the node annotations.


## Important Notes

The current Kunlun chip driver supports a maximum of 32 handles. Eight XPU devices occupy 8 handles, so it is not possible to split all 8 devices into 4 each.
```yaml
# valid
kunlunxin.com/vxpu: 8

# valid
kunlunxin.com/vxpu: 6
kunlunxin.com/vxpu-memory: 24576

# valid
kunlunxin.com/vxpu: 8
kunlunxin.com/vxpu-memory: 49152

# invalid
kunlunxin.com/vxpu: 8 # not support
kunlunxin.com/vxpu-memory: 24576
```
