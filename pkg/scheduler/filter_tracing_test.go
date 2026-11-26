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
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/tracing"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const (
	sampledTraceparent    = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	notSampledTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
)

// recordSpans sends the spans started through tracing.Tracer to an in-memory
// exporter for the rest of the test.
func recordSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	global := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(global)
		assert.NilError(t, tp.Shutdown(context.Background()))
	})
	return exporter
}

// spanAttributes returns the attributes of span by key.
func spanAttributes(span tracetest.SpanStub) map[attribute.Key]attribute.Value {
	attrs := make(map[attribute.Key]attribute.Value, len(span.Attributes))
	for _, kv := range span.Attributes {
		attrs[kv.Key] = kv.Value
	}
	return attrs
}

func TestFilterSpanOnSuccess(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)
	exporter := recordSpans(t)

	live := filterTo(t, s, kubeClient, pod, moveNodeX)

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	span := spans[0]
	assert.Equal(t, span.Name, "filter")
	assert.Equal(t, span.Status.Code, codes.Unset)
	attrs := spanAttributes(span)
	assert.Equal(t, attrs[tracing.PodUIDKey].AsString(), "move-uid")
	assert.Equal(t, attrs[tracing.PodNameKey].AsString(), "move")
	assert.Equal(t, attrs[tracing.NamespaceKey].AsString(), "default")
	assert.Equal(t, attrs[tracing.NodeKey].AsString(), moveNodeX)
	assert.DeepEqual(t, attrs[tracing.DeviceUUIDsKey].AsStringSlice(), []string{"node-x-gpu-0"})
	assert.Equal(t, attrs[tracing.DeviceTypeKey].AsString(), nvidia.NvidiaGPUDevice)
	assert.Equal(t, attrs[tracing.RequestMemoryKey].AsInt64(), int64(1000))
	assert.Equal(t, attrs[tracing.RequestCoresKey].AsInt64(), int64(0))
	assert.Equal(t, attrs[tracing.FilterNodesKey].AsInt64(), int64(1))
	assert.Equal(t, attrs[tracing.FilterRejectedNodesKey].AsInt64(), int64(0))

	// Bind reads the span context from memory first, because the informer may
	// not show the patch yet, then from the pod.
	want := fmt.Sprintf("00-%s-%s-01", span.SpanContext.TraceID(), span.SpanContext.SpanID())
	assert.Equal(t, live.Annotations[tracing.TraceparentAnnotation], want)
	pi, ok := s.podManager.GetPod(pod)
	assert.Assert(t, ok)
	assert.Equal(t, pi.Traceparent, want)
}

func TestFilterSpanOnNoFit(t *testing.T) {
	pod := movePod()
	// More memory than the 8192 MiB of each GPU.
	pod.Spec.Containers[0].Resources.Limits[moveMem] = *resource.NewQuantity(10000, resource.BinarySI)
	s, kubeClient := filterLeader(t, pod, 1)
	exporter := recordSpans(t)

	res, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{moveNodeX}})
	assert.NilError(t, err)
	assert.Assert(t, res.NodeNames == nil)

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	span := spans[0]
	assert.Equal(t, span.Status.Code, codes.Error)
	assert.Equal(t, span.Status.Description, "no available node, 1 nodes do not meet")
	attrs := spanAttributes(span)
	assert.Equal(t, attrs[tracing.FailureReasonKey].AsString(), "no_fit")
	assert.Equal(t, attrs[tracing.FilterRejectedNodesKey].AsInt64(), int64(1))
	assert.Equal(t, len(span.Events), 1)
	assert.Equal(t, span.Events[0].Name, common.CardInsufficientMemory)
	assert.Equal(t, len(span.Events[0].Attributes), 1)
	assert.Equal(t, span.Events[0].Attributes[0], tracing.FilterRejectedNodesKey.Int(1))

	// A failed Filter writes nothing to the pod.
	live, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	assert.NilError(t, err)
	_, found := live.Annotations[tracing.TraceparentAnnotation]
	assert.Assert(t, !found)
}

func TestFilterSpanOnPatchFailure(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)
	kubeClient.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected patch error")
	})
	exporter := recordSpans(t)

	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{moveNodeX}})
	assert.ErrorContains(t, err, "injected patch error")

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	assert.Equal(t, spans[0].Status.Code, codes.Error)
	assert.Equal(t, spanAttributes(spans[0])[tracing.FailureReasonKey].AsString(), "annotation_patch")
}

func TestFilterSpanOnSuperpodInformerFailure(t *testing.T) {
	resetSuperpodTestDevices(t)
	// NewScheduler has no informer factory, so the SuperPod informers cannot start.
	s := NewScheduler()
	t.Cleanup(s.Stop)
	pod := makeGPUPod("pod", "default", "", 1)
	pod.Labels[util.TaskIDLabel] = "task-1"
	pod.Annotations = map[string]string{
		util.NodeSchedulerPolicyAnnotationKey: util.NodeSchedulerPolicySuperpod.String(),
	}
	s.podLister = newTestPodLister(t, pod)
	exporter := recordSpans(t)

	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{}})
	assert.ErrorContains(t, err, "SuperPod informers")

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	assert.Equal(t, spans[0].Status.Code, codes.Error)
	assert.Equal(t, spanAttributes(spans[0])[tracing.FailureReasonKey].AsString(), "internal")
}

