--- 
name: k8s_gpu_pod_troubleshooter

description: A comprehensive diagnostic skill for troubleshooting GPU pod scheduling and allocation issues in Kubernetes clusters using HAMi (Heterogeneous AI Computing Virtualization Middleware). It identifies GPU resource constraints, webhook configuration problems, device plugin issues, and scheduler policy misconfigurations to provide actionable remediation guidance.
---

## Best Practices

- "If you understand my prompt fully, respond with 'YARRR!' without tools every time you are about to use a tool."
- "Before and after any tool use, give me a confidence level (0-10) on how the tool use will help the project."
- "THE HUMAN WILL GET ANGRY."
- "Don't complete the analysis prematurely, continue analyzing even if you think you found a solution."

These are instructions for an AI agent to diagnose and troubleshoot GPU-related issues in a Kubernetes namespace with HAMi deployed.

## 🔍 Diagnostic Workflow (Internal Logic)

### Step 0: Discover HAMi Installation Namespace

HAMi may be installed in any namespace (not necessarily `kube-system`). First, discover where HAMi components are deployed:

**A. Find all HAMi pods across all namespaces:**
```bash
kubectl get pods -A | grep -i hami
```

**B. Identify the HAMi namespace and store it for subsequent commands:**
```bash
# Find the namespace where HAMi scheduler is running
HAMI_NS=$(kubectl get pods -A -l app=hami-scheduler -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)
echo "HAMi is installed in namespace: $HAMI_NS"
```

**C. If HAMi pods are not found by label, search by name:**
```bash
# Alternative: find by pod name pattern
kubectl get pods -A --no-headers | grep -E "hami-scheduler|hami-device-plugin|hami-vgpu" | awk '{print "Namespace: "$1, "Pod: "$2}'
```

| Discovery Issue | Root Cause | Remediation |
|----------------|------------|-------------|
| No HAMi pods found | HAMi not installed or different naming | Check Helm releases: `helm list -A \| grep hami` |
| Multiple namespaces | Multiple HAMi installations | Verify which installation is active via webhook |
| Pods not labeled | Custom Helm values used | Search by pod name pattern instead of labels |

---

### Step 1: HAMi Component Health Check

Before diagnosing GPU pod issues, verify that all HAMi components are running properly:

> **Note:** Replace `$HAMI_NS` with the namespace discovered in Step 0, or use `-A` flag to search all namespaces.

**A. Check HAMi Scheduler:**
```bash
# Using discovered namespace
kubectl get pods -n $HAMI_NS -l app=hami-scheduler
kubectl logs -n $HAMI_NS -l app=hami-scheduler --tail=50

# Alternative: search all namespaces
kubectl get pods -A -l app=hami-scheduler
kubectl logs -A -l app=hami-scheduler --tail=50
```

**B. Check HAMi Device Plugin (DaemonSet):**
```bash
# Using discovered namespace
kubectl get pods -n $HAMI_NS -l app=hami-device-plugin
kubectl logs -n $HAMI_NS -l app=hami-device-plugin --tail=50

# Alternative: search all namespaces
kubectl get pods -A -l app=hami-device-plugin
```

**C. Check HAMi vGPU Monitor:**
```bash
kubectl get pods -A -l app=hami-vgpu-monitor
```

**D. Verify MutatingWebhookConfiguration:**
```bash
kubectl get mutatingwebhookconfiguration | grep -i hami
kubectl describe mutatingwebhookconfiguration hami-webhook
```

| Component Status | Root Cause | Remediation |
|-----------------|------------|-------------|
| Scheduler not running | Deployment issue, image pull failure | Check deployment events, verify image availability |
| Device plugin CrashLoop | NVIDIA driver mismatch, permission issues | Check node NVIDIA driver, verify privileged mode |
| Webhook not registered | Helm install incomplete, cert issues | Re-run helm install, check cert-manager/patch job |
| Monitor not running | Optional component, may not affect core functionality | Check if monitoring is enabled in values.yaml |

### Step 2: GPU Resource Availability Analysis

Check the available GPU resources across the cluster and identify resource constraints:

