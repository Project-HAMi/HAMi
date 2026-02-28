## 简介

本组件支持复用瀚博设备，并为此提供以下几种复用功能，包括：

***支持整卡模式和die模式***: 目前只支持整卡模式和die模式

***die模式拓扑感知***: die模式下，申请多个资源时尽可能的分配到同一个AIC上

***设备 UUID 选择***: 你可以通过注解指定使用或排除特定的设备

## 复用瀚博设备

### 开启复用瀚博设备

部署 vastai-device-plugin。部署方式参考 https://github.com/Project-HAMi/vastai-device-plugin/?tab=readme-ov-file#deployment


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
