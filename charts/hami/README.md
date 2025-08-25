# HAMi Helm Chart Values Documentation

This document provides detailed descriptions of all configurable values parameters for the HAMi Helm Chart.

## Global Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `global.imageRegistry` | Global Docker image registry | `""` |
| `global.imagePullSecrets` | Global Docker image pull secrets | `[]` |
| `global.imageTag` | Image tag | `"v2.6.1"` |
| `global.gpuHookPath` | GPU Hook path | `/usr/local` |
| `global.labels` | Global labels | `{}` |
| `global.annotations` | Global annotations | `{}` |
| `global.managedNodeSelectorEnable` | Whether to enable managed node selector | `false` |
| `global.managedNodeSelector.usage` | Managed node selector usage | `"gpu"` |
| `nameOverride` | Name override | `""` |
| `fullnameOverride` | Full name override | `""` |
| `namespaceOverride` | Namespace override | `""` |

## Resource Name Configuration

### NVIDIA GPU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `resourceName` | GPU resource name | `"nvidia.com/gpu"` |
| `resourceMem` | GPU memory resource name | `"nvidia.com/gpumem"` |
| `resourceMemPercentage` | GPU memory percentage resource name | `"nvidia.com/gpumem-percentage"` |
| `resourceCores` | GPU core resource name | `"nvidia.com/gpucores"` |
| `resourcePriority` | GPU priority resource name | `"nvidia.com/priority"` |

### Cambricon MLU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `mluResourceName` | MLU resource name | `"cambricon.com/vmlu"` |
| `mluResourceMem` | MLU memory resource name | `"cambricon.com/mlu.smlu.vmemory"` |
| `mluResourceCores` | MLU core resource name | `"cambricon.com/mlu.smlu.vcore"` |

### Hygon DCU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `dcuResourceName` | DCU resource name | `"hygon.com/dcunum"` |
| `dcuResourceMem` | DCU memory resource name | `"hygon.com/dcumem"` |
| `dcuResourceCores` | DCU core resource name | `"hygon.com/dcucores"` |

### Iluvatar GPU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `iluvatarResourceName` | GPU resource name | `"iluvatar.ai/vgpu"` |
| `iluvatarResourceMem` | GPU memory resource name | `"iluvatar.ai/vcuda-memory"` |
| `iluvatarResourceCore` | GPU core resource name | `"iluvatar.ai/vcuda-core"` |

### Metax GPU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `metaxResourceName` | GPU resource name | `"metax-tech.com/sgpu"` |
| `metaxResourceCore` | GPU core resource name | `"metax-tech.com/vcore"` |
| `metaxResourceMem` | GPU memory resource name | `"metax-tech.com/vmemory"` |
| `metaxsGPUTopologyAware` | GPU topology awareness | `"false"` |

### Kunlunxin XPU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `kunlunResourceName` | XPU resource name | `"kunlunxin.com/xpu"` |

## Scheduler Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `schedulerName` | Scheduler name | `"hami-scheduler"` |
| `scheduler.nodeName` | Define node name, scheduler will schedule to this node | `""` |
| `scheduler.overwriteEnv` | Whether to overwrite environment variables | `"false"` |
| `scheduler.defaultSchedulerPolicy.nodeSchedulerPolicy` | Node scheduler policy | `binpack` |
| `scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy` | GPU scheduler policy | `spread` |
| `scheduler.metricsBindAddress` | Metrics bind address | `":9395"` |
| `scheduler.forceOverwriteDefaultScheduler` | Whether to force overwrite default scheduler | `true` |
| `scheduler.livenessProbe` | Whether to enable liveness probe | `false` |
| `scheduler.leaderElect` | Whether to enable leader election | `true` |
| `scheduler.replicas` | Number of replicas | `1` |

### Kube Scheduler Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.kubeScheduler.enabled` | Whether to run kube-scheduler container in scheduler pod | `true` |
| `scheduler.kubeScheduler.image.registry` | Kube scheduler image registry | `"registry.cn-hangzhou.aliyuncs.com"` |
| `scheduler.kubeScheduler.image.repository` | Kube scheduler image repository | `"google_containers/kube-scheduler"` |
| `scheduler.kubeScheduler.image.tag` | Kube scheduler image tag | `""` |
| `scheduler.kubeScheduler.image.pullPolicy` | Kube scheduler image pull policy | `IfNotPresent` |
| `scheduler.kubeScheduler.image.pullSecrets` | Kube scheduler image pull secrets | `[]` |
| `scheduler.kubeScheduler.extraNewArgs` | Extra new arguments | `["--config=/config/config.yaml", "-v=4"]` |
| `scheduler.kubeScheduler.extraArgs` | Extra arguments | `["--policy-config-file=/config/config.json", "-v=4"]` |

### Extender Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.extender.image.registry` | Scheduler extender image registry | `"docker.io"` |
| `scheduler.extender.image.repository` | Scheduler extender image repository | `"projecthami/hami"` |
| `scheduler.extender.image.tag` | Scheduler extender image tag | `""` |
| `scheduler.extender.image.pullPolicy` | Scheduler extender image pull policy | `IfNotPresent` |
| `scheduler.extender.image.pullSecrets` | Scheduler extender image pull secrets | `[]` |
| `scheduler.extender.extraArgs` | Scheduler extender extra arguments | `["--debug", "-v=4"]` |

