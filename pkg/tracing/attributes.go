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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// Attribute keys of HAMi spans. Pod and node values use the stable
// OpenTelemetry semantic conventions; the others use hami.* keys.
const (
	PodUIDKey = semconv.K8SPodUIDKey
	// PodNameKey is the pod name, or the generateName prefix at admission.
	PodNameKey   = semconv.K8SPodNameKey
	NamespaceKey = semconv.K8SNamespaceNameKey
	// NodeKey is the node the pod is placed on.
	NodeKey = semconv.K8SNodeNameKey

	// DeviceTypeKey is the requested device type, such as NVIDIA.
	DeviceTypeKey = attribute.Key("hami.device.type")
	// DeviceUUIDsKey lists the UUIDs of the devices given to the pod.
	DeviceUUIDsKey   = attribute.Key("hami.device.uuids")
	RequestMemoryKey = attribute.Key("hami.request.memory")
	RequestCoresKey  = attribute.Key("hami.request.cores")
	// FilterNodesKey is the number of candidate nodes in a Filter call.
	FilterNodesKey = attribute.Key("hami.filter.nodes")
	// FilterRejectedNodesKey is the number of candidate nodes Filter rejected.
	FilterRejectedNodesKey = attribute.Key("hami.filter.rejected_nodes")
	// FilterSkippedAttemptsKey is the number of failed Filter attempts skipped
	// since the last recorded one, because they failed for the same reasons.
	FilterSkippedAttemptsKey = attribute.Key("hami.filter.skipped_attempts")
	// FailureReasonKey is why a step failed, as one of the failure_reason
	// values of the scheduler metrics.
	FailureReasonKey = attribute.Key("hami.failure_reason")
)

// SetError marks span as failed, with message as its status.
func SetError(span trace.Span, message string) {
	span.SetStatus(codes.Error, message)
}

// SetFailure marks span as failed, with message as its status and reason as
// hami.failure_reason.
func SetFailure(span trace.Span, reason, message string) {
	span.SetAttributes(FailureReasonKey.String(reason))
	SetError(span, message)
}
