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

package scheduler

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/common"
	metrics "github.com/Project-HAMi/HAMi/pkg/metrics"
	"github.com/Project-HAMi/HAMi/pkg/tracing"
)

// startAdmissionSpan starts the span of a pod's admission, dated back to
// start, when the request arrived.
func startAdmissionSpan(ctx context.Context, start time.Time, namespace string, pod *corev1.Pod) (context.Context, trace.Span) {
	name := pod.Name
	if name == "" {
		name = pod.GenerateName
	}
	return tracing.Tracer().Start(ctx, "admission",
		trace.WithTimestamp(start),
		trace.WithAttributes(
			tracing.NamespaceKey.String(namespace),
			tracing.PodNameKey.String(name),
		))
}

// startFilterSpan starts the span of one Filter call once its result is
// known, dated back to start, when the call began.
func startFilterSpan(start time.Time, pod *corev1.Pod, resourceReqs device.PodDeviceRequests, nodes int, failedNodes map[string]string) (context.Context, trace.Span) {
	ctx, span := tracing.Tracer().Start(context.Background(), "filter",
		trace.WithTimestamp(start),
		trace.WithLinks(traceparentLinks(pod)...),
		trace.WithAttributes(
			tracing.PodUIDKey.String(string(pod.UID)),
			tracing.PodNameKey.String(pod.Name),
			tracing.NamespaceKey.String(pod.Namespace),
		))
	if !span.IsRecording() {
		return ctx, span
	}
	deviceType := deviceTypeForRequests(resourceReqs)
	span.SetAttributes(
		tracing.DeviceTypeKey.String(deviceType),
		tracing.FilterNodesKey.Int(nodes),
		tracing.FilterRejectedNodesKey.Int(len(failedNodes)),
	)
	// A pod that asks for more than one device type is "mixed" and gets no
	// request amounts: the backends count memory and cores in different units.
	if dev, ok := device.GetDevices()[deviceType]; ok {
		if memory, cores, err := podDeviceRequestTotal(pod, dev); err == nil {
			span.SetAttributes(tracing.RequestMemoryKey.Int64(memory), tracing.RequestCoresKey.Int64(cores))
		}
	}
	return ctx, span
}

// traceparentLinks links to the span context on the pod: the webhook's, or
// after a failed bind the last Filter's. A pod without one gets no link.
func traceparentLinks(pod *corev1.Pod) []trace.Link {
	sc := trace.SpanContextFromContext(tracing.Extract(context.Background(), pod.Annotations))
	if !sc.IsValid() {
		return nil
	}
	return []trace.Link{{SpanContext: sc}}
}

// filterFailure is the last recorded failed Filter of a pod and the number of
// the same failures since.
type filterFailure struct {
	summary string
	skipped int
}

// recordFilterFailure records a failed Filter call as a span that ends now,
// unless the pod's last recorded failure is the same one, which it counts.
func (s *Scheduler) recordFilterFailure(start time.Time, pod *corev1.Pod, resourceReqs device.PodDeviceRequests, nodes int, failedNodes map[string]string, reason metrics.SchedulerFailureReason, message string) {
	s.filterFailureLock.Lock()
	defer s.filterFailureLock.Unlock()
	last, seen := s.filterFailures[pod.UID]
	if seen && last.summary == fmt.Sprint(reason, message, rejectionCounts(failedNodes)) {
		last.skipped++
		s.filterFailures[pod.UID] = last
		return
	}
	_, span := startFilterSpan(start, pod, resourceReqs, nodes, failedNodes)
	defer span.End()
	if !span.IsRecording() {
		return
	}
	counts := rejectionCounts(failedNodes)
	if last.skipped > 0 {
		span.SetAttributes(tracing.FilterSkippedAttemptsKey.Int(last.skipped))
	}
	if reason == metrics.FailureReasonNoFit {
		for _, reasonType := range slices.Sorted(maps.Keys(counts)) {
			span.AddEvent(reasonType, trace.WithAttributes(tracing.FilterRejectedNodesKey.Int(counts[reasonType])))
		}
	}
	tracing.SetFailure(span, string(reason), message)
	// The informer drops a deleted pod from its cache before onDelPod clears
	// the pod's entry, so a pod no longer cached would keep its entry forever.
	if s.podLister != nil {
		if cur, err := s.podLister.Pods(pod.Namespace).Get(pod.Name); err != nil || cur.UID != pod.UID {
			return
		}
	}
	if s.filterFailures == nil {
		s.filterFailures = make(map[k8stypes.UID]filterFailure)
	}
	s.filterFailures[pod.UID] = filterFailure{summary: fmt.Sprint(reason, message, counts)}
}

// clearFilterFailure forgets the pod's last recorded failed Filter. It returns
// the number of failed Filters since that were not recorded.
func (s *Scheduler) clearFilterFailure(pod *corev1.Pod) int {
	s.filterFailureLock.Lock()
	defer s.filterFailureLock.Unlock()
	last := s.filterFailures[pod.UID]
	delete(s.filterFailures, pod.UID)
	return last.skipped
}

// rejectionCounts returns the number of nodes rejected for each reason.
func rejectionCounts(failedNodes map[string]string) map[string]int {
	counts := map[string]int{}
	for _, nodeReason := range failedNodes {
		for reasonType := range common.ParseReason(nodeReason) {
			counts[reasonType]++
		}
	}
	return counts
}

// bindParentContext returns a context with the span context Filter wrote for
// the pod: from memory first, then from the pod's annotation.
func (s *Scheduler) bindParentContext(uid k8stypes.UID, pod *corev1.Pod) context.Context {
	if pi, ok := s.podManager.GetPod(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: uid}}); ok && pi.Traceparent != "" {
		return tracing.Extract(context.Background(), map[string]string{tracing.TraceparentAnnotation: pi.Traceparent})
	}
	if pod != nil {
		return tracing.Extract(context.Background(), pod.Annotations)
	}
	return context.Background()
}

// podDeviceUUIDs lists the UUIDs of devices, each once, in sorted order.
func podDeviceUUIDs(devices device.PodDevices) []string {
	var uuids []string
	for _, podSingle := range devices {
		for _, ctrDevices := range podSingle {
			uuids = append(uuids, containerDeviceIDs(ctrDevices)...)
		}
	}
	slices.Sort(uuids)
	return slices.Compact(uuids)
}
