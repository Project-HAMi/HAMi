# Quick Start Guide for New Contributors

This guide will help you set up your development environment and start contributing to HAMi.

## Prerequisites

- **Go 1.26.5** or later (see `go.mod` for the exact version)
- **Git**
- **Docker** (for building images)
- **kubectl** with access to a Kubernetes cluster
- **Helm** (optional, for chart development)

## Setting Up the Development Environment

1. Fork and clone the repository:

```bash
git clone https://github.com/<your-username>/HAMi.git
cd HAMi
```

2. Install dependencies:

```bash
make tidy
```

## Building

Build all binaries (scheduler, vGPUmonitor, nvidia-device-plugin):

```bash
make build
```

Binaries are output to `bin/`.

## Running Tests

Run the full test suite:

```bash
make test
```

Run a specific test:

```bash
go test ./pkg/scheduler/... -run TestSpecificFunc -short --race -count=1
```

## Code Quality

Run linting:

```bash
make lint
```

Run all verification checks (license headers, import aliases, linting):

```bash
make verify
```

## Project Structure

- `cmd/` — Entry points for scheduler, vGPUmonitor, and device plugins
- `pkg/device/` — Device abstraction layer with backends for NVIDIA, Cambricon, Hygon, etc.
- `pkg/scheduler/` — Scheduler extender logic (filter, score, bind)
- `pkg/device-plugin/` — Kubernetes device plugin internals
- `charts/` — Helm charts
- `test/e2e/` — End-to-end tests using Ginkgo/Gomega

## Contribution Workflow

1. Create a feature branch from `master`:

```bash
git checkout -b feature/your-feature master
```

2. Make your changes and ensure tests pass:

```bash
make test
make verify
```

3. Commit with a descriptive message following [conventional commits](https://www.conventionalcommits.org/):

```
feat: add support for new device type

Signed-off-by: Your Name <your@email.com>
```

4. Push and create a pull request against `master`.

## Commit Convention

HAMi follows conventional commits. Use prefixes like:

- `feat:` for new features
- `fix:` for bug fixes
- `docs:` for documentation changes
- `test:` for test additions/changes
- `refactor:` for code refactoring
- `chore:` for maintenance tasks

Always include `Signed-off-by:` in your commits (use `git commit -s`).

## Getting Help

- Open an issue for bugs or feature requests
- Join the community discussions
- Check existing documentation in `docs/`
