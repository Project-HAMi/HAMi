## Introduction

We now support sharing `birentech.com/gpu` (Biren) devices.

#### Deploy the `biren-device-plugin`

refer https://github.com/Project-HAMi/biren-device-plugin

### Run Biren jobs

```
apiVersion: v1
kind: Pod
metadata:
  name: pod1
spec:
  restartPolicy: OnFailure
  containers:
    - image: ubuntu
      name: pod1
      command: ["sleep"]
      args: ["infinity"]
      resources:
        limits:
          birentech.com/gpu: 1
```