# HAMi Helm Chart

Configure HAMi with a values file. See the user guide for
[installation](https://project-hami.io/docs/next/installation/online-installation)
and [configuration](https://project-hami.io/docs/next/userguide/configure).

## Upgrade to v2.11

Device-specific Helm values use `devices.<vendor>`. Move your existing overrides
to the paths below before upgrading. The `device-config.yaml` format read by
HAMi and the device plugins is unchanged.

### Field migration table

The previous fields are no longer read. The chart rejects them even when their
value is `0`, `false`, or empty. Move each value to its new path and remove the
old field.

| Old Helm value | New Helm value |
|----------------|----------------|
| `resourceName` | `devices.nvidia.resourceCountName` |
| `resourceMem` | `devices.nvidia.resourceMemoryName` |
| `resourceMemPercentage` | `devices.nvidia.resourceMemoryPercentageName` |
| `resourceCores` | `devices.nvidia.resourceCoreName` |
| `resourcePriority` | `devices.nvidia.resourcePriorityName` |
| `mluResourceName` | `devices.cambricon.resourceCountName` |
| `mluResourceMem` | `devices.cambricon.resourceMemoryName` |
| `mluResourceCores` | `devices.cambricon.resourceCoreName` |
| `hcuResourceName` | `devices.hygon.resourceCountName` |
| `hcuResourceMem` | `devices.hygon.resourceMemoryName` |
| `hcuResourceCores` | `devices.hygon.resourceCoreName` |
| `metaxResourceName` | `devices.metax.resourceVCountName` |
| `metaxResourceCore` | `devices.metax.resourceVCoreName` |
| `metaxResourceMem` | `devices.metax.resourceVMemoryName` |
| `metaxsGPUTopologyAware` | `devices.metax.sgpuTopologyAware` |
| `enflameResourceNameDRSGCU` | `devices.enflame.resourceNameDRSGCU` |
| `enflameResourceNameGCUMemory` | `devices.enflame.resourceNameGCUMemory` |
| `enflameResourceNameGCUCore` | `devices.enflame.resourceNameGCUCore` |
| `kunlunResourceName` | `devices.kunlun.resourceCountName` |
| `kunlunResourceVCountName` | `devices.kunlun.resourceVCountName` |
| `kunlunResourceVMemoryName` | `devices.kunlun.resourceVMemoryName` |
| `vastaiResourceName` | `devices.vastai.resourceCountName` |
| `birenResourceName` | `devices.biren.resourceCountName` |
| `devicePlugin.deviceSplitCount` | `devices.nvidia.deviceSplitCount` |
| `devicePlugin.deviceMemoryScaling` | `devices.nvidia.deviceMemoryScaling` |
| `devicePlugin.deviceCoreScaling` | `devices.nvidia.deviceCoreScaling` |
| `devicePlugin.preConfiguredDeviceMemory` | `devices.nvidia.preConfiguredDeviceMemory` |
| `devicePlugin.enableNumaTopology` | `devices.nvidia.enableNumaTopology` |
| `devicePlugin.runtimeClassName` | `devices.nvidia.runtimeClassName` |
| `devicePlugin.createRuntimeClass` | `devices.nvidia.createRuntimeClass` |

Keep `scheduler.overwriteEnv` and other `devicePlugin` settings at their existing
paths, including `enabled`, images, `deviceListStrategy`, `migStrategy`,
`disablecorelimit`, and `nodeConfiguration`.

For Enflame, Kunlun, Vastai, and Biren, standard extender resources are now
added from the named device resource fields. Their `customresources` lists
contain only additional resources and default to `[]`. Remove any copied
standard-resource entries from these lists and keep the extra resources you
need. Standard resources are included even when `customresources` is empty.

### Back up the current configuration

Back up your release values and ConfigMaps before upgrading. See the
[upgrade guide](https://project-hami.io/docs/next/installation/upgrade) for the
backup commands.

If you use node configuration, back up its ConfigMap too. Use the external
ConfigMap name instead when `devicePlugin.nodeConfiguration.externalConfigName`
is set:

```bash
kubectl get configmap hami-device-plugin -n kube-system -o yaml > node-config-backup.yaml
```

### Prepare the values file

Create `my-values.yaml` from the saved user values. Apply the field migrations
above and keep all other settings required by your installation.

If you edited a ConfigMap manually, transfer only the settings you need to keep
into the corresponding Helm values. Store node settings in
`devicePlugin.nodeConfiguration.config` or your external ConfigMap. Do not
restore the entire old ConfigMap over the new one, because this can overwrite
new chart defaults.

### Upgrade with the migrated values

Update the chart repository and upgrade using the new chart defaults and your
migrated values:

```bash
helm repo update hami-charts
helm upgrade hami hami-charts/hami \
  --namespace kube-system \
  --version 2.11.0 \
  --reset-values \
  --values my-values.yaml
```

Replace `2.11.0` with the v2.11 patch version you want to install. `--reset-values`
discards the release's previous values. Include every override you need to keep
in `my-values.yaml` or the other values files passed to this command.

Avoid `--reuse-values` and `--reset-then-reuse-values` during this migration.
They can carry removed fields into the new chart and cause rendering to fail.

After upgrading, check the generated ConfigMaps and the scheduler and device
plugin rollout status. If you changed only the NVIDIA node configuration,
restart the NVIDIA device plugin to load it:

```bash
kubectl rollout restart daemonset/hami-device-plugin -n kube-system
kubectl rollout status daemonset/hami-device-plugin -n kube-system
```

<!-- The Parameters section is generated from values.yaml. -->

## Parameters

### Global configuration

| Name                      | Description                      | Value |
| ------------------------- | -------------------------------- | ----- |
| `global.imageRegistry`    | Global Docker image registry     | `""`  |
| `global.imagePullSecrets` | Global Docker image pull secrets | `[]`  |

### OpenShift configuration

| Name                                          | Description                                                                                                                                                                                                                                    | Value                |
| --------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------- |
| `openshift.securityContextConstraints.create` | Create the named device-plugin SCC and its use ClusterRole when OpenShift support is enabled. Set this to false only when both the SCC and `system:openshift:scc:<name>` ClusterRole already exist, such as for the built-in `privileged` SCC. | `true`               |
| `openshift.securityContextConstraints.name`   | SCC granted to enabled device-plugin service accounts. When `create=false`, the matching `system:openshift:scc:<name>` ClusterRole must already exist. The built-in `privileged` SCC requires `create=false`.                                  | `hami-device-plugin` |

### Scheduler configuration

| Name                                        | Description                                                              | Value                               |
| ------------------------------------------- | ------------------------------------------------------------------------ | ----------------------------------- |
| `scheduler.nodeName`                        | Node name on which to run the scheduler Pod.                             | `""`                                |
| `scheduler.kubeScheduler.enabled`           | Run the kube-scheduler container in the scheduler Pod.                   | `true`                              |
| `scheduler.kubeScheduler.image.registry`    | kube scheduler image registry                                            | `registry.cn-hangzhou.aliyuncs.com` |
| `scheduler.kubeScheduler.image.repository`  | kube scheduler image repository                                          | `google_containers/kube-scheduler`  |
| `scheduler.kubeScheduler.image.tag`         | kube scheduler image tag (immutable tags are recommended)                | `""`                                |
| `scheduler.kubeScheduler.image.pullPolicy`  | kube scheduler image pull policy                                         | `IfNotPresent`                      |
| `scheduler.kubeScheduler.image.pullSecrets` | Specify docker-registry secret names as an array                         | `[]`                                |
| `scheduler.extender.image.registry`         | scheduler extender image registry                                        | `docker.io`                         |
| `scheduler.extender.image.repository`       | scheduler extender image repository                                      | `projecthami/hami`                  |
| `scheduler.extender.image.tag`              | scheduler extender image tag (immutable tags are recommended)            | `""`                                |
| `scheduler.extender.image.pullPolicy`       | scheduler extender image pull policy                                     | `IfNotPresent`                      |
| `scheduler.extender.image.pullSecrets`      | Specify docker-registry secret names as an array                         | `[]`                                |
| `scheduler.patch.image.registry`            | webhook certificate generator image registry                             | `docker.io`                         |
| `scheduler.patch.image.repository`          | webhook certificate generator image repository                           | `jettech/kube-webhook-certgen`      |
| `scheduler.patch.image.tag`                 | webhook certificate generator image tag (immutable tags are recommended) | `v1.5.2`                            |
| `scheduler.patch.image.pullPolicy`          | webhook certificate generator image pull policy                          | `IfNotPresent`                      |
| `scheduler.patch.image.pullSecrets`         | Specify docker-registry secret names as an array                         | `[]`                                |
| `scheduler.patch.imageNew.registry`         | webhook certificate generator image registry                             | `docker.io`                         |
| `scheduler.patch.imageNew.repository`       | webhook certificate generator image repository                           | `liangjw/kube-webhook-certgen`      |
| `scheduler.patch.imageNew.tag`              | webhook certificate generator image tag (immutable tags are recommended) | `v1.1.1`                            |
| `scheduler.patch.imageNew.pullPolicy`       | webhook certificate generator image pull policy                          | `IfNotPresent`                      |
| `scheduler.patch.imageNew.pullSecrets`      | Specify docker-registry secret names as an array                         | `[]`                                |

### Device plugin configuration

| Name                                     | Description                                             | Value              |
| ---------------------------------------- | ------------------------------------------------------- | ------------------ |
| `devicePlugin.image.registry`            | devicePlugin image registry                             | `docker.io`        |
| `devicePlugin.image.repository`          | devicePlugin image repository                           | `projecthami/hami` |
| `devicePlugin.image.tag`                 | devicePlugin image tag (immutable tags are recommended) | `""`               |
| `devicePlugin.image.pullPolicy`          | devicePlugin image pull policy                          | `IfNotPresent`     |
| `devicePlugin.image.pullSecrets`         | Specify docker-registry secret names as an array        | `[]`               |
| `devicePlugin.vgpuCache.gracePeriod`     | Minimum directory age before stale cache GC.            | `5m`               |
| `devicePlugin.monitor.image.registry`    | monitor image registry                                  | `docker.io`        |
| `devicePlugin.monitor.image.repository`  | monitor image repository                                | `projecthami/hami` |
| `devicePlugin.monitor.image.tag`         | monitor image tag (immutable tags are recommended)      | `""`               |
| `devicePlugin.monitor.image.pullPolicy`  | monitor image pull policy                               | `IfNotPresent`     |
| `devicePlugin.monitor.image.pullSecrets` | Specify docker-registry secret names as an array        | `[]`               |

### Device configuration

| Name                              | Description                                                                                                                               | Value |
| --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ----- |
| `devices.kunlun.customresources`  | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. | `[]`  |
| `devices.enflame.customresources` | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. | `[]`  |
| `devices.vastai.customresources`  | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. | `[]`  |
| `devices.biren.customresources`   | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. | `[]`  |


## Additional configuration

These tables retain settings that do not yet have generator annotations in
`values.yaml`. The generated Parameters section above covers the annotated settings.

## Global Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `global.imageTag` | Image tag | `"v2.10.0"` |
| `global.gpuHookPath` | GPU Hook path | `/usr/local` |
| `global.labels` | Global labels | `{}` |
| `global.annotations` | Global annotations | `{}` |
| `global.managedNodeSelectorEnable` | Whether to enable managed node selector | `false` |
| `global.managedNodeSelector.usage` | Managed node selector usage | `"gpu"` |
| `nameOverride` | Name override | `""` |
| `fullnameOverride` | Full name override | `""` |
| `namespaceOverride` | Namespace override | `""` |
| `platform.openshift` | Enable OpenShift-specific resources and handling | `false` |
| `selinux.enabled` | Relabel shared vGPU host directories on SELinux-enabled Kubernetes nodes | `false` |
| `selinux.type` | SELinux type applied to shared vGPU host directories | `"container_file_t"` |
| `selinux.level` | SELinux level applied to shared vGPU host directories | `"s0"` |

## Resource Name Configuration

### NVIDIA GPU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.nvidia.resourceCountName` | GPU resource name | `"nvidia.com/gpu"` |
| `devices.nvidia.resourceMemoryName` | GPU memory resource name | `"nvidia.com/gpumem"` |
| `devices.nvidia.resourceMemoryPercentageName` | GPU memory percentage resource name | `"nvidia.com/gpumem-percentage"` |
| `devices.nvidia.resourceCoreName` | GPU core resource name | `"nvidia.com/gpucores"` |
| `devices.nvidia.resourcePriorityName` | GPU priority resource name | `"nvidia.com/priority"` |

### Cambricon MLU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.cambricon.resourceCountName` | MLU resource name | `"cambricon.com/vmlu"` |
| `devices.cambricon.resourceMemoryName` | MLU memory resource name | `"cambricon.com/mlu.smlu.vmemory"` |
| `devices.cambricon.resourceCoreName` | MLU core resource name | `"cambricon.com/mlu.smlu.vcore"` |

### Hygon HCU Resources

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.hygon.resourceCountName` | HCU resource name | `"hygon.com/hcunum"` |
| `devices.hygon.resourceMemoryName` | HCU memory resource name | `"hygon.com/hcumem"` |
| `devices.hygon.resourceCoreName` | HCU core resource name | `"hygon.com/hcucores"` |

### Metax GPU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.metax.resourceVCountName` | GPU resource name | `"metax-tech.com/sgpu"` |
| `devices.metax.resourceVCoreName` | GPU core resource name | `"metax-tech.com/vcore"` |
| `devices.metax.resourceVMemoryName` | GPU memory resource name | `"metax-tech.com/vmemory"` |
| `devices.metax.sgpuTopologyAware` | GPU topology awareness | `false` |

### Enflame GCU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.enflame.resourceNameDRSGCU` | DRS GCU resource name | `"enflame.com/drs-gcu"` |
| `devices.enflame.resourceNameGCUMemory` | GCU memory request resource name | `"enflame.com/gcu-memory"` |
| `devices.enflame.resourceNameGCUCore` | GCU core request resource name | `"enflame.com/gcu-core"` |

### Kunlunxin XPU Resources
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.kunlun.resourceCountName` | XPU resource name | `"kunlunxin.com/xpu"` |

## Scheduler Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `schedulerName` | Scheduler name | `"hami-scheduler"` |
| `scheduler.overwriteEnv` | Whether to overwrite environment variables | `"false"` |
| `scheduler.defaultSchedulerPolicy.nodeSchedulerPolicy` | Node scheduler policy | `binpack` |
| `scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy` | GPU scheduler policy | `spread` |
| `scheduler.metricsBindAddress` | Metrics bind address | `":9395"` |
| `scheduler.profilingBindAddress` | Dedicated pprof HTTP bind address; only used when `--profiling` is enabled | `"127.0.0.1:6060"` |
| `scheduler.kubeQPS` | QPS to use while talking with the kube-apiserver; empty keeps the binary default (`5`) | `""` |
| `scheduler.kubeBurst` | Burst to use while talking with the kube-apiserver; empty keeps the binary default (`10`) | `""` |
| `scheduler.kubeTimeout` | Timeout in seconds while talking with the kube-apiserver; empty keeps the binary default (`0`, no timeout) | `""` |
| `scheduler.nodeLockRetryTimeout` | How long Bind retries LockNode when another PodGroup member holds the node lock; empty keeps the binary default (`28s`), `0` disables retry | `""` |
| `scheduler.extenderHTTPTimeout` | `httpTimeout` given to the HAMi extender in the generated scheduler configuration, in seconds. Applies to both the KubeSchedulerConfiguration used on Kubernetes 1.22+ and the legacy Policy format used below it. Keep it above `scheduler.nodeLockRetryTimeout` | `30` |
| `scheduler.forceOverwriteDefaultScheduler` | Whether to force overwrite default scheduler | `true` |
| `scheduler.livenessProbe` | Whether to enable liveness probe | `false` |
| `scheduler.leaderElect` | Whether to enable leader election | `true` |
| `scheduler.replicas` | Number of replicas | `1` |
| `scheduler.podDisruptionBudget.minAvailable` | Minimum number of available scheduler pods during voluntary disruptions (only rendered when `scheduler.leaderElect` is `true` and `scheduler.replicas` is greater than `1`) | `1` |
| `scheduler.podDisruptionBudget.maxUnavailable` | Maximum number of unavailable scheduler pods during voluntary disruptions; takes precedence over `minAvailable` when set | unset |

### Scheduler profiling

Profiling is disabled by default. To enable it, include `--profiling` in the
scheduler's extra arguments while retaining any other arguments you need:

```yaml
scheduler:
  profilingBindAddress: "127.0.0.1:6060"
  extender:
    extraArgs:
      - --debug
      - -v=4
      - --profiling
```

The dedicated profiling listener serves plain HTTP and defaults to loopback.
The scheduler Service does not expose its port. Access it using port-forwarding:

```bash
kubectl -n kube-system port-forward pod/<scheduler-pod> 6060:6060
```

Then open `http://127.0.0.1:6060/debug/pprof/` locally.

Previously, enabling profiling exposed pprof on the scheduler's cluster-facing
HTTP server. Those routes are now available only on the dedicated profiling
listener. Operators using the old endpoint must switch to port-forwarding or
explicitly set `scheduler.profilingBindAddress` to a non-loopback address.
Changing this address does not expose pprof through the scheduler Service;
the old Service endpoint still returns 404 for pprof routes. For non-loopback
access, reach the Pod IP and profiling port through an appropriately restricted
network path, or use the Pod port-forward shown above.
Non-loopback binding logs a security warning: pprof has no authentication and
can expose process diagnostics. Restrict network access if you choose to expose
it. With profiling disabled, the bind address is ignored and no profiling
listener is started.

### Kube Scheduler Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.kubeScheduler.extraNewArgs` | Extra new arguments | `["--config=/config/config.yaml", "-v=4"]` |
| `scheduler.kubeScheduler.extraArgs` | Extra arguments | `["--policy-config-file=/config/config.json", "-v=4"]` |

### Extender Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.extender.extraArgs` | Scheduler extender extra arguments | `["--debug", "-v=4"]` |

### Admission Webhook Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.admissionWebhook.enabled` | Whether to enable admission webhook | `true` |
| `scheduler.admissionWebhook.customURL.enabled` | Whether to enable custom URL | `false` |
| `scheduler.admissionWebhook.customURL.host` | Custom URL host | `127.0.0.1` |
| `scheduler.admissionWebhook.customURL.port` | Custom URL port | `31998` |
| `scheduler.admissionWebhook.customURL.path` | Custom URL path | `/webhook` |
| `scheduler.admissionWebhook.manageNamespaceSelector` | Whether the chart renders and manages the webhook namespaceSelector field | `true` |
| `scheduler.admissionWebhook.reinvocationPolicy` | Reinvocation policy | `Never` |
| `scheduler.admissionWebhook.failurePolicy` | Failure policy | `Ignore` |

### TLS Certificate Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.certManager.enabled` | Whether to use cert-manager to generate self-signed certificates | `false` |
| `scheduler.patch.enabled` | Whether to use kube-webhook-certgen to generate self-signed certificates | `true` |

### Scheduler Service Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `scheduler.service.type` | Service type | `ClusterIP` |
| `scheduler.service.httpPort` | HTTP port | `443` |
| `scheduler.service.schedulerPort` | Scheduler NodePort | `31998` |
| `scheduler.service.monitorPort` | Monitor port | `31993` |
| `scheduler.service.monitorTargetPort` | Monitor target port | `metrics` |

### Monitor Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devicePlugin.monitor.ctrPath` | Shared per-container libvgpu cache path used by the Device Plugin and monitor | `/usr/local/vgpu/containers` |
| `devicePlugin.monitor.resyncInterval` | Monitor Pod informer resync interval and grace period for releasing mappings of missing Pods; independent of directory GC and Prometheus scrape frequency | `"5m"` |
| `devicePlugin.monitor.extraArgs` | Monitor extra arguments | `["-v=4"]` |
| `devicePlugin.monitor.extraEnvs` | Monitor extra environments | `{}` |

### vGPU Cache Garbage Collection

| Parameter | Description | Default Value |
|-----------|-------------|---------------|

The chart passes `devicePlugin.vgpuCache.gracePeriod` to the Device Plugin as
`HAMI_VGPU_CACHE_GRACE_PERIOD`. Use a Go duration string such as `"30s"`, `"5m"`,
or `"1h"`. An omitted or empty value uses `"5m"`. An invalid duration string logs
a warning and uses `"5m"`; negative durations prevent Device Plugin startup.
`"0s"` removes the grace period, **not** garbage collection: deletion still
requires live Pod confirmation and the existing safety checks.

```yaml
devicePlugin:
  monitor:
    resyncInterval: "30s"
  vgpuCache:
    gracePeriod: "5m"
```

**Migration:** `devicePlugin.monitor.resyncInterval` / `HAMI_RESYNC_INTERVAL`
no longer controls Device Plugin directory GC. If you previously used a custom
monitor resync value to tune cleanup, set `devicePlugin.vgpuCache.gracePeriod`
explicitly to retain that GC grace period. For deployments without Helm, set
`HAMI_VGPU_CACHE_GRACE_PERIOD` on the Device Plugin container. Without the new
setting, GC uses `5m`, regardless of monitor resync. The monitor retains its
existing resync and mmap-release behavior and does not delete directories.

This setting does not change the GC scan interval, confirmation retry backoff,
or the five-failure limit for directory deletion.

### Device Plugin Other Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.nvidia.deviceSplitCount` | Integer type, default value: 10. Maximum number of tasks assigned to a single GPU device | `10` |
| `devices.nvidia.deviceMemoryScaling` | Device memory scaling ratio | `1` |
| `devices.nvidia.deviceCoreScaling` | Device core scaling ratio | `1` |
| `devices.nvidia.runtimeClassName` | Runtime class name | `""` |
| `devices.nvidia.createRuntimeClass` | Create the NVIDIA RuntimeClass named by `devices.nvidia.runtimeClassName` when the device plugin is enabled | `false` |
| `devicePlugin.migStrategy` | String type, "none" means ignore MIG functionality, "mixed" means allocate MIG devices through independent resources | `"none"` |
| `devicePlugin.disablecorelimit` | String type, "true" means disable core limit, "false" means enable core limit | `"false"` |
| `devicePlugin.passDeviceSpecsEnabled` | Whether to enable passing device specs | `true` |
| `devicePlugin.nvidiaDriverRoot` | NVIDIA driver root on the host. `auto` reads GPU Operator's `driver-ready` contract and defaults to `/` when it is absent | `"auto"` |
| `devicePlugin.extraArgs` | Device plugin extra arguments | `["-v=4"]` |
| `devicePlugin.nodeConfiguration.config` | Node configuration for device plugin by json | An example of default configuration. |
| `devicePlugin.nodeConfiguration.externalConfigName` | Node configuration for device plugin by external configmap | `""` |
| `devicePlugin.extraEnvs` | Device plugin extra environments | `{}` |
| `devicePlugin.nvidiaDriverRoot` | NVIDIA driver root path on the host. When set, the chart passes it as `NVIDIA_DRIVER_ROOT` and mounts it read-only at `/driver-root` in both the device-plugin and vGPUmonitor containers. | `null` |
| `devicePlugin.tolerations` | Tolerations applied to device plugin Pods | `[{"key":"nvidia.com/gpu","operator":"Exists","effect":"NoSchedule"}]` |
| `devicePlugin.hostNetwork` | Use the host network for device plugin Pods. | `false` |

When `devicePlugin.nvidiaDriverRoot=auto`, the device plugin reads
`/run/nvidia/validations/driver-ready`. If the file is absent, HAMi assumes a
host-installed driver and uses `/` for both the driver and device roots. When
HAMi starts before GPU Operator validation completes, wait for GPU Operator to
become ready and restart the HAMi device plugin DaemonSet. The chart mounts the
host root at `/host`; the device plugin uses that mount directly for
host-installed drivers, or appends the GPU Operator path suffix
(`/run/nvidia/driver`) for GPU Operator installations.

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
| `devicePlugin.hostPID` | Use the host PID namespace for the device plugin | `true` |
| `devicePlugin.hostPIDBroker.enabled` | Let HAMi core ask the device plugin for its host PID. This requires `devicePlugin.hostPID`. See [Host PID broker](https://project-hami.io/docs/next/developers/hostpid-broker) | `false` |
| `devicePlugin.nvidiaNodeSelector` | NVIDIA node selector | `{"gpu": "on"}` |
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

### Mthreads
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.mthreads.enabled` | Whether to enable | `true` |
| `devices.mthreads.memoryPerCard` | List of integer memory units of 512 MiB per card model, for example `[96, 160]`. Legacy scalar values are rendered as a one-item list. | `[96]` |
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

### Iluvatar
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.iluvatar.enabled` | Whether to enable | `false` |
| `devices.iluvatar.customresources` | Custom resources | `["iluvatar.ai/BI-V150-vgpu", "iluvatar.ai/BI-V150.vMem","iluvatar.ai/BI-V150.vCore", ...]` |

## Device Config Overrides

Device configuration defaults are defined in `values.yaml`. Override individual
fields under `devices.<vendor>` in your own values file. The chart keeps the
existing runtime field names: `devices.ascend.configs` renders as `vnpus.configs`,
and `devices.iluvatar.configs` renders as `iluvatars`.

```yaml
devices:
  nvidia:
    defaultMemory: 4096
    defaultCores: 50
  hygon:
    memoryFactor: 2
```

Lists such as `migProfileAllowlist`, Ascend `configs`, and Iluvatar `configs`
replace the corresponding default list in full; entries do not merge by model
or chip name. Maps merge with chart defaults. Explicit `0`, `false`, and `[]`
are preserved in the rendered device configuration.

Configuration precedence remains `device-config.content`, then a bundled
`files/device-config.yaml`, then the configuration generated from values.
`device-config.content` replaces the complete document; it does not merge with
`devices.*`.

For Enflame, Kunlun, Vastai, and Biren, the scheduler extender's standard
resources follow the named device resource fields. Their `customresources`
lists add extra resource names and default to `[]`. Standard resources are
always included when the vendor is enabled, and duplicate names within each
vendor are removed.

For other vendors whose extender resources use `customresources`, update those
lists when changing resource names or chip definitions.

Changing the device configuration through Helm updates the scheduler and NVIDIA
device-plugin checksum annotations.

For MIG profile selection and device-specific chip definitions, see:

- [NVIDIA dynamic MIG configuration](https://project-hami.io/docs/next/userguide/nvidia-device/dynamic-mig-support)
- [Ascend device templates](https://project-hami.io/docs/next/userguide/ascend-device/device-template)
- [Iluvatar GPU sharing](https://project-hami.io/docs/next/userguide/iluvatar-device/enable-iluvatar-gpu-sharing)

## NVIDIA Driver Root

When `devicePlugin.nvidiaDriverRoot=auto`, the device plugin reads GPU Operator's
`/run/nvidia/validations/driver-ready` contract. If the file is absent, HAMi uses
`/` for host-installed drivers. When HAMi starts before GPU Operator validation
completes, wait for GPU Operator to become ready and restart the device-plugin
DaemonSet. The chart mounts the host root at `/host` for driver discovery.

## Adding Device Configuration

For a new vendor, define every default under `devices.<vendor>` in values.yaml
and reference those fields in `templates/scheduler/device-configmap.yaml`.
Preserve the backend's runtime field names and types. Register its extender
resources in `hami-vgpu.scheduler.managedResources`.
Ascend and Iluvatar demonstrate mappings whose runtime section names differ
from the vendor name.

## Updating this reference

The Parameters section includes settings with `@param` annotations in
`values.yaml`. Fields marked with `@skip` are omitted from that section. To
document a skipped field, replace its `@skip` with `@param` and a description.
Edit other text directly in this README. Install
[readme-generator-for-helm](https://github.com/bitnami/readme-generator-for-helm),
then run these commands
from the repository root:

```bash
make update-chart-docs
make -C charts verify-docs
```
