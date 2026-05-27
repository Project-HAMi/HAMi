---
name: k8s-debug-pending-pod
description: >
  Use when pods are stuck in Pending, CrashLoopBackOff, or ImagePullBackOff state.
  Performs event-driven triage to quickly identify root cause, then deep-dives into
  scheduling failures, resource exhaustion, image pull errors, and crash loops.
---

## Interaction

Collect the following from the user **in a single exchange** before starting diagnosis:

| Input | Required | Default |
|-------|----------|---------|
| Namespace | Yes | — |
| Kubeconfig file path | No | `~/.kube/config` |
| Context name | No | current-context in the kubeconfig |

If the user provides a cluster name instead of a context, fuzzy-match it against available contexts in the kubeconfig and confirm the match.

The skill automatically discovers and diagnoses **all unhealthy pods** (Pending, CrashLoopBackOff, ImagePullBackOff, Error) in the namespace. No pod name is needed.

## Command Convention

All `kubectl` commands use shorthand below. **Actual execution must always include explicit flags:**

```bash
kubectl --kubeconfig=<kubeconfig> --context=<context> ...
```

Never rely on default kubeconfig or current-context.

## Diagnostic Workflow

### Step 1: Global Pod Overview

Get a full picture of all pod states in one command:

```bash
kubectl get pods -n <namespace> -o wide
```

Categorize pods into:
- ✅ Running/Completed — healthy, skip
- 🟡 Pending — go to Step 3
- 🔴 CrashLoopBackOff/Error — go to Step 4
- 🔴 ImagePullBackOff/ErrImagePull — go to Step 5
- ⚠️ Other abnormal states — note for report

All unhealthy pods are diagnosed. No manual pod selection needed.

### Step 2: Event Triage

Quick scan of recent warning events for immediate signal:

```bash
kubectl get events -n <namespace> --field-selector type=Warning --sort-by=.lastTimestamp | tail -50
```

Use events to guide which deep-dive steps are most relevant. Common event→action mapping:

| Event Reason | Points to |
|---|---|
| `FailedScheduling` | Step 3 (Pending) |
| `CrashLoopBackOff` / `BackOff` | Step 4 (Crash) |
| `ImagePullBackOff` / `ErrImagePull` | Step 5 (Image) |
| `OOMKilled` | Step 4 (Crash) |
| `FailedAttachVolume` / `FailedMount` | PVC issue — note in report |
| `Unhealthy` | Probe misconfiguration — note in report |
| `Evicted` | Node pressure — note in report |
| `NodeNotReady` | Infrastructure issue — note in report |

> **Short-circuit rule:** If events clearly point to a single root cause (e.g., all events are `FailedScheduling` with the same message), mark it as the **confirmed primary cause** but still scan remaining steps at reduced depth for additional findings.

### Step 3: Pending Pod Root Cause Analysis

For each Pending pod:

```bash
kubectl describe pod <pod-name> -n <namespace>
```

Parse the `Events` and `Conditions` sections. Common causes:

| Event Message Pattern | Root Cause | Remediation |
|---|---|---|
| `Insufficient cpu` / `Insufficient memory` | Resource exhaustion | Reduce requests, scale cluster, or check quota (→ Step 6) |
| `0/N nodes are available` + taint | Taint/toleration mismatch | Add toleration or untaint node |
| `0/N nodes are available` + node selector | No matching nodes | Fix nodeSelector or add matching nodes |
| `0/N nodes are available` + affinity | Affinity rule unsatisfiable | Relax affinity rules |
| `persistentvolumeclaim "X" not found` | Missing PVC | Create PVC or fix claim name |
| `pod has unbound immediate PersistentVolumeClaims` | PVC not bound | Check StorageClass and PV availability |
| `exceeded quota` | Quota limit hit | → Step 6 |

### Step 4: CrashLoopBackOff / Error Diagnosis

Only execute if Step 1 found pods in CrashLoopBackOff or Error state.

**A. Termination reason:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.status.containerStatuses[*].lastState.terminated}'
```

| Termination Reason | Root Cause | Remediation |
|---|---|---|
| `OOMKilled` | Memory limit exceeded | Increase memory limits, profile usage |
| exitCode `1` | Application failure | Check logs, configs, secrets, dependencies |
| exitCode `137` | SIGKILL / OOM | Check node pressure and memory |
| exitCode `139` | Segfault | Debug binary compatibility |
| exitCode `143` | SIGTERM not handled | Review graceful shutdown |

**B. Logs (current + previous):**
```bash
kubectl logs <pod-name> -n <namespace> --tail=100
kubectl logs <pod-name> -n <namespace> --previous --tail=100
```

**C. Resource config:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.containers[*].resources}' | jq .
```

**D. Restart count:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.status.containerStatuses[*].restartCount}'
```

High restart count (>5) combined with short uptime indicates persistent failure — check probes, dependencies, and startup ordering.

### Step 5: ImagePullBackOff Diagnosis

Only execute if Step 1 found pods in ImagePullBackOff or ErrImagePull state.

**A. Image reference:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.containers[*].image}'
```

**B. Pull error from events:**
```bash
kubectl describe pod <pod-name> -n <namespace> | grep -A10 "Events:"
```

| Error Pattern | Root Cause | Remediation |
|---|---|---|
| `repository does not exist` | Wrong image name/tag | Verify image name |
| `unauthorized` | Missing credentials | Fix `imagePullSecrets` |
| `manifest unknown` | Tag doesn't exist | Verify tag in registry |
| `connection refused` | Registry unreachable | Check network/DNS |
| `x509: certificate` | TLS issue | Fix CA trust chain |

**C. imagePullSecrets check:**
```bash
kubectl get pod <pod-name> -n <namespace> -o jsonpath='{.spec.imagePullSecrets}'
```

If empty and the image is from a private registry, this is likely the root cause.

### Step 6: Resource Quota Check (Conditional)

Only execute if Step 3 indicates quota exhaustion or resource issues.

```bash
kubectl describe resourcequota -n <namespace>
```

Analyze:
- Which resources are at or near limits (Used/Hard ratio > 80%)
- Whether the pending pod's requests would exceed remaining capacity
- Recommend: request quota increase or reduce pod resource requests

**Optional — Resource allocation efficiency:**
```bash
kubectl get pods -n <namespace> -o custom-columns='NAME:.metadata.name,CPU_REQ:.spec.containers[*].resources.requests.cpu,CPU_LIM:.spec.containers[*].resources.limits.cpu,MEM_REQ:.spec.containers[*].resources.requests.memory,MEM_LIM:.spec.containers[*].resources.limits.memory'
kubectl top pods -n <namespace>
```

Flag:
- Requests >> actual usage → waste, can reduce to free quota
- No limits set → unbounded usage risk
- High requests with low limits → OOM risk

### Step 7: Report

Generate a structured report:

```
Kubernetes Pod Troubleshooting Report
=====================================
Namespace: <namespace>
Cluster:   <context> (via <kubeconfig>)
Scope:     all unhealthy pods in namespace

Primary Issue
- <confirmed root cause with evidence>

Additional Findings
- <secondary issues discovered during scan>

Pod Status Summary
- Total: N | Running: N | Pending: N | CrashLoop: N | ImagePull: N

Detailed Findings
- [Per-pod breakdown with cause and evidence]

Recommended Actions (priority order)
1. <highest impact fix>
2. <next>
3. <next>

Skipped Checks
- <any steps skipped due to permissions, unreachable API, or missing metrics-server>
```
