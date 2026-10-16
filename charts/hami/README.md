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

## Scheduler Configuration

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

## Device Plugin Configuration

### vGPU Cache Garbage Collection

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

### NVIDIA driver discovery

When `devicePlugin.nvidiaDriverRoot=auto`, the device plugin reads
`/run/nvidia/validations/driver-ready`. If the file is absent, HAMi assumes a
host-installed driver and uses `/` for both the driver and device roots. When
HAMi starts before GPU Operator validation completes, wait for GPU Operator to
become ready and restart the HAMi device plugin DaemonSet.

The detected roots only describe the host paths that the device plugin returns
to the runtime: `NVIDIA_DRIVER_ROOT` is used in generated CDI specifications and
`NVIDIA_DEV_ROOT` is used for `DeviceSpec.HostPath` when
`devicePlugin.passDeviceSpecsEnabled=true`. The device plugin itself initializes
NVML and discovers GPUs through the driver libraries and device nodes that the
NVIDIA Container Toolkit injects into its container, so the chart mounts only
the GPU Operator validation directory read-only. This requires the device
plugin Pod to run with the NVIDIA runtime, either because it is the node's
default container runtime or through `devices.nvidia.runtimeClassName`.

CDI specifications must reference the host driver layout, which can differ
from the layout inside the device plugin container. When
`devicePlugin.deviceListStrategy` includes a `cdi-*` strategy, the chart
additionally mounts the host root read-only at `/host`; the device plugin uses
that mount directly for host-installed drivers, or appends the GPU Operator
path suffix (`/run/nvidia/driver`) for GPU Operator installations. Set
`devicePlugin.nvidiaDriverRoot` explicitly to mount only the driver root instead.

## Device Configuration

### Huawei Ascend

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
ENPU only on the intended nodes through the separately deployed plugin's node
configuration (`hamiVnpuMode: enpu`). The plugin publishes the corresponding
`hami.io/enpu` annotation; changing the Node annotation alone does not change
the plugin's mode and is overwritten by its next report.

