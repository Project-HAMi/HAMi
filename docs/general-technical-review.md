# General Technical Review - HAMi / Incubation

- **Project:** HAMi
- **Project Version:** v2.8.0
- **Website:** https://project-hami.io
- **Date Updated:** 2026-04-13
- **Template Version:** v1.0
- **Description:** HAMi is a Kubernetes middleware for heterogeneous AI device sharing, isolation, and scheduling across GPUs/NPUs and other accelerators.

---

## Day 0 - Planning Phase

### Scope

- **Roadmap and scope process:** Scope is defined through issues/PRs and maintainer review, with governance maintained in the HAMi community repo. The roadmap is maintained in GitHub issues (for example, per-version tracking such as https://github.com/Project-HAMi/HAMi/issues/1615). After each version is released, the next weekly community meeting discusses roadmap goals for upcoming releases in detail, and each goal is assigned an assignee on the issue. Community users can reply on those issues to add or request features.  
  - Design docs: https://github.com/Project-HAMi/HAMi/blob/master/docs/develop/design.md  
  - Governance: https://github.com/Project-HAMi/community/blob/main/governance.md  
- **Target personas:**  
  - **Platform engineers:** Deploy HAMi as middleware on Kubernetes 1.20+ clusters with NVIDIA driver 440+ support for straightforward integration into existing platform products. GPU management and isolation are a common product differentiator that HAMi supports end to end.  
  - **MLOps teams:** Use GPU virtualization to reduce job wait times; rely on topology-aware scheduling and device-type selection to place training and inference workloads efficiently.  
  - **Individual AI workload owners:** Often operate a small GPU footprint; use sharing and multiplexing to improve utilization without changing application code.  
  - **IT operations teams:** Raise cluster utilization through GPU sharing; consolidate heterogeneous accelerators with integrated monitoring in a single cluster to lower maintenance cost.  

- **Primary use case:** Share and isolate accelerator resources (especially GPU memory/core) for multiple workloads on Kubernetes without application code changes. 
- **Additional supported use cases:** Topology-aware scheduling, multiple scheduling policies (binpack/spread), dynamic MIG support for NVIDIA, and heterogeneous device support (e.g., NVIDIA, Cambricon, Hygon, Iluvatar, MetaX, Enflame, Ascend integration references).
- **Unsupported/non-goal use cases:** HAMi is not a model serving framework, training framework, or general-purpose cluster autoscaler. It focuses on accelerator virtualization/scheduling.
- **Organizations that benefit:** Public cloud, private cloud, enterprise AI platforms, telecom, finance, education, manufacturing, and internet companies operating shared accelerator clusters.
- **End-user research:** Formal, survey-style user research is still limited. The clearest public evidence of production adoption is **CNCF end-user case studies**, including multiple community-submitted stories that feature HAMi; the [HAMi-filtered case study list](https://www.cncf.io/case-studies/?_sft_lf-project=hami) is the updating catalog on cncf.io. Representative named end users include:  
  - [SF Technology](https://www.cncf.io/case-studies/sf-technology/) (technology arm of SF Express)  
  - [Ke Holdings Inc.](https://www.cncf.io/case-studies/ke-holdings-inc/)  
  - [NIO](https://www.cncf.io/case-studies/nio/)  

### Usability

- **Interaction model:** Operators and workload owners interact with HAMi through Helm, Kubernetes Pod specs, component Services, and chart-driven scheduler/webhook settings.  
  - **Lifecycle:** Install, upgrade, and uninstall with Helm (see **Installation** below and the repository README quick start).  
  - **Workloads:** Request shared accelerators with Pod `requests`/`limits` plus HAMi annotations for slices, device selection, and scheduling policy.  
  - **Observability:** Inspect cluster-wide device and scheduling overview via the HAMi scheduler metrics HTTP endpoint; query the device-plugin vGPU monitor interface for live NVIDIA per-pod usage (listen ports are Helm-configurable—defaults are documented in the README and `docs/config.md`).  

- **UX/UI:** The default experience is Kubernetes-native: `kubectl`, the Pod/Node APIs, and YAML/Helm values. For graphical administration and monitoring, deploy the optional **HAMi WebUI**; Grafana-oriented examples are documented separately.  
  - HAMi WebUI user guide: https://project-hami.io/docs/userguide/hami-webui-user-guide  
  - Dashboard examples: https://github.com/Project-HAMi/HAMi/blob/master/docs/dashboard.md  

- **Production integration:** HAMi is in-cluster middleware: teams must already run a supported Kubernetes cluster. In production it extends the control plane with a mutating admission webhook, scheduler extender integration (default `kube-scheduler` or the chart-managed scheduler), device plugins, and CDI-capable runtimes where applicable. Additional batch/AI scheduler ecosystems are documented or tracked as follows:  
  - **Volcano:** Volcano-managed NVIDIA sharing via the Volcano vGPU device plugin (HAMi-core-based isolation)—see https://github.com/Project-HAMi/HAMi/blob/master/docs/how-to-use-volcano-vgpu.md  
  - **Koordinator:** End-to-end GPU sharing with HAMi—see https://koordinator.sh/docs/user-manuals/device-scheduling-gpu-share-with-hami  
  - **KAI Scheduler:** GPU sharing isolation design and implementation are still evolving—see https://github.com/kai-scheduler/KAI-Scheduler/pull/60  

### Design

#### Design principles

The following principles guide HAMi's architecture and feature trade-offs (see also the in-repo [design overview](https://github.com/Project-HAMi/HAMi/blob/master/docs/develop/design.md)):

1. **Kubernetes-native control paths**  
   - Accelerators are requested, scheduled, and allocated through standard Kubernetes surfaces: Pod specs, the scheduler extender protocol, the device plugin gRPC API, and admission webhooks.  
   - HAMi avoids introducing proprietary control planes; cluster state remains in the Kubernetes API and node/device plugin reporting paths operators already trust.
2. **No application code changes**  
   - Workloads express sharing and isolation with resource requests, limits, and annotations. No rewrites of training, inference, or HPC code to "speak HAMi."  
   - This keeps adoption friction low for platform teams onboarding existing AI/ML jobs.
3. **Pluggable heterogeneous devices**  
   - Device-specific logic lives in device plugins and in-container control libraries (for example HAMi-core for NVIDIA, analogous components for other vendors).
   - On the scheduling side, HAMi exposes a device API; each device implementation can extend this API to add device-specific scheduling optimizations.  
   - New hardware families integrate by extending these layers rather than forking the whole stack.
4. **Policy-driven scheduling**  
   - Placement uses filter/score semantics on the scheduler extender so administrators can steer binpack, spread, topology awareness, and vendor-specific constraints consistently.  
   - Decisions are recorded on Pods (via annotations), which enables scenario-specific scheduling behavior for different workload classes.
5. **Hard isolation on shared accelerators**  
   - Where the stack supports it, memory and compute caps are enforced inside containers—not only at admission time—so tenants cannot exceed declared slices on shared devices.
6. **Clear separation of responsibilities**  
   - Mutating admission validates and normalizes requests; the extender selects nodes; device plugins allocate and mount; in-container libraries enforce limits.  
   - Each stage has a narrow contract, and intermediate state is written to Pod/Node annotations, which helps operators localize issues quickly.

#### Best practices

Engineering and community practices that keep HAMi maintainable and safe to run in production:

1. **Stay aligned with Kubernetes semantics**  
   - Follow upstream scheduler extender, device plugin, and admission webhook contracts; prefer deprecating non-standard behavior with clear release notes rather than silent divergence. 
2. **Automate verification**  
   - Gate changes with CI (lint, unit tests, chart packaging, and workflows that exercise realistic scheduling and device paths) to catch regressions before merge. 
3. **Test in realistic environments**  
   - Validate features against representative accelerator hardware, drivers, and runtime combinations; document supported matrices and known limitations per vendor. 
4. **Community-driven prioritization**  
   - Use public issues, roadmap discussions, and community meetings so roadmap items reflect operator pain and contributor capacity (see **Roadmap and scope process** under Scope). 
5. **Documentation and operability first**  
   - Ship user-facing docs, examples, and Helm values commentary alongside features so platforms can install, tune, and observe HAMi without reading the implementation. 

#### Architecture requirements

Core components include MutatingWebhook, Scheduler Extender, Device Plugins, and in-container control libraries.  
- Architecture and flow: https://github.com/Project-HAMi/HAMi/blob/master/docs/develop/design.md

#### Environment differences

- PoC/dev/test/prod: the core deployment model is the same (GPU-enabled Kubernetes with HAMi); production environments typically apply stricter node labeling, webhook TLS hardening, scheduler/device policy tuning, and monitoring integration.

#### Service dependencies in cluster

Kubernetes API server, kube-scheduler/Volcano scheduling integration, kubelet device plugin APIs, and (optionally) cert-manager for webhook certificates.

#### IAM model

Uses Kubernetes RBAC for component service accounts and least-required API operations for webhook patch jobs and scheduler operations.

#### Sovereignty/compliance

Data plane is cluster-local; no mandatory external telemetry service. Regional/organizational compliance posture depends on deployment choices.

#### HA requirements

Scheduler supports leader election and configurable replicas.
  - Workload side: since HAMi v2.5, already-running tasks are designed to remain stable and are not expected to fail solely due to cluster-side events such as HAMi upgrades/uninstallations or transient Kubernetes/HAMi control-plane faults.
  - Scheduling side: since HAMi v2.8, multi-replica scheduler deployment with leader election is supported to provide high availability for scheduling decisions.
#### Resource requirements (CPU/memory/network)

Configurable per component via Helm values. The chart leaves `resources` unset by default, so production clusters should set explicit requests/limits. The following estimates are practical planning baselines for HAMi v2.8.0 on Kubernetes 1.20+, with NVIDIA sharing enabled and normal scheduling churn.  
- **Assumptions for estimates:** 1 scheduler replica (`kube-scheduler` + HAMi extender), 1 device-plugin DaemonSet pod per GPU node (`device-plugin` + `vgpu-monitor`), Prometheus scraping every 15-30s, and no unusual pod-creation spikes.  

| Component / scope | CPU (recommended request to typical peak) | Memory (recommended request to typical peak) | Network (control/observability plane) |
|---|---|---|---|
| Scheduler pod (per replica) | 300m-800m | 512Mi-2Gi | 0.2-1 Mbps typical, 2-5 Mbps burst during heavy pod churn |
| Device-plugin pod (per GPU node) | 150m-600m | 256Mi-1Gi | 0.05-0.2 Mbps per node (telemetry and control signaling) |
| Metrics scraping overhead (per target, 15s interval) | N/A | N/A | 50-300 KiB/scrape, about 0.03-0.16 Mbps |
| Cluster total (N GPU nodes) | (0.3 to 0.8) + N * (0.15 to 0.6) cores | (0.5 to 2) + N * (0.25 to 1) GiB | Approximately linear with N based on per-node telemetry plus scheduler control traffic |
| Example footprint (N=20 GPU nodes) | 3.3-12.8 cores | 5.5-22 GiB | 1.2-5 Mbps typical aggregate control/metrics traffic, with higher short bursts during large scheduling events |

#### Storage requirements

HAMi has no mandatory external database. Storage use is mainly image layers, logs/metrics buffers, and hostPath-mounted runtime paths used by scheduler/device-plugin components. The following are practical sizing estimates for production planning.  
- **Assumptions for estimates:** default image retention policy, standard Kubernetes/container runtime logging, and mixed training/inference workloads with normal pod churn.  

| Component / scope | Estimated storage footprint | Notes |
|---|---|---|
| Scheduler pod (per replica) | 0.2-0.8 GiB node-local ephemeral storage | Mostly image layers, logs, and temporary runtime files |
| Device-plugin + monitor pod (per GPU node) | 0.3-1.2 GiB node-local ephemeral storage | Includes plugin/monitor image layers, logs, and runtime temp/cache paths |
| Host runtime paths used by HAMi on GPU nodes | 0.2-1 GiB recommended free space on top of pod ephemeral usage | Covers `/var/lib/kubelet/device-plugins`, `/usr/local/vgpu`, `/var/run/cdi`, and transient files under `/tmp` |
| Cluster total (N GPU nodes, 1 scheduler replica) | approximately `(0.2 to 0.8) + N * (0.5 to 2.2)` GiB | Combines scheduler ephemeral + per-GPU-node pod + host runtime reserve |
| Example footprint (N=20 GPU nodes) | approximately 10.2-44.8 GiB cluster-wide reserved capacity | Planning envelope for stable operations and upgrade headroom |

#### API design

- Uses Kubernetes native APIs (Pods, Nodes, annotations, admission webhooks, device plugin gRPC).  
- Defaults and optional configurations are exposed in Helm chart values and config docs.  
- No custom CRD is required for core workflows.
- Device-slice resource names are exposed as Kubernetes resources, so users can request and limit slices directly in `resources.limits` / `resources.requests`.
- GPU scheduling policies and advanced behaviors are expressed through Pod annotations.
- HAMi runtime behavior can be configured through scheduler and device-plugin ConfigMaps, with Helm values as the primary entry point.

#### Release process

Semantic versioning, tagged releases, release branches, automated image/chart/release-note workflows, with documented manual verification steps.  
- Release process doc: https://github.com/Project-HAMi/HAMi/blob/master/docs/release-process.md

### Installation

- **Installation and validation:** Helm-based install with node labeling and chart value customization.  
  - Quick start:https://project-hami.io/docs/installation/online-installation
  - Example deployment commands:
```bash
# 1) Label GPU nodes for HAMi management
kubectl label nodes <gpu-node-name> gpu=on

# 2) Add HAMi Helm repository
helm repo add hami-charts https://project-hami.github.io/HAMi/
helm repo update

# 3) Install HAMi into kube-system
helm install hami hami-charts/hami -n kube-system

# 4) (Optional) Customize values during install
# helm install hami hami-charts/hami -n kube-system -f values.yaml

# 5) Verify core components are running
kubectl get pods -n kube-system | grep -E "hami-scheduler|hami-device-plugin"
```

### Security

See separate [document](https://github.com/cncf/toc/blob/main/projects/hami/security-assesment/self-assessment.md)

---

## Day 1 - Installation and Deployment Phase

### Project Installation and Configuration

- Installation uses the Helm chart, including configuration for scheduler policy, resource naming, webhook selectors, TLS strategy, and device-plugin behavior.  
  - Installation guide: https://project-hami.io/docs/installation/online-installation
- Operational behavior is configurable through Helm values and ConfigMaps (for example scheduler and device policy config maps).  
  - Configuration guide: https://project-hami.io/docs/userguide/configure

### Project Enablement and Rollback

- **Enable/disable in live cluster:** Deploy/upgrade/uninstall via Helm. Enabling does not require control-plane downtime; workloads may experience scheduling behavior changes when webhook/scheduler integration changes.
- **Behavior changes when enabled:**  
  - Pod mutation for recognized device requests.  
  - Scheduling for device-sharing workloads routed through HAMi logic and policy.  
  - Device plugin allocation/mount/env handling for runtime enforcement.
- **Testing enable/disable:** Deployment and uninstall paths are exercised through e2e workflows; PRs are expected to pass required e2e checks before merge.
- **Resource cleanup:** Helm uninstall removes chart-managed resources; no core CRDs are introduced by default path.

### Rollout, Upgrade and Rollback Planning

- **Compatibility cadence:** Kubernetes and related dependencies are tracked through CI and automated dependency updates (including Dependabot). Version evolution is governed by the documented release process and release workflows.
- **Rollback procedures:** Use `helm rollback` for in-place rollback, or uninstall and redeploy with a previous chart version when a full re-provisioning path is preferred.
- **How rollout/rollback can fail:**  
  - Rolling back images without rolling back the matching chart/configuration revision.  
  - Failing to roll back customized values and policy ConfigMaps together with workloads/images.  
  - Performing multi-version jump rollbacks (for example two or more minor versions) without staged validation.
- **Impact to running workloads:** Since HAMi v2.5, already-running pods are generally expected to remain unaffected by control-plane rollout actions; new scheduling/allocation decisions may still change during upgrades/rollbacks.
- **Rollback indicators:** Primary indicators include admission/scheduling errors, abnormal pending-pod growth, device plugin registration issues, and policy mismatch symptoms. Operationally, the project recommends staying on recent supported releases whenever possible.
- **Upgrade path testing:** Unit/e2e coverage exists; explicit long-chain upgrade->downgrade->upgrade matrices are still evolving and should be expanded for incubation.
- **Deprecation communication:** Deprecations are communicated in documentation and community meetings, with explicit in-doc annotations. A typical transition window keeps old and new APIs available for one subsequent release, followed by old API removal after the next release cycle.
- **Alpha/beta capabilities:** Exposed by configuration flags and values; users opt in through chart values and documented settings.

---

