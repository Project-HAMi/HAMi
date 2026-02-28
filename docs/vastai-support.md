## Introduction

We now support sharing `vastaitech.com/va` (Vastaitech) devices and provides the following capabilities:

***Supports both Full-Card mode and Die mode***: Only Full-Card mode and Die mode are supported currently.

***die-mode topology awareness***: When multiple resources are requested in die mode, the scheduler will try to allocate them on the same AIC whenever possible.

***Device UUID selection***: You can specify or exclude particular devices through annotations.

## Using Vastai Devices

### Enabling Vastai Device Sharing

Deploy the `vastai-device-plugin`. Refer to the deployment guide: https://github.com/Project-HAMi/vastai-device-plugin/?tab=readme-ov-file#deployment

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