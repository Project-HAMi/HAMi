# HAMi Documentation Audit Report
For: v2.9.0 Documentation Revamp  
Date: 2026-04-15  
Author: Documentation Revamp Working Group  
Scope: All English `.md` files under `/docs` (Chinese `_cn.md` files excluded, tracked separately as translation sync)

## Summary

This audit covers all 30 English documentation files in `/docs`. 
The goal is to give the reader a clear picture of what we have, what is broken, what is missing, and what to do next, before any content is changed.

Three immediate problems require maintainer attention:

1. `benchmark.md` is misleading users. It references hardware and Kubernetes versions from 2018. New users read this and form incorrect expectations about HAMi's performance profile. Decision needed: rewrite with current hardware or archive.

2. `develop/design.md` has no version stamp and may not reflect the v2.8+ architecture. Contributors reading this for onboarding will get an inaccurate picture of how components interact. A maintainer familiar with the current architecture needs to review this before anyone cites it.

3. The overall doc structure has no information architecture. 21 device support files sit flat next to operator guides, developer internals, and CNCF audit artifacts, with no separation. A new contributor or user has no way to navigate this without already knowing what they are looking for. This is the root cause of onboarding friction.

Four high-priority docs are missing entirely: Quick Start (standalone), Architecture (standalone), FAQ, and Concepts. These are the first things any new user or contributor needs.

What maintainers need to decide:
- Archive or rewrite `benchmark.md`?
- Who reviews `develop/design.md` for accuracy?
- Is the proposed new directory structure (Section 3) acceptable as the target for v2.9.0?

What contributors can start on immediately (no maintainer review needed first):
- Flag `develop/tasklist.md` for removal (stale, duplicates `roadmap.md`)
- Add deprecation notices to `benchmark.md`
- Draft `docs/guides/quick-start.md` and `docs/guides/troubleshooting/faq.md`

## 1. File Inventory & Assessment