Pods select ENPU explicitly with `huawei.com/vnpu-mode: enpu`. The scheduler also
accepts `ubs-virt` and `vcann-rt`, matching the mode aliases in the companion
[device plugin's `podUsesENPU`](https://github.com/maverick-woo/ascend-device-plugin/blob/5afdaec52dfd628a05066bfbbf0f39e2c47e3281/internal/server/server.go#L116-L125).
Use `enpu` for new workloads. All three names use the same single-DIE admission,
exact memory allocation, and policy-isolation rules.

Set `huawei.com/enpu-policy` on a Pod to override `devices.ascend.enpuPolicy`.
Admission records the selected policy on the Pod so later changes to the default
do not change existing tenants' policies. Invalid policies are rejected.

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
| device-config.content | string | `""` | Full replacement of device-config.yaml; takes precedence over a bundled file and values. This is a complete document, not a merge; prefer `devices.<vendor>` fields for partial overrides. Please note that extender resources still use `devices.*`, keep resource names consistent. Values validation still applies. |
| devicePlugin.deviceListStrategy | string | `"envvar"` | Comma-separated device list strategies passed to the container runtime: `envvar`, `volume-mounts`, `cdi-annotations`, or `cdi-cri`. CDI strategies require a CDI-capable runtime; `cdi-cri` also requires kubelet/CRI support for CDI devices. |
| devicePlugin.enabled | bool | `true` | Deploy the NVIDIA device plugin DaemonSet, including the vGPU monitor container. |
| devicePlugin.extraArgs | list | `["-v=4"]` | Additional NVIDIA device plugin command-line arguments as a list of strings. |
| devicePlugin.extraEnvs | list | `[]` | Extra Kubernetes env entries for the device plugin, as a list of objects with name and value or valueFrom. |
| devicePlugin.gdrcopyEnabled | bool | `nil` | Enable GDRCopy support for allocated GPU containers. A boolean sets `GDRCOPY_ENABLED`; null leaves the plugin default unchanged (disabled). Requires the corresponding host drivers and runtime support. |
| devicePlugin.gdsEnabled | bool | `nil` | Enable GPUDirect Storage support for allocated GPU containers. A boolean sets `GDS_ENABLED`; null leaves the plugin default unchanged (disabled). Requires the corresponding host drivers and runtime support. |
| devicePlugin.gpuOperatorToolkitReady.enabled | bool | `false` | Wait for `<hostPath>/toolkit-ready` in an init container before starting the device plugin and monitor. |
| devicePlugin.gpuOperatorToolkitReady.hostPath | string | `"/run/nvidia/validations"` | Host directory containing `toolkit-ready`. Also supplies `driver-ready` when `devicePlugin.nvidiaDriverRoot=auto`. |
| devicePlugin.gpuOperatorToolkitReady.securityContext | object | `{"privileged":true,"runAsUser":0}` | Security context for the toolkit-validation init container. |
| devicePlugin.hostNetwork | bool | `false` | Use the host network for NVIDIA device plugin Pods. When enabled, the chart sets `dnsPolicy: ClusterFirstWithHostNet` so the scheduler Service remains resolvable. |
| devicePlugin.hostPID | bool | `true` | Use the host PID namespace for NVIDIA device plugin Pods. Must be true when `hostPIDBroker.enabled` is true. |
| devicePlugin.hostPIDBroker.enabled | bool | `false` | Enable the host PID broker so HAMi-core can query the device plugin for its host PID. Requires `hostPID=true`; mounts the broker socket directory at `/var/run/hami/hostpid`. See [Host PID broker](https://project-hami.io/docs/next/developers/hostpid-broker). |
| devicePlugin.image.pullPolicy | string | `"IfNotPresent"` | Image pull policy for the NVIDIA device plugin and its enabled init containers: `Always`, `IfNotPresent`, or `Never`. |
| devicePlugin.image.pullSecrets | list | `[]` | Image pull Secret names for the device plugin Pod, appended to `global.imagePullSecrets`, e.g. `[myRegistryKeySecretName]`. |
| devicePlugin.image.registry | string | `"docker.io"` | NVIDIA device plugin image registry. Overridden by a non-empty `global.imageRegistry`. |
| devicePlugin.image.repository | string | `"projecthami/hami"` | Image repository for the NVIDIA device plugin container and the `selinux-relabel` and `toolkit-validation` init containers when enabled. |
| devicePlugin.image.tag | string | `""` | NVIDIA device plugin image tag. Empty uses `global.imageTag`. |
| devicePlugin.libPath | string | `"/usr/local/vgpu"` | Host directory for HAMi-core library files, mounted at `<global.gpuHookPath>/vgpu` inside the device plugin container. |
| devicePlugin.migStrategy | string | `"none"` | Strategy for exposing NVIDIA MIG devices: `none`, `single`, or `mixed`. `mixed` exposes MIG profiles as separate resources. Passed as `--mig-strategy`. |
| devicePlugin.mofedEnabled | bool | `nil` | Enable MOFED support for allocated GPU containers. A boolean sets `MOFED_ENABLED`; null leaves the plugin default unchanged (disabled). Requires the corresponding host drivers and runtime support. |
| devicePlugin.monitor.ctrPath | string | `"/usr/local/vgpu/containers"` | Shared per-container libvgpu cache path used by the device plugin and monitor. |
| devicePlugin.monitor.disableHAMiCoreForWholeGPU | bool | `true` | Disable the HAMi-core hook for whole-GPU containers when memory and core scaling are both 1; the monitor reads their metrics from NVML instead. Whole-GPU means a non-MIG device with full device memory allocated and cores unrequested or `gpucores=100`. |
| devicePlugin.monitor.extraArgs | list | `["-v=4"]` | Additional monitor command-line arguments as a list of strings. |
| devicePlugin.monitor.extraEnvs | list | `[]` | Extra Kubernetes env entries for the vGPU monitor, as a list of objects with name and value or valueFrom. |
| devicePlugin.monitor.image.pullPolicy | string | `"IfNotPresent"` | Monitor image pull policy: `Always`, `IfNotPresent`, or `Never`. |
| devicePlugin.monitor.image.pullSecrets | list | `[]` | Monitor image pull Secret names. Currently unused by the DaemonSet; configure `devicePlugin.image.pullSecrets` or `global.imagePullSecrets` for the shared Pod. |
| devicePlugin.monitor.image.registry | string | `"docker.io"` | Monitor image registry. Overridden by a non-empty `global.imageRegistry`. |
| devicePlugin.monitor.image.repository | string | `"projecthami/hami"` | Monitor image repository. |
| devicePlugin.monitor.image.tag | string | `""` | Monitor image tag. Empty uses `global.imageTag`. |
| devicePlugin.monitor.metricsBindAddress | string | `":9394"` | Address the monitor serves `/metrics` on. The container port, Service target port and probes follow it. Loopback addresses are rejected. Set it here, not in `extraArgs`. |
| devicePlugin.monitor.probes.enabled | bool | `true` | Run the monitor liveness and readiness probes. Disable to debug inside the container. |
| devicePlugin.monitor.resources | object | `{}` | Resource requests and limits for the monitor container. |
| devicePlugin.monitor.resyncInterval | string | `"5m"` | Monitor Pod informer resync interval and grace period for releasing mappings of missing Pods. Does not control directory GC or Prometheus scrape frequency. |
| devicePlugin.monitor.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"add":["SYS_ADMIN"],"drop":["ALL"]}}` | Security context for the monitor container. |
| devicePlugin.nodeConfiguration.config | string | Example node configuration; see [values.yaml](values.yaml). | JSON content for the generated device plugin ConfigMap key `config.json`; ignored when `externalConfigName` is set. The `nodeconfig` list selects entries whose `name` matches the node. If none match, it selects matching `nodelabelselector` entries; if neither matches, it uses entries named `*`. For example, use `{"nodelabelselector":{"matchLabels":{"gpu.example.com/pool":"mig"}},"operatingmode":"mig"}`. Node labels are read at plugin startup; restart the plugin after changing them and use labels available when the node registers. Selected entries apply in list order; later overrides take precedence. For per-node resource settings, null, zero, or false does not replace an existing non-zero or true value. Empty `operatingmode` and `filterdevices` values are ignored. Entries with both or neither of `name` and `nodelabelselector`, or with an empty or invalid label selector, are skipped. |
| devicePlugin.nodeConfiguration.externalConfigName | string | `""` | Existing ConfigMap containing a `config.json` key in the Pod namespace. When non-empty, the chart mounts it instead of creating a ConfigMap from `nodeConfiguration.config`. |
| devicePlugin.numaRefit.caFile | string | `""` | Path to a CA bundle already present inside the device-plugin container. |
| devicePlugin.numaRefit.caSecret | string | `""` | Name of a Secret (key ca.crt) to mount as the CA bundle instead; used when caFile is empty. |
| devicePlugin.numaRefit.enabled | bool | `false` | NUMA alignment refit (#2080): when enabled, the device plugin asks the scheduler to re-run its fit over the NUMA-restricted device set kubelet allows, keeping scheduler accounting authoritative. Requires devices.nvidia.enableNumaTopology plus the per-node enablegetpreferredallocation node configuration; pods opt in with the hami.io/numa-alignment annotation (best-effort or strict). Disabled by default: mismatches are only logged. |
| devicePlugin.numaRefit.schedulerEndpoint | string | `""` | Scheduler base URL override. Defaults to the in-cluster scheduler service, `https://<release>-scheduler.<namespace>.svc:<scheduler.service.httpPort>`. |
| devicePlugin.numaRefit.tlsInsecure | bool | `true` | The scheduler serves the admission webhook's self-signed certificate, so verification is skipped by default with the same posture as the extender configmap (tlsConfig.insecure). Set false to verify, and use caFile to point at a CA bundle mounted into the device plugin. |
| devicePlugin.nvidiaDriverRoot | string | `"auto"` | Root of the NVIDIA driver on the host. Set to "auto" to read the driver and device roots from GPU Operator's driver-ready contract. If the contract is absent, both roots default to / for a host-installed driver. In auto mode the device plugin uses the driver injected by the NVIDIA Container Toolkit, so it must run with the NVIDIA runtime; the host root is mounted read-only only when deviceListStrategy uses a cdi-* strategy. |
| devicePlugin.nvidiaHookPath | string | `nil` | Path for NVIDIA CDI hooks in generated CDI specifications. Null omits `NVIDIA_CDI_HOOK_PATH` and keeps the plugin default `/usr/bin/nvidia-ctk`. |
| devicePlugin.nvidiaNodeSelector | object | `{"gpu":"on"}` | Node labels required for NVIDIA device plugin Pods, as a map of label names to string values. |
| devicePlugin.passDeviceSpecsEnabled | bool | `true` | Pass device specifications to kubelet in Allocate responses. Ignored when `deviceListStrategy` contains only CDI strategies. Null leaves the plugin default unchanged (disabled). |
| devicePlugin.pluginPath | string | `"/var/lib/kubelet/device-plugins"` | Host kubelet device-plugin socket directory, mounted at `/var/lib/kubelet/device-plugins` inside the device plugin container. |
| devicePlugin.podAnnotations | object | `{}` | Additional annotations for NVIDIA device plugin Pods, as a map of annotation names to string values. |
| devicePlugin.reportNodeCapacity | bool | `false` | Report GPU memory and core resources in Node capacity and allocatable. The `reportnodecapacity` JSON field in `nodeConfiguration.config` overrides this setting for matching nodes. |
| devicePlugin.resources | object | `{}` | Kubernetes resource requests and limits for the NVIDIA device plugin container, for example `{requests: {cpu: 100m, memory: 100Mi}, limits: {memory: 1Gi}}`. |
| devicePlugin.securityContext | object | `{"allowPrivilegeEscalation":true,"capabilities":{"add":["SYS_ADMIN"],"drop":["ALL"]},"privileged":true}` | Kubernetes container security context for the NVIDIA device plugin. |
| devicePlugin.service.annotations | object | `{}` | Annotations for the vGPU monitor Service, as a map of annotation names to string values. |
| devicePlugin.service.httpPort | int | `31992` | Monitor Service port. Also used as the nodePort when `service.type=NodePort`. Empty or zero uses 31992; the target port follows `monitor.metricsBindAddress`. |
| devicePlugin.service.labels | object | `{}` | Additional labels for the vGPU monitor Service, as a map of label names to string values. |
| devicePlugin.service.type | string | `"NodePort"` | Service type for vGPU monitor metrics. Empty uses `NodePort`; `ClusterIP` exposes metrics only within the cluster. |
| devicePlugin.tolerations | list | `[{"effect":"NoSchedule","key":"nvidia.com/gpu","operator":"Exists"}]` | Kubernetes tolerations for NVIDIA device plugin Pods, as a list of objects with key, operator, value, effect, and optional tolerationSeconds. |
| devicePlugin.updateStrategy | object | `{"rollingUpdate":{"maxUnavailable":1},"type":"RollingUpdate"}` | Kubernetes DaemonSet update strategy: `RollingUpdate` or `OnDelete`. For RollingUpdate, configure `rollingUpdate.maxUnavailable` as a count or percentage. |
| devicePlugin.vgpuCache.gracePeriod | string | `"5m"` | Minimum cache directory age, measured from its modification time, before garbage collection. Use Go duration syntax such as `30s`, `5m`, or `1h`. Empty or invalid values use `5m`; negative durations prevent startup. `0s` removes the age grace period but still requires live Pod confirmation. Independent of monitor resync and the GC scan interval. |
| devices.ascend.configs | list | Per-chip capacities and templates; see [values.yaml](values.yaml). | Per-chip objects shared with the separately deployed Ascend device plugin as `vnpus.configs`; overrides replace the whole list. String `chipName` must match the detected chip name; string `commonWord` identifies the HAMi device type. `resourceName`, `resourceMemoryName`, and `resourceCoreName` are resource-name strings for Pod device-count, memory, and core requests. Pod counts are non-negative integers. Memory requests and `memoryAllocatable`, `memoryCapacity`, and template `memory` use integer MiB (16384 = 16 GiB), without a `Gi` suffix. `memoryAllocatable` is usable memory; `memoryCapacity` is full-device capacity. `memoryFactor` is an integer request multiplier, applied only above 1. <br>Pod core requests in `hami-core`/`enpu` mode are integer percentages, 0-100 (`40` means 40%); ENPU treats 0 or omission as 100%. Configured `aiCore`/`aiCPU` values are integer hardware core counts, not percentages or virtual-device slots. `templates` lists driver slice definitions: a string `name`, integer `memory`, and optional integer `aiCore`/`aiCPU`. The named template must be supported by the driver; editing these numbers does not create a new driver template. The boolean `superPod` enables Ascend910C module pairing: non-ENPU requests of 1 become 2; other odd counts are rejected. See [single-card soft-slicing workload examples](https://project-hami.io/docs/next/userguide/ascend-device/examples/allocate-soft-slicing#single-card-soft-slicing). |
| devices.ascend.customresources | list | Ascend device-count, memory, and core resource names; see [values.yaml](values.yaml). | Resource-name strings managed by the bundled kube-scheduler's extender when `devices.ascend.enabled` is true. Include the resource names from `devices.ascend.configs`; they are not added automatically. This does not register kubelet resources; configure an external kube-scheduler separately. |
| devices.ascend.enabled | bool | `false` | Enable Ascend support in the HAMi scheduler and add `devices.ascend.customresources` to the bundled kube-scheduler's extender configuration. Install the Ascend device plugin separately; this chart does not deploy it. |
| devices.ascend.enpuPolicy | string | `"elastic"` | Default ENPU policy: `fixed-share`, `elastic`, or `best-effort`; numeric string aliases `"1"`, `"2"`, and `"3"` are accepted. Empty uses `elastic`. A Pod's `huawei.com/enpu-policy` overrides this value; invalid policies are rejected for ENPU workloads. |
| devices.ascend.extraArgs | list | `[]` | Not consumed by this chart. Configure additional Ascend device plugin arguments in its separate deployment. |
| devices.ascend.hamiVnpuMode | string | `""` | Default VNPU mode: `template`, `hami-core` (alias: `hamiCore`), or `enpu`. Node capability annotations take priority. An empty or null value uses `template`, or `hami-core` when the deprecated `devices.ascend.hamiVnpuCore` is true. An explicit mode takes precedence over that flag; unknown modes are rejected. |
| devices.ascend.image | string | `""` | Not consumed by this chart. Configure the Ascend device plugin image in its separate deployment. |
| devices.ascend.imagePullPolicy | string | `"IfNotPresent"` | Not consumed by this chart. Configure the Ascend device plugin's image pull policy in its separate deployment. |
| devices.ascend.nodeSelector | object | `{"ascend":"on"}` | Not consumed by this chart. Configure the Ascend device plugin Pod's node selector in its separate deployment. |
| devices.ascend.runtimeClassName | string | `""` | RuntimeClass name to set on Pods requesting Ascend resources when their `spec.runtimeClassName` is unset. Empty leaves the Pod unchanged; an existing Pod value is preserved. |
| devices.ascend.tolerations | list | `[]` | Not consumed by this chart. Configure the Ascend device plugin Pod's tolerations in its separate deployment. |
| devices.awsneuron.customresources | list | `["aws.amazon.com/neuron","aws.amazon.com/neuroncore"]` | List of resource-name strings managed by the scheduler extender. Include the names configured by `devices.awsneuron.resourceCountName` and `devices.awsneuron.resourceCoreName`; they are not added automatically. |
| devices.awsneuron.resourceCoreName | string | `"aws.amazon.com/neuroncore"` | Resource name for positive integer NeuronCore counts. Cores per device depend on the node; one core is not always half a device. See the [NeuronCore workload example](https://project-hami.io/docs/next/userguide/awsneuron-device/examples/allocate-neuron-core). |
| devices.awsneuron.resourceCountName | string | `"aws.amazon.com/neuron"` | Resource name for positive integer counts of whole Neuron devices. Takes precedence over `devices.awsneuron.resourceCoreName` requests when both are present. |
| devices.biren.customresources | list | `[]` | Additional resource-name strings managed by the scheduler extender when `devices.biren.enabled` is true. Standard names are added automatically from the resource fields. |
| devices.biren.enabled | bool | `true` | Add Biren resource names to the bundled kube-scheduler's extender configuration. Setting `false` retains the device configuration. The vendor device plugin must be installed separately. |
| devices.biren.resourceCountName | string | `"birentech.com/gpu"` | Resource name for non-negative integer Biren device counts. Must match the device plugin and Pod resources. See [Biren usage](https://project-hami.io/docs/next/userguide/biren-device/enable-biren-sharing). |
| devices.cambricon.resourceCoreName | string | `"cambricon.com/mlu.smlu.vcore"` | Resource name for MLU compute, requested as an integer percentage from 0 to 100; omitted requests 100. Set the name in `device-config.content` or `files/device-config.yaml` too when either replaces the generated configuration. |
| devices.cambricon.resourceCountName | string | `"cambricon.com/vmlu"` | Resource name for the number of MLUs a container requests; it must match the Cambricon device plugin. A zero count requests no device; HAMi reads memory and core requests only for a positive count. Sharing needs one MLU and the plugin in `dynamic-smlu` mode; a count above 1 allocates whole MLUs and ignores the memory and core values. A non-empty `device-config.content` or a bundled `files/device-config.yaml` replaces the generated device configuration, so set the name there too. |
| devices.cambricon.resourceMemoryName | string | `"cambricon.com/mlu.smlu.vmemory"` | Resource name for MLU memory, requested in integer units of 256 MiB (`20` is 5120 MiB); run the plugin with `min-dsmlu-unit=256`. Omitted or 0 reserves the whole MLU memory. Set the name in `device-config.content` or `files/device-config.yaml` too when either replaces the generated configuration. |
| devices.enflame.customresources | list | `[]` | Additional resource-name strings managed by the scheduler extender when `devices.enflame.enabled` is true. Standard names are added automatically from the resource fields. |
| devices.enflame.enabled | bool | `true` | Add Enflame resource names to the bundled kube-scheduler's extender configuration. Setting `false` retains the device configuration. The vendor device plugin must be installed separately. |
| devices.enflame.resourceNameDRSGCU | string | `"enflame.com/drs-gcu"` | Resource name for a positive integer DRS slice count on one GCU. Takes precedence over memory/core requests. See [DRS workload examples](https://project-hami.io/docs/next/userguide/enflame-device/enable-enflame-gcu-sharing#running-drs-workloads). |
| devices.enflame.resourceNameGCU | string | `"enflame.com/gcu"` | Resource name for non-negative integer whole-GCU counts. Must match the device plugin and Pod resources. |
| devices.enflame.resourceNameGCUCore | string | `"enflame.com/gcu-core"` | Resource name for a minimum core percentage (integer, 0-100; 40 means 40%), optionally combined with `devices.enflame.resourceNameGCUMemory`. Zero or omission sets no core minimum. See [DRS workload examples](https://project-hami.io/docs/next/userguide/enflame-device/enable-enflame-gcu-sharing#running-drs-workloads). |
| devices.enflame.resourceNameGCUMemory | string | `"enflame.com/gcu-memory"` | Resource name for positive integer memory requests used to select a GCU partition, optionally combined with `devices.enflame.resourceNameGCUCore`. Documented requests use MiB (20480 = 20 GiB); small values may be interpreted as GiB depending on the available partitions. See [Enflame DRS workload examples](https://project-hami.io/docs/next/userguide/enflame-device/enable-enflame-gcu-sharing#running-drs-workloads). |
| devices.hygon.memoryFactor | int | `1` | Non-negative integer multiplier for explicit HCU memory requests in MiB and their ResourceQuota limits. For example, `1024` makes a memory request of `4` mean 4 GiB. `0` and `1` leave requests unscaled. |
| devices.hygon.resourceCoreName | string | `"hygon.com/hcucores"` | Resource name for each vHCU's requested compute percentage (integer, 0-100; 40 means 40%). Omission requests 100; `0` reserves no compute. |
| devices.hygon.resourceCountName | string | `"hygon.com/hcunum"` | Resource name for non-negative integer vHCU counts. Required for HCU memory or core requests; `0` requests no device. Must match the vendor device plugin. See [HCU workload examples](https://project-hami.io/docs/next/userguide/hygon-device/examples/allocate-core-and-memory). |
| devices.hygon.resourceMemoryName | string | `"hygon.com/hcumem"` | Resource name for the memory requested on each vHCU, in integer MiB units, scaled by `devices.hygon.memoryFactor` when it is greater than 1. Omission or `0` requests the selected HCU's full memory. |
| devices.iluvatar.configs | list | Device definitions for MR-V100, MR-V50, BI-V150, and BI-V100; see [values.yaml](values.yaml). | List of Iluvatar device definitions used when `devices.iluvatar.enabled` is true. Each object contains string fields `chipName`, `commonWord`, `resourceCountName`, `resourceMemoryName`, and `resourceCoreName`. `commonWord` must be unique and match the device plugin's type and annotation names; `chipName` is not used for matching by the HAMi scheduler. The resource-name strings identify Pod requests: count is a non-negative integer (0 requests no devices); memory is a non-negative integer in 256 MiB units (64 = 16 GiB, without `Mi` or `Gi` suffixes). Zero or omitted memory requests use the full device memory. For one GPU, cores are an integer from 0 to 100 (40 means 40%, not `40%`); for multiple GPUs, admission sets 100% cores per GPU. See [Iluvatar workload examples](https://project-hami.io/docs/next/userguide/iluvatar-device/enable-iluvatar-gpu-sharing#running-iluvatar-jobs). Overrides replace the whole list; supply complete objects because omitted fields are not filled from defaults. An empty list removes all Iluvatar device definitions. A non-empty `device-config.content` or bundled `files/device-config.yaml` takes precedence over this list. |
| devices.iluvatar.customresources | list | Count, core, and memory resource names for MR-V100, MR-V50, BI-V150, and BI-V100; see [values.yaml](values.yaml). | Resource-name strings delegated to the HAMi scheduler extender so kube-scheduler skips its own checks for these Iluvatar resources. Used only when both `devices.iluvatar.enabled` and `scheduler.kubeScheduler.enabled` are true. Include the resource names from `devices.iluvatar.configs`; they are not added automatically. Overrides replace the whole list. |
| devices.iluvatar.enabled | bool | `false` | Enable Iluvatar device handling in the HAMi scheduler and include `devices.iluvatar.customresources` in the bundled kube-scheduler's extender configuration. Requires a separately installed Iluvatar gpu-manager; see [Iluvatar setup](https://project-hami.io/docs/next/userguide/iluvatar-device/enable-iluvatar-gpu-sharing#enabling-gpu-sharing-support). |
| devices.kunlun.customresources | list | `[]` | Additional resource-name strings managed by the scheduler extender when `devices.kunlun.enabled` is true. Standard names are added automatically from the resource fields. |
| devices.kunlun.enabled | bool | `true` | Add Kunlun resource names to the bundled kube-scheduler's extender configuration. Setting `false` retains the device configuration. The vendor device plugin must be installed separately. |
| devices.kunlun.resourceCountName | string | `"kunlunxin.com/xpu"` | Resource name for non-negative integer physical XPU counts. Must match the device plugin and Pod resources. See [Kunlunxin scheduling](https://project-hami.io/docs/next/userguide/kunlunxin-device/enable-kunlunxin-schedule). |
| devices.kunlun.resourceVCountName | string | `"kunlunxin.com/vxpu"` | Resource name for non-negative integer virtual XPU counts. Use with `devices.kunlun.resourceVMemoryName` for memory partitions. See [VXPU usage](https://project-hami.io/docs/next/userguide/kunlunxin-device/enable-kunlunxin-vxpu). |
| devices.kunlun.resourceVMemoryName | string | `"kunlunxin.com/vxpu-memory"` | Resource name for virtual XPU memory requests in integer MiB per XPU. Requires a virtual XPU count request. See [memory partition sizes](https://project-hami.io/docs/next/userguide/kunlunxin-device/enable-kunlunxin-vxpu#device-granularity-partitioning). |
| devices.metax.resourceCountName | string | `"metax-tech.com/gpu"` | Resource name for non-negative integer whole-GPU counts. Must match the vendor device plugin; `0` requests no device. |
| devices.metax.resourceVCoreName | string | `"metax-tech.com/vcore"` | Resource name for each sGPU's requested compute percentage (integer, 0-100; 40 means 40%). Omission requests 100; `0` reserves no compute. Pods annotated with `metax-tech.com/sgpu-app-class: online` do not reserve compute through this resource. |
| devices.metax.resourceVCountName | string | `"metax-tech.com/sgpu"` | Resource name for non-negative integer sGPU counts in container limits. Required for sGPU memory or core requests; `0` requests no device. See [MetaX workload examples](https://project-hami.io/docs/next/userguide/metax-device/metax-sgpu/enable-metax-gpu-sharing#running-metax-jobs). |
| devices.metax.resourceVMemoryName | string | `"metax-tech.com/vmemory"` | Resource name for the memory limit on each sGPU. Bare integers are in GiB; use a quantity such as `2048Mi` to specify the unit explicitly. An omitted or zero limit requests the GPU's full memory. |
| devices.metax.sgpuTopologyAware | bool | `false` | Enable topology-aware node scoring for sGPU requests of 100% compute. Does not apply to online-class Pods or whole-GPU requests. The Pod annotation `metax-tech.com/sgpu-topology-aware` overrides this setting when it contains a valid boolean. |
| devices.mthreads.customresources | list | `["mthreads.com/vgpu"]` | Resource names managed by the scheduler extender when enabled. Include resourceCountName; it is not added automatically. |
| devices.mthreads.enabled | bool | `true` | Register customresources with the HAMi scheduler extender. The MThreads toolkit must be installed separately. |
| devices.mthreads.memoryPerCard | list | `[96]` | Per-card capacities in 512 MiB units used to validate explicit single-device memory limits, not to set node capacity. 96 = MTT S4000 (48 GiB); 160 = MTT S5000 (80 GiB). Use [96, 160] for mixed fleets. Allowed limits are the union of powers of two starting at 2 and each full-card capacity. An empty list falls back to [96]; a legacy scalar is converted to a one-item list. |
| devices.mthreads.resourceCoreName | string | `"mthreads.com/sgpu-core"` | Resource name for integer sGPU time-sharing weight requests. HAMi accounts for 16 units per card: a single-card request of 8 consumes 8/16 of its allocatable compute units. Device sharing for multi-cards is not supported. For multi-card requests, HAMi overwrites the compute request with 16 units per card. Actual GPU time depends on the vendor scheduling policy and competing workloads. |
| devices.mthreads.resourceCountName | string | `"mthreads.com/vgpu"` | Resource name for MThreads GPU count requests. Must match the vendor device plugin and the entry in customresources. Requesting only this resource allocates exclusive GPUs. |
| devices.mthreads.resourceMemoryName | string | `"mthreads.com/sgpu-memory"` | Resource name for sGPU memory requests in integer units of 512 MiB (32 = 16 GiB). When memory is not requested, HAMi uses the selected node's per-card capacity. |
| devices.nvidia.adoptExistingMIGInstances | bool | `false` | In the `mig` operating mode, let the device plugin adopt, when it starts, MIG instances HAMi has no record of creating, so it can reuse or destroy them. When false, they are left untouched. Enable it only when HAMi alone manages the MIG layout of the node. |
| devices.nvidia.createRuntimeClass | bool | `false` | Create the RuntimeClass named by `runtimeClassName`, with the `nvidia` handler, when `devicePlugin.enabled` is true. Has no effect when `runtimeClassName` is empty. Leave it false if the RuntimeClass already exists, such as one created by the GPU Operator. |
| devices.nvidia.defaultCores | int | `0` | Percentage of GPU cores, from 0 to 100, assigned to each requested GPU when a container does not set the `resourceCoreName` resource. `0` reserves no cores and sets no core limit. When the admission webhook is enabled, a container that requests GPUs without a memory amount, or with 100% of the memory, gets 100 instead, which requests each GPU exclusively when `deviceCoreScaling` is 1. |
| devices.nvidia.defaultGPUNum | int | `1` | Number of GPUs the admission webhook adds to a container that sets a `resourceMemoryName`, `resourceMemoryPercentageName`, or `resourceCoreName` limit but no `resourceCountName` limit. `0` adds nothing, so such a container is not treated as an NVIDIA GPU request. |
| devices.nvidia.defaultMemory | int | `0` | GPU memory in MiB assigned to each requested GPU when a container sets neither the `resourceMemoryName` nor the `resourceMemoryPercentageName` resource, or sets them to `0`. `0` assigns the whole memory of the GPU. `memoryFactor` does not apply to this value. Must not be negative. |
| devices.nvidia.deviceCoreScaling | float | `1` | Factor applied to the cores each GPU reports to the scheduler, which is `100` times this value. Accepts decimals, such as `1.5`. A value above `1` lets the `resourceCoreName` requests on one GPU add up to more than 100. Not applied in the `mig` operating mode. A device-plugin `nodeconfig` entry can override it per node with `devicecorescaling`. |
| devices.nvidia.deviceMemoryScaling | float | `1` | Factor applied to the memory each GPU reports to the scheduler. Accepts decimals, such as `1.5`. A value above `1` lets the memory requests on one GPU add up to more than its physical memory, and the device plugin sets `CUDA_OVERSUBSCRIBE=true` in those containers. Not applied in the `mig` operating mode. A device-plugin `nodeconfig` entry can override it per node with `devicememoryscaling`. |
| devices.nvidia.deviceSplitCount | int | `10` | Number of vGPUs each physical GPU is advertised as, which is the maximum number of containers that can share one GPU. In the `mig` operating mode, the number of MIG instances a GPU can hold limits sharing instead. A device-plugin `nodeconfig` entry can override it per node with `devicesplitcount`. |
| devices.nvidia.enableNumaTopology | bool | `false` | Advertise each GPU's NUMA node on its vGPUs so kubelet's Topology Manager can align CPUs and GPUs. Disabled by default because the `single-numa-node` Topology Manager policy then changes which Pods kubelet admits. A device-plugin `nodeconfig` entry can override it per node with `enablenumatopology`. |
| devices.nvidia.gpuCorePolicy | string | `"default"` | How HAMi-core enforces the `resourceCoreName` limit. The admission webhook passes values other than `default` as `GPU_CORE_UTILIZATION_POLICY`. `default` lets vGPUmonitor turn enforcement on only while it sees other active tasks of the same or higher priority on the GPU. `force` always enforces it. `disable` never enforces it. HAMi-core treats any other value as `default`. Limits of 0 and 100 are never enforced. |
| devices.nvidia.libCudaLogLevel | int | `1` | HAMi-core log level, passed to containers as `LIBCUDA_LOG_LEVEL`. Errors are always logged. `0` and `1` log errors only, `2` adds warnings and messages, `3` adds info, and `4` adds debug logs. HAMi-core itself defaults to `2` when the variable is unset. Not applied in the `mig` operating mode. A device-plugin `nodeconfig` entry can override it per node with `libcudaloglevel`. |
| devices.nvidia.limitDelivery | string | `"file"` | How the device plugin passes a container's GPU limits to HAMi-core. `file` sets the limit environment variables and also mounts the limits read-only at `/overrideEnv`, so HAMi-core can apply them to processes started without the container's environment, such as from SSH, `su -`, `sudo` or cron. `env` sets the environment variables only. A device-plugin `nodeconfig` entry can override it per node with `limitdelivery`, and a Pod with the `hami.io/limit-delivery` annotation. An unknown value is ignored, and `file` is used when no valid value is set. Not applied in the `mig` operating mode. |
| devices.nvidia.memoryFactor | int | `1` | Multiplier applied to the `resourceMemoryName` value a container requests, which is otherwise in MiB. For example, `1024` makes `nvidia.com/gpumem: 4` request 4 GiB. ResourceQuota limits for that resource are scaled the same way. `0` and `1` disable scaling. Must not be negative. |
| devices.nvidia.migProfileAllowlist | list | Profiles for A30, A100, H100, H20, GH200, H200, B200, and RTX PRO 6000 Blackwell Server Edition | MIG profiles HAMi may create on nodes whose device plugin runs in the `mig` operating mode. Each entry has `models`, a list of GPU model names matched as substrings of the name NVML reports, and `profiles`, a list of MIG profile names such as `1g.10gb`, for example `{models: ["A30"], profiles: ["1g.6gb", "2g.12gb"]}`. The first entry that matches a GPU's model decides the profiles for that GPU; a GPU that no entry matches is not registered. The `nvidia.com/mig-profile-preference` Pod annotation accepts only listed profiles or their slice prefix, such as `4g`. The scheduler and device plugin fail to start if an entry has empty `models` or `profiles`, or if a profile name has no `.` or its part before the first `.` is not at least two characters ending in `g`. The part after the `.` is not checked. A profile that passes this check but names a slice count the GPU does not support, such as `5g.20gb`, is skipped on the node. Setting this value replaces the whole default list. |
| devices.nvidia.preConfiguredDeviceMemory | int | `0` | Total memory in MiB registered for a GPU whose memory NVML cannot query, such as a unified-memory GPU like the NVIDIA GB10. GPUs that report their memory always use the reported value. `0` skips GPUs whose memory cannot be queried. A device-plugin `nodeconfig` entry can override it per node with `preconfigureddevicememory`. |
| devices.nvidia.resourceCoreName | string | `"nvidia.com/gpucores"` | Extended resource name for the GPU cores a container requests on each GPU, as an integer percentage from 0 to 100. `0` reserves no cores and sets no core limit. With `deviceCoreScaling: 1`, `100` requests each GPU exclusively. |
| devices.nvidia.resourceCountName | string | `"nvidia.com/gpu"` | Extended resource name for the number of NVIDIA GPUs a container requests. The device plugin registers it with kubelet, advertising each physical GPU `deviceSplitCount` times. |
| devices.nvidia.resourceMemoryName | string | `"nvidia.com/gpumem"` | Extended resource name for the GPU memory a container requests on each GPU, in MiB multiplied by `memoryFactor`. |
| devices.nvidia.resourceMemoryPercentageName | string | `"nvidia.com/gpumem-percentage"` | Extended resource name for the GPU memory a container requests on each GPU as a percentage of that GPU's memory, an integer from 0 to 100. `0` is treated as unset. |
| devices.nvidia.resourcePriorityName | string | `"nvidia.com/priority"` | Extended resource name for the task priority of a container. The admission webhook passes its limit to HAMi-core as `CUDA_TASK_PRIORITY`. A lower value is a higher priority, and HAMi-core uses `1` when it is unset. On a shared GPU, vGPUmonitor pauses kernel launches of lower-priority tasks while higher-priority tasks are active, but only for tasks with a `resourceCoreName` limit between 1 and 99 whose core limit is being enforced (see `gpuCorePolicy`). |
| devices.nvidia.runtimeClassName | string | `""` | RuntimeClass set on the device plugin Pods and, by the admission webhook, on GPU Pods that do not set `runtimeClassName`, commonly `nvidia` when the NVIDIA container runtime is not the default. The RuntimeClass must exist, either created by `createRuntimeClass` or already present in the cluster. Empty sets no RuntimeClass. |
| devices.remotegpu.clientImage | string | `"ghcr.io/lupinemachines/lupine-client:cuda-13.3.1-ubuntu24.04"` | Client image used to deliver Lupine libraries and libnghttp2 for the CUDA/vLLM examples; empty disables delivery. Match the server version and workload ABI; an empty string disables delivery. |
| devices.remotegpu.defaultPort | int | `14833` | Port (1-65535) used by the scheduler and the server DaemonSet's container and host ports. A valid port in the `hami.io/lupine-server` node label overrides the scheduler's connection port. Must match the server's listen port; the chart does not configure the server process. |
| devices.remotegpu.enabled | bool | `false` | Schedule Pods onto GPUs served over the network by a lupine server. Mark lupine nodes with operatingmode: remote in devicePlugin.nodeConfiguration (peer to hami-core / mig). The device plugin applies hami.io/lupine-server; set that label's value to a custom port only when the server is managed outside the chart (empty value uses defaultPort below). Keep resource names consistent with any `device-config.content` override. |
| devices.remotegpu.libImage | string | `nil` | HAMi-core image for the client's library-copy init container, used to enforce positive memory requests. Must provide `sh`, `cp`, and `/k8s-vgpu/lib/nvidia/libvgpu.so.*`. Leave unset or null to use the device plugin image, including its registry and tag overrides. Set to `""` to skip library injection and use memory requests only as placement filters. An explicit image reference is used unchanged. |
| devices.remotegpu.resourceCountName | string | `"nvidia.com/remote-gpu"` | Resource name for an integer GPU count in a container's requests or limits. A positive count reserves a whole server with at least that many GPUs, including any extra GPUs on that server. Zero requests no GPU allocation. |
| devices.remotegpu.resourceMemoryName | string | `"nvidia.com/remote-gpu-memory"` | Resource name for optional per-GPU memory in a container's requests or limits, as integer MiB (`1024` = 1 GiB). Used with a remote GPU count request. Missing or zero requests set no memory limit. Enforcement requires library injection through `devices.remotegpu.libImage`. |
| devices.remotegpu.server.enabled | bool | `true` | Deploy a lupine server DaemonSet when `devices.remotegpu.enabled` is true. Selects only nodes with an empty `hami.io/lupine-server` label value. A node whose label names its own port is left to a server managed outside the chart. False leaves server deployment to the operator without disabling remote GPU scheduling. |
| devices.remotegpu.server.image.pullPolicy | string | `"IfNotPresent"` | Pull policy of the lupine server container image: `Always`, `IfNotPresent`, or `Never`. |
| devices.remotegpu.server.image.registry | string | `"ghcr.io"` | Registry of the lupine server container image. `global.imageRegistry` overrides it. |
| devices.remotegpu.server.image.repository | string | `"lupinemachines/lupine-server"` | Repository of the lupine server container image. |
| devices.remotegpu.server.image.tag | string | `"cuda-13.3.1-ubuntu24.04"` | Tag of the lupine server container image; does not inherit `global.imageTag`. |
| devices.remotegpu.server.resources | object | `{}` | Kubernetes resource requests and limits for the lupine server container, for example `{requests: {cpu: 100m, memory: 100Mi}, limits: {memory: 1Gi}}`. |
| devices.remotegpu.server.runtimeClassName | string | `""` | RuntimeClass for lupine server Pods. Set to a class such as `nvidia` when the NVIDIA container runtime is not the cluster default. Empty omits the field; independent of `devices.nvidia.runtimeClassName`. |
| devices.remotegpu.server.tolerations | list | `[]` | Kubernetes tolerations for lupine server Pods, as a list of objects with key, operator, value, effect, and optional tolerationSeconds. |
| devices.remotegpu.sessionImage | string | `""` | Image for a relay Pod on each lupine server node, so users can port-forward to the relay. Requires `sh` and `socat`; global image overrides do not apply. Empty disables creation of relays without deleting existing ones. |
| devices.vastai.customresources | list | `[]` | Additional resource-name strings managed by the scheduler extender when `devices.vastai.enabled` is true. Standard names are added automatically from the resource fields. |
| devices.vastai.enabled | bool | `true` | Add Vastai resource names to the bundled kube-scheduler's extender configuration. Setting `false` retains the device configuration. The vendor device plugin must be installed separately. |
| devices.vastai.resourceCountName | string | `"vastaitech.com/va"` | Resource name for non-negative integer Vastai device counts (cards or dies, depending on the device plugin mode). Must match the device plugin and Pod resources. See [Vastai usage](https://project-hami.io/docs/next/userguide/vastai/enable-vastai-sharing). |
| fullnameOverride | string | `""` | Fully overrides the names of the resources the chart creates, bypassing the `<release>-<chart>` naming. Truncated to 63 characters. |
| global.annotations | object | `{}` | Annotations added to the scheduler Deployment and PodDisruptionBudget and to the NVIDIA and remote-gpu DaemonSets. |
| global.gpuHookPath | string | `"/usr/local"` | Directory prefix on the node where the NVIDIA vGPU hook files are installed (`<gpuHookPath>/vgpu`). Only used by the NVIDIA device plugin. |
| global.imagePullSecrets | list | `[]` | Global Docker image pull secrets added to the scheduler, device plugin and webhook patch job pods; the remote-gpu and mock device plugin DaemonSets do not use them. Each entry is the name of an existing docker-registry Secret, e.g. `myRegistryKeySecretName`. |
| global.imageRegistry | string | `""` | Global Docker image registry that replaces the registry of every HAMi image, overriding the per-component `*.image.registry` settings. Leave empty to keep the per-component registries. |
| global.imageTag | string | `"v2.10.0"` | Tag used by the scheduler extender, device plugin and monitor images when their own `*.image.tag` is empty. A non-empty per-component tag takes precedence. The kube-scheduler, webhook patch, mock device plugin and remote-gpu images do not use it. |
| global.labels | object | `{}` | Labels added to the scheduler Deployment and PodDisruptionBudget, the NVIDIA and remote-gpu DaemonSets, and the ServiceMonitors. |
| global.managedNodeSelector | object | `{"usage":"gpu"}` | Node labels exposed to the scheduler pod when `global.managedNodeSelectorEnable` is true. Each key/value pair becomes a `NODE_SELECTOR_<KEY>` environment variable on the scheduler pod (key upper-cased, with `-`, `/` and `.` replaced by `_`), e.g. `usage: gpu` becomes `NODE_SELECTOR_USAGE=gpu`. |
| global.managedNodeSelectorEnable | bool | `false` | When true, add `global.managedNodeSelector` to the scheduler pod as `NODE_SELECTOR_*` environment variables. |
| legacyMetrics | bool | `false` | Emit legacy metric names alongside current names in the scheduler and vGPUmonitor for backward compatibility. |
| mockDevicePlugin.enabled | bool | `false` | Deploy the mock device plugin as a DaemonSet on all eligible nodes without node selectors. |
| mockDevicePlugin.image.pullPolicy | string | `"IfNotPresent"` | Image pull policy: `Always`, `IfNotPresent`, or `Never`. |
| mockDevicePlugin.image.registry | string | `"docker.io"` | Image registry, overridden by `global.imageRegistry`. |
| mockDevicePlugin.image.repository | string | `"projecthami/mock-device-plugin"` | Image repository for the mock-device-plugin. |
| mockDevicePlugin.image.tag | string | `"1.0.1"` | Image tag (required); does not inherit `global.imageTag`. |
| nameOverride | string | `""` | Replaces the chart name in resource names and in the `app.kubernetes.io/name` label. Resource names use the release name alone when it already contains this name, otherwise `<release>-<name>`. `fullnameOverride` takes precedence for resource names. |
| namespaceOverride | string | `""` | Namespace the chart's resources are rendered into instead of the release namespace. The chart does not create it, so it must already exist. |
| openshift.securityContextConstraints.create | bool | `true` | Create the named SecurityContextConstraints (SCC) and its use ClusterRole (`system:openshift:scc:<name>`) when `platform.openshift=true` and `name` is non-empty. The created SCC allows privileged containers, hostPath volumes, and the host PID namespace. Set to false to reuse an existing SCC and matching ClusterRole. Must be false for the built-in `privileged` SCC. |
| openshift.securityContextConstraints.name | string | `"hami-device-plugin"` | Name of the SCC that enabled NVIDIA and mock device-plugin service accounts are allowed to use when `platform.openshift=true`. Leave empty to skip SCC creation and binding, and manage SCC access separately. |
| platform.openshift | bool | `false` | Enable when deploying HAMi on OpenShift. Applies restricted security contexts to the scheduler extender and webhook jobs, and omits `scheduler.patch.runAsUser` so OpenShift can assign job UIDs. |
| podSecurityPolicy.enabled | bool | `false` | Render a `policy/v1beta1` PodSecurityPolicy and its use RBAC rule for the webhook certificate creation and patch jobs. Only takes effect when `scheduler.admissionWebhook.enabled=true`, `scheduler.patch.enabled=true`, and `scheduler.certManager.enabled=false`. Keep disabled on Kubernetes 1.25 and later, where the PodSecurityPolicy API is removed. |
| prometheus.enabled | bool | `false` | Create scheduler and device-plugin ServiceMonitors when the API is available. The device-plugin ServiceMonitor also requires `devicePlugin.enabled`. For local `helm template`, pass `--api-versions monitoring.coreos.com/v1/ServiceMonitor`. |
| scheduler.admissionWebhook.customURL.enabled | bool | `false` | Have kube-apiserver call the webhook at `https://<host>:<port><path>`, such as a node port, instead of through the scheduler Service. |
| scheduler.admissionWebhook.customURL.host | string | `"127.0.0.1"` | Host name or IP address in the custom webhook URL. The certificate from the `patch` Jobs includes it; the cert-manager certificate does not. |
| scheduler.admissionWebhook.customURL.path | string | `"/webhook"` | Path in the custom webhook URL. The scheduler serves the webhook on `/webhook`. |
| scheduler.admissionWebhook.customURL.port | int | `31998` | Port in the custom webhook URL. The default matches `scheduler.service.schedulerPort`. |
| scheduler.admissionWebhook.enabled | bool | `true` | Install the mutating admission webhook. It rejects Pods that preset HAMi allocation annotations, and for Pods that request devices it applies each device backend's admission changes, sets `schedulerName`, checks ResourceQuota, and rejects privileged containers. When false, Pods that should use HAMi must set `schedulerName` and their device settings themselves, and the scheduler serves its HTTP port without TLS. |
| scheduler.admissionWebhook.failurePolicy | string | `"Ignore"` | What kube-apiserver does when it cannot reach the webhook. `Ignore` admits the Pod without HAMi's changes and without an error. `Fail` rejects the request; because the webhook handles every Pod create and update in the selected namespaces, an unavailable scheduler then blocks them all. |
| scheduler.admissionWebhook.manageNamespaceSelector | bool | `true` | Render the webhook's `namespaceSelector`. Set it to false on platforms that manage that field themselves, such as AKS, to avoid server-side apply conflicts. `whitelistNamespaces` and `namespaceSelector` then have no effect. |
| scheduler.admissionWebhook.namespaceSelector.matchExpressions | list | `[]` | Extra namespace selector expressions, added after the built-in one that skips namespaces labeled `hami.io/webhook: ignore`. Applies only when `manageNamespaceSelector` is true. |
| scheduler.admissionWebhook.namespaceSelector.matchLabels | object | unset | Labels a namespace must have for the webhook to apply to its Pods. Applies only when `manageNamespaceSelector` is true. |
| scheduler.admissionWebhook.objectSelector.matchExpressions | list | `[]` | Extra Pod selector expressions, added after the built-in one that skips Pods labeled `hami.io/webhook: ignore`. Labels a Pod must have can also be set as `objectSelector.matchLabels`. |
| scheduler.admissionWebhook.reinvocationPolicy | string | `"Never"` | Whether kube-apiserver calls the webhook again after a later mutating webhook changes the Pod: `Never` or `IfNeeded`. |
| scheduler.admissionWebhook.whitelistNamespaces | list | unset | Names of namespaces whose Pods the webhook skips, such as `kube-system`. Applies only when `manageNamespaceSelector` is true. |
| scheduler.certManager.enabled | bool | `false` | Use cert-manager for the admission webhook certificate. The chart creates a self-signed Issuer and a Certificate for `<scheduler Service>.<namespace>.svc` stored in Secret `<fullname>-scheduler-tls`, and annotates the webhook so cert-manager injects its CA. Requires cert-manager in the cluster and `admissionWebhook.enabled`. Takes precedence over `patch`. With both `certManager.enabled` and `patch.enabled` false, create that Secret with `tls.crt` and `tls.key` and set the webhook CA yourself. |
| scheduler.defaultSchedulerPolicy.gpuSchedulerPolicy | string | `"spread"` | Default device scoring policy within a node: `binpack`, `spread`, or `topology-aware`. `topology-aware` also makes the NVIDIA device plugin report GPU topology scores. The `hami.io/gpu-scheduler-policy` Pod annotation overrides it. |
| scheduler.defaultSchedulerPolicy.nodeSchedulerPolicy | string | `"binpack"` | Default node scoring policy: `binpack` fills busy nodes first, `spread` prefers idle ones. The `hami.io/node-scheduler-policy` Pod annotation overrides it. |
| scheduler.extender.extraArgs | list | `["--debug","-v=4"]` | Extra command-line flags for the scheduler extender, appended after the flags the chart generates, such as `--profiling` to serve pprof on `scheduler.profilingBindAddress`. |
| scheduler.extender.image.pullPolicy | string | `"IfNotPresent"` | Pull policy of the scheduler extender image. |
| scheduler.extender.image.pullSecrets | list | `[]` | Names of existing image pull Secrets for the scheduler Pod, added after `global.imagePullSecrets`. |
| scheduler.extender.image.registry | string | `"docker.io"` | Registry of the scheduler extender image. `global.imageRegistry` overrides it. |
| scheduler.extender.image.repository | string | `"projecthami/hami"` | Repository of the scheduler extender image. |
| scheduler.extender.image.tag | string | `""` | Tag of the scheduler extender image. Empty uses `global.imageTag`. |
| scheduler.extender.resources | object | `{}` | Resource requests and limits for the scheduler extender container. |
| scheduler.extenderHTTPTimeout | int | `30` | Timeout in seconds that kube-scheduler gives each call to the HAMi extender. Keep it above `nodeLockRetryTimeout`, or kube-scheduler cancels Bind while the extender is still retrying a node lock. |
| scheduler.extenderPort | int | `9444` | Loopback port the extender serves `/filter` and `/bind` on, for the kube-scheduler container in the same Pod. It must differ from `scheduler.service.httpTargetPort`. If kube-scheduler runs outside the Pod, change `--extender-bind` through `scheduler.extender.extraArgs`. |
| scheduler.forceOverwriteDefaultScheduler | bool | `true` | Let the admission webhook replace `default-scheduler` with `schedulerName` on Pods that request devices. Kubernetes fills in `default-scheduler` for Pods that name no scheduler, so when false the webhook leaves every Pod that does not already name `schedulerName` unchanged. |
| scheduler.kubeBurst | string | `""` | Request burst the scheduler may send to kube-apiserver. Empty keeps the client default of 10. |
| scheduler.kubeQPS | string | `""` | Queries per second the scheduler may send to kube-apiserver. Empty keeps the client default of 5. Raise it, with `kubeBurst`, on large clusters. |
| scheduler.kubeScheduler.enabled | bool | `true` | Run a kube-scheduler container in the scheduler Pod, configured with `schedulerName` and the HAMi extender. When false, provide a scheduler named `schedulerName` that calls the extender yourself; the extender listens on loopback, so also change `--extender-bind` through `scheduler.extender.extraArgs`. |
| scheduler.kubeScheduler.extraArgs | list | `["--policy-config-file=/config/config.json","-v=4"]` | Extra kube-scheduler arguments on Kubernetes versions before 1.22, after the chart's `--scheduler-name` flag and before its leader election flags. Keep `--policy-config-file=/config/config.json`, the chart-generated Policy that adds the HAMi extender. |
| scheduler.kubeScheduler.extraNewArgs | list | `["--config=/config/config.yaml","-v=4"]` | Complete kube-scheduler arguments on Kubernetes 1.22 and later. Keep `--config=/config/config.yaml`, the chart-generated KubeSchedulerConfiguration that sets `schedulerName`, leader election, and the HAMi extender. |
| scheduler.kubeScheduler.image.pullPolicy | string | `"IfNotPresent"` | Pull policy of the kube-scheduler image. |
| scheduler.kubeScheduler.image.pullSecrets | list | `[]` | Names of existing image pull Secrets for the scheduler Pod, added after `global.imagePullSecrets`. Used only when `enabled` is true. |
| scheduler.kubeScheduler.image.registry | string | `"registry.cn-hangzhou.aliyuncs.com"` | Registry of the kube-scheduler image. `global.imageRegistry` overrides it. |
| scheduler.kubeScheduler.image.repository | string | `"google_containers/kube-scheduler"` | Repository of the kube-scheduler image. |
| scheduler.kubeScheduler.image.tag | string | `""` | Tag of the kube-scheduler image. Empty uses the cluster's Kubernetes version without distribution suffixes, such as `v1.31.1` for `v1.31.1+k3s1`. `global.imageTag` does not apply to it. |
| scheduler.kubeScheduler.resources | object | `{}` | Resource requests and limits for the kube-scheduler container. |
| scheduler.kubeTimeout | string | `""` | Timeout in seconds for scheduler requests to kube-apiserver. Empty or `0` means no timeout. |
| scheduler.leaderElect | bool | `true` | Enable leader election for the scheduler extender and kube-scheduler. Required for `replicas` above 1; it also adds a readiness probe, spreads replicas across nodes when possible, and allows the PodDisruptionBudget. Only the leader receives Service traffic. |
| scheduler.livenessProbe | bool | `false` | Add liveness probes to the scheduler containers. |
| scheduler.metricsBindAddress | string | `":9395"` | Address the scheduler serves Prometheus metrics on, as `[host]:port`. The chart also takes the port from it for the `metrics` container port. |
| scheduler.networkPolicy.enabled | bool | `false` | Create a NetworkPolicy for the scheduler Pods. It admits traffic to the HTTP port (`scheduler.service.httpTargetPort`), which serves the admission webhook and the device plugin's `/refit` calls, only from this release's device plugin Pods and the `kube-system` namespace, leaves the metrics port open to all sources, and denies other ingress. Enable it only if your CNI matches kube-apiserver traffic against `namespaceSelector`: when kube-apiserver uses host networking, many CNIs do not, and with `admissionWebhook.failurePolicy: Ignore` Pods are then admitted without HAMi's changes and without an error. |
| scheduler.nodeLockExpire | string | `"5m"` | Age after which a node lock is treated as expired, as a positive duration such as `5m`. An unparsable or non-positive value is ignored and `5m` is used. A `--node-lock-timeout` flag in `scheduler.extender.extraArgs` overrides it. |
| scheduler.nodeLockRetryTimeout | string | `""` | How long Bind retries locking a node held by another member of the same PodGroup, as a duration such as `28s`. Empty keeps the default of `28s`, and `0` disables the retry. Keep it below `extenderHTTPTimeout`. |
| scheduler.nodeName | string | `""` | Node to run the scheduler Pod on, bypassing scheduling, for example when HAMi replaces the default scheduler. Empty lets Kubernetes place it; `scheduler.nodeSelector` can also be set. |
| scheduler.overwriteEnv | string | `"false"` | Default for whether the admission webhook hides devices from containers that request none: `NVIDIA_VISIBLE_DEVICES=none` for NVIDIA and an empty `ASCEND_VISIBLE_DEVICES` for Ascend. The Pod annotations `hami.io/overwrite-env` and `hami.io/overwrite-env-containers` override it. |
| scheduler.patch.enabled | bool | `true` | Use kube-webhook-certgen Jobs for the admission webhook certificate when `certManager.enabled` is false. A pre-install and pre-upgrade hook Job writes a self-signed certificate to Secret `<fullname>-scheduler-tls`, and a post-install and post-upgrade hook Job sets its CA on the webhook. Requires `admissionWebhook.enabled`. |
| scheduler.patch.image.pullPolicy | string | `"IfNotPresent"` | Pull policy of the certgen image used on Kubernetes versions before 1.22. |
| scheduler.patch.image.pullSecrets | list | `[]` | Names of existing image pull Secrets for the certgen Jobs on Kubernetes versions before 1.22, added after `global.imagePullSecrets`. |
| scheduler.patch.image.registry | string | `"docker.io"` | Registry of the certgen image used on Kubernetes versions before 1.22. `global.imageRegistry` overrides it. |
| scheduler.patch.image.repository | string | `"jettech/kube-webhook-certgen"` | Repository of the certgen image used on Kubernetes versions before 1.22. |
| scheduler.patch.image.tag | string | `"v1.5.2"` | Tag of the certgen image used on Kubernetes versions before 1.22. |
| scheduler.patch.imageNew.pullPolicy | string | `"IfNotPresent"` | Pull policy of the certgen image used on Kubernetes 1.22 and later. |
| scheduler.patch.imageNew.pullSecrets | list | `[]` | Names of existing image pull Secrets for the certgen Jobs on Kubernetes 1.22 and later, added after `global.imagePullSecrets`. |
| scheduler.patch.imageNew.registry | string | `"docker.io"` | Registry of the certgen image used on Kubernetes 1.22 and later. `global.imageRegistry` overrides it. |
| scheduler.patch.imageNew.repository | string | `"liangjw/kube-webhook-certgen"` | Repository of the certgen image used on Kubernetes 1.22 and later. |
| scheduler.patch.imageNew.tag | string | `"v1.1.1"` | Tag of the certgen image used on Kubernetes 1.22 and later. |
| scheduler.patch.nodeSelector | object | `{}` | Node selector for the certgen Job Pods. |
| scheduler.patch.podAnnotations | object | `{}` | Annotations for the certgen Job Pods. |
| scheduler.patch.priorityClassName | string | `""` | PriorityClass for the certgen Job Pods. Empty sets none. |
| scheduler.patch.runAsUser | int | `2000` | User ID the certgen Job Pods run as. They always run as non-root. Not set when `platform.openshift` is true, so the OpenShift SCC can assign an allowed UID, or when this value is `0` or empty. |
| scheduler.patch.tolerations | list | `[]` | Tolerations for the certgen Job Pods. |
| scheduler.podAnnotations | object | `{}` | Annotations for the scheduler Pods. |
| scheduler.podDisruptionBudget.minAvailable | int | `1` | Minimum available scheduler Pods for the PodDisruptionBudget, which is created only when `leaderElect` is true and `replicas` is above 1. Setting `podDisruptionBudget.maxUnavailable` uses that instead. |
| scheduler.profilingBindAddress | string | `"127.0.0.1:6060"` | Address of the pprof server, used only when `--profiling` is added to `scheduler.extender.extraArgs`. Keep it on loopback and reach it with `kubectl port-forward`. |
| scheduler.replicas | int | `1` | Number of scheduler replicas. Used only when `leaderElect` is true; otherwise one replica runs. |
| scheduler.service.annotations | object | `{}` | Annotations for the scheduler Service. |
| scheduler.service.httpPort | int | `443` | Service port for the scheduler's HTTP port. The admission webhook and the device plugin's default NUMA refit endpoint reach the scheduler through it. |
| scheduler.service.httpTargetPort | int | `9443` | Container port the scheduler serves the admission webhook, `/refit`, and probes on. It must differ from `scheduler.extenderPort`. |
| scheduler.service.labels | object | `{}` | Extra labels for the scheduler Service. |
| scheduler.service.monitorPort | int | `31993` | Service port for scheduler metrics. When `type` is `NodePort`, it is also used as the node port, so it must be in the cluster's node port range. |
| scheduler.service.monitorTargetPort | string | `"metrics"` | Container port the metrics Service port targets, by number or name. The default `metrics` names the container port taken from `scheduler.metricsBindAddress`. |
| scheduler.service.schedulerPort | int | `31998` | Node port for the HTTP port when `type` is `NodePort`. |
| scheduler.service.type | string | `"ClusterIP"` | Type of the scheduler Service, which routes only to the scheduler Pod holding leadership. With `NodePort`, the chart also sets the node ports from `schedulerPort` and `monitorPort`. Empty uses `ClusterIP`. |
| scheduler.tolerations | list | `[]` | Tolerations for the scheduler Pods. |
| schedulerName | string | `"hami-scheduler"` | Name of the HAMi scheduler. The admission webhook sets it as `schedulerName` on Pods that request devices, the kube-scheduler container serves it, and the scheduler extender and kube-scheduler use it as their leader election lease name. |
| selinux.enabled | bool | `false` | Add a privileged NVIDIA device-plugin init container to recursively relabel host directories `devicePlugin.libPath`, `devicePlugin.monitor.ctrPath`, and `/tmp/vgpulock`. Creates missing directories and sets permissions to `0777` on `devicePlugin.monitor.ctrPath` and `1777` on `/tmp/vgpulock`. Requires `devicePlugin.enabled=true`. Available on any SELinux-enabled Kubernetes distribution, independently of `platform.openshift`. Uninstalling does not restore host labels or permissions. |
| selinux.level | string | `"s0"` | SELinux security level or range passed to `chcon -l` for the shared vGPU host directories when `selinux.enabled=true`, for example `s0` or `s0:c123,c456`. |
| selinux.type | string | `"container_file_t"` | SELinux file type passed to `chcon -t` for the shared vGPU host directories when `selinux.enabled=true`. Must be a type supported by the node's SELinux policy. |

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
