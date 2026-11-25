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

	"go.opentelemetry.io/otel/propagation"
)

// TraceparentAnnotation holds the W3C traceparent that links the spans of one
// pod placement across the webhook, the scheduler and the device plugin.
const TraceparentAnnotation = "hami.io/traceparent"

// traceparentHeader is the carrier key that propagation.TraceContext reads and writes.
const traceparentHeader = "traceparent"

// Inject writes the span context in ctx to annotations and returns the map,
// creating it when annotations is nil. It writes nothing when ctx has no valid
// span context, so callers never store an empty trace.
func Inject(ctx context.Context, annotations map[string]string) map[string]string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	traceparent, ok := carrier[traceparentHeader]
	if !ok {
		return annotations
	}
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[TraceparentAnnotation] = traceparent
	return annotations
}

// Extract returns ctx with the remote span context stored in annotations.
// A missing or malformed annotation leaves ctx unchanged.
func Extract(ctx context.Context, annotations map[string]string) context.Context {
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{traceparentHeader: annotations[TraceparentAnnotation]})
}
