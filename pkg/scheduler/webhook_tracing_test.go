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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"gotest.tools/v3/assert"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Project-HAMi/HAMi/pkg/tracing"
)

// Only HAMi components may change the trace context after a pod is created,
// so a user cannot attach a pod to another trace or force it to be sampled.
func TestTraceparentUpdateDenied(t *testing.T) {
	initHAMiScheduler(t)
	allowNodeWriters(t)
	key := tracing.TraceparentAnnotation

	tests := []struct {
		name   string
		before map[string]string
		after  map[string]string
	}{
		{name: "add", after: map[string]string{key: sampledTraceparent}},
		{name: "rewrite", before: map[string]string{key: notSampledTraceparent}, after: map[string]string{key: sampledTraceparent}},
		{name: "drop", before: map[string]string{key: sampledTraceparent}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPod := scheduledGPUPod("hami-scheduler", tt.before)
			newPod := scheduledGPUPod("hami-scheduler", tt.after)
			resp := updatePod(t, oldPod, newPod, "kubernetes-admin")
			assert.Assert(t, !resp.Allowed, "%s of %s was allowed", tt.name, key)
			assert.Check(t, strings.Contains(resp.Result.Message, key), "denial message %q does not name %s", resp.Result.Message, key)
		})
	}
}

// Filter writes the trace context in the patch that places the pod.
func TestTraceparentUpdateBySchedulerAllowed(t *testing.T) {
	initHAMiScheduler(t)
	allowNodeWriters(t, "system:serviceaccount:hami-system:hami-scheduler")

	oldPod := scheduledGPUPod("hami-scheduler", nil)
	newPod := scheduledGPUPod("hami-scheduler", map[string]string{tracing.TraceparentAnnotation: sampledTraceparent})
	resp := updatePod(t, oldPod, newPod, "system:serviceaccount:hami-system:hami-scheduler")
	assert.Assert(t, resp.Allowed, "the scheduler's own patch was denied: %v", resp.Result)
}

// A trace context a pod arrives with is never a reason to reject the pod.
// This pod goes to another scheduler, so the webhook leaves it as is.
func TestPodWithTraceparentAdmitted(t *testing.T) {
	initHAMiScheduler(t)

	resp := admitPod(t, gpuPodWithAnnotations(map[string]string{tracing.TraceparentAnnotation: sampledTraceparent}))
	assert.Assert(t, resp.Allowed, "a pod carrying %s was denied: %v", tracing.TraceparentAnnotation, resp.Result)
}

// admissionGPUPod returns a pod that asks for one HAMi GPU and has no
// scheduler set yet, so the webhook mutates it.
func admissionGPUPod(annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "train-1", Namespace: "ml", Annotations: annotations},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "main",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{"hami.io/gpu": resource.MustParse("1")},
			},
		}}},
	}
}

// admitted applies the webhook's patch to pod and returns the result.
func admitted(t *testing.T, pod *corev1.Pod, resp admission.Response) *corev1.Pod {
	t.Helper()

	rawPatch, err := json.Marshal(resp.Patches)
	assert.NilError(t, err)
	patch, err := jsonpatch.DecodePatch(rawPatch)
	assert.NilError(t, err)
	rawPod, err := json.Marshal(pod)
	assert.NilError(t, err)
	patched, err := patch.Apply(rawPod)
	assert.NilError(t, err)
	var result corev1.Pod
	assert.NilError(t, json.Unmarshal(patched, &result))
	return &result
}

func TestAdmissionSpanWritesTraceparent(t *testing.T) {
	initHAMiScheduler(t)
	exporter := recordSpans(t)
	pod := admissionGPUPod(nil)

	resp := admitPod(t, pod)
	assert.Assert(t, resp.Allowed, resp.Result)

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	span := spans[0]
	assert.Equal(t, span.Name, "admission")
	assert.Assert(t, !span.Parent.IsValid())
	attrs := spanAttributes(span)
	assert.Equal(t, attrs[tracing.NamespaceKey].AsString(), "ml")
	assert.Equal(t, attrs[tracing.PodNameKey].AsString(), "train-1")
	// kube-apiserver sets the UID only after the webhook has run.
	_, found := attrs[tracing.PodUIDKey]
	assert.Assert(t, !found)

	want := fmt.Sprintf("00-%s-%s-01", span.SpanContext.TraceID(), span.SpanContext.SpanID())
	assert.Equal(t, admitted(t, pod, resp).Annotations[tracing.TraceparentAnnotation], want)
}

