# HAMi Helm Chart Values Documentation

This document provides detailed descriptions of all configurable values parameters for the HAMi Helm Chart.

Device configuration fields use `devices.<vendor>` and match the field names in `device-config.yaml`. Supply the current field paths in a values file when upgrading; the previous root-level device fields are no longer read. Supplying removed fields fails chart rendering and lists every old field with its replacement path.

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

These examples use release `hami` in namespace `kube-system`. Replace the release,
namespace, and ConfigMap names with those used by your installation.

Export the release's user-supplied values and back up the device ConfigMap:

```bash
helm get values hami -n kube-system -o yaml > previous-values.yaml
kubectl get configmap hami-scheduler-device -n kube-system -o yaml > device-config-backup.yaml
```

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

## Global Configuration

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `global.imageRegistry` | Global Docker image registry | `""` |
| `global.imagePullSecrets` | Global Docker image pull secrets | `[]` |
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
| `openshift.securityContextConstraints.create` | Create the named device-plugin SCC and its use ClusterRole when OpenShift support is enabled. Set this to false only when both the SCC and `system:openshift:scc:<name>` ClusterRole already exist, such as for the built-in `privileged` SCC. | `true` |
| `openshift.securityContextConstraints.name` | SCC granted to enabled device-plugin service accounts. When `create=false`, the matching `system:openshift:scc:<name>` ClusterRole must already exist. The built-in `privileged` SCC requires `create=false`. | `"hami-device-plugin"` |
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
| `scheduler.nodeName` | Define node name, scheduler will schedule to this node | `""` |
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
| `scheduler.admissionWebhook.manageNamespaceSelector` | Whether the chart renders and manages the webhook namespaceSelector field | `true` |
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
| `scheduler.service.type` | Service type | `ClusterIP` |
| `scheduler.service.httpPort` | HTTP port | `443` |
| `scheduler.service.schedulerPort` | Scheduler NodePort | `31998` |
| `scheduler.service.monitorPort` | Monitor port | `31993` |
| `scheduler.service.monitorTargetPort` | Monitor target port | `metrics` |

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
| `devicePlugin.monitor.ctrPath` | Shared per-container libvgpu cache path used by the Device Plugin and monitor | `/usr/local/vgpu/containers` |
| `devicePlugin.monitor.probes.enabled` | Run the monitor liveness and readiness probes; disable to debug inside the container | `true` |
| `devicePlugin.monitor.metricsBindAddress` | Address the monitor serves `/metrics` on; the container port, Service target port and probes follow it; loopback addresses are rejected | `":9394"` |
| `devicePlugin.monitor.resyncInterval` | Monitor Pod informer resync interval and grace period for releasing mappings of missing Pods; independent of directory GC and Prometheus scrape frequency | `"5m"` |
| `devicePlugin.monitor.extraArgs` | Monitor extra arguments | `["-v=4"]` |

