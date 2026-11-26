/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

const (
	sdkDisabledEnvName    = "OTEL_SDK_DISABLED"
	endpointEnvName       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	tracesEndpointEnvName = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	protocolEnvName       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	tracesProtocolEnvName = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"

	// tracerName is the instrumentation scope of every HAMi span.
	tracerName = "github.com/Project-HAMi/HAMi/pkg/tracing"

	// defaultAttributeValueLengthLimit caps attribute values when no length limit is set.
	defaultAttributeValueLengthLimit = 1024
)

// Setup turns tracing on when an OTLP endpoint is set and OTEL_SDK_DISABLED is
// not true. It returns an error when the endpoint is not an http or https URL
// with a host.
// It returns a shutdown function: call it before the process exits to
// send the spans still waiting in memory and close the connection to the
// collector. When tracing is off, it leaves the global TracerProvider unset, so
// Tracer returns a no-op tracer, and the shutdown function does nothing.
func Setup(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	envName, endpoint := tracesEndpointEnvName, os.Getenv(tracesEndpointEnvName)
	if endpoint == "" {
		envName, endpoint = endpointEnvName, os.Getenv(endpointEnvName)
	}
	if endpoint == "" || sdkDisabled() {
		return func(context.Context) error { return nil }, nil
	}
	if u, err := url.Parse(endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid %s %q: it must be an http or https URL with a host, such as http://otel-collector:4317", envName, endpoint)
	}
	checkProtocol()
	// Send OpenTelemetry logs and export errors to klog. Export errors go to the
	// error handler, which writes to the standard logger by default.
	otel.SetLogger(klog.NewKlogr())
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		klog.ErrorS(err, "OpenTelemetry error")
	}))
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	res := newResource(ctx, serviceName)
	sampler := newSampler()
	tp := newTracerProvider(res, sampler, exporter)
	otel.SetTracerProvider(tp)
	name, _ := res.Set().Value(semconv.ServiceNameKey)
	klog.InfoS("Tracing enabled", "endpoint", endpoint, "sampler", sampler.Description(), "serviceName", name.AsString())
	return tp.Shutdown, nil
}

// Tracer returns the tracer that HAMi components use to start spans.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// WithTraceID returns keysAndValues for a klog line, followed by "traceID" and
// span's trace ID when span is recording and sampled, so the line leads to its
// trace. With tracing off, a span still carries its parent's sampled flag.
func WithTraceID(span trace.Span, keysAndValues ...any) []any {
	sc := span.SpanContext()
	if !span.IsRecording() || !sc.IsSampled() {
		return keysAndValues
	}
	return append(keysAndValues, "traceID", sc.TraceID().String())
}

// sdkDisabled reports whether OTEL_SDK_DISABLED is "true", in any case. Any
// other value is treated as false.
func sdkDisabled() bool {
	raw := os.Getenv(sdkDisabledEnvName)
	value := strings.TrimSpace(raw)
	if strings.EqualFold(value, "true") {
		return true
	}
	if value != "" && !strings.EqualFold(value, "false") {
		klog.Errorf("Invalid %s value %q, using default %v", sdkDisabledEnvName, raw, false)
	}
	return false
}

// checkProtocol logs an error when OTEL_EXPORTER_OTLP_TRACES_PROTOCOL or
// OTEL_EXPORTER_OTLP_PROTOCOL asks for a protocol other than grpc, the only one
// the exporter uses.
func checkProtocol() {
	envName, raw := tracesProtocolEnvName, os.Getenv(tracesProtocolEnvName)
	if raw == "" {
		envName, raw = protocolEnvName, os.Getenv(protocolEnvName)
	}
	value := strings.TrimSpace(raw)
	if value != "" && !strings.EqualFold(value, "grpc") {
		klog.Errorf("Unsupported %s value %q, using %v", envName, raw, "grpc")
	}
}

// newResource sets service.name to serviceName unless OTEL_SERVICE_NAME or
// OTEL_RESOURCE_ATTRIBUTES sets it. It reads no host files.
func newResource(ctx context.Context, serviceName string) *resource.Resource {
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		// resource.New still returns the attributes it could read.
		klog.ErrorS(err, "Failed to read OpenTelemetry resource attributes")
	}
	return res
}

// newTracerProvider caps attribute values at defaultAttributeValueLengthLimit
// unless a length limit is set in the environment.
func newTracerProvider(res *resource.Resource, sampler sdktrace.Sampler, exporter sdktrace.SpanExporter) *sdktrace.TracerProvider {
	limits := sdktrace.NewSpanLimits()
	if limits.AttributeValueLengthLimit < 0 {
		limits.AttributeValueLengthLimit = defaultAttributeValueLengthLimit
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithRawSpanLimits(limits),
	)
}