| File                                         | Topic                                                                      | Audience      | Freshness         | README Overlap   | Recommendation               |
|----------------------------------------------|----------------------------------------------------------------------------|---------------|-------------------|------------------|------------------------------|
| `ascend910b-support.md`                      | Huawei Ascend NPU sharing (910A/B2/B3/B4, 310P), templates, granularity    | Operator/User | Fresh             | Partial          | Keep                         |
| `benchmark.md`                               | Performance comparison: nvidia-dp vs vGPU-dp on Tesla V100, K8s v1.12      | User          | Outdated          | Partial          | Update or Archive            |
| `cambricon-mlu-support.md`                   | Cambricon MLU (370, 590) sharing, dynamic SMLU config                      | Operator/User | Fresh             | Partial          | Keep                         |
| `config.md`                                  | Global config (ConfigMap, node configs, Helm params, pod annotations)      | Operator      | Fresh             | Yes              | Keep (critical reference)    |
| `dashboard.md`                               | Monitoring setup: kube-prometheus, dcgm-exporter, Grafana                  | Operator      | Fresh             | Partial          | Keep                         |
| `dynamic-mig-support.md`                     | Dynamic MIG for Ampere/Hopper/Blackwell, prerequisites, known issues       | Operator/User | Fresh             | Partial          | Keep                         |
| `enflame-gcu-support.md`                     | Enflame GCU (S60) whole-card scheduling                                    | Operator/User | Fresh             | Partial          | Keep                         |
| `enflame-vgcu-support.md`                    | Enflame GCU sharing, memory/core control, UUID selection                   | Operator/User | Fresh             | Partial          | Keep                         |
| `general-technical-review.md`                | CNCF review for v2.8.0: usability, security, design                        | Operator/Dev  | Fresh             | Yes              | Keep (audit artifact)        |
| `how-to-profiling-scheduler.md`              | pprof-based CPU/memory profiling for the scheduler                         | Developer     | Fresh             | No               | Keep                         |
| `how-to-use-volcano-ascend.md`               | Volcano + Ascend vNPU integration guide                                    | Operator      | Fresh             | Partial          | Keep                         |
| `how-to-use-volcano-vgpu.md`                 | Volcano + vGPU integration guide                                           | Operator      | Fresh             | Partial          | Keep                         |
| `hygon-dcu-support.md`                       | Hygon DCU (Z100, Z100L) sharing, memory/core control                       | Operator/User | Fresh             | Partial          | Keep                         |
| `iluvatar-gpu-support.md`                    | iluvatar GPU sharing, device granularity, UUID selection                   | Operator/User | Fresh             | Partial          | Keep                         |
| `kunlun-vxpu-support.md`                     | Kunlunxin XPU (P800-OAM) sharing, 1/4 and 1/2 card modes                   | Operator/User | Fresh             | Partial          | Keep                         |
| `metax-support.md`                           | MetaX GPU sharing, topology-aware scheduling (MetaXLink/PCIe)              | Operator/User | Fresh             | Partial          | Keep                         |
| `mthreads-support.md`                        | Moore Threads GPU (MTT S4000) sharing, exclusive allocation                | Operator/User | Fresh             | Partial          | Keep                         |
| `offline-install.md`                         | Air-gapped cluster installation (image pull/load/push, Helm)               | Operator      | Fresh             | Partial          | Keep                         |
| `release-process.md`                         | Release management: versioning, branch strategy, GitHub Actions            | Maintainer    | Fresh             | No               | Keep                         |
| `scheduler-event-log.md`                     | Scheduler event logging proposal: structured error codes, v4/v5 log levels | Developer     | Fresh             | No               | Keep (proposal)              |
| `vastai-support.md`                          | Vastaitech device (full-card, die-mode), topology awareness                | Operator/User | Fresh             | Partial          | Keep                         |
| `develop/design.md`                          | Architecture overview: webhook, scheduler, device-plugin flow              | Developer     | Possibly outdated | Partial          | Update                       |
| `develop/dynamic-mig.md`                     | Dynamic MIG slice plugin design, binpack/spread, ConfigMap                 | Developer     | Fresh             | Partial          | Keep                         |
| `develop/protocol.md`                        | Device registration & scheduling protocol, node annotations                | Developer     | Fresh             | No               | Keep                         |
| `develop/roadmap.md`                         | Device support matrix, feature roadmap per accelerator                     | Developer     | Fresh             | No               | Keep                         |
| `develop/scheduler-policy.md`                | Scheduler policy design (binpack/spread), scoring formulas                 | Operator/Dev  | Fresh             | Partial          | Keep                         |
| `develop/tasklist.md`                        | Task list: supported devices, resource types, scheduling features          | Developer     | Possibly outdated | Yes (partial)    | Merge into roadmap or Remove |
| `proposals/e2e_test.md`                      | E2E testing proposal using Ginkgo, test scope, environment                 | Developer     | Fresh             | No               | Keep                         |
| `proposals/gpu-topo-policy.md`               | GPU topology scheduling strategy, scoring logic                            | Developer     | Fresh             | Partial          | Keep                         |
| `proposals/nvidia-gpu-topology-scheduler.md` | NVIDIA topology scheduler guide: enable methods, log examples              | Operator/User | Fresh             | Partial          | Keep                         |
| `mind-map/readme`                            | Attribution and language notes for mind map files                          | Community     | Fresh             | No               | Keep                         |

## 2. Critical Issues

### 2.1 Outdated Content

#### `benchmark.md` — Archive or Rewrite
What it says: Compares nvidia-device-plugin vs. vGPU-device-plugin performance on a Tesla V100, running Kubernetes v1.12.9 and Docker 18.09.  
Why this matters: This is the only performance reference in the entire docs. Users evaluating HAMi will read it and assume the numbers reflect today's reality. They don't. V100 is a 2017 GPU. A100 and H100 are the current baseline. K8s v1.12 is EOL. If a user benchmarks HAMi today and gets different results, they will distrust the project.  
Decision needed (maintainer): Rewrite with current hardware, or add a prominent deprecation notice and open an issue for a replacement.

