## 动态MIG功能简介

**HAMi将在2.5版本后支持动态MIG切分模式**, 其功能包括:

***动态MIG实例管理***: 用户不需要在节点上事先生成MIG实例，HAMi会根据任务需要自动创建

***动态切换MIG切分方案***: HAMi会根据设备上的任务情况和新任务的需求，动态的切换MIG模版

***MIG实例监控***: 每个由HAMi管理的MIG实例都可以在调度器监控中找到，用户可以通过该监控清晰地获取整个集群的MIG视图

***可与使用hami-core的节点进行统一的资源池化***: HAMi将MIG与hami-core这两种切分方案进行了统一的池化处理，若任务未指定切分模式的话，分配给hami-core或者mig都是有可能的

***统一的API***: 使用动态MIG功能完全不需要进行任务层的适配工作

## 需求

* NVIDIA Blackwell and Hopper™ and Ampere Devices
* HAMi >= v2.5.0
* Nvidia-container-toolkit

## 开启动态MIG功能

* 通过[这里](https://github.com/Project-HAMi/HAMi#enabling-vgpu-support-in-kubernetes)的文档部署HAMi

* 通过以下指令修改configMap，并将节点的工作模式修改为`mig`
```
kubectl describe cm  hami-device-plugin -n kube-system
```

```json
{
    "nodeconfig": [
        {
            "name": "MIG-NODE-A",
            "operatingmode": "mig",
            "filterdevices": {
              "uuid": [],
              "index": []
            }
        }
    ]
}
```

* 重启以下2个pod使修改后的配置生效:
  * hami-scheduler 
  * 在'MIG-NODE-A'上的hami-device-plugin 

## 修改MIG模版列表 (可选)

HAMi目前包含[MIG配置模版](https://github.com/Project-HAMi/HAMi/blob/master/charts/hami/templates/scheduler/device-configmap.yaml)

你可以根据自己的集群环境，通过以下的方式去进行修改:

  ### 修改`charts/hami/templates/scheduler`路径下的`device-configmap.yaml`

  ```yaml
    nvidia:
      resourceCountName: {{ .Values.resourceName }}
      resourceMemoryName: {{ .Values.resourceMem }}
      resourceMemoryPercentageName: {{ .Values.resourceMemPercentage }}
      resourceCoreName: {{ .Values.resourceCores }}
      resourcePriorityName: {{ .Values.resourcePriority }}
      overwriteEnv: false
      defaultMemory: 0
      defaultCores: 0
      defaultGPUNum: 1
      deviceSplitCount: {{ .Values.devicePlugin.deviceSplitCount }}
      deviceMemoryScaling: {{ .Values.devicePlugin.deviceMemoryScaling }}
      deviceCoreScaling: {{ .Values.devicePlugin.deviceCoreScaling }}
      knownMigGeometries:
      - models: [ "A30" ]
        allowedGeometries:
          - 
            - name: 1g.6gb
              memory: 6144
              count: 4
          - 
            - name: 2g.12gb
              memory: 12288
              count: 2
          - 
            - name: 4g.24gb
              memory: 24576
              count: 1
      - models: [ "A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB", "A100-SXM4-40GB" ]
        allowedGeometries:
          - 
            - name: 1g.5gb
              memory: 5120
              count: 7
          - 
            - name: 2g.10gb
              memory: 10240
              count: 3
            - name: 1g.5gb
              memory: 5120
              count: 1
          - 
            - name: 3g.20gb
              memory: 20480
              count: 2
          - 
            - name: 7g.40gb
              memory: 40960
              count: 1
      - models: [ "A100-SXM4-80GB", "A100-80GB-PCIe", "A100-PCIE-80GB"]
        allowedGeometries:
          - 
            - name: 1g.10gb
              memory: 10240
              count: 7
          - 
            - name: 2g.20gb
              memory: 20480
              count: 3
            - name: 1g.10gb
              memory: 10240
              count: 1
          - 
            - name: 3g.40gb
              memory: 40960
              count: 2
          - 
            - name: 7g.79gb
              memory: 80896
              count: 1
  ```
  > **Note** 修改后可以通过更新或重新部署chart来生效

  > **Note** 在收到任务请求后，HAMi会在上述定义的MIG模版中的依次查找，直到找到一个可以运行任务的模版

## 使用MIG模式运行任务

MIG实例子可以通过和使用hami-core相同的方式进行申请，只需要指定`nvidia.com/gpu`和`nvidia.com/gpumem`即可

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
  annotations:
    nvidia.com/vgpu-mode: "mig" #(Optional), if not set, this pod can be assigned to a MIG instance or a hami-core instance
spec:
  containers:
    - name: ubuntu-container
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          nvidia.com/gpu: 2 
          nvidia.com/gpumem: 8000
```

在上面的例子中，该任务申请了2个MIG实例，每个实例至少需要8G显存

## 监控MIG实例

由HAMi管理和生成的MIG实例可以从调度器监控中看到（scheduler node ip:31993/metrics），如下所示：

```bash
# HELP nodeGPUMigInstance GPU Sharing mode. 0 for hami-core, 1 for mig, 2 for mps
# TYPE nodeGPUMigInstance gauge
nodeGPUMigInstance{deviceidx="0",deviceuuid="GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313",migname="3g.20gb-0",nodeid="aio-node15",zone="vGPU"} 1
nodeGPUMigInstance{deviceidx="0",deviceuuid="GPU-936619fc-f6a1-74a8-0bc6-ecf6b3269313",migname="3g.20gb-1",nodeid="aio-node15",zone="vGPU"} 0
nodeGPUMigInstance{deviceidx="1",deviceuuid="GPU-30f90f49-43ab-0a78-bf5c-93ed41ef2da2",migname="3g.20gb-0",nodeid="aio-node15",zone="vGPU"} 1
nodeGPUMigInstance{deviceidx="1",deviceuuid="GPU-30f90f49-43ab-0a78-bf5c-93ed41ef2da2",migname="3g.20gb-1",nodeid="aio-node15",zone="vGPU"} 1
```

## 备注

1. 你不需要在MIG节点上进行任何操作，所有MIG实例的创建和维护都是由hami-vgpu-device-plugin进行的

2. 安培架构之前的NVIDIA设备无法使用`MIG`模式

3. 你不会在节点上看到MIG资源名(例如, `nvidia.com/mig-1g.10gb`)，HAMi对于hami-core和mig使用统一的资源名进行管理