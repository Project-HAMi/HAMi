# Tasks

## Support Moore threads MTT S4000

```
resources:
requests:
  mthreads.com/gpu: ${num}
  mthreads.com/vcuda-core: ${core}
  mthreads.com/vcuda-memory: ${mem}
limits:
  mthreads.com/gpu: ${num}
  mthreads.com/vcuda-core: ${core}
  mthreads.com/vcuda-memory: ${mem}
```

## Support Birentech Model 110

```
resources:
requests:
  birentech.com/gpu: ${num}
  birentech.com/vcuda-core: ${core}
  birentech.com/vcuda-memory: ${mem}
limits:
  birentech.com/gpu: ${num}
  birentech.com/vcuda-core: ${core}
  birentech.com/vcuda-memory: ${mem}
```

## Support iluvatar MR-V100

```
resources:
requests:
  iluvatar.ai/gpu: ${num}
  iluvatar.ai/vcuda-core: ${core}
  iluvatar.ai/vcuda-memory: ${mem}
limits:
  iluvatar.ai/gpu: ${num}
  iluvatar.ai/vcuda-core: ${core}
  iluvatar.ai/vcuda-memory: ${mem}
```

## Support HuaWei Ascend 910B device

```
resources:
  requests:
    ascend.com/npu: ${num}
    ascend.com/npu-core: ${core}
    ascend.com/npu-mem: ${mem}
  limits:
    ascend.com/npu: ${num}
    ascend.com/npu-core: ${core}
    ascend.com/npu-mem: ${mem}
```

## Support resourceQuota for Kubernetes

Description: ResourceQuota is frequently used in kubernetes namespace. Since the number of virtual devices doesn't mean anything, we need to support the limitation in deviceMemory.

For example, the following resourceQuota
```
cat <<EOF > compute-resources.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: compute-resources
spec:
  hard:
    requests.cpu: "1"
    requests.memory: 1Gi
    limits.cpu: "2"
    limits.memory: 2Gi
    requests.nvidia.com/gpu-memory: 30000
EOF
```

with the following command
```
kubectl create -f ./compute-resources.yaml--namespace=myspace
```

will limit the maxinum device memory allocated to namespace 'myspace' to 30G

## Support multiple schedule policies

Description: HAMi needs to support multiple schedule policies, to provide meets the need in complex senarios, a pod can select a schedule policy in annotations field.

The effect of each schedule policy is shown in the table below

| Schedule Policy    | Effect |
| -------- | ------- |
| best-fit  | the fewer device memory remains, the higher score    |
| idle-first | idle GPU has higher score     |
| numa-first    | for multiple GPU allocations, GPUs on the same numa have higher score    |


For example, if a pod want to select a 'best-fit' schedule policy, it can specify .metadata.annotations as the code below:

```
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  annotations:
    nvidia.com/schedule-policy: "best-fit"
spec:
  containers:
    - name: ubuntu-container
      image: ubuntu:18.04
      command:["bash"，"-c"，"sleep 86400"]
      resources:
        limits:
          nvidia.com/gpu: 2 # requesting 2 VGPUs
```