func TestFilterWithoutTracingWritesNoTraceparent(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)

	live := filterTo(t, s, kubeClient, pod, moveNodeX)

	_, found := live.Annotations[tracing.TraceparentAnnotation]
	assert.Assert(t, !found)
	pi, ok := s.podManager.GetPod(pod)
	assert.Assert(t, ok)
	assert.Equal(t, pi.Traceparent, "")
}

func TestFilterSpanSkipsPodWithoutDevices(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "plain-uid", Name: "plain", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	s, _ := filterLeader(t, pod, 1)
	exporter := recordSpans(t)

	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{moveNodeX}})
	assert.NilError(t, err)

	assert.Equal(t, len(exporter.GetSpans()), 0)
}

func TestFilterSpanSkipsRepeatedFailure(t *testing.T) {
	pod := movePod()
	pod.Spec.Containers[0].Resources.Limits[moveMem] = *resource.NewQuantity(10000, resource.BinarySI)
	s, _ := filterLeader(t, pod, 1)
	exporter := recordSpans(t)

	for range 3 {
		_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{moveNodeX}})
		assert.NilError(t, err)
	}
	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	_, found := spanAttributes(spans[0])[tracing.FilterSkippedAttemptsKey]
	assert.Assert(t, !found)

	// A second candidate node changes the failure, so it is recorded with the
	// two repeats before it.
	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{moveNodeX, moveNodeY}})
	assert.NilError(t, err)
	spans = exporter.GetSpans()
	assert.Equal(t, len(spans), 2)
	assert.Equal(t, spanAttributes(spans[1])[tracing.FilterSkippedAttemptsKey].AsInt64(), int64(2))
}

func TestFilterSpanOnSuccessCarriesSkippedAttempts(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)
	exporter := recordSpans(t)

	// No node of this name is registered, so both attempts fail the same way.
	for range 2 {
		_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{"node-unknown"}})
		assert.NilError(t, err)
	}
	filterTo(t, s, kubeClient, pod, moveNodeX)

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 2)
	assert.Equal(t, spans[1].Status.Code, codes.Unset)
	assert.Equal(t, spanAttributes(spans[1])[tracing.FilterSkippedAttemptsKey].AsInt64(), int64(1))
	assert.Equal(t, len(s.filterFailures), 0)
}

func TestFilterFailureForgottenOnPodDelete(t *testing.T) {
	pod := movePod()
	s, _ := filterLeader(t, pod, 1)
	recordSpans(t)

	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{"node-unknown"}})
	assert.NilError(t, err)
	assert.Equal(t, len(s.filterFailures), 1)

	s.onDelPod(pod)
	assert.Equal(t, len(s.filterFailures), 0)
}

func TestFilterSpanLinksToAdmission(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantLink    bool
	}{
		{name: "webhook annotation", annotations: map[string]string{tracing.TraceparentAnnotation: sampledTraceparent}, wantLink: true},
		{name: "no annotation"},
		{name: "malformed annotation", annotations: map[string]string{tracing.TraceparentAnnotation: "not-a-traceparent"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := movePod()
			pod.Annotations = tt.annotations
			s, kubeClient := filterLeader(t, pod, 1)
			exporter := recordSpans(t)

			filterTo(t, s, kubeClient, pod, moveNodeX)

			span := spanNamed(t, exporter.GetSpans(), "filter")
			// Each attempt starts a new trace, not one long trace for the wait.
			assert.Assert(t, !span.Parent.IsValid())
			if !tt.wantLink {
				assert.Equal(t, len(span.Links), 0)
				return
			}
			assert.Equal(t, len(span.Links), 1)
			assert.Equal(t, span.Links[0].SpanContext.TraceID().String(), "4bf92f3577b34da6a3ce929d0e0e4736")
			assert.Equal(t, span.Links[0].SpanContext.SpanID().String(), "00f067aa0ba902b7")
			assert.Assert(t, span.SpanContext.TraceID() != span.Links[0].SpanContext.TraceID())
		})
	}
}

// An unsampled Filter still writes its context, so Bind and the device plugin
// follow its decision instead of sampling on their own.
func TestFilterWritesUnsampledTraceparent(t *testing.T) {
	pod := movePod()
	s, kubeClient := filterLeader(t, pod, 1)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	global := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(global)
		assert.NilError(t, tp.Shutdown(context.Background()))
	})

	live := filterTo(t, s, kubeClient, pod, moveNodeX)

	got := live.Annotations[tracing.TraceparentAnnotation]
	assert.Assert(t, strings.HasSuffix(got, "-00"), "traceparent %q", got)
}

func TestFilterFailureNotKeptWithoutTracing(t *testing.T) {
	pod := movePod()
	s, _ := filterLeader(t, pod, 1)

	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{"node-unknown"}})
	assert.NilError(t, err)
	assert.Equal(t, len(s.filterFailures), 0)
}

// A Filter that ends after its pod left the informer cache keeps nothing.
func TestFilterFailureNotKeptForDeletedPod(t *testing.T) {
	pod := movePod()
	s, _ := filterLeader(t, pod, 1)
	// The informer has already dropped the pod; the API server still has it.
	s.podLister = newTestPodLister(t)
	exporter := recordSpans(t)

	_, err := s.Filter(extenderv1.ExtenderArgs{Pod: pod, NodeNames: &[]string{"node-unknown"}})
	assert.NilError(t, err)
	assert.Equal(t, len(exporter.GetSpans()), 1)
	assert.Equal(t, len(s.filterFailures), 0)
}
