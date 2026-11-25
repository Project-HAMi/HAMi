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
	"testing"

	"go.opentelemetry.io/otel/trace"
	"gotest.tools/v3/assert"
)

func testSpanContext(t *testing.T, flags trace.TraceFlags) trace.SpanContext {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	assert.NilError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	assert.NilError(t, err)
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: flags})
}

func TestInjectExtractRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		flags trace.TraceFlags
		want  string
	}{
		{name: "sampled", flags: trace.FlagsSampled, want: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{name: "not sampled", flags: 0, want: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := testSpanContext(t, tt.flags)
			annotations := Inject(trace.ContextWithSpanContext(context.Background(), sc), map[string]string{})
			assert.Equal(t, annotations[TraceparentAnnotation], tt.want)

			got := trace.SpanContextFromContext(Extract(context.Background(), annotations))
			assert.Assert(t, got.Equal(sc.WithRemote(true)))
		})
	}
}

func TestInjectNilAnnotations(t *testing.T) {
	sc := testSpanContext(t, trace.FlagsSampled)
	got := Inject(trace.ContextWithSpanContext(context.Background(), sc), nil)
	assert.DeepEqual(t, got, map[string]string{
		TraceparentAnnotation: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
}

func TestInjectNoSpanContext(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
	}{
		{name: "nil annotations", annotations: nil},
		{name: "empty annotations", annotations: map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Inject(context.Background(), tt.annotations)
			assert.DeepEqual(t, got, tt.annotations)
		})
	}
}

func TestInjectDropsTracestate(t *testing.T) {
	ts, err := trace.ParseTraceState("vendor=value")
	assert.NilError(t, err)
	sc := testSpanContext(t, trace.FlagsSampled).WithTraceState(ts)

	annotations := Inject(trace.ContextWithSpanContext(context.Background(), sc), map[string]string{})
	assert.DeepEqual(t, annotations, map[string]string{
		TraceparentAnnotation: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
}

func TestExtractIgnoresBadAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
	}{
		{name: "nil annotations", annotations: nil},
		{name: "no traceparent", annotations: map[string]string{"hami.io/vgpu-node": "node-1"}},
		{name: "empty", annotations: map[string]string{TraceparentAnnotation: ""}},
		{name: "zero trace ID and span ID", annotations: map[string]string{TraceparentAnnotation: "00-00000000000000000000000000000000-0000000000000000-01"}},
		{name: "hand-edited upper case trace ID", annotations: map[string]string{TraceparentAnnotation: "00-AB000000000000000000000000000000-cd00000000000000-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext(t, trace.FlagsSampled))
			assert.Assert(t, Extract(ctx, tt.annotations) == ctx)
		})
	}
}