#### `develop/design.md` — Review Against Current Codebase
What it says: Architecture overview covering MutatingWebhook, Scheduler, and DevicePlugin interaction.  
Why this matters: This is the primary onboarding document for new contributors. It has no version stamp, no date, and no mention of HA support (added in v2.8) or DRA direction. A contributor relying on this to understand how HAMi works will have an incomplete and possibly incorrect mental model.  
Decision needed (maintainer): A maintainer familiar with v2.8+ internals should do a pass and either validate it or flag what is wrong.

#### `develop/tasklist.md` — Remove
What it says: A flat list of supported devices and resource types, tracking which features are implemented.  
Why this matters: This is internal task tracking that duplicates `develop/roadmap.md` and belongs in GitHub Issues, not in public docs. It adds noise and may show incomplete items as if they are open questions rather than shipped features.  
Action (contributor-safe): Content that is not already in `roadmap.md` should be migrated as GitHub Issues, then this file removed.

### 2.2 README ↔ Docs Duplication
The README is 192 lines and currently tries to be both an entry point and a complete manual. This creates a maintenance burden: any change to installation, config, or architecture must be reflected in two places. More importantly, users who follow the README installation steps and hit problems have nowhere deeper to go.

Target model: README = entry point (problem → solution → 3 commands → links). Docs = where the depth lives.

| README Section            | Where depth already exists   | Fix                                             |
|---------------------------|------------------------------|-------------------------------------------------|
| "Quick Start / Install"   | Partially in `config.md`     | README: 3-step install → link to quick-start.md |
| "Supported devices" list  | All device support docs      | README: summary table + links only              |
| "Architecture"            | `develop/design.md`          | README: one diagram → link to architecture.md   |
| "Monitor" section         | `dashboard.md`               | README: one-liner + link                        |
| "Notes"                   | Nowhere (embedded caveats)   | Move to FAQ                                     |

### 2.3 Missing Documentation (Gaps)
These topics are either absent entirely, or only referenced inline in the README with no dedicated doc. Each gap is a point where a user or contributor hits a dead end.

| Gap                                                             | Why it blocks users                                                                                                                                             | Priority  | Workstream       |
|-----------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------|------------------|
| Quick Start (standalone)                                        | README install steps have no expected-output verification. User cannot tell if their install succeeded.                                                         | High      | WS7              |
| Architecture doc (standalone)                                   | New contributors have no accurate reference for how components interact. `develop/design.md` exists but is unverified and buried.                               | High      | WS4              |
| Concepts doc - vGPU virtualization, control plane vs data plane | Users conflate HAMi's resource model with NVIDIA's MIG model. No doc explains the difference.                                                                   | High      | WS1 + WS8 Blog 1 |
| FAQ                                                             | GitHub Issues and Slack/Discord carry recurring questions (scheduling not working, label not applied, etc.) that have no canonical answer. Zero FAQ docs exist. | High      | WS6              |
| NVIDIA GPU setup guide                                          | README links to `#preparing-your-gpu-nodes` - an anchor that resolves to the Prerequisites section. There is no dedicated NVIDIA setup guide.                   | Medium    | WS1              |
| Troubleshooting guide                                           | No structured path for users to self-diagnose.                                                                                                                  | Medium    | WS1              |
| DRA doc                                                         | Mentioned in roadmap as a direction, zero docs. Early adopters have no reference.                                                                               | Medium    | WS1              |
| vLLM integration guide                                          | Flagship AI workload integration use case, no guide exists.                                                                                                     | Medium    | WS8 Blog 2       |

## 3. Structure Analysis

