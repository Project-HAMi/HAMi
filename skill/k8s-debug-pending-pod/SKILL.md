--- 
name: k8s_namespace_troubleshooter

description: A comprehensive diagnostic skill that analyzes the health, resource usage, and scheduling issues within a specified Kubernetes namespace. It identifies quota constraints, pending pods, workload status anomalies, inefficient resource allocation, and critical events to provide actionable remediation guidance.
---

## Best Practices

- "If you understand my prompt fully, respond with 'YARRR!' without tools every time you are about to use a tool."
- "Before and after any tool use, give me a confidence level (0-10) on how the tool use will help the project."
- "THE HUMAN WILL GET ANGRY."
- "Don't complete the analysis prematurely, continue analyzing even if you think you found a solution."

These are instructions for an AI agent to diagnose and troubleshoot issues in a Kubernetes namespace.

## 🔍 Diagnostic Workflow (Internal Logic)

### Step 1: Resource Quota & Usage Analysis

Please help me check the resource quotas (ResourceQuota) and current resource usage in the Kubernetes namespace `knowledge-graph`. Summarize the following:

1. The resource quota limits for the namespace (CPU, memory, storage, etc.).
2. The current amount of resources used.
3. Whether there are any quota violations.
4. Ignore the case when you did not find any ResourceQuota in the namespace.

### Step 2: Pending Pod Root Cause Analysis

- Identify pending pods:  
    kubectl get pods -n knowledge-graph --field-selector=status.phase=Pending
  
- For each pod, run:  
    kubectl describe pod <pod-name> -n knowledge-graph
  
- Parse Events section to determine cause:
  - Insufficient cpu/memory → Resource exhaustion
  - 0/xx nodes are available → Node selector/taint/affinity mismatch
  - persistentvolumeclaim "xxx" not found → PVC issue
  - image can't be pulled → Not applicable (usually ImagePullBackOff, not Pending)
- Recommend fixes accordingly.

### Step 2.1: CrashLoopBackOff Pod Diagnosis

- Identify CrashLoopBackOff pods:
    kubectl get pods -n knowledge-graph | grep -E "CrashLoopBackOff|Error"
  
- For each CrashLoopBackOff pod, perform deep analysis:

  **A. Check container termination reason:**
    kubectl get pod <pod-name> -n knowledge-graph -o jsonpath='{.status.containerStatuses[*].lastState.terminated}'
  
  Common termination reasons:
  - `OOMKilled` (exitCode: 137) → Memory limit exceeded
  - `Error` (exitCode: 1) → Application error
  - `Error` (exitCode: 137) → SIGKILL (usually OOM or external kill)
  - `Error` (exitCode: 139) → SIGSEGV (segmentation fault)
  - `Error` (exitCode: 143) → SIGTERM (graceful shutdown failed)

  **B. Check current and previous container logs:**
    kubectl logs <pod-name> -n knowledge-graph --tail=100
    kubectl logs <pod-name> -n knowledge-graph --previous --tail=100
  
  **C. Check resource configuration:**
    kubectl get pod <pod-name> -n knowledge-graph -o jsonpath='{.spec.containers[*].resources}' | jq .
  
  **D. Check for common issues:**

  | Termination Reason | Root Cause | Remediation |
  |-------------------|------------|-------------|
  | OOMKilled | Memory usage exceeds limit | Increase memory limits in deployment/pod spec |
  | Error (exitCode: 1) | Application startup failure | Check logs for application errors, config issues |
  | Error (exitCode: 137) | SIGKILL (external kill or OOM) | Check OOM events, increase memory or fix memory leak |
  | Error (exitCode: 139) | Segmentation fault | Debug application, check for incompatible libraries |
  | Error (exitCode: 143) | SIGTERM handling issue | Increase terminationGracePeriodSeconds, fix signal handling |

  **E. For OOMKilled specifically:**
  - Check current resource requests vs limits ratio
  - Recommend increasing memory limit (typically 2x current limit)
  - Example patch command:

    ```bash
    kubectl patch deployment <deployment-name> -n knowledge-graph --type='json' -p='[
      {"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/memory", "value": "<new-limit>"},
      {"op": "replace", "path": "/spec/template/spec/containers/0/resources/requests/memory", "value": "<new-request>"}
    ]'
    ```
  
  **F. Check pod restart count and frequency:**
    kubectl get pod <pod-name> -n knowledge-graph -o jsonpath='{.status.containerStatuses[*].restartCount}'
  
  High restart counts indicate:
  - Persistent issues requiring investigation
  - Potential liveness probe misconfiguration
  - Resource constraint problems

