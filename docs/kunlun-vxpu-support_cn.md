## 简介

本组件支持复用昆仑芯XPU设备(P800-OAM)，并为此提供以下几种与vGPU类似的复用功能，包括：

***XPU 共享***: 每个任务可以只占用一部分显卡，多个任务可以共享一张显卡

***可限制分配的显存大小***: 你现在可以用显存值（例如24576M）来分配XPU，本组件会确保任务使用的显存不会超过分配数值

***设备 UUID 选择***: 你可以通过注解指定使用或排除特定的 XPU 设备


## 节点需求
* driver version >= 5.0.21.16
* xpu-container-toolkit >= xpu_container_1.0.2-1
* XPU device type: P800-OAM

## 开启GPU复用

* 获取[vxpu-device-plugin](https://hub.docker.com/r/riseunion/vxpu-device-plugin)

* 部署`vxpu-device-plugin`，清单如下:
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


> **说明:** 默认资源名称如下：
> - `kunlunxin.com/vxpu` 用于 VXPU 数量
> - `kunlunxin.com/vxpu-memory` 用于内存分配
>
> 你可以通过上述参数自定义这些名称。

## 设备粒度切分

XPU P800-OAM支持2种粒度的切分，分别是1/4卡和1/2卡，分配的显存会自动对齐。规则如下：
> - 申请显存小于等于24576M(24G)，会自动对齐成24576M(24G)
> - 申请显存大于24576M(24G)，小于等于49152M(48G)，会自动对齐成49152M(48G)
> - 申请显存大于49152M(48G)，会按整卡分配

## 运行XPU任务

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
n
> **注意2:** *查看更多的[用例](../examples/kunlun/).*

## 设备 UUID 选择

你可以通过 Pod 注解来指定要使用或排除特定的 GPU 设备：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: poddemo
  annotations:
    # 使用特定的 XPU 设备（逗号分隔的列表）
    hami.io/use-xpu-uuid: ""
    # 或者排除特定的 XPU 设备（逗号分隔的列表）
    hami.io/no-use-xpu-uuid: ""
spec:
  # ... 其余 Pod 配置
```

> **说明:** 设备 ID 格式为 `{BusID}`。你可以在节点状态中找到可用的设备 ID。

### 查找设备 UUID

你可以使用以下命令查找节点上昆仑芯P800-OAM XPU 设备 UUID：

```bash
kubectl get pod <pod-name> -o yaml | grep -A 10 "hami.io/xpu-devices-allocated"
```

或者通过检查节点注解：

```bash
kubectl get node <node-name> -o yaml | grep -A 10 "hami.io/node-register-xpu"
```

在节点注解中查找包含设备信息的注解。


## 注意事项

当前昆仑芯驱动最多支持32个句柄，8张XPU设备占8个句柄，无法支持8个设备都切成4个。
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
