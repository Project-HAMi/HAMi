## 简介

本组件支持按整卡调度燧原GCU设备(S60)

## 节点需求

* Enflame gcu-k8s-device-plugin >= 2.0
* driver version >= 1.2.3.14
* kubernetes >= 1.24
* enflame-container-toolkit >=2.0.50

## 开启GCU调度

* 部署'gcu-k8s-device-plugin'，燧原的GCU整卡调度需要配合厂家提供的'gcu-k8s-device-plugin'一起使用，请联系设备提供方获取

* 在安装HAMi时配置参数'devices.enflame.enabled=true'

```
helm install hami hami-charts/hami --set devices.enflame.enabled=true -n kube-system
```

> **说明:** 资源名称如下：
> - `enflame.com/gcu` 用于GCU数量，不支持修改


## 运行GCU任务

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
> **注意:** *查看更多的[用例](../examples/enflame/).*
