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

package plugin

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/tracing"
)

// startAllocateSpan starts the span of one Allocate call for pod, a child of
// the Filter span whose context Filter wrote to the pod.
func startAllocateSpan(ctx context.Context, pod *corev1.Pod, node string) (context.Context, trace.Span) {
	return tracing.Tracer().Start(tracing.Extract(ctx, pod.Annotations), "allocate",
		trace.WithAttributes(
			tracing.PodUIDKey.String(string(pod.UID)),
			tracing.PodNameKey.String(pod.Name),
			tracing.NamespaceKey.String(pod.Namespace),
			tracing.NodeKey.String(node),
			tracing.DeviceTypeKey.String(nvidia.NvidiaGPUDevice),
		))
}
