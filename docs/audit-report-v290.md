# HAMi v2.9.0 Documentation Status

Last updated: 2026-05-07
Scope: All English .md files under /docs (upstream master)

## Summary

Three structural problems remain unresolved as of v2.9.0.

First, develop/design.md has no version stamp and predates HA support in v2.8. It is the primary reference for new contributors building a mental model of HAMi internals. If it is inaccurate, they start wrong.

Second, the /docs root has 40+ files with no grouping by audience. Users, operators, and developers land in the same flat list.

Third, four high-priority docs are missing: a standalone Quick Start, an Architecture page, a FAQ, and a Concepts overview (vGPU vs MIG, scheduling model).

Resolved since initial audit (2026-04-15):
- benchmark.md updated with A100/vLLM data, no longer outdated
- develop/tasklist.md removed
- New files added: ascend910-hami-vnpu-core-support.md, resource-quota.md, scheduling-policy.md

Open questions for maintainers:
1. Is develop/design.md still accurate against the v2.8/v2.9 codebase?
2. Is the directory structure in Section 3 acceptable as a v2.9.0 target?


## 1. File Inventory

| File | Audience | Freshness | Recommendation |
|------|----------|-----------|----------------|
| ascend910b-support.md | Operator/User | Fresh | Keep |
| ascend910-hami-vnpu-core-support.md | Operator/User | Fresh | Keep |
| benchmark.md | User | Fresh (updated) | Keep |
| cambricon-mlu-support.md | Operator/User | Fresh | Keep |
| config.md | Operator | Fresh | Keep |
| dashboard.md | Operator | Fresh | Keep |
| dynamic-mig-support.md | Operator/User | Fresh | Keep |
| enflame-gcu-support.md | Operator/User | Fresh | Keep |
| enflame-vgcu-support.md | Operator/User | Fresh | Keep |
| general-technical-review.md | Operator/Dev | Fresh | Keep |
| how-to-profiling-scheduler.md | Developer | Fresh | Keep |
| how-to-use-volcano-ascend.md | Operator | Fresh | Keep |
| how-to-use-volcano-vgpu.md | Operator | Fresh | Keep |
| hygon-dcu-support.md | Operator/User | Fresh | Keep |
| iluvatar-gpu-support.md | Operator/User | Fresh | Keep |
| kunlun-vxpu-support.md | Operator/User | Fresh | Keep |
| metax-support.md | Operator/User | Fresh | Keep |
| mthreads-support.md | Operator/User | Fresh | Keep |
| offline-install.md | Operator | Fresh | Keep |
| release-process.md | Maintainer | Fresh | Keep |
| resource-quota.md | Operator | Fresh | Keep |
| scheduler-event-log.md | Developer | Fresh | Keep |
| scheduling-policy.md | Operator/Dev | Fresh | Keep |
| vastai-support.md | Operator/User | Fresh | Keep |
| develop/design.md | Developer | Unverified | Validate against v2.8/v2.9 |
| develop/dynamic-mig.md | Developer | Fresh | Keep |
| develop/protocol.md | Developer | Fresh | Keep |
| develop/roadmap.md | Developer | Fresh | Keep |
| develop/scheduler-policy.md | Operator/Dev | Fresh | Keep |
| proposals/e2e_test.md | Developer | Fresh | Keep |
| proposals/gpu-topo-policy.md | Developer | Fresh | Keep |
| proposals/nvidia-gpu-topology-scheduler.md | Operator/User | Fresh | Keep |
| mind-map/ | Community | Fresh | Keep |


## 2. Missing Docs

| Gap | Priority | Notes |
|-----|----------|-------|
| Quick Start, standalone | High | PR 1718 was redirected to website repo. /docs still has no equivalent |
| Architecture doc | High | design.md covers internals but no user-facing architecture page exists |
| Concepts: vGPU vs MIG | High | No doc explains the core scheduling model to a new user |
| FAQ | High | Common questions are scattered across Issues and Slack |
| Troubleshooting guide | Medium | |
| DRA integration doc | Medium | DRA support merged but undocumented |
| vLLM integration guide | Medium | Referenced in benchmark.md, no standalone guide |


## 3. Proposed Structure

The current flat layout mixes user guides, developer internals, operator references, and historical proposals with no separation. The structure below groups files by audience. Moving existing files can happen incrementally after the missing docs are written.

```
docs/
    concepts/
        architecture.md
        device-virtualization.md
        scheduling.md
    guides/
        quick-start.md
        installation/
            online.md
            offline.md
        devices/
            (existing per-vendor files)
        integrations/
            volcano-vgpu.md
            volcano-ascend.md
            vllm.md
        monitoring/
            dashboard.md
        troubleshooting/
            faq.md
            scheduler-events.md
    reference/
        config.md
        resource-quota.md
        scheduling-policy.md
    contributing/
        development.md
        release-process.md
        roadmap.md
        profiling.md
```

Chinese translations would mirror the same structure under i18n/zh/. The proposals/ directory stays as-is.


## 4. Action Plan

### Immediate

| Action | Label |
|--------|-------|
| Validate develop/design.md against v2.8/v2.9 internals | docs/accuracy, needs-maintainer |
| Write docs/guides/quick-start.md | docs/quickstart, good-first-issue |
| Write docs/guides/troubleshooting/faq.md | docs/faq, good-first-issue |
| Write docs/concepts/architecture.md | docs/diagram, needs-maintainer |

### After missing docs are merged

| Action | Label |
|--------|-------|
| Reorganize files into the proposed structure | docs/migration |
| Update all internal cross-links | docs/migration |
| Update README to use new paths | docs/migration |


## 5. Diagrams

All diagram assets currently exist as .png or .jpg with no editable source, except mind-maps which have .xmind sources. This makes updates expensive.

| Asset | Problem |
|-------|---------|
| imgs/hami-arch.png (source: .pptx) | No SVG. Source not in repo |
| imgs/example.png | No editable source |
| imgs/hard_limit.jpg | Low quality format, no source |
| imgs/release-process.png | No editable source |
| imgs/metax_*.png | No editable source |

Target: redraw as SVG using draw.io, which renders in GitHub without extra tooling. Existing .xmind mind-map sources can serve as the template for how to track source files.
