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
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/tracing"
)

// laterFilterTraceparent is the context of a later Filter of the same pod.
const laterFilterTraceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

// bindPod returns a pod for Bind, carrying traceparent when it is not empty.
func bindPod(traceparent string) *corev1.Pod {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-bind", Namespace: "default", UID: "uid-bind"}}
	if traceparent != "" {
		pod.Annotations = map[string]string{tracing.TraceparentAnnotation: traceparent}
	}
	return pod
}

// spanNamed returns the one span called name.
func spanNamed(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	var found []tracetest.SpanStub
	for _, span := range spans {
		if span.Name == name {
			found = append(found, span)
		}
	}
	assert.Equal(t, len(found), 1, "spans named %q", name)
	return found[0]
}

func TestBindSpanOnSuccess(t *testing.T) {
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, bindPod(sampledTraceparent), &bindLockMockDevice{})
	defer cleanup()
	exporter := recordSpans(t)

	res, err := s.Bind(args)
	assert.NilError(t, err)
	assert.Equal(t, res.Error, "")

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 4)
	bind := spanNamed(t, spans, "bind")
	assert.Equal(t, bind.Status.Code, codes.Unset)
	assert.Equal(t, bind.Parent.TraceID().String(), "4bf92f3577b34da6a3ce929d0e0e4736")
	assert.Equal(t, bind.Parent.SpanID().String(), "00f067aa0ba902b7")
	attrs := spanAttributes(bind)
	assert.Equal(t, attrs[tracing.PodUIDKey].AsString(), "uid-bind")
	assert.Equal(t, attrs[tracing.PodNameKey].AsString(), "pod-bind")
	assert.Equal(t, attrs[tracing.NamespaceKey].AsString(), "default")
	assert.Equal(t, attrs[tracing.NodeKey].AsString(), "node1")
	for _, name := range []string{"lock node", "patch pod", "bind pod"} {
		child := spanNamed(t, spans, name)
		assert.Equal(t, child.Parent.SpanID(), bind.SpanContext.SpanID(), "parent of %q", name)
		assert.Equal(t, child.Status.Code, codes.Unset, "status of %q", name)
	}
}

func TestBindSpanPrefersMemoryOverAnnotation(t *testing.T) {
	// The informer still shows an earlier Filter's context; the latest Filter
	// kept its own in memory.
	pod := bindPod(sampledTraceparent)
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, pod, &bindLockMockDevice{})
	defer cleanup()
	s.podManager.AddPod(pod, "node1", nil)
	s.podManager.SetTraceparent(pod, laterFilterTraceparent)
	exporter := recordSpans(t)

	_, err := s.Bind(args)
	assert.NilError(t, err)

	bind := spanNamed(t, exporter.GetSpans(), "bind")
	assert.Equal(t, bind.Parent.TraceID().String(), "0af7651916cd43dd8448eb211c80319c")
	assert.Equal(t, bind.Parent.SpanID().String(), "b7ad6b7169203331")
}

func TestBindSpanWithoutParent(t *testing.T) {
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, bindPod(""), &bindLockMockDevice{})
	defer cleanup()
	exporter := recordSpans(t)

	_, err := s.Bind(args)
	assert.NilError(t, err)

	bind := spanNamed(t, exporter.GetSpans(), "bind")
	assert.Assert(t, !bind.Parent.IsValid())
}

func TestBindSpanOnLockFailure(t *testing.T) {
	mock := &bindLockMockDevice{lockErr: errContention, lockErrOnce: true}
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, bindPod(sampledTraceparent), mock)
	defer cleanup()
	exporter := recordSpans(t)

	res, err := s.Bind(args)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(res.Error, "node lock contention"), "bind result: %q", res.Error)

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 2)
	bind := spanNamed(t, spans, "bind")
	assert.Equal(t, bind.Status.Code, codes.Error)
	assert.Equal(t, spanAttributes(bind)[tracing.FailureReasonKey].AsString(), "lock")
	assert.Equal(t, len(bind.Events), 1)
	assert.Equal(t, bind.Events[0].Name, "reservation released")

	// The step that failed is an error too, without the metric reason.
	lock := spanNamed(t, spans, "lock node")
	assert.Equal(t, lock.Status.Code, codes.Error)
	_, found := spanAttributes(lock)[tracing.FailureReasonKey]
	assert.Assert(t, !found)
}

func TestBindSpanOnPatchFailure(t *testing.T) {
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, bindPod(sampledTraceparent), &bindLockMockDevice{})
	defer cleanup()
	kubeClient, ok := s.kubeClient.(*fake.Clientset)
	assert.Assert(t, ok)
	kubeClient.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected patch error")
	})
	exporter := recordSpans(t)

	_, err := s.Bind(args)
	assert.NilError(t, err)

	spans := exporter.GetSpans()
	bind := spanNamed(t, spans, "bind")
	assert.Equal(t, bind.Status.Code, codes.Error)
	assert.Equal(t, spanAttributes(bind)[tracing.FailureReasonKey].AsString(), "annotation_patch")
	assert.Equal(t, spanNamed(t, spans, "patch pod").Status.Code, codes.Error)
}

func TestBindSpanOnLookupFailure(t *testing.T) {
	s, args, cleanup := setupBindLockRetryTest(t, 5*time.Second, bindPod(sampledTraceparent), &bindLockMockDevice{})
	defer cleanup()
	args.PodName = "pod-missing"
	exporter := recordSpans(t)

	_, err := s.Bind(args)
	assert.ErrorContains(t, err, "not found")

	spans := exporter.GetSpans()
	assert.Equal(t, len(spans), 1)
	assert.Equal(t, spans[0].Status.Code, codes.Error)
	assert.Equal(t, spanAttributes(spans[0])[tracing.FailureReasonKey].AsString(), "lookup")
}
