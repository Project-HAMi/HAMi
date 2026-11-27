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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/tracing"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

// filterTraceparent is the context Filter wrote to the pod.
const filterTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

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
		require.NoError(t, tp.Shutdown(context.Background()))
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

// allocatePod returns a pod with one container on GPU-aaa, carrying
// traceparent when it is not empty.
func allocatePod(traceparent string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "GPU-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c0"}},
		},
	}
	if traceparent != "" {
		pod.Annotations[tracing.TraceparentAnnotation] = traceparent
	}
	return pod
}

func TestAllocateSpan(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	t.Setenv(util.NodeNameEnvName, "node-a")
	pod := allocatePod(filterTraceparent)
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)
	exporter := recordSpans(t)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	}
	_, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	span := spans[0]
	require.Equal(t, "allocate", span.Name)
	require.Equal(t, codes.Unset, span.Status.Code)
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", span.Parent.TraceID().String())
	require.Equal(t, "00f067aa0ba902b7", span.Parent.SpanID().String())
	attrs := spanAttributes(span)
	require.Equal(t, "pod-uid", attrs[tracing.PodUIDKey].AsString())
	require.Equal(t, "test-pod", attrs[tracing.PodNameKey].AsString())
	require.Equal(t, "default", attrs[tracing.NamespaceKey].AsString())
	require.Equal(t, "node-a", attrs[tracing.NodeKey].AsString())
	require.Equal(t, nvidia.NvidiaGPUDevice, attrs[tracing.DeviceTypeKey].AsString())
	require.Equal(t, []string{"GPU-aaa"}, attrs[tracing.DeviceUUIDsKey].AsStringSlice())
}

func TestAllocateSpanWithoutFilterContext(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	pod := allocatePod("")
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)
	exporter := recordSpans(t)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	}
	_, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.False(t, spans[0].Parent.IsValid())
}

func TestAllocateNoSpanWithoutPendingPod(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	prevGetPendingPod := getPendingPod
	getPendingPod = func(context.Context, string) (*corev1.Pod, error) {
		return nil, errors.New("pod not found")
	}
	t.Cleanup(func() { getPendingPod = prevGetPendingPod })
	exporter := recordSpans(t)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0"}},
		},
	}
	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)

	require.Empty(t, exporter.GetSpans())
}

func TestAllocateSpanOnFailure(t *testing.T) {
	setupInRequestDevices(t)
	plugin := newTestPlugin(t)
	pod := allocatePod(filterTraceparent)
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)
	exporter := recordSpans(t)

	// The pod's annotation names one device, the request two.
	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"GPU-aaa-0", "GPU-aaa-1"}},
		},
	}
	_, err := plugin.Allocate(context.Background(), request)
	require.Error(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status.Code)
	require.EqualError(t, err, spans[0].Status.Description)
	require.NotContains(t, spanAttributes(spans[0]), tracing.DeviceUUIDsKey)
}

// With time-slicing, kubelet requests MIG device replicas.
// The span records the device UUID without the replica suffix.
func TestAllocateSpanMIGReplicaUUID(t *testing.T) {
	setupInRequestDevices(t)
	migDevice := &rm.Device{}
	migDevice.ID = "MIG-aaa::1"
	plugin := newTestPluginWithRM(t, map[string]*rm.Device{"MIG-aaa::1": migDevice})
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mig-pod", Namespace: "default", UID: "mig-uid",
			Annotations: map[string]string{
				"hami.io/vgpu-devices-to-allocate": "MIG-aaa,NVIDIA,3000,50:;",
			},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c0"}}},
	}
	setupFakeClient(t, pod)
	mockAllocateGlobals(t, pod)
	exporter := recordSpans(t)

	request := &kubeletdevicepluginv1beta1.AllocateRequest{
		ContainerRequests: []*kubeletdevicepluginv1beta1.ContainerAllocateRequest{
			{DevicesIds: []string{"MIG-aaa::1"}},
		},
	}
	_, err := plugin.Allocate(context.Background(), request)
	require.NoError(t, err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, []string{"MIG-aaa"}, spanAttributes(spans[0])[tracing.DeviceUUIDsKey].AsStringSlice())
}
