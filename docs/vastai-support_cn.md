## 简介

本组件支持复用瀚博设备，并为此提供以下几种复用功能，包括：

***支持整卡模式和die模式***: 目前只支持整卡模式和die模式

***die模式拓扑感知***: die模式下，申请多个资源时尽可能的分配到同一个AIC上

***设备 UUID 选择***: 你可以通过注解指定使用或排除特定的设备

## 复用瀚博设备

### 开启复用瀚博设备

#### 给node打标签

```
kubectl label node {vastai-node} vastai=on
```

#### 部署 vastai-device-plugin

##### 整卡模式

```
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: hami-vastai
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "update", "watch", "patch"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: hami-vastai
subjects:
  - kind: ServiceAccount
    name: hami-vastai
    namespace: kube-system
roleRef:
  kind: ClusterRole
  name: hami-vastai
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: hami-vastai
  namespace: kube-system
  labels:
    app.kubernetes.io/component: "hami-vastai"
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: vastai-device-plugin-daemonset
  namespace: kube-system
  labels:
    app.kubernetes.io/component: hami-vastai-device-plugin
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: hami-vastai-device-plugin
      hami.io/webhook: ignore
  updateStrategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        app.kubernetes.io/component: hami-vastai-device-plugin
        hami.io/webhook: ignore
    spec:
      priorityClassName: "system-node-critical"
      serviceAccountName: hami-vastai
      nodeSelector:
        vastai-device: "vastai"
      containers:
        - image: projecthami/vastai-device-plugin:latest
          imagePullPolicy: Always
          name: vastai-device-plugin-dp
          env:
          - name: NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
          args: ["--fail-on-init-error=false", "--pass-device-specs=true"]
          securityContext:
            privileged: true
          volumeMounts:
            - name: device-plugin
              mountPath: /var/lib/kubelet/device-plugins
            - name: libvaml-lib
              mountPath: /usr/lib/libvaml.so
            - name: libvaml-lib64
              mountPath: /usr/lib64/libvaml.so
      volumes:
        - name: device-plugin
          hostPath:
            path: /var/lib/kubelet/device-plugins
        - name: libvaml-lib
          hostPath:
            path: /usr/lib/libvaml.so
        - name: libvaml-lib64
          hostPath:
            path: /usr/lib64/libvaml.so
      nodeSelector:
        vastai: "on"
```

##### die 模式

```
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: hami-vastai
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "update", "watch", "patch"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: hami-vastai
subjects:
  - kind: ServiceAccount
    name: hami-vastai
    namespace: kube-system
roleRef:
  kind: ClusterRole
  name: hami-vastai
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: hami-vastai
  namespace: kube-system
  labels:
    app.kubernetes.io/component: "hami-vastai"
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: vastai-device-plugin-daemonset
  namespace: kube-system
  labels:
    app.kubernetes.io/component: hami-vastai-device-plugin
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: hami-vastai-device-plugin
      hami.io/webhook: ignore
  updateStrategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        app.kubernetes.io/component: hami-vastai-device-plugin
        hami.io/webhook: ignore
    spec:
      priorityClassName: "system-node-critical"
      serviceAccountName: hami-vastai
      nodeSelector:
        vastai-device: "vastai"
      containers:
        - image: projecthami/vastai-device-plugin:latest
          imagePullPolicy: Always
          name: vastai-device-plugin-dp
          env:
          - name: NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
          args: ["--fail-on-init-error=false", "--pass-device-specs=true", "--device-strategy=die", "--rename-on-die=false"]
          securityContext:
            privileged: true
          volumeMounts:
            - name: device-plugin
              mountPath: /var/lib/kubelet/device-plugins
            - name: libvaml-lib
              mountPath: /usr/lib/libvaml.so
            - name: libvaml-lib64
              mountPath: /usr/lib64/libvaml.so
      volumes:
        - name: device-plugin
          hostPath:
            path: /var/lib/kubelet/device-plugins
        - name: libvaml-lib
          hostPath:
            path: /usr/lib/libvaml.so
        - name: libvaml-lib64
          hostPath:
            path: /usr/lib64/libvaml.so
      nodeSelector:
        vastai: "on"
```

### 运行瀚博任务

```
apiVersion: v1
kind: Pod
metadata:
  name: vastai-pod
spec:
  restartPolicy: Never
  containers:
  - name: vastai-container
    image: harbor.vastaitech.com/ai_deliver/vllm_vacc:VVI-25.12.SP2
    command: ["sleep", "infinity"]
    resources:
      limits:
        vastaitech.com/va: "1"
```

## 注意事项
1. 申请瀚博资源时不可以指定显存大小
2. `vastai-device-plugin` 没有把 `vasmi` 文件挂载到容器中。如果想在容器里使用 `vasmi` 命令，请自行挂载
