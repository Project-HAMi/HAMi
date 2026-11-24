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
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"gotest.tools/v3/assert"
)

func TestSetup(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       string
		tracesEndpoint string
		disabled       string
		wantOn         bool
	}{
		{name: "no endpoint"},
		{name: "disabled", endpoint: "http://localhost:4317", disabled: "true"},
		{name: "disabled in upper case", endpoint: "http://localhost:4317", disabled: "TRUE"},
		{name: "endpoint", endpoint: "http://localhost:4317", wantOn: true},
		{name: "traces endpoint", tracesEndpoint: "http://localhost:4317", wantOn: true},
		{name: "disabled set to 1 is not true", endpoint: "http://localhost:4317", disabled: "1", wantOn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(endpointEnvName, tt.endpoint)
			t.Setenv(tracesEndpointEnvName, tt.tracesEndpoint)
			t.Setenv(sdkDisabledEnvName, tt.disabled)
			global := otel.GetTracerProvider()
			t.Cleanup(func() {
				if otel.GetTracerProvider() != global {
					otel.SetTracerProvider(global)
				}
			})

			shutdown, err := Setup(context.Background(), "hami-scheduler")
			assert.NilError(t, err)
			assert.Equal(t, otel.GetTracerProvider() != global, tt.wantOn)
			assert.NilError(t, shutdown(context.Background()))
		})
	}
}

func TestSetupChildFollowsParent(t *testing.T) {
	t.Setenv(endpointEnvName, "http://localhost:4317")
	t.Setenv(tracesEndpointEnvName, "")
	t.Setenv(sdkDisabledEnvName, "")
	t.Setenv(tracesSamplerEnvName, "always_on")
	global := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(global) })

	shutdown, err := Setup(context.Background(), "hami-scheduler")
	assert.NilError(t, err)
	t.Cleanup(func() { assert.NilError(t, shutdown(context.Background())) })

	// The webhook decided not to trace this pod, so Bind must not be traced either.
	parent := trace.ContextWithRemoteSpanContext(context.Background(), testSpanContext(t, 0))
	_, span := Tracer().Start(parent, "bind")
	defer span.End()
	assert.Assert(t, !span.SpanContext().IsSampled())
}

func TestNewResource(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		attributes  string
		want        string
	}{
		{name: "default", want: "hami-scheduler"},
		{name: "OTEL_SERVICE_NAME", serviceName: "hami-scheduler-prod", want: "hami-scheduler-prod"},
		{name: "OTEL_RESOURCE_ATTRIBUTES", attributes: "service.name=hami-scheduler-gpu-a", want: "hami-scheduler-gpu-a"},
		{name: "both set, OTEL_SERVICE_NAME is used", serviceName: "hami-scheduler-prod", attributes: "service.name=hami-scheduler-gpu-a", want: "hami-scheduler-prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_SERVICE_NAME", tt.serviceName)
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", tt.attributes)
			res := newResource(context.Background(), "hami-scheduler")
			got, _ := res.Set().Value(semconv.ServiceNameKey)
			assert.Equal(t, got.AsString(), tt.want)
		})
	}
}

func TestNewTracerProviderAttributeValueLengthLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     string
		spanLimit string
		want      int
	}{
		{name: "unset", want: 1024},
		{name: "OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT", limit: "10", want: 10},
		{name: "span limit is used before the general limit", limit: "10", spanLimit: "5", want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT", tt.limit)
			t.Setenv("OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT", tt.spanLimit)
			exporter := tracetest.NewInMemoryExporter()
			tp := newTracerProvider(resource.Empty(), sdktrace.AlwaysSample(), exporter)

			_, span := tp.Tracer(tracerName).Start(context.Background(), "filter")
			span.SetAttributes(attribute.String("error.message", strings.Repeat("x", 2000)))
			span.End()
			assert.NilError(t, tp.ForceFlush(context.Background()))

			spans := exporter.GetSpans()
			assert.Equal(t, len(spans), 1)
			assert.Equal(t, len(spans[0].Attributes[0].Value.AsString()), tt.want)
			assert.NilError(t, tp.Shutdown(context.Background()))
		})
	}
}