### Current Structure (flat / unorganized)
```
docs/
├── [21 flat .md files]        ← no hierarchy
├── [21 mirrored _cn.md files] ← inline with English, not in a separate tree
├── develop/                   ← internal dev docs mixed with stale task lists
├── proposals/                 ← internal KEP-style docs, not labeled as such
├── mind-map/                  ← assets with no navigation context
└── CHANGELOG/                 ← changelog only
```

### Why the current structure fails

A user landing on `/docs` for the first time sees 42 files in a flat list. There is no signal for which file they should read first, which are user-facing vs. developer-internal, or which are current vs. historical. The result: users skip docs entirely and go to GitHub Issues or Slack, increasing maintainer load.

Specific problems:
- No reader-role separation. `config.md` (operator reference) sits next to `develop/protocol.md` (contributor internals) with no distinction.
- No content-type separation. Concepts, how-to guides, reference material, and design proposals are all in the same flat list.
- Chinese translations inline. `cambricon-mlu-support.md` and `cambricon-mlu-support_cn.md` are adjacent. This makes it hard to see the English structure and will create path conflicts if/when docs move to a website.
- `proposals/` has no framing. Users landing on `proposals/nvidia-gpu-topology-scheduler.md` from a search engine do not know if this is a current guide or a historical proposal.

### Proposed New Structure (for WS1)
Goal: A user or contributor should be able to answer "where do I start?" by looking at the top-level folder names alone.

> Note for maintainers: This structure is a proposal, not a commitment. The priority for v2.9.0 is creating the missing high-priority docs (Quick Start, Architecture, FAQ) and moving them into place. A full migration of all existing files can happen incrementally across patch releases.
```
docs/
├── concepts/
│   ├── what-is-hami.md
│   ├── architecture.md         ← replace/update develop/design.md
│   ├── device-virtualization.md
│   └── scheduling.md           ← from scheduler-policy.md
├── guides/
│   ├── quick-start.md          ← new standalone doc
│   ├── installation/
│   │   ├── online.md
│   │   └── offline.md          ← from offline-install.md
│   ├── devices/
│   │   ├── nvidia-gpu.md
│   │   ├── ascend-npu.md       ← from ascend910b-support.md
│   │   ├── dynamic-mig.md      ← from dynamic-mig-support.md
│   │   ├── cambricon-mlu.md
│   │   ├── hygon-dcu.md
│   │   ├── iluvatar-gpu.md
│   │   ├── moore-threads.md
│   │   ├── metax-gpu.md
│   │   ├── enflame-gcu.md
│   │   ├── kunlun-xpu.md
│   │   └── vastai.md
│   ├── integrations/
│   │   ├── volcano-vgpu.md     ← from how-to-use-volcano-vgpu.md
│   │   ├── volcano-ascend.md   ← from how-to-use-volcano-ascend.md
│   │   └── vllm.md             ← new (Blog 2 → guide)
│   ├── monitoring/
│   │   └── dashboard.md        ← from dashboard.md
│   └── troubleshooting/
│       ├── faq.md              ← new
│       └── scheduler-events.md ← from scheduler-event-log.md
├── reference/
│   └── config.md               ← from config.md
├── contributing/
│   ├── development.md          ← from develop/protocol.md, develop/dynamic-mig.md
│   ├── release-process.md      ← from release-process.md
│   ├── roadmap.md              ← from develop/roadmap.md
│   └── profiling.md            ← from how-to-profiling-scheduler.md
└── [proposals/ → GitHub Issues or separate internal wiki]
    Note: proposals/ files are marked "Keep" in the inventory table (Section 1),
    meaning their content should be preserved. The recommendation here is to
    relocate them out of the user-facing docs tree — either as GitHub Issues
    (for actionable items) or an internal wiki (for historical design records).
```

## 4. Recommended Action Plan (Phase 1 Sequence)

Actions are labeled by who should own them:

- [Maintainer] - requires architectural knowledge or merge authority
- [Contributor] - safe for any contributor to pick up, no deep context needed
- [good-first-issue] - low risk, isolated, well-defined

