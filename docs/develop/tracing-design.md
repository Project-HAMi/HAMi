# OpenTelemetry Tracing Design

This document describes how OpenTelemetry distributed tracing is integrated into the HAMi scheduler. The initial implementation covers the `Bind` workflow in the scheduler control plane.

## Summary

The scheduler's `Bind` handler executes several steps in sequence — pod lookup, node lookup, node lock acquisition, annotation patching, and the final apiserver bind call. When a bind request is slow, operators currently have no way to tell which step caused the delay without parsing logs manually.

This feature adds optional OpenTelemetry tracing to the Bind workflow. When enabled, each `/bind` request is broken down into timed spans that show exactly where time is spent. This allows operators to quickly identify whether a delay is caused by lock contention, API server slowness, or cache misses.

Tracing is disabled by default. It can be turned on with a CLI flag.

## Motivation

In multi-tenant clusters with many GPU workloads, the `Bind` phase can become a bottleneck. During this phase, the scheduler performs the following operations:

- Looking up Pod and Node metadata from local informers.
- Acquiring node-level locks (`acquireNodeLocks`) to prevent concurrent GPU over-allocation.
- Patching GPU allocation annotations back to the Kubernetes API server.
- Executing the final binding API call.

Standard Prometheus metrics report overall latency and failure counts, but they do not provide request-level context. For example, if a bind request takes 5 seconds, metrics alone cannot tell whether the delay was caused by lock contention (multiple pods competing for the same GPU) or by the API server being slow.

Distributed tracing solves this by breaking down each request into granular spans. Each span records the start time, end time, and relevant attributes, giving operators a clear view of what happened during each step of the bind process.

## Usage

To enable tracing, start the scheduler with the `--enable-tracing` flag:

```bash
./bin/scheduler --enable-tracing=true
```

When enabled, spans are formatted as compact JSON (single line per span) and printed to `stdout`. External collectors such as OpenTelemetry Collector or Fluent Bit can scrape stdout to route traces to backends like Tempo, Jaeger, or any OTLP-compatible endpoint.

When the flag is not set (default), the scheduler installs a no-op tracer provider (`noop.NewTracerProvider()`). To prevent interfering with other components that might use OpenTelemetry, this no-op provider is explicitly NOT registered as the global `otel.SetTracerProvider` when tracing is disabled.

## Architecture

The tracing implementation lives in `pkg/scheduler/tracing/` and consists of two files:

- **`tracing.go`** — Sets up the `TracerProvider`. When tracing is enabled, it creates a stdout exporter with `AlwaysSample` and registers it as the global provider. The exporter must use a bounded `BatchSpanProcessor` (rather than synchronous `SimpleSpanProcessor`) with explicit export timeouts, queue overflow protection, and a proper `SIGTERM` trap for graceful shutdown, to ensure that tracing never blocks the scheduler control plane or drops traces during pod eviction.

- **`bind_instrument.go`** — Provides helper functions for creating and managing spans in the bind workflow. These helpers ensure consistent span naming, attribute keys, and error handling across the codebase.

The tracing is wired at two layers:

1. **HTTP handler layer** (`pkg/scheduler/routes/route.go`) — The `Bind` handler creates the root server span and records HTTP-level attributes and errors (decode failures, marshal failures, status codes).

2. **Scheduler layer** (`pkg/scheduler/scheduler.go`) — The `Scheduler.Bind` method creates child spans for each internal step of the bind process.

## Span Model

The `/bind` request creates a root server span and child spans for each internal step:

```text
hami.scheduler.bind                          (Root Server Span)
├── hami.scheduler.bind.pod_lookup           (Cache lookup for the target Pod)
├── hami.scheduler.bind.node_lookup          (Cache lookup for the target Node)
├── hami.scheduler.bind.node_lock            (Lock acquisition wait time)
├── hami.scheduler.bind.patch_annotations    (Patching annotations on the Pod)
└── hami.scheduler.bind.apiserver_bind       (Final API server bind call)
```

Each child span records only the time spent in that specific step. If the node lock is slow due to contention, only the `node_lock` span will show a long duration while the other spans remain fast. This makes it straightforward to identify the bottleneck. Failed phase spans explicitly set their OpenTelemetry span status to `Error` and record the underlying Go error for visibility.

## Span Attributes

The following attributes are recorded on spans:

| Attribute | Where | Description |
|-----------|-------|-------------|
| `http.route` | Root span | Always `/bind` |
| `http.method` | Root span | HTTP method (`POST`) |
| `http.status_code` | Root span | HTTP response status code |
| `hami.bind.phase` | Child spans | Name of the current execution phase |
| `hami.bind.result` | Root span | Terminal outcome: `success` or `error` |
| `hami.bind.error_kind` | Root span | Error classification: `decode`, `scheduler`, `marshal` |
| `hami.pod.name` | Root span | Name of the target Pod |
| `hami.pod.namespace` | Root span | Namespace of the target Pod |
| `hami.pod.uid` | Root span | UID of the target Pod |
| `hami.node.name` | Root span | Name of the target Node |
| `hami.scheduler.instance` | Root span | Identifier of the scheduler instance |

All custom attributes use the `hami.` prefix, consistent with the existing `hami_*` Prometheus metric namespace.

## Out of Scope

The following items are intentionally not included in this implementation:

- Tracing for mutating webhooks, extender filter/score handlers, and device-plugin allocate calls.
- Native OTLP gRPC/HTTP exporters (only the stdout exporter is provided).
- Sampling configuration options.
- Helm chart changes to configure exporter endpoints.

## Future Scope

- Support native OTLP gRPC/HTTP exporters via a `--tracing-exporter` flag, allowing operators to send traces directly to a backend without scraping stdout.
- Extend tracing to cover mutating webhooks, extender filter/score handlers, and device-plugin allocate calls.
- Add configurable sampling to reduce trace volume in high-throughput clusters.
