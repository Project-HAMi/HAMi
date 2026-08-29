# Quick Start Guide for New Contributors

Welcome to HAMi! This guide will help you get started with contributing to the project.

## Prerequisites

- Go 1.27.1 or later
- Docker
- Kubernetes cluster (for testing)
- Git

## Setting Up the Development Environment

1. Fork the repository on GitHub
2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/HAMi.git
   cd HAMi
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/Project-HAMi/HAMi.git
   ```

## Building the Project

```bash
# Build all components
make build

# Build specific components
make build-scheduler
make build-device-plugin
make build-monitor
```

## Running Tests

```bash
# Run all tests
make test

# Run specific tests
go test ./pkg/scheduler/...
go test ./pkg/device/...
```

## Code Quality

```bash
# Run linter
make lint

# Format code
gofmt -s -w .

# Run verification
make verify
```

## Development Workflow

1. Create a feature branch from master:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and commit them:
   ```bash
   git add .
   git commit -s -m "feat: add your feature description"
   ```

3. Push to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

4. Create a Pull Request on GitHub

## Commit Message Convention

Follow the conventional commits format:
- `feat:` for new features
- `fix:` for bug fixes
- `docs:` for documentation changes
- `test:` for adding tests
- `chore:` for maintenance tasks

## Getting Help

- Join the CNCF Slack channel: `#hami-dev`
- Check existing issues and PRs
- Read the [CONTRIBUTING.md](../CONTRIBUTING.md) for detailed guidelines

## AI Assistance Disclosure

This documentation was created with AI assistance. All content has been reviewed for accuracy.

> For questions, reach out on CNCF Slack `#hami-dev` channel.
