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
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

// Define events for ResourceBinding, ResourceFilter objects and their associated resources.
const (
	// EventReasonFilteringFailed indicates that filtering failed.
	EventReasonFilteringFailed = "FilteringFailed"
	// EventReasonFilteringSucceed indicates that filtering succeed.
	EventReasonFilteringSucceed = "FilteringSucceed"

	// EventReasonBindingFailed indicates that  binding failed.
	EventReasonBindingFailed = "BindingFailed"
	// EventReasonBindingSucceed indicates that  binding succeed.
	EventReasonBindingSucceed = "BindingSucceed"

	// EventReasonNumaRefitFailed indicates that a NUMA refit failed.
	EventReasonNumaRefitFailed = "NumaRefitFailed"
	// EventReasonNumaRefitSucceed indicates that a NUMA refit succeeded.
	EventReasonNumaRefitSucceed = "NumaRefitSucceed"

	// EventReasonAllocationDecodeFailed indicates that a live Pod allocation
	// annotation could not be decoded during scheduler accounting.
	EventReasonAllocationDecodeFailed = "AllocationDecodeFailed"

	// EventReasonDeviceBackendUnhealthy indicates that a device backend became unhealthy.
	EventReasonDeviceBackendUnhealthy = "DeviceBackendUnhealthy"
	// EventReasonDeviceBackendRecovered indicates that a device backend recovered.
	EventReasonDeviceBackendRecovered = "DeviceBackendRecovered"
)

func (s *Scheduler) addAllEventHandlers() {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: s.kubeClient.CoreV1().Events(metav1.NamespaceAll)})
	schema := runtime.NewScheme()

	_ = clientgoscheme.AddToScheme(schema)
	s.eventRecorder = eventBroadcaster.NewRecorder(schema, corev1.EventSource{Component: config.SchedulerName})
}

func (s *Scheduler) recordScheduleBindingResultEvent(pod *corev1.Pod, eventReason string, nodeResult []string, schedulerErr error) {
	if pod == nil {
		return
	}
	if schedulerErr == nil {
		successMsg := fmt.Sprintf("Successfully binding node %v to %v/%v", nodeResult, pod.Namespace, pod.Name)
		s.eventRecorder.Event(pod, corev1.EventTypeNormal, eventReason, successMsg)
	} else {
		s.eventRecorder.Event(pod, corev1.EventTypeWarning, eventReason, schedulerErr.Error())
	}
}

// recordNumaRefitResultEvent emits the outcome of a NUMA refit on the pod.
func (s *Scheduler) recordNumaRefitResultEvent(pod *corev1.Pod, successMsg string, refitErr error) {
	// eventRecorder maybe  nil
	if pod == nil || s.eventRecorder == nil {
		return
	}
	if refitErr == nil {
		s.eventRecorder.Event(pod, corev1.EventTypeNormal, EventReasonNumaRefitSucceed, successMsg)
	} else {
		s.eventRecorder.Event(pod, corev1.EventTypeWarning, EventReasonNumaRefitFailed, refitErr.Error())
	}
}

// recordAllocationDecodeFailureEvent reports an allocation that could not be
// reconstructed without changing candidate-node selection.
func (s *Scheduler) recordAllocationDecodeFailureEvent(pod *corev1.Pod, nodeName string, decodeErr error) {
	if pod == nil || s.eventRecorder == nil || decodeErr == nil {
		return
	}
	s.eventRecorder.Eventf(pod, corev1.EventTypeWarning, EventReasonAllocationDecodeFailed,
		"Failed to decode HAMi allocated-device annotations for node %q: %v", nodeName, decodeErr)
}

// recordDeviceBackendHealthTransition records the first observation as a
// baseline, then emits a Node event only when the backend's health changes.
// The caller must hold s.lock while updating backendHealth.
func (s *Scheduler) recordDeviceBackendHealthTransition(node *corev1.Node, backend string, healthy bool) {
	if node == nil || backend == "" {
		return
	}
	if s.backendHealth == nil {
		s.backendHealth = make(map[string]map[string]bool)
	}
	nodeHealth, ok := s.backendHealth[node.Name]
	if !ok {
		nodeHealth = make(map[string]bool)
		s.backendHealth[node.Name] = nodeHealth
	}
	previousHealth, observed := nodeHealth[backend]
	nodeHealth[backend] = healthy
	if !observed || previousHealth == healthy || s.eventRecorder == nil {
		return
	}

	if healthy {
		s.eventRecorder.Eventf(node, corev1.EventTypeNormal, EventReasonDeviceBackendRecovered,
			"Device backend %q on node %q recovered and is healthy", backend, node.Name)
		return
	}
	s.eventRecorder.Eventf(node, corev1.EventTypeWarning, EventReasonDeviceBackendUnhealthy,
		"Device backend %q on node %q is unhealthy", backend, node.Name)
}

func (s *Scheduler) recordScheduleFilterResultEvent(pod *corev1.Pod, eventReason string, successMsg string, schedulerErr error) {
	// eventRecorder maybe  nil
	if pod == nil || s.eventRecorder == nil {
		return
	}
	if schedulerErr == nil {
		s.eventRecorder.Event(pod, corev1.EventTypeNormal, eventReason, successMsg)
	} else {
		s.eventRecorder.Event(pod, corev1.EventTypeWarning, eventReason, schedulerErr.Error())
	}
}
