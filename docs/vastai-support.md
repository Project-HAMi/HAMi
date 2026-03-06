## Introduction

We now support sharing `vastaitech.com/va` (Vastaitech) devices and provides the following capabilities:

***Supports both Full-Card mode and Die mode***: Only Full-Card mode and Die mode are supported currently.

***die-mode topology awareness***: When multiple resources are requested in die mode, the scheduler will try to allocate them on the same AIC whenever possible.

***Device UUID selection***: You can specify or exclude particular devices through annotations.

## Using Vastai Devices

### Enabling Vastai Device Sharing

#### Label the Node

```
kubectl label node {vastai-node} vastai=on
```

#### Deploy the `vastai-device-plugin`

##### Full Card Mode

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

##### Die Mode

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

### Run Vastai jobs

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

## Notes
1. When requesting Vastai resources, you cannot specify the memory size.
2. The `vastai-device-plugin` does not mount the `vasmi` into the container.If you need to use the `vasmi` command inside the container, please mount it manually.