### Admission Webhook Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.admissionWebhook.enabled` | Whether to enable admission webhook | `true` |
| `scheduler.admissionWebhook.customURL.enabled` | Whether to enable custom URL | `false` |
| `scheduler.admissionWebhook.customURL.host` | Custom URL host | `127.0.0.1` |
| `scheduler.admissionWebhook.customURL.port` | Custom URL port | `31998` |
| `scheduler.admissionWebhook.customURL.path` | Custom URL path | `/webhook` |
| `scheduler.admissionWebhook.reinvocationPolicy` | Reinvocation policy | `Never` |
| `scheduler.admissionWebhook.failurePolicy` | Failure policy | `Ignore` |

### TLS Certificate Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.certManager.enabled` | Whether to use cert-manager to generate self-signed certificates | `false` |
| `scheduler.patch.enabled` | Whether to use kube-webhook-certgen to generate self-signed certificates | `true` |
| `scheduler.patch.image.registry` | Certgen image registry | `"docker.io"` |
| `scheduler.patch.image.repository` | Certgen image repository | `"jettech/kube-webhook-certgen"` |
| `scheduler.patch.image.tag` | Certgen image tag | `"v1.5.2"` |
| `scheduler.patch.image.pullPolicy` | Certgen image pull policy | `IfNotPresent` |
| `scheduler.patch.imageNew.registry` | New certgen image registry | `"docker.io"` |
| `scheduler.patch.imageNew.repository` | New certgen image repository | `"liangjw/kube-webhook-certgen"` |
| `scheduler.patch.imageNew.tag` | New certgen image tag | `"v1.1.1"` |

### Scheduler Service Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.service.type` | Service type | `NodePort` |
| `scheduler.service.httpPort` | HTTP port | `443` |
| `scheduler.service.schedulerPort` | Scheduler NodePort | `31998` |
| `scheduler.service.monitorPort` | Monitor port | `31993` |
| `scheduler.service.monitorTargetPort` | Monitor target port | `9395` |

## Device Plugin Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devicePlugin.image.registry` | Device plugin image registry | `"docker.io"` |
| `devicePlugin.image.repository` | Device plugin image repository | `"projecthami/hami"` |
| `devicePlugin.image.tag` | Device plugin image tag | `""` |
| `devicePlugin.image.pullPolicy` | Device plugin image pull policy | `IfNotPresent` |
| `devicePlugin.image.pullSecrets` | Device plugin image pull secrets | `[]` |

### Monitor Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devicePlugin.monitor.image.registry` | Monitor image registry | `"docker.io"` |
| `devicePlugin.monitor.image.repository` | Monitor image repository | `"projecthami/hami"` |
| `devicePlugin.monitor.image.tag` | Monitor image tag | `""` |
| `devicePlugin.monitor.image.pullPolicy` | Monitor image pull policy | `IfNotPresent` |
| `devicePlugin.monitor.image.pullSecrets` | Monitor image pull secrets | `[]` |
| `devicePlugin.monitor.ctrPath` | Container path | `/usr/local/vgpu/containers` |

### Device Plugin Other Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devicePlugin.deviceSplitCount` | Integer type, default value: 10. Maximum number of tasks assigned to a single GPU device | `10` |
| `devicePlugin.deviceMemoryScaling` | Device memory scaling ratio | `1` |
| `devicePlugin.deviceCoreScaling` | Device core scaling ratio | `1` |
| `devicePlugin.runtimeClassName` | Runtime class name | `""` |
| `devicePlugin.createRuntimeClass` | Whether to create runtime class | `false` |
| `devicePlugin.migStrategy` | String type, "none" means ignore MIG functionality, "mixed" means allocate MIG devices through independent resources | `"none"` |
| `devicePlugin.disablecorelimit` | String type, "true" means disable core limit, "false" means enable core limit | `"false"` |
| `devicePlugin.passDeviceSpecsEnabled` | Whether to enable passing device specs | `false` |
| `devicePlugin.extraArgs` | Device plugin extra arguments | `["-v=4"]` |

### Device Plugin Service Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devicePlugin.service.type` | Service type | `NodePort` |
| `devicePlugin.service.httpPort` | HTTP port | `31992` |

### Device Plugin Deployment Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devicePlugin.pluginPath` | Plugin path | `/var/lib/kubelet/device-plugins` |
| `devicePlugin.libPath` | Library path | `/usr/local/vgpu` |
| `devicePlugin.nvidianodeSelector` | NVIDIA node selector | `{"gpu": "on"}` |
| `devicePlugin.updateStrategy.type` | Update strategy type | `RollingUpdate` |
| `devicePlugin.updateStrategy.rollingUpdate.maxUnavailable` | Maximum unavailable count | `1` |

## Device Configuration

### AWS Neuron
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.awsneuron.customresources` | Custom resources | `["aws.amazon.com/neuron", "aws.amazon.com/neuroncore"]` |

### Kunlunxin
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.kunlun.enabled` | Whether to enable | `true` |
| `devices.kunlun.customresources` | Custom resources | `["kunlunxin.com/xpu"]` |

### Mthreads
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.mthreads.enabled` | Whether to enable | `true` |
| `devices.mthreads.customresources` | Custom resources | `["mthreads.com/vgpu"]` |

### NVIDIA
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.nvidia.gpuCorePolicy` | GPU core policy | `default` |
| `devices.nvidia.libCudaLogLevel` | CUDA library log level | `1` |

### Huawei Ascend
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.ascend.enabled` | Whether to enable | `false` |
| `devices.ascend.image` | Image | `""` |
| `devices.ascend.imagePullPolicy` | Image pull policy | `IfNotPresent` |
| `devices.ascend.extraArgs` | Extra arguments | `[]` |
| `devices.ascend.nodeSelector` | Node selector | `{"ascend": "on"}` |
| `devices.ascend.tolerations` | Tolerations | `[]` |
| `devices.ascend.customresources` | Custom resources | `["huawei.com/Ascend910A", "huawei.com/Ascend910A-memory", ...]` |
