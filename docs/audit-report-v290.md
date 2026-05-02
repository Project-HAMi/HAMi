# HAMi v2.9.0 Documentation Audit Report

Date: 2026-04-15
Scope: All English .md files under /docs (30 files, _cn.md translations tracked separately)

## Summary

This audit covers all 30 English docs in /docs. The goal is to get a clear picture of what we have, what is broken, and what is missing before anything gets changed.

Three things need a maintainer decision before any work starts.

First, benchmark.md references a Tesla V100 and Kubernetes v1.12, both from 2018. This is the only performance document we have. Users evaluating HAMi read it and assume the numbers are current. They are not. Someone needs to decide whether to rewrite it with A100/H100 data or archive it.

Second, develop/design.md has no version stamp and was likely written before HA support landed in v2.8. New contributors use this to understand how HAMi works internally. If it is inaccurate, they start with a wrong mental model. A maintainer familiar with the v2.8 internals should do a quick pass and confirm whether it is still correct.

Third, the doc structure is flat. There are 42 files at the top level with no separation between user guides, operator references, developer internals, and historical proposals. A new contributor or user has no way to figure out where to start. This is the main reason onboarding takes so long.

Four high-priority docs do not exist yet: a standalone Quick Start, an Architecture page, a FAQ, and a Concepts overview.

Decisions needed from maintainers:
1. Archive or rewrite benchmark.md?
2. Who reviews develop/design.md against the v2.8 codebase?
3. Is the proposed directory structure in Section 3 acceptable as the v2.9.0 target?

Things contributors can start on now without waiting for a maintainer:
1. Add a deprecation notice to benchmark.md
2. Remove develop/tasklist.md, which duplicates roadmap.md
3. Draft docs/guides/quick-start.md and docs/guides/troubleshooting/faq.md


## 1. File Inventory

| File | Audience | Freshness | Recommendation |
|------|----------|-----------|----------------|
| ascend910b-support.md | Operator/User | Fresh | Keep |
| benchmark.md | User | Outdated | Update or archive |
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
| scheduler-event-log.md | Developer | Fresh | Keep |
| vastai-support.md | Operator/User | Fresh | Keep |
| develop/design.md | Developer | Possibly outdated | Validate against v2.8 |
| develop/dynamic-mig.md | Developer | Fresh | Keep |
| develop/protocol.md | Developer | Fresh | Keep |
| develop/roadmap.md | Developer | Fresh | Keep |
| develop/scheduler-policy.md | Operator/Dev | Fresh | Keep |
| develop/tasklist.md | Developer | Possibly outdated | Remove, duplicates roadmap |
| proposals/e2e_test.md | Developer | Fresh | Keep |
| proposals/gpu-topo-policy.md | Developer | Fresh | Keep |
| proposals/nvidia-gpu-topology-scheduler.md | Operator/User | Fresh | Keep |
| mind-map/readme | Community | Fresh | Keep |


## 2. Issues

### README and Docs Overlap

The README is currently trying to be both an entry point and a full manual. That creates a maintenance problem: any change to installation steps or architecture has to be updated in two places. The cleaner model is README as entry point (short intro, three commands, links to docs) and /docs as the place where depth lives.

| README section | Where the detail already lives | What to do |
|----------------|-------------------------------|------------|
| Quick Start / Install | Partially in config.md | Shorten to 3 commands, link to quick-start.md |
| Supported devices list | All device support docs | Replace with a summary table and links |
| Architecture | develop/design.md | Keep one diagram, link to architecture.md |
| Monitor section | dashboard.md | One sentence and a link |
| Notes | Nowhere | Move content to FAQ |


### Missing Docs

| Gap | Priority | Workstream |
|-----|----------|------------|
| Quick Start, standalone | High | WS7 |
| Architecture doc, standalone | High | WS4 |
| Concepts: vGPU vs MIG model | High | WS1 and WS8 |
| FAQ | High | WS6 |
| NVIDIA GPU setup guide | Medium | WS1 |
| Troubleshooting guide | Medium | WS1 |
| DRA doc | Medium | WS1 |
| vLLM integration guide | Medium | WS8 |


## 3. Proposed Structure

Right now there are 42 files at the top level with no grouping. Chinese translations sit inline next to English files. The proposals directory has no framing, so users landing there from a search engine cannot tell if they are reading a current guide or an old design proposal.

The structure below groups files by who reads them and why. This is a proposal for maintainer review, not a commitment. The priority for v2.9.0 is getting the four missing high-priority docs written and placed correctly. Moving existing files can happen incrementally.

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
            nvidia-gpu.md
            ascend-npu.md
            dynamic-mig.md
            cambricon-mlu.md
            hygon-dcu.md
            iluvatar-gpu.md
            moore-threads.md
            metax-gpu.md
            enflame-gcu.md
            kunlun-xpu.md
            vastai.md
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
    contributing/
        development.md
        release-process.md
        roadmap.md
        profiling.md
```

The proposals directory would move out of the user-facing tree. Actionable items go to GitHub Issues. Historical design records go to an internal wiki. Chinese translations would move to an i18n/zh mirror of the same structure.


## 4. Action Plan

### Step 1: Immediate cleanup, no content changes needed

| Action | Owner | Label |
|--------|-------|-------|
| Add a deprecation notice to benchmark.md | Contributor | docs/cleanup, good-first-issue |
| Open an issue to rewrite benchmark.md with A100/H100 data | Maintainer | docs/cleanup, help-wanted |
| Move any unique content from develop/tasklist.md to GitHub Issues, then delete the file | Contributor | docs/cleanup |


### Step 2: README rewrite (maintainer sign-off needed before merge)

| Action | Owner | Label |
|--------|-------|-------|
| Rewrite the intro as a problem and solution narrative | Contributor | docs/readme |
| Shorten Quick Start to three commands and a link | Contributor | docs/readme |
| Replace the supported devices section with a summary table and links | Contributor | docs/readme |
| Update the architecture section with a current diagram and link | Maintainer | docs/readme |
| Move the Notes section content to the future FAQ | Contributor | docs/readme |


### Step 3: Write the four missing high-priority docs

| Action | Owner | Label |
|--------|-------|-------|
| Write docs/guides/quick-start.md, deploy and verify in under 10 minutes | Contributor | docs/quickstart, good-first-issue |
| Write docs/guides/troubleshooting/faq.md, seed from GitHub Issues and Slack | Contributor | docs/faq, good-first-issue |
| Write docs/concepts/architecture.md, based on design.md and validated against v2.8 | Maintainer | docs/diagram |


### Step 4: Reorganize existing files (do this after Step 3 is merged)

| Action | Owner | Label |
|--------|-------|-------|
| Move files into the new structure | Contributor | docs/migration |
| Update all internal cross-links | Contributor | docs/migration |
| Update README links to match new paths | Contributor | docs/migration |


## 5. Diagrams

| Asset | Location | Problem |
|-------|----------|---------|
| Main architecture | imgs/hami-arch.png (source: hami-arch.pptx) | No SVG version |
| Device sharing example | imgs/example.png | No editable source |
| Hard limit demo | imgs/hard_limit.jpg | Low quality format, no source file |
| Release process | imgs/release-process.png | No editable source |
| Mind maps | docs/mind-map/*.png and *.xmind | Editable source exists |
| Benchmark charts | imgs/benchmark*.png | Tied to outdated benchmark.md |
| MetaX topology | imgs/metax_*.png | No editable source |

All of these should eventually be redrawn as SVG with editable source files. draw.io is the preferred format since it works in GitHub without extra tooling.
