# 从普通 HAMi 集群使用 Remote GPU

Remote GPU 让跑在无卡节点上的 Pod，通过网络使用其他节点上的 NVIDIA GPU。GPU 节点跑 [lupine](https://github.com/lupinemachines/lupine) server，客户端镜像里的 lupine libcuda 把 CUDA 调用转到那台机器。调度和显存上限仍由 HAMi 负责。

一个 Pod 独占一整台被划出去的 GPU 机器。

## 1. 划出服务节点

集群里要同时留下两类节点：

- **服务节点**：有 GPU。打上标签后只对外提供卡，新的 `nvidia.com/gpu` Pod 不再调度到这里。
- **客户端节点**：不打这个标签。Pod 调度到这里，再连到服务节点。CPU 节点即可；也可以留一部分 GPU 节点继续做本地 HAMi。

先打标签，再升级。device plugin 随升级重启时会读到标签，切到 remote 模式：不再向 kubelet 注册本地卡，只把卡登记给 Remote GPU。

```bash
kubectl label node gpu-1 hami.io/lupine-server=
```

空值表示 lupine 听默认端口 `14833`，chart 会在这些节点上拉起 `lupine-server`（hostNetwork）。标签值写成端口号时，chart 不管这台机器，需要自己保证该端口上已有 lupine。

```bash
helm upgrade hami charts/hami -n kube-system \
  --reuse-values \
  --set devices.remotegpu.enabled=true
```

已发布的 chart 带这个开关时，把 `charts/hami` 换成 `hami-charts/hami`。

节点的默认运行时不是 NVIDIA runtime 时，加上 `--set devices.remotegpu.server.runtimeClassName=nvidia`。

默认 server 镜像是 `ghcr.io/lupinemachines/lupine-server:cuda-13.3.1-ubuntu24.04`。驱动要能跑这个镜像，客户端镜像的 CUDA 版本与它一致。换版本改 `devices.remotegpu.server.image.tag`。

客户端节点要能访问服务节点 InternalIP 的 `14833`。

先升级、后打标签时，删掉该节点上的 device plugin Pod，让它重启后进入 remote 模式：

```bash
kubectl delete pod -n kube-system -l app.kubernetes.io/component=hami-device-plugin --field-selector spec.nodeName=gpu-1
```

## 2. 提交 Pod

镜像自带 lupine client。`LUPINE_SERVER` 由 webhook 按调度结果注入，Pod 里不用写。

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: remote-gpu-pod
spec:
  containers:
    - name: cuda
      image: ghcr.io/lupinemachines/lupine-client:cuda-13.3.1-ubuntu24.04
      command: ["nvidia-smi", "-L"]
      resources:
        limits:
          nvidia.com/remote-gpu: 1
          nvidia.com/remote-gpu-memory: 3000
```

- `nvidia.com/remote-gpu`：目标机器至少要有这么多张卡。Pod 能看到这台机器上的每一张卡，这台机器不会再分给别的 Pod。
- `nvidia.com/remote-gpu-memory`：可选，每张卡的显存上限，单位 MB。写了才会注入 HAMi-core；不写则不限制。
- 申请的卡数超过任意一台服务机器时，即使集群里卡的总数够，也调度不上。
- Deployment 的每个副本各占一台服务机器，副本数不能超过服务节点数。
- 同一个 Pod 里的多个容器共用这一台机器。

## 3. 确认

```bash
kubectl get node -l hami.io/lupine-server
kubectl get pod -n kube-system -l app.kubernetes.io/component=hami-lupine-server -o wide
kubectl get pod remote-gpu-pod -o jsonpath='{.metadata.annotations.hami\.io/lupine-endpoint}{"\n"}'
```

注解应是服务节点的 `InternalIP:14833`。

## 从本机连（可选）

把 `devices.remotegpu.sessionImage` 设成一个带 `socat` 的镜像并升级后，scheduler 会在每台服务节点上放一个转发 Pod：

```bash
kubectl -n kube-system get pods -l hami.io/lupine-session
kubectl -n kube-system port-forward pod/lupine-session-<node> 14833:14833
export LUPINE_SERVER=127.0.0.1:14833
```

本机同样需要 lupine client 库。这种连接不占卡、也不计入调度，会和已经分出去的任务打到同一台机器上。集群内的 Pod 不需要这一步。
