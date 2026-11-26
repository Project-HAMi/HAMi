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

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gotest.tools/v3/assert"
)

func TestSetFailure(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := newTracerProvider(resource.Empty(), sdktrace.AlwaysSample(), exporter)

	_, span := tp.Tracer(tracerName).Start(context.Background(), "filter")
	SetFailure(span, "no_fit", "no available node, 12 nodes do not meet")
	span.End()
	assert.NilError(t, tp.ForceFlush(context.Background()))

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	assert.Equal(t, spans[0].Status.Code, codes.Error)
	assert.Equal(t, spans[0].Status.Description, "no available node, 12 nodes do not meet")
	assert.Equal(t, len(spans[0].Attributes), 1)
	assert.Equal(t, spans[0].Attributes[0], FailureReasonKey.String("no_fit"))
	assert.NilError(t, tp.Shutdown(context.Background()))
}