### Step 2.2: ImagePullBackOff Pod Diagnosis

- Identify ImagePullBackOff pods:
    kubectl get pods -n knowledge-graph | grep -E "ImagePullBackOff|ErrImagePull"

- For each ImagePullBackOff pod, check:

  **A. Get image details:**
    kubectl get pod <pod-name> -n knowledge-graph -o jsonpath='{.spec.containers[*].image}'
  
  **B. Check events for specific error:**
    kubectl describe pod <pod-name> -n knowledge-graph | grep -A5 "Events:"
  
  **C. Common causes and remediation:**

  | Error Message | Root Cause | Remediation |
  |--------------|------------|-------------|
  | "repository does not exist" | Wrong image name/tag | Verify image name and tag exist in registry |
  | "unauthorized" | Missing/invalid credentials | Create/update imagePullSecrets |
  | "manifest unknown" | Tag doesn't exist | Verify tag exists, check for typos |
  | "connection refused" | Registry unreachable | Check network, firewall, registry status |
  | "x509: certificate" | TLS/SSL issues | Add CA cert or configure insecure registry |

  **D. For private registry issues:**

  # Check if imagePullSecrets is configured

    kubectl get pod <pod-name> -n knowledge-graph -o jsonpath='{.spec.imagePullSecrets}'

### Step 3: Resource Allocation Efficiency Audit

Please analyze the resource allocation in the Kubernetes namespace `knowledge-graph` and answer the following questions:

1. Is the current allocation of CPU and memory reasonable?
2. Are there any cases of resource wastage or resource shortages?
3. Suggestions for optimizing resource allocation.

- Commands:

  # Get resource requests and limits for all pods

    kubectl get pods -n knowledge-graph -o custom-columns=\
    'NAME:.metadata.name,CPU_REQ:.spec.containers[*].resources.requests.cpu,CPU_LIM:.spec.containers[*].resources.limits.cpu,MEM_REQ:.spec.containers[*].resources.requests.memory,MEM_LIM:.spec.containers[*].resources.limits.memory'

  # Check actual resource usage (requires metrics-server)

    kubectl top pods -n knowledge-graph

- Analyze for common issues:
  - High memory requests with low limits → Risk of OOMKilled
  - Very low requests compared to limits → May cause scheduling issues
  - No limits set → Unbounded resource usage risk
  - Requests > actual usage → Resource wastage

### Step 4: Event Log Inspection

Please check the events in the Kubernetes namespace `knowledge-graph` and answer the following questions:

1. Are there any error or warning events?
2. What are the specific reasons for these events?
3. Possible solutions.

- Command:  
    kubectl get events -n knowledge-graph --sort-by=.lastTimestamp
  
- Filter for Type = Warning or Error:
    kubectl get events -n knowledge-graph --field-selector type=Warning --sort-by=.lastTimestamp

- Group by Reason and correlate with affected objects
- Map common reasons to solutions:

  | Event Reason | Description | Remediation |
  |-------------|-------------|-------------|
  | ImagePullBackOff | Cannot pull container image | See Step 2.2 for detailed diagnosis |
  | CrashLoopBackOff | Container keeps crashing | See Step 2.1 for detailed diagnosis |
  | BackOff | Container restart backoff | Check logs, may be OOMKilled or app error |
  | OOMKilled | Out of memory | Increase memory limits |
  | FailedScheduling | Cannot schedule pod | Check resources, node selectors, taints |
  | FailedAttachVolume | Volume attach failed | Check PVC, StorageClass, node issues |
  | FailedMount | Volume mount failed | Check PVC bound, node access to storage |
  | Unhealthy | Probe failed | Check liveness/readiness probe config |
  | NodeNotReady | Node is not ready | Check node status, kubelet logs |
  | Evicted | Pod evicted | Check node resources, pod priority |

### Step 5: Comprehensive Report Generation

Please generate a comprehensive report for the Kubernetes namespace `knowledge-graph` that includes:

- Summarize findings from all previous steps.
- Provide prioritized action items for remediation.
