## Benchmarks

Three instances from ai-benchmark have been used to evaluate vGPU-device-plugin performance as follows

| Test Environment | description                                              |
| ---------------- | :------------------------------------------------------: |
| Kubernetes version | v1.12.9                                                |
| Docker  version    | 18.09.1                                                |
| GPU Type           | Tesla V100                                             |
| GPU Num            | 2                                                      |

| Test instance |                         description                         |
| ------------- | :---------------------------------------------------------: |
| nvidia-device-plugin      |               k8s + nvidia k8s-device-plugin                |
| vGPU-device-plugin        | k8s + VGPU k8s-device-plugin，without virtual device memory |
| vGPU-device-plugin(virtual device memory) |  k8s + VGPU k8s-device-plugin，with virtual device memory   |

Test Cases:

| test id |     case      |   type    |         params          |
| ------- | :-----------: | :-------: | :---------------------: |
| 1.1     | Resnet-V2-50  | inference |  batch=50,size=346*346  |
| 1.2     | Resnet-V2-50  | training  |  batch=20,size=346*346  |
| 2.1     | Resnet-V2-152 | inference |  batch=10,size=256*256  |
| 2.2     | Resnet-V2-152 | training  |  batch=10,size=256*256  |
| 3.1     |    VGG-16     | inference |  batch=20,size=224*224  |
| 3.2     |    VGG-16     | training  |  batch=2,size=224*224   |
| 4.1     |    DeepLab    | inference |  batch=2,size=512*512   |
| 4.2     |    DeepLab    | training  |  batch=1,size=384*384   |
| 5.1     |     LSTM      | inference | batch=100,size=1024*300 |
| 5.2     |     LSTM      | training  | batch=10,size=1024*300  |

Test Result: ![img](../imgs/benchmark_inf.png)

![img](../imgs/benchmark_train.png)

To reproduce:

1. install k8s-vGPU-scheduler，and configure properly
2. run benchmark job

```
$ kubectl apply -f benchmarks/ai-benchmark/ai-benchmark.yml
```

3. View the result by using kubctl logs

```
$ kubectl logs [pod id]