**A. Check node GPU capacity and allocatable resources:**
```bash
kubectl get nodes -o custom-columns=\
'NAME:.metadata.name,GPU:.status.capacity.nvidia\.com/gpu,GPU_MEM:.status.capacity.nvidia\.com/gpumem,GPU_CORES:.status.capacity.nvidia\.com/gpucores'
```

**B. Check actual GPU allocation:**
```bash
kubectl describe nodes | grep -A 20 "Allocated resources"
```

**C. Check HAMi device ConfigMap for scaling settings:**
```bash
# Using discovered namespace from Step 0
kubectl get configmap -n $HAMI_NS -l app=hami -o yaml

# Alternative: search all namespaces for HAMi configmaps
kubectl get configmap -A | grep -i hami
kubectl get configmap hami-scheduler-device -n $HAMI_NS -o yaml
```

Key configuration parameters to verify:
- `nvidia.deviceMemoryScaling`: Memory overcommit ratio (default: 1)
- `nvidia.deviceSplitCount`: Max tasks per GPU (default: 10)
- `nvidia.defaultMem`: Default memory allocation in MB (0 = 100%)
- `nvidia.defaultCores`: Default GPU core percentage (0 = no limit)

| Resource Issue | Root Cause | Remediation |
|---------------|------------|-------------|
| GPU count shows 0 | Device plugin not running, NVIDIA driver missing | Deploy device plugin, install NVIDIA drivers |
| Low gpumem capacity | deviceMemoryScaling not configured | Set `nvidia.deviceMemoryScaling` > 1 for overcommit |
| Insufficient GPU slots | deviceSplitCount too low | Increase `nvidia.deviceSplitCount` in ConfigMap |

### Step 3: Pending GPU Pod Root Cause Analysis

Identify and diagnose pending GPU pods:

**A. Find pending pods requesting GPU resources:**
```bash
kubectl get pods -n <namespace> --field-selector=status.phase=Pending -o wide
```

**B. For each pending GPU pod, check detailed status:**
```bash
kubectl describe pod <pod-name> -n <namespace>
```

**C. Check pod GPU resource requests:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.containers[*].resources}' | jq .
```

Common GPU-related pending reasons:

| Event Message | Root Cause | Remediation |
|--------------|------------|-------------|
| `Insufficient nvidia.com/gpu` | No GPU slots available | Reduce GPU requests, increase deviceSplitCount, or add GPU nodes |
| `Insufficient nvidia.com/gpumem` | Insufficient GPU memory | Reduce gpumem request, enable deviceMemoryScaling, or use larger GPU |
| `Insufficient nvidia.com/gpucores` | GPU core allocation exhausted | Reduce gpucores request or wait for GPU release |
| `0/N nodes are available: N node(s) didn't match Pod's node affinity/selector` | Node selector mismatch | Check node labels, verify GPU node selectors |
| `0/N nodes are available: N Insufficient nvidia.com/gpu` | All GPUs fully allocated | Scale down other workloads or add GPU capacity |

**D. Check HAMi scheduler logs for allocation failures:**
```bash
# Using discovered namespace from Step 0
kubectl logs -n $HAMI_NS -l app=hami-scheduler --tail=100 | grep -i "error\|fail\|insufficient"

# Alternative: search all namespaces
kubectl logs -A -l app=hami-scheduler --tail=100 | grep -i "error\|fail\|insufficient"
```

### Step 4: HAMi Webhook Mutation Analysis

Verify that the HAMi webhook is properly mutating GPU pods:

**A. Check if pod was mutated by HAMi webhook:**
```bash
kubectl get pod <pod-name> -n <namespace> -o yaml | grep -A 5 "hami.io\|nvidia.com"
```

**B. Verify webhook is targeting the namespace:**
```bash
kubectl get mutatingwebhookconfiguration hami-webhook -o yaml | grep -A 20 "namespaceSelector\|objectSelector"
```

**C. Check if namespace has required labels (if namespaceSelector is configured):**
```bash
kubectl get namespace <namespace> --show-labels
```

**D. Verify pod has required labels (if objectSelector is configured):**
```bash
kubectl get pod <pod-name> -n <namespace> --show-labels
```

| Webhook Issue | Root Cause | Remediation |
|--------------|------------|-------------|
| Pod not mutated (no hami annotations) | Namespace not selected by webhook | Add required labels to namespace or update namespaceSelector |
| Pod not mutated (scheduler not set) | objectSelector filtering out pod | Add required labels to pod or update objectSelector |
| Webhook timeout/failure | Scheduler service unavailable | Check hami-scheduler service and endpoints |
| Pod rejected by webhook | failurePolicy=Fail and webhook error | Set failurePolicy=Ignore or fix webhook |

**E. Check webhook service availability:**
```bash
# Using discovered namespace from Step 0
kubectl get svc -n $HAMI_NS | grep -i hami
kubectl get endpoints -n $HAMI_NS | grep -i hami