// An unsampled pod still gets the annotation, so Filter can tell "not sampled"
// from "no webhook".
func TestAdmissionWritesUnsampledTraceparent(t *testing.T) {
	initHAMiScheduler(t)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	global := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(global)
		assert.NilError(t, tp.Shutdown(context.Background()))
	})
	pod := admissionGPUPod(nil)

	resp := admitPod(t, pod)
	assert.Assert(t, resp.Allowed, resp.Result)

	got := admitted(t, pod, resp).Annotations[tracing.TraceparentAnnotation]
	assert.Assert(t, strings.HasSuffix(got, "-00"), "traceparent %q", got)
}

// kube-apiserver sends its trace context in the traceparent header.
func TestAdmissionSpanFollowsAPIServerParent(t *testing.T) {
	initHAMiScheduler(t)
	exporter := recordSpans(t)

	scheme := runtime.NewScheme()
	assert.NilError(t, corev1.AddToScheme(scheme))
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)
	podBytes, err := runtime.Encode(codec, admissionGPUPod(nil))
	assert.NilError(t, err)
	review, err := json.Marshal(admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
		Request: &admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Namespace: "ml",
			Name:      "train-1",
			Object:    runtime.RawExtension{Raw: podBytes},
		},
	})
	assert.NilError(t, err)

	wh, err := NewWebHook()
	assert.NilError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(review))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", sampledTraceparent)
	wh.ServeHTTP(httptest.NewRecorder(), req)

	span := spanNamed(t, exporter.GetSpans(), "admission")
	assert.Equal(t, span.Parent.TraceID().String(), "4bf92f3577b34da6a3ce929d0e0e4736")
	assert.Equal(t, span.Parent.SpanID().String(), "00f067aa0ba902b7")
}

// With tracing off, a trace context the pod arrives with is still removed, so
// it cannot be picked up once tracing is turned on.
func TestAdmissionStripsTraceparent(t *testing.T) {
	initHAMiScheduler(t)

	for _, value := range []string{sampledTraceparent, ""} {
		t.Run(fmt.Sprintf("value %q", value), func(t *testing.T) {
			pod := admissionGPUPod(map[string]string{tracing.TraceparentAnnotation: value})

			resp := admitPod(t, pod)
			assert.Assert(t, resp.Allowed, resp.Result)

			_, found := admitted(t, pod, resp).Annotations[tracing.TraceparentAnnotation]
			assert.Assert(t, !found)
		})
	}
}

func TestAdmissionSpanOnDenied(t *testing.T) {
	initHAMiScheduler(t)
	exporter := recordSpans(t)
	pod := admissionGPUPod(nil)
	privileged := true
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: &privileged}

	resp := admitPod(t, pod)
	assert.Assert(t, !resp.Allowed)

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	assert.Equal(t, spans[0].Status.Code, codes.Error)
	assert.Equal(t, spans[0].Status.Description, "container main is privileged")
}

func TestAdmissionSkipsPodWithoutDevices(t *testing.T) {
	initHAMiScheduler(t)
	exporter := recordSpans(t)
	pod := admissionGPUPod(map[string]string{tracing.TraceparentAnnotation: sampledTraceparent})
	pod.Spec.Containers[0].Resources = corev1.ResourceRequirements{}

	resp := admitPod(t, pod)
	assert.Assert(t, resp.Allowed, resp.Result)

	assert.Equal(t, len(exporter.GetSpans()), 0)
	_, found := admitted(t, pod, resp).Annotations[tracing.TraceparentAnnotation]
	assert.Assert(t, !found)
}

// A pod created by a controller has only a generateName prefix at admission.
func TestAdmissionSpanUsesGenerateName(t *testing.T) {
	initHAMiScheduler(t)
	exporter := recordSpans(t)
	pod := admissionGPUPod(nil)
	pod.Name, pod.GenerateName = "", "train-"

	resp := admitPod(t, pod)
	assert.Assert(t, resp.Allowed, resp.Result)

	span := spanNamed(t, exporter.GetSpans(), "admission")
	assert.Equal(t, spanAttributes(span)[tracing.PodNameKey].AsString(), "train-")
}
