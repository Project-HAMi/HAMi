# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

HAMi (Heterogeneous AI Computing Virtualization Middleware) is a CNCF incubating project that provides device virtualization for heterogeneous AI accelerators (GPU, NPU, HCU, etc.) in Kubernetes. It enables device sharing, memory/core isolation, and topology-aware scheduling across multiple hardware vendors.

## Build Commands

```bash
make build              # Build all binaries (scheduler, vGPUmonitor, nvidia-device-plugin) to bin/
make docker             # Build Docker image
make tidy               # Run go mod tidy
make test               # Run unit tests with coverage (output in _output/coverage/)
make lint               # Run golangci-lint (version pinned in hack/verify-staticcheck.sh)
make verify             # Run all verification checks (lint, license, import aliases, RBAC permissions)
make bench              # Run the benchmarks (CI only checks that they still execute)
make verify_chart       # Lint and render the chart, run Trivy, check chart and app versions
make update-helm-chart-docs   # Regenerate the values table in charts/hami/README.md (needs helm-docs)
make verify-helm-chart-docs   # Check that generated table is current
make local-deploy       # Build the image and deploy it to a local minikube/kind cluster
```

### Running a single test

```bash
go test ./pkg/scheduler/... -run TestSpecificFunc -short --race -count=1
go test ./pkg/device/nvidia/... -run TestName -short --race -count=1
```

### Binary targets (from version.mk)

- `scheduler` — the scheduler extender
- `vGPUmonitor` — in-container GPU monitoring daemon
- `nvidia` — builds as `nvidia-device-plugin` (device plugin for NVIDIA GPUs)

## Architecture

### Three Main Components

1. **Scheduler Extender** (`cmd/scheduler/`) — HTTP server that extends kube-scheduler with device-aware filter/score/bind logic. Entry point calls `config.InitDevices()` to register all device backends.

2. **Device Plugin** (`cmd/device-plugin/nvidia/`) — Implements the Kubernetes device plugin API for NVIDIA GPUs. Registers vGPU resources with the kubelet.

3. **vGPU Monitor** (`cmd/vGPUmonitor/`) — Runs as a DaemonSet, monitors GPU usage per container, exposes Prometheus metrics, and enforces resource limits.

### Core Package Layout

- **`pkg/device/`** — The device abstraction layer. `devices.go` defines the `Devices` interface that all hardware backends must implement. Each subdirectory (`nvidia/`, `cambricon/`, `hygon/`, `iluvatar/`, `metax/`, `mthreads/`, `ascend/`, `amd/`, `enflame/`, `kunlun/`, `awsneuron/`, `vastai/`, `biren/`, `remotegpu/`) is a concrete implementation.

- **`pkg/scheduler/`** — Scheduler extender logic: node filtering, scoring, webhook handling. `config/config.go` contains `InitDevicesWithConfig()` which registers all device backends into `device.DevicesMap`.

- **`pkg/scheduler/policy/`** — GPU scheduling policies (`binpack`, `spread`, `topology-aware`, `mutex`, and `numa` as a chained sort key; comma-separated combinations are accepted) and node-level scoring.

- **`pkg/util/`** — Shared utilities including K8s client setup, node locking (annotation-based distributed lock), and leader election.

- **`pkg/device-plugin/`** — NVIDIA device plugin internals (forked/adapted from NVIDIA's k8s-device-plugin).

### Key Data Flow

Pod submission → MutatingWebhook (injects scheduler name + annotations) → Scheduler extender filter/score (calls `Devices.Fit()` and `Devices.ScoreNode()`) → Bind (writes device allocation to pod annotations) → Device plugin `Allocate()` (reads annotations, sets container env vars) → vGPUmonitor enforces limits inside container.

### Device Registration Pattern

Each device backend provides an `Init*Device(config)` function returning a `device.Devices` implementation. All backends are registered in `pkg/scheduler/config/config.go:InitDevicesWithConfig()` which populates the global `device.DevicesMap`.

## CI Checks (must pass before merge)

- License headers on all `.go` files (`hack/verify-license.sh`, uses `addlicense`)
- Import aliases must match `hack/.import-aliases` (e.g., `corev1` for `k8s.io/api/core/v1`)
- RBAC permissions: the ClusterRole in the chart must cover the API calls in `pkg/` and `cmd/` (`hack/verify-rbac.sh`)
- golangci-lint with config in `.golangci.yaml` (version pinned in `hack/verify-staticcheck.sh`)
- `goimports` with local prefix `github.com/Project-HAMi/HAMi`
- Unit tests with race detector
- DCO: every commit needs a `Signed-off-by` trailer (`git commit -s`)
- Chart docs: the generated values table in `charts/hami/README.md` must match `charts/hami/values.yaml` (`make verify-helm-chart-docs`)
- Nightly Trivy scan of the built images, plus CodeQL and OpenSSF Scorecard

## Conventions

- Go module: `github.com/Project-HAMi/HAMi`
- Import grouping: stdlib, then external, then `github.com/Project-HAMi/HAMi/...` (enforced by goimports local-prefixes)
- K8s API import aliases are enforced (see `hack/.import-aliases`): `corev1`, `metav1`, `apierrors`, etc.
- All Go files require Apache 2.0 license header
- Helm chart lives in `charts/hami/`; vendor settings live under `devices.<vendor>` in `values.yaml`, and values are documented with `# -- Description` comments for helm-docs
- Image and tool versions are pinned in `version.mk`, `hack/verify-staticcheck.sh` and the workflows; HAMi-core is the `libvgpu` submodule
- E2E tests use Ginkgo/Gomega (`test/e2e/`)
- Annotations use the `hami.io/` prefix

## AI Assistance Disclosure

Per CONTRIBUTING.md, all PRs using AI assistance must disclose it in the PR description.