### Step 1 > Immediate cleanup (no content changes, low risk)

| Action                                                                            | Owner       | Label                             |
|-----------------------------------------------------------------------------------|-------------|-----------------------------------|
| Add a deprecation notice to the top of `benchmark.md` pointing to a future update | Contributor | `docs/cleanup` `good-first-issue` |
| Open a GitHub Issue for rewriting `benchmark.md` with A100/H100 data              | Maintainer  | `docs/cleanup` `help-wanted`      |
| Move `develop/tasklist.md` content to GitHub Issues, then delete the file         | Contributor | `docs/cleanup`                    |

### Step 2 > README first pass (WS2)

> Maintainer sign-off needed before merging. README is the project's front door.

| Action                                                                          | Owner       | Label                        |
|---------------------------------------------------------------------------------|-------------|------------------------------|
| Rewrite README intro: problem → solution narrative (no feature list)            | Contributor | `docs/readme`                |
| Collapse "Quick Start" to 3 commands + link to future `quick-start.md`          | Contributor | `docs/readme`                |
| Collapse "Supported devices" to summary table + links to device docs            | Contributor | `docs/readme`                |
| Replace "Architect" section: update diagram, link to `concepts/architecture.md` | Maintainer  | `docs/readme` `docs/diagram` |
| Move "Notes" section content to future FAQ                                      | Contributor | `docs/readme`                |

### Step 3 > Create missing high-priority docs (WS1 foundation)

> These are new files. Contributors can draft. Maintainer validates before merge.

| Action                                                                                       | Owner       | Label                                |
|----------------------------------------------------------------------------------------------|-------------|--------------------------------------|
| Write `docs/guides/quick-start.md` - deploy + run + verify, target ≤10 min                   | Contributor | `docs/quickstart` `good-first-issue` |
| Write `docs/guides/troubleshooting/faq.md` - seed from GitHub Issues + Slack                 | Contributor | `docs/faq` `good-first-issue`        |
| Write `docs/concepts/architecture.md` - based on `develop/design.md`, verified against v2.8+ | Maintainer  | `docs/diagram`                       |

### Step 4 > Reorganize existing docs (WS1 migration)

> Do this after Steps 1–3 are merged, so new files land in the right place from the start.

| Action                                               | Owner       | Label            |
|------------------------------------------------------|-------------|------------------|
| Move files into the new IA structure (see Section 3) | Contributor | `docs/migration` |
| Update all internal cross-links                      | Contributor | `docs/migration` |
| Update README links to match new paths               | Contributor | `docs/migration` |

## 5. Chinese Translation Status

All 21 English docs have a `_cn.md` counterpart. These are out of scope for structural changes, but should be flagged:
- Translations should move into a mirrored folder structure (e.g., `i18n/zh/`) when restructuring happens
- A sync check between English and Chinese versions is needed separately

## 6. Diagrams Inventory (WS4 Pre-work)

| Asset                  | Location                          | Format                           | Issue                         |
|------------------------|-----------------------------------|----------------------------------|-------------------------------|
| Main architecture      | `imgs/hami-arch.png`              | PNG (editable: `hami-arch.pptx`) | No SVG; PPTX source exists    |
| Device sharing example | `imgs/example.png`                | PNG                              | No editable source            |
| Hard limit demo        | `imgs/hard_limit.jpg`             | JPG                              | Low quality format, no source |
| Release process        | `imgs/release-process.png`        | PNG                              | No editable source            |
| Mind maps              | `docs/mind-map/*.png` + `*.xmind` | PNG + XMind                      | Editable source exists        |
| Benchmark charts       | `imgs/benchmark*.png`             | PNG                              | Tied to outdated benchmark.md |
| MetaX topology         | `imgs/metax_*.png`                | PNG                              | No editable source            |

All diagrams should be redrawn as SVG with editable sources (draw.io preferred).