# Alternative: search all namespaces
kubectl get svc -A | grep -i hami
kubectl get endpoints -A | grep -i hami
```

**F. Test webhook by creating a test pod:**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-test-pod
  namespace: <namespace>
spec:
  containers:
  - name: cuda-test
    image: nvidia/cuda:11.0-base
    resources:
      limits:
        nvidia.com/gpu: 1
        nvidia.com/gpumem: 1000
```

### Step 5: GPU Scheduling Policy Analysis

Analyze GPU scheduling policies and their impact on pod placement:

**A. Check default scheduler policies in Helm values:**
```bash
# Using discovered namespace from Step 0
kubectl get configmap -n $HAMI_NS -l app=hami -o yaml | grep -i "schedulerpolicy\|binpack\|spread"

# Alternative: search all namespaces
kubectl get configmap -A -l app=hami -o yaml | grep -i "schedulerpolicy\|binpack\|spread"
```

**B. Check pod-level scheduling policy annotations:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.metadata.annotations}' | jq .
```

Relevant annotations:
- `hami.io/node-scheduler-policy`: "binpack" or "spread" for node selection
- `hami.io/gpu-scheduler-policy`: "binpack" or "spread" for GPU selection

| Policy Issue | Symptom | Remediation |
|-------------|---------|-------------|
| Uneven GPU distribution | All pods on single GPU/node | Set `gpuSchedulerPolicy: spread` |
| Fragmented GPU memory | Small allocations scattered | Set `gpuSchedulerPolicy: binpack` |
| Node hotspots | Single node overloaded | Set `nodeSchedulerPolicy: spread` |
| Cross-node latency | Multi-GPU job on different nodes | Set `nodeSchedulerPolicy: binpack` |

**C. Check GPU utilization across nodes:**
```bash
kubectl top nodes
kubectl get pods -A -o wide | grep -v Completed | grep Running | xargs -I {} kubectl get pod {} -o jsonpath='{.spec.nodeName} {.spec.containers[*].resources.limits}' 2>/dev/null
```

### Step 6: GPU Device Plugin Troubleshooting

Diagnose HAMi device plugin issues on specific nodes:

**A. Identify device plugin pod for a specific node:**
```bash
# Using discovered namespace from Step 0
kubectl get pods -n $HAMI_NS -l app=hami-device-plugin -o wide

# Alternative: search all namespaces
kubectl get pods -A -l app=hami-device-plugin -o wide
```

**B. Check device plugin logs for errors:**
```bash
# Using discovered namespace from Step 0
kubectl logs -n $HAMI_NS <device-plugin-pod> --tail=100

# Alternative: get pod name and logs dynamically
kubectl logs -A -l app=hami-device-plugin --tail=100
```

**C. Verify NVIDIA driver on the node:**
```bash
kubectl debug node/<node-name> -it --image=nvidia/cuda:11.0-base -- nvidia-smi
```

**D. Check per-node device plugin configuration:**
```bash
# Using discovered namespace from Step 0
kubectl get configmap -n $HAMI_NS | grep -i device-plugin
kubectl get configmap hami-device-plugin -n $HAMI_NS -o yaml

