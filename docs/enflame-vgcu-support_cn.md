## 简介

HAMi 现已支持燧原 **DRS 硬切分**调度，并对齐燧原原生调度器链路。

DRS 是类似 NVIDIA MIG / Ascend VNPU 的硬切分方案。

## 节点需求

* Enflame gcushare-device-plugin >= 2.2.14
* driver version >= 1.8.7
* kubernetes >= 1.24
* enflame-container-toolkit >=2.0.50

## 开启GCU复用

* 部署 `gcushare-device-plugin`，并确保版本支持 DRS。

> **注意:** *只需要安装gcushare-device-plugin，不要安装gcushare-scheduler-plugin.*

* 在安装HAMi时配置参数'devices.enflame.enabled=true'

```
helm install hami hami-charts/hami --set devices.enflame.enabled=true -n kube-system
```

> **说明:** 默认资源名称包括：
> - `enflame.com/drs-gcu`（直接 DRS 切片请求）
> - `enflame.com/gcu-memory`（按显存请求，单位 MiB）
> - `enflame.com/gcu-core`（按算力百分比请求）

## 运行GCU任务

HAMi 支持两种申请方式：

1. 直接申请 DRS 切片数（`enflame.com/drs-gcu`）
2. 按显存/算力申请（`enflame.com/gcu-memory` + `enflame.com/gcu-core`），由 HAMi 在调度内部换算为 DRS profile。

### 1）直接申请 DRS 切片

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
      command:
        - sleep
      args:
        - '100000'
      resources:
        limits:
          enflame.com/drs-gcu: 3
        requests:
          enflame.com/drs-gcu: 3
```
> **注意:** *查看更多的[用例](../examples/enflame/).*

### 2）按显存/算力申请（统一 API）

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
      command:
        - sleep
      args:
        - '100000'
      resources:
        limits:
          enflame.com/gcu-memory: 20480 # MiB
          enflame.com/gcu-core: 40      # 百分比
        requests:
          enflame.com/gcu-memory: 20480
          enflame.com/gcu-core: 40
```

## 注意事项

HAMi 在调度阶段会写入与 DRS 兼容的注解，包括：

* `assigned-containers`
* `enflame.com/gcu-assigned`
* `enflame.com/gcu-assigned-index`
* `enflame.com/gcu-assigned-minor`
* `enflame.com/gcu-request-size`

这些注解将由燧原 device-plugin 在 Allocate 阶段继续处理并完成 DRS instance 绑定。