### vGPU Cache Garbage Collection

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devicePlugin.vgpuCache.gracePeriod` | Minimum directory age (based on modification time) before the NVIDIA Device Plugin considers a stale libvgpu cache directory for deletion. Independent of monitor resync. | `"5m"` |

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
| `devices.nvidia.adoptExistingMIGInstances` | Adopt pre-existing MIG GI/CI pairs for lazy reuse. Enable only when HAMi exclusively manages the node's MIG layout | `false` |
| `devicePlugin.disablecorelimit` | String type, "true" means disable core limit, "false" means enable core limit | `"false"` |
| `devicePlugin.passDeviceSpecsEnabled` | Whether to enable passing device specs | `true` |
| `devicePlugin.nvidiaDriverRoot` | NVIDIA driver root on the host. `auto` reads GPU Operator's `driver-ready` contract and defaults to `/` when it is absent | `"auto"` |
| `devicePlugin.extraArgs` | Device plugin extra arguments | `["-v=4"]` |
| `devicePlugin.nodeConfiguration.config` | Node configuration for device plugin by json | An example of default configuration. |
| `devicePlugin.nodeConfiguration.externalConfigName` | Node configuration for device plugin by external configmap | `""` |
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
| `devices.kunlun.customresources` | Additional resources; standard resource names are added automatically | `[]` |

### Enflame
| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `devices.enflame.customresources` | Additional resources; standard resource names are added automatically | `[]` |

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
| `devices.ascend.hamiVnpuMode` | Default VNPU mode: `template`, `hami-core` (alias `hamiCore`), or `enpu`; node capability annotations take precedence | `""` (resolves to `template`) |
| `devices.ascend.enpuPolicy` | Default ENPU policy: `fixed-share`, `elastic`, or `best-effort`; numeric aliases `"1"`, `"2"`, and `"3"` are also accepted | `elastic` |
| `devices.ascend.customresources` | Custom resources | `["huawei.com/Ascend910A", "huawei.com/Ascend910A-memory", ...]` |

Set `devices.ascend.hamiVnpuMode` to choose the global default. The chart renders
it as `vnpus.hamiVnpuMode` in the scheduler configuration. Empty uses `template`,
or `hami-core` when the deprecated `devices.ascend.hamiVnpuCore: true` is present.
An explicit mode always takes precedence over that legacy flag. Direct scheduler
configuration has the same fallback for `vnpus.hamiVnpuCore`. Unknown modes are
rejected. Replace the former `devices.ascend.enpu: true` with
`devices.ascend.hamiVnpuMode: enpu`.

The device plugin's `hami-vnpu-core` and `hami.io/enpu` node annotations override
the corresponding capability with `"true"` or `"false"`; absent annotations use
the global mode. ENPU requires the companion Ascend device plugin with ENPU
support. In a mixed cluster, use `devices.ascend.hamiVnpuMode: template` and enable
ENPU only on the intended nodes through the plugin's `hami.io/enpu: "true"`
annotation.

Pods select ENPU explicitly with `huawei.com/vnpu-mode: enpu`. The scheduler also
accepts `ubs-virt` and `vcann-rt`, matching the mode aliases in the companion
[device plugin's `podUsesENPU`](https://github.com/maverick-woo/ascend-device-plugin/blob/5afdaec52dfd628a05066bfbbf0f39e2c47e3281/internal/server/server.go#L116-L125).
Use `enpu` for new workloads. All three names use the same single-DIE admission,
exact memory allocation, and policy-isolation rules.

Set `huawei.com/enpu-policy` on a Pod to override `devices.ascend.enpuPolicy`.
Admission records the selected policy on the Pod so later changes to the default
do not change existing tenants' policies. Invalid policies are rejected.

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
replace the corresponding default list in full.

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

<!-- BEGIN GENERATED VALUES -->

## Generated values (partial)

This table includes only values with helm-docs descriptions.
More values will be added in follow-up PRs.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| devicePlugin.extraEnvs | list | `[]` | Extra Kubernetes env entries for the device plugin, as a list of objects with name and value or valueFrom. |
| devicePlugin.monitor.extraEnvs | list | `[]` | Extra Kubernetes env entries for the vGPU monitor, as a list of objects with name and value or valueFrom. |
| devices.ascend.vnpuDeviceSplitCount | int | `10` | Positive integer virtual-device slots per physical Ascend NPU in hami-core mode. Requires an ascend-device-plugin version supporting vnpus.vnpuDeviceSplitCount (introduced in [#144](https://github.com/Project-HAMi/ascend-device-plugin/pull/144)). A positive node vDeviceCount takes precedence; template and ENPU modes keep their own capacity. |
| devices.biren.customresources | list | `[]` | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. |
| devices.enflame.customresources | list | `[]` | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. |
| devices.kunlun.customresources | list | `[]` | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. |
| devices.nvidia.adoptExistingMIGInstances | bool | `false` | In the `mig` operating mode, let the device plugin adopt MIG instances it has no ownership record for when it starts. When false, such instances are left untouched. When true, each GPU instance with a compute instance becomes an idle HAMi instance that can be reused for new Pods or destroyed when it blocks a new placement, and on GPUs no Pod is using, GPU instances without a compute instance are destroyed. Enable it only when HAMi alone manages the MIG layout of the node. |
| devices.nvidia.createRuntimeClass | bool | `false` | Create the RuntimeClass named by `runtimeClassName`, with the `nvidia` handler, when `devicePlugin.enabled` is true. Has no effect when `runtimeClassName` is empty. Leave it false if the RuntimeClass already exists, such as one created by the GPU Operator. |
| devices.nvidia.defaultCores | int | `0` | Percentage of GPU cores, from 0 to 100, assigned to each requested GPU when a container requests GPU memory but not the `resourceCoreName` resource. `0` reserves no cores and sets no core limit. When the admission webhook is enabled, a container that requests GPUs without a memory amount, or with 100% of the memory, gets 100 instead, which requests each GPU exclusively when `deviceCoreScaling` is 1. |
| devices.nvidia.defaultGPUNum | int | `1` | Number of GPUs the admission webhook adds to a container that sets a `resourceMemoryName`, `resourceMemoryPercentageName`, or `resourceCoreName` limit but no `resourceCountName` limit. `0` adds nothing, so such a container is not treated as an NVIDIA GPU request. |
| devices.nvidia.defaultMemory | int | `0` | GPU memory in MiB assigned to each requested GPU when a container sets neither the `resourceMemoryName` nor the `resourceMemoryPercentageName` resource, or sets them to `0`. `0` assigns the whole memory of the GPU. `memoryFactor` does not apply to this value. Must not be negative. |
| devices.nvidia.deviceCoreScaling | float | `1` | Factor applied to the cores each GPU reports to the scheduler, which is `100` times this value. Accepts decimals, such as `1.5`. A value above `1` lets the `resourceCoreName` requests on one GPU add up to more than 100. Not applied in the `mig` operating mode. A device-plugin `nodeconfig` entry can override it per node with `devicecorescaling`. |
| devices.nvidia.deviceMemoryScaling | float | `1` | Factor applied to the memory each GPU reports to the scheduler. Accepts decimals, such as `1.5`. A value above `1` lets the memory requests on one GPU add up to more than its physical memory, and the device plugin sets `CUDA_OVERSUBSCRIBE=true` in those containers. Not applied in the `mig` operating mode. A device-plugin `nodeconfig` entry can override it per node with `devicememoryscaling`. |
| devices.nvidia.deviceSplitCount | int | `10` | Number of vGPUs each physical GPU is advertised as, which is the maximum number of containers that can share one GPU. In the `mig` operating mode, the number of MIG instances a GPU can hold limits sharing instead. A device-plugin `nodeconfig` entry can override it per node with `devicesplitcount`. |
| devices.nvidia.enableNumaTopology | bool | `false` | Advertise each GPU's NUMA node on its vGPUs so kubelet's Topology Manager can align CPUs and GPUs. Disabled by default because the `single-numa-node` Topology Manager policy then changes which Pods kubelet admits. A device-plugin `nodeconfig` entry can override it per node with `enablenumatopology`. |
| devices.nvidia.gpuCorePolicy | string | `"default"` | How HAMi-core enforces the `resourceCoreName` limit. The admission webhook passes values other than `default` as `GPU_CORE_UTILIZATION_POLICY`. `default` lets vGPUmonitor turn enforcement on only while it sees other active tasks of the same or higher priority on the GPU. `force` always enforces it. `disable` never enforces it. HAMi-core treats any other value as `default`. Limits of 0 and 100 are never enforced. |
| devices.nvidia.libCudaLogLevel | int | `1` | HAMi-core log level, passed to containers as `LIBCUDA_LOG_LEVEL`. Errors are always logged. `0` and `1` log errors only, `2` adds warnings and messages, `3` adds info, and `4` adds debug logs. HAMi-core itself defaults to `2` when the variable is unset. Not applied in the `mig` operating mode. A device-plugin `nodeconfig` entry can override it per node with `libcudaloglevel`. |
| devices.nvidia.memoryFactor | int | `1` | Multiplier applied to the `resourceMemoryName` value a container requests, which is otherwise in MiB. For example, `1024` makes `nvidia.com/gpumem: 4` request 4 GiB. ResourceQuota limits for that resource are scaled the same way. `0` and `1` disable scaling. Must not be negative. |
| devices.nvidia.migProfileAllowlist | list | Profiles for A30, A100, H100, H20, GH200, H200, B200, and RTX PRO 6000 Blackwell Server Edition | MIG profiles HAMi may create on nodes whose device plugin runs in the `mig` operating mode. Each entry has `models`, a list of GPU model names matched as substrings of the name NVML reports, and `profiles`, a list of MIG profile names such as `1g.10gb`, for example `{models: ["A30"], profiles: ["1g.6gb", "2g.12gb"]}`. The first entry that matches a GPU's model decides the profiles for that GPU; a GPU that no entry matches is not registered. The `nvidia.com/mig-profile-preference` Pod annotation accepts only listed profiles or their slice prefix, such as `4g`. The scheduler and device plugin fail to start if an entry has empty `models` or `profiles`, or if a profile name has no `.` or its part before the first `.` is not at least two characters ending in `g`. The part after the `.` is not checked. A profile that passes this check but names a slice count the GPU does not support, such as `5g.20gb`, is skipped on the node. Setting this value replaces the whole default list. |
| devices.nvidia.preConfiguredDeviceMemory | int | `0` | Total memory in MiB registered for a GPU whose memory NVML cannot query, such as a unified-memory GPU like the NVIDIA GB10. GPUs that report their memory always use the reported value. `0` skips GPUs whose memory cannot be queried. A device-plugin `nodeconfig` entry can override it per node with `preconfigureddevicememory`. |
| devices.nvidia.resourceCoreName | string | `"nvidia.com/gpucores"` | Extended resource name for the GPU cores a container requests on each GPU, as an integer percentage from 0 to 100. `0` reserves no cores and sets no core limit. With `deviceCoreScaling: 1`, `100` requests each GPU exclusively. |
| devices.nvidia.resourceCountName | string | `"nvidia.com/gpu"` | Extended resource name for the number of NVIDIA GPUs a container requests. The device plugin registers it with kubelet, advertising each physical GPU `deviceSplitCount` times. |
| devices.nvidia.resourceMemoryName | string | `"nvidia.com/gpumem"` | Extended resource name for the GPU memory a container requests on each GPU, in MiB multiplied by `memoryFactor`. |
| devices.nvidia.resourceMemoryPercentageName | string | `"nvidia.com/gpumem-percentage"` | Extended resource name for the GPU memory a container requests on each GPU as a percentage of that GPU's memory, an integer from 0 to 100. `0` is treated as unset. |
| devices.nvidia.resourcePriorityName | string | `"nvidia.com/priority"` | Extended resource name for the task priority of a container. The admission webhook passes its limit to HAMi-core as `CUDA_TASK_PRIORITY`. A lower value is a higher priority, and HAMi-core uses `1` when it is unset. On a shared GPU, vGPUmonitor pauses kernel launches of lower-priority tasks while higher-priority tasks are active, but only for tasks with a `resourceCoreName` limit between 1 and 99 whose core limit is being enforced (see `gpuCorePolicy`). |
| devices.nvidia.runtimeClassName | string | `""` | RuntimeClass set on the device plugin Pods and, by the admission webhook, on GPU Pods that do not set `runtimeClassName`, commonly `nvidia` when the NVIDIA container runtime is not the default. The RuntimeClass must exist, either created by `createRuntimeClass` or already present in the cluster. Empty sets no RuntimeClass. |
| devices.vastai.customresources | list | `[]` | Additional resource names forwarded to the scheduler extender; standard resources are added automatically from the named resource fields. |
| openshift.securityContextConstraints.create | bool | `true` | Create the named device-plugin SCC and its use ClusterRole when OpenShift support is enabled. Set this to false only when both the SCC and `system:openshift:scc:<name>` ClusterRole already exist, such as for the built-in `privileged` SCC. |
| openshift.securityContextConstraints.name | string | `"hami-device-plugin"` | SCC granted to enabled device-plugin service accounts. When `create=false`, the matching `system:openshift:scc:<name>` ClusterRole must already exist. The built-in `privileged` SCC requires `create=false`. |

<!-- END GENERATED VALUES -->

## Updating chart documentation

Install [helm-docs](https://github.com/norwoodj/helm-docs), then run these commands
from the repository root:

```bash
make update-helm-chart-docs
make verify-helm-chart-docs
```

Only values with helm-docs descriptions appear in the generated section.
Add descriptions with `# -- Description` immediately above a value. The commands
use `--ignore-non-descriptions`; strict documentation checking is disabled until
the remaining descriptions are complete. Text outside the generated markers
is maintained directly in this README.