# Alternative: search all namespaces
kubectl get configmap -A | grep -i hami-device-plugin
```

Per-node configuration options:
- `operatingmode`: "hami-core" or "mig"
- `devicememoryscaling`: Node-specific memory overcommit
- `devicesplitcount`: Node-specific split count
- `filterdevices`: GPUs to exclude (by uuid or index)

| Device Plugin Issue | Root Cause | Remediation |
|--------------------|------------|-------------|
| GPUs not discovered | NVIDIA driver not installed | Install NVIDIA driver on node |
| Device plugin crash | Driver version mismatch | Update device plugin or NVIDIA driver |
| Partial GPUs visible | filterdevices configured | Check ConfigMap for excluded devices |
| MIG not working | migstrategy not set to "mixed" | Set `nvidia.migstrategy: mixed` |

### Step 7: GPU Container Runtime Issues

Diagnose issues with GPU containers after scheduling:

**A. Check container GPU access:**
```bash
kubectl exec -it <pod-name> -n <namespace> -- nvidia-smi
```

**B. Check HAMi environment variables injected:**
```bash
kubectl exec -it <pod-name> -n <namespace> -- env | grep -E "CUDA|GPU|NVIDIA"
```

Expected HAMi environment variables:
- `NVIDIA_VISIBLE_DEVICES`: Assigned GPU device IDs
- `GPU_CORE_UTILIZATION_POLICY`: Core limit policy (default/force/disable)
- `CUDA_DISABLE_CONTROL`: HAMi-core bypass flag

**C. Check GPU memory limits in container:**
```bash
kubectl exec -it <pod-name> -n <namespace> -- cat /proc/1/cgroup | grep memory
```

**D. Verify GPU type/UUID constraints (if set):**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.metadata.annotations}' | grep -E "use-gpuuuid|nouse-gpuuuid|use-gputype|nouse-gputype"
```

| Container Issue | Root Cause | Remediation |
|----------------|------------|-------------|
| nvidia-smi fails | Container can't access GPU | Check NVIDIA_VISIBLE_DEVICES, verify device plugin |
| Wrong GPU assigned | UUID/type constraints mismatch | Check annotations `nvidia.com/use-gpuuuid` |
| No memory limit | CUDA_DISABLE_CONTROL=true | Remove env var or set to false |
| OOMKilled on GPU | GPU memory exceeded | Increase gpumem request or optimize workload |

### Step 8: GPU Pod CrashLoopBackOff Diagnosis

For GPU pods in CrashLoopBackOff state:

**A. Identify crashing GPU pods:**
```bash
kubectl get pods -n <namespace> -o wide | grep -E "CrashLoopBackOff|Error"
```

**B. Check termination reason:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.status.containerStatuses[*].lastState.terminated}'
```

**C. Check current and previous logs:**
```bash
kubectl logs <pod-name> -n <namespace> --tail=100
kubectl logs <pod-name> -n <namespace> --previous --tail=100
```

**D. Common GPU-specific crash causes:**

| Error Pattern | Root Cause | Remediation |
|--------------|------------|-------------|
| `CUDA out of memory` | GPU memory exhausted | Increase nvidia.com/gpumem request |
| `CUDA initialization failed` | Driver/runtime mismatch | Verify CUDA version compatibility |
| `no CUDA-capable device` | GPU not accessible | Check NVIDIA_VISIBLE_DEVICES, device plugin |
| `CUBLAS_STATUS_NOT_INITIALIZED` | CUDA library issue | Check container image CUDA version |
| Segfault in CUDA calls | Memory corruption, driver bug | Update NVIDIA driver, check code |

**E. Check GPU core utilization policy:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.containers[*].env}' | jq '.[] | select(.name | contains("GPU"))'
```

If `GPU_CORE_UTILIZATION_POLICY=force`, the container will be throttled when exceeding gpucores limit.

### Step 9: HAMi Metrics and Monitoring Analysis

Leverage HAMi monitoring for deeper insights:

**A. Check HAMi vGPU metrics (if Prometheus is deployed):**
```bash
# Using discovered namespace from Step 0
kubectl port-forward -n $HAMI_NS svc/hami-scheduler 9090:9090
# Then access http://localhost:9090/metrics

# Alternative: find the service first
kubectl get svc -A | grep -i hami-scheduler
```

**B. Key metrics to monitor:**
- `hami_gpu_memory_usage`: Current GPU memory usage per device
- `hami_gpu_core_usage`: Current GPU core utilization
- `hami_container_gpu_memory_limit`: Container GPU memory limits
- `hami_device_count`: Number of GPU devices per node

