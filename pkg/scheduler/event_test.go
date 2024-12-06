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
		fakeClient := fake.NewSimpleClientset()
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
				for i := 0; i < 5; i++ {
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
					assert.Equal(t, event.Type, corev1.EventTypeWarning)
				} else {
					assert.Equal(t, event.Type, corev1.EventTypeNormal)
				}
			} else {
				events, err = fakeClient.CoreV1().Events(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
				assert.Equal(t, len(events.Items), 0)
			}
		})
	}
}

func TestRecordScheduleFilterResultEvent(t *testing.T) {
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
		fakeClient := fake.NewSimpleClientset()
		eventBroadcaster := record.NewBroadcaster()
		eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: fakeClient.CoreV1().Events(metav1.NamespaceAll)})

		s := &Scheduler{
			kubeClient:    fakeClient,
			eventRecorder: eventBroadcaster.NewRecorder(runtime.NewScheme(), corev1.EventSource{Component: "test-fake-scheduler"}),
		}

		t.Run(test.name, func(t *testing.T) {
			s.recordScheduleFilterResultEvent(test.pod, test.eventReason, test.nodeResult, test.schedulerErr)

			var events *corev1.EventList
			var err error

			if test.pod != nil {
				for i := 0; i < 5; i++ {
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
					assert.Equal(t, event.Type, corev1.EventTypeWarning)
				} else {
					assert.Equal(t, event.Type, corev1.EventTypeNormal)
				}
			} else {
				events, err = fakeClient.CoreV1().Events(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
				assert.Equal(t, len(events.Items), 0)
			}
		})
	}
}
