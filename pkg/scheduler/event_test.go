/*
Copyright 2024 The HAMi Authors.

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

func TestRecordAllocationDecodeFailureEvent(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	s := &Scheduler{eventRecorder: recorder}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"}}

	s.recordAllocationDecodeFailureEvent(pod, "node-a", fmt.Errorf("invalid allocation annotation"))

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, corev1.EventTypeWarning)
		assert.Contains(t, event, EventReasonAllocationDecodeFailed)
		assert.Contains(t, event, "node-a")
	case <-time.After(time.Second):
		t.Fatal("expected allocation decode failure event")
	}
}

func TestRecordDeviceBackendHealthTransition(t *testing.T) {
	recorder := record.NewFakeRecorder(4)
	s := &Scheduler{eventRecorder: recorder}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-a",
		Annotations: map[string]string{
			"example.com/token": "sensitive-value",
		},
	}}

	s.recordDeviceBackendHealthTransition(node, "backend-a", true)
	assertNoEvent(t, recorder)

	s.recordDeviceBackendHealthTransition(node, "backend-a", false)
	event := receiveEvent(t, recorder)
	assert.Contains(t, event, corev1.EventTypeWarning)
	assert.Contains(t, event, EventReasonDeviceBackendUnhealthy)
	assert.Contains(t, event, "backend-a")
	assert.Contains(t, event, "node-a")
	assert.NotContains(t, event, "sensitive-value")

	s.recordDeviceBackendHealthTransition(node, "backend-a", false)
	assertNoEvent(t, recorder)

	s.recordDeviceBackendHealthTransition(node, "backend-a", true)
	event = receiveEvent(t, recorder)
	assert.Contains(t, event, corev1.EventTypeNormal)
	assert.Contains(t, event, EventReasonDeviceBackendRecovered)
	assert.Contains(t, event, "backend-a")
	assert.Contains(t, event, "node-a")
	assert.NotContains(t, event, "sensitive-value")

	s.recordDeviceBackendHealthTransition(node, "backend-a", true)
	assertNoEvent(t, recorder)
}

func TestRecordDeviceBackendHealthTransitionTracksBackendsIndependently(t *testing.T) {
	recorder := record.NewFakeRecorder(4)
	s := &Scheduler{eventRecorder: recorder}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}

	s.recordDeviceBackendHealthTransition(node, "backend-a", true)
	s.recordDeviceBackendHealthTransition(node, "backend-b", false)
	assertNoEvent(t, recorder)

	s.recordDeviceBackendHealthTransition(node, "backend-a", false)
	event := receiveEvent(t, recorder)
	assert.Contains(t, event, "backend-a")

	s.recordDeviceBackendHealthTransition(node, "backend-b", false)
	assertNoEvent(t, recorder)

	s.recordDeviceBackendHealthTransition(node, "backend-b", true)
	event = receiveEvent(t, recorder)
	assert.Contains(t, event, "backend-b")
	assert.NotContains(t, event, "backend-a")

	s.recordDeviceBackendHealthTransition(node, "backend-a", false)
	assertNoEvent(t, recorder)
}

func receiveEvent(t *testing.T, recorder *record.FakeRecorder) string {
	t.Helper()
	select {
	case event := <-recorder.Events:
		return event
	case <-time.After(time.Second):
		t.Fatal("expected event")
		return ""
	}
}

func assertNoEvent(t *testing.T, recorder *record.FakeRecorder) {
	t.Helper()
	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected event: %s", event)
	default:
	}
}

func TestRecordScheduleBindingResultEvent(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		eventReason   string
		nodeResult    []string
		schedulerErr  error
		wantEventType string
	}{
		{
			name: "no pod",
			pod:  nil,
		},
		{
			name: "schedule success",
			pod: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind: "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			nodeResult:    []string{"node-1"},
			schedulerErr:  nil,
			wantEventType: corev1.EventTypeNormal,
		},
		{
			name: "schedule failed",
			pod: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind: "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			eventReason:   "FailedBinding",
			schedulerErr:  fmt.Errorf("schedule failed"),
			wantEventType: corev1.EventTypeWarning,
		},
	}
	for _, test := range tests {
		fakeClient := fake.NewClientset()
		eventBroadcaster := record.NewBroadcaster()
		eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: fakeClient.CoreV1().Events(metav1.NamespaceAll)})

		s := &Scheduler{
			kubeClient:    fakeClient,
			eventRecorder: eventBroadcaster.NewRecorder(runtime.NewScheme(), corev1.EventSource{Component: "test-fake-scheduler"}),
		}

		t.Run(test.name, func(t *testing.T) {
			s.recordScheduleBindingResultEvent(test.pod, test.eventReason, test.nodeResult, test.schedulerErr)

			var events *corev1.EventList
			var err error

			if test.pod != nil {
				for range 5 {
					events, err = fakeClient.CoreV1().Events(test.pod.Namespace).List(context.Background(), metav1.ListOptions{})
					if err != nil {
						if len(events.Items) > 0 {
							break
						}
					}
					time.Sleep(100 * time.Millisecond)
				}
				if err != nil {
					t.Errorf("get event list err: %v", err)
				}
				event := events.Items[0]
				if test.schedulerErr != nil {
					assert.Equal(t, corev1.EventTypeWarning, event.Type)
				} else {
					assert.Equal(t, corev1.EventTypeNormal, event.Type)
				}
			} else {
				events, _ = fakeClient.CoreV1().Events(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
				assert.Empty(t, events.Items)
			}
		})
	}
}

func TestRecordScheduleFilterResultEvent(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		eventReason   string
		successMsg    string
		schedulerErr  error
		wantEventType string
	}{
		{
			name: "no pod",
			pod:  nil,
		},
		{
			name: "schedule success",
			pod: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind: "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			successMsg:    "find fit node(node-1)",
			schedulerErr:  nil,
			wantEventType: corev1.EventTypeNormal,
		},
		{
			name: "schedule failed",
			pod: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind: "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			eventReason:   "FailedBinding",
			schedulerErr:  fmt.Errorf("schedule failed"),
			wantEventType: corev1.EventTypeWarning,
		},
	}
	for _, test := range tests {
		fakeClient := fake.NewClientset()
		eventBroadcaster := record.NewBroadcaster()
		eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: fakeClient.CoreV1().Events(metav1.NamespaceAll)})

		s := &Scheduler{
			kubeClient:    fakeClient,
			eventRecorder: eventBroadcaster.NewRecorder(runtime.NewScheme(), corev1.EventSource{Component: "test-fake-scheduler"}),
		}

		t.Run(test.name, func(t *testing.T) {
			s.recordScheduleFilterResultEvent(test.pod, test.eventReason, test.successMsg, test.schedulerErr)

			var events *corev1.EventList
			var err error

			if test.pod != nil {
				for range 5 {
					events, err = fakeClient.CoreV1().Events(test.pod.Namespace).List(context.Background(), metav1.ListOptions{})
					if err != nil {
						if len(events.Items) > 0 {
							break
						}
					}
					time.Sleep(100 * time.Millisecond)
				}
				if err != nil {
					t.Errorf("get event list err: %v", err)
				}
				event := events.Items[0]
				if test.schedulerErr != nil {
					assert.Equal(t, corev1.EventTypeWarning, event.Type)
				} else {
					assert.Equal(t, corev1.EventTypeNormal, event.Type)
				}
			} else {
				events, _ = fakeClient.CoreV1().Events(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
				assert.Empty(t, events.Items)
			}
		})
	}
}