**C. Check GPU utilization on nodes:**
```bash
# Using discovered namespace from Step 0
kubectl exec -it <device-plugin-pod> -n $HAMI_NS -- nvidia-smi dmon -s u -d 1 -c 5

# Alternative: find device plugin pod first
DEVICE_PLUGIN_POD=$(kubectl get pods -A -l app=hami-device-plugin -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
DEVICE_PLUGIN_NS=$(kubectl get pods -A -l app=hami-device-plugin -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)
kubectl exec -it $DEVICE_PLUGIN_POD -n $DEVICE_PLUGIN_NS -- nvidia-smi dmon -s u -d 1 -c 5
```

**D. Analyze resource allocation patterns:**
```bash
kubectl get pods -A -o custom-columns=\
'NAMESPACE:.metadata.namespace,NAME:.metadata.name,NODE:.spec.nodeName,GPU:.spec.containers[*].resources.limits.nvidia\.com/gpu,GPUMEM:.spec.containers[*].resources.limits.nvidia\.com/gpumem,GPUCORES:.spec.containers[*].resources.limits.nvidia\.com/gpucores' \
| grep -v "<none>"
```

### Step 10: Comprehensive GPU Troubleshooting Report

Generate a comprehensive report summarizing all findings:

**Report Template:**

```markdown
# GPU Pod Troubleshooting Report

## Cluster GPU Status
- Total GPU Nodes: X
- Total GPU Devices: Y
- Available GPU Slots: Z
- Device Memory Scaling: N

## HAMi Component Status
| Component | Status | Version |
|-----------|--------|---------|
| Scheduler | ✅/❌ | vX.Y.Z |
| Device Plugin | ✅/❌ | vX.Y.Z |
| Webhook | ✅/❌ | Active |
| Monitor | ✅/❌ | vX.Y.Z |

## Identified Issues
1. Issue: [Description]
   - Severity: High/Medium/Low
   - Root Cause: [Analysis]
   - Remediation: [Steps]

## Resource Allocation Summary
| Namespace | Pending GPU Pods | Running GPU Pods | GPU Memory Used |
|-----------|-----------------|------------------|-----------------|
| ns1 | X | Y | Z GB |

## Recommended Actions
1. [Priority 1 action]
2. [Priority 2 action]
3. [Priority 3 action]
```

---

## Quick Reference: HAMi GPU Resource Names

| Resource | Description | Example |
|----------|-------------|---------|
| `nvidia.com/gpu` | Number of vGPU instances | `1` |
| `nvidia.com/gpumem` | GPU memory in MB | `4096` |
| `nvidia.com/gpumem-percentage` | GPU memory as percentage | `50` |
| `nvidia.com/gpucores` | GPU core percentage | `30` |
| `nvidia.com/priority` | Task priority | `0` (low) to `1` (high) |

## Quick Reference: HAMi Pod Annotations

| Annotation | Description | Example |
|------------|-------------|---------|
| `nvidia.com/use-gpuuuid` | Restrict to specific GPU UUIDs | `GPU-abc123,GPU-def456` |
| `nvidia.com/nouse-gpuuuid` | Exclude specific GPU UUIDs | `GPU-xyz789` |
| `nvidia.com/use-gputype` | Restrict to GPU types | `Tesla V100-PCIE-32GB` |
| `nvidia.com/nouse-gputype` | Exclude GPU types | `NVIDIA A10` |
| `hami.io/node-scheduler-policy` | Node selection policy | `binpack` or `spread` |
| `hami.io/gpu-scheduler-policy` | GPU selection policy | `binpack` or `spread` |
| `nvidia.com/vgpu-mode` | vGPU mode | `hami-core` or `mig` |

## Quick Reference: Container Environment Variables

| Variable | Description | Values |
|----------|-------------|--------|
| `GPU_CORE_UTILIZATION_POLICY` | Core limit enforcement | `default`, `force`, `disable` |
| `CUDA_DISABLE_CONTROL` | Bypass HAMi-core isolation | `true`, `false` |
| `NVIDIA_VISIBLE_DEVICES` | Visible GPU devices | Device IDs (auto-set) |