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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

const template = "Processing admission hook for pod %v/%v, UID: %v"

// serviceAccountPrefix starts the username the API server reports for a
// service account, as opposed to a person or an external client.
const serviceAccountPrefix = "system:serviceaccount:"

type webhook struct {
	decoder admission.Decoder
}

func NewWebHook() (*admission.Webhook, error) {
	logf.SetLogger(klog.NewKlogr())
	schema := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(schema); err != nil {
		return nil, err
	}
	decoder := admission.NewDecoder(schema)
	wh := &admission.Webhook{Handler: &webhook{decoder: decoder}}
	return wh, nil
}

func (h *webhook) Handle(_ context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	err := h.decoder.Decode(req, pod)
	if err != nil {
		klog.Errorf("Failed to decode request: %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	if req.Operation == admissionv1.Update {
		return h.handleUpdate(req, pod)
	}
	if len(pod.Spec.Containers) == 0 {
		klog.Warningf(template+" - Denying admission as pod has no containers", pod.Namespace, pod.Name, pod.UID)
		return admission.Denied("pod has no containers")
	}
	// This must come before the different-scheduler case below: that path
	// returns without validating anything, and a pod taking it keeps whatever
	// allocation annotations it was created with. The device plugin hands out
	// devices from exactly those annotations, so a pod naming its own GPU and
	// memory would be served them, outside the scheduler's accounting.
	if annotation, found := schedulerOwnedAnnotation(pod); found {
		klog.Warningf(template+" - Denying admission as pod presets %s", pod.Namespace, pod.Name, pod.UID, annotation)
		return admission.Denied(fmt.Sprintf("annotation %s is written by the scheduler and cannot be set when creating a pod", annotation))
	}
	if pod.Spec.SchedulerName != "" &&
		(pod.Spec.SchedulerName != corev1.DefaultSchedulerName || !config.ForceOverwriteDefaultScheduler) &&
		(len(config.SchedulerName) == 0 || pod.Spec.SchedulerName != config.SchedulerName) {
		klog.V(3).Infof(template+" - Pod already has different scheduler assigned", req.Namespace, req.Name, req.UID)
		return admission.Allowed("pod already has different scheduler assigned")
	}
	// Reject invalid numa-alignment values at admission, so a typo surfaces
	// to the user instead of only appearing in a device-plugin log later.
	if _, err := util.GetNumaAlignmentModeByPod(pod); err != nil {
		klog.Warningf(template+" - Denying admission: %v", pod.Namespace, pod.Name, pod.UID, err)
		return admission.Denied(err.Error())
	}
	klog.V(5).Infof(template, pod.Namespace, pod.Name, pod.UID)
	privilegedName, hasPrivileged := privilegedContainerName(pod)
	hasResource := false

	// 1. Process InitContainers
	for idx := range pod.Spec.InitContainers {
		c := &pod.Spec.InitContainers[idx]
		for _, val := range device.GetDevices() {
			found, err := val.MutateAdmission(c, pod)
			if err != nil {
				klog.Errorf("validating pod failed:%s", err.Error())
				return admission.Errored(http.StatusInternalServerError, err)
			}
			hasResource = hasResource || found
		}
	}

	// 2. Process Regular Containers (Keep your existing loop here)
	for idx := range pod.Spec.Containers {
		c := &pod.Spec.Containers[idx]
		for _, val := range device.GetDevices() {
			found, err := val.MutateAdmission(c, pod)
			if err != nil {
				klog.Errorf("validating pod failed:%s", err.Error())
				return admission.Errored(http.StatusInternalServerError, err)
			}
			hasResource = hasResource || found
		}
	}
	// Validate device-scoring weights for HAMi workloads so malformed input is
	// reported before scheduling. The scheduler retains the same validation as
	// a defense-in-depth check.
	if hasResource {
		if _, err := util.GetDeviceScoringWeightsByPod(pod); err != nil {
			klog.Warningf(template+" - Denying admission: %v", pod.Namespace, pod.Name, pod.UID, err)
			return admission.Denied(err.Error())
		}
	}
	if hasPrivileged && hasResource {
		klog.Warningf(template+" - Denying admission as container %s is privileged", pod.Namespace, pod.Name, pod.UID, privilegedName)
		return admission.Denied(fmt.Sprintf("container %s is privileged", privilegedName))
	}

	if !hasResource {
		klog.V(3).Infof(template+" - Allowing admission: no GPU resource found", pod.Namespace, pod.Name, pod.UID)
		//return admission.Allowed("no resource found")
	} else if len(config.SchedulerName) > 0 {
		pod.Spec.SchedulerName = config.SchedulerName
		if pod.Spec.NodeName != "" {
			klog.Infof(template+" - Pod already has node assigned", pod.Namespace, pod.Name, pod.UID)
			return admission.Denied("pod has node assigned")
		}
	}
	if !fitResourceQuota(pod) {
		return admission.Denied("exceeding resource quota")
	}
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		klog.Errorf(template+" - Failed to marshal pod, error: %v", pod.Namespace, pod.Name, pod.UID, err)
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

// handleUpdate guards the same annotations on an existing pod. Denying them at
// create closes only half the door: the keys can be patched in afterward by
// anyone holding update on pods, and the device plugin hands out devices from
// whatever they name, so the pod is served memory and cores the scheduler never
// accounted for. An update that leaves those keys alone is not ours to judge
// and passes through as-is, which also keeps the create path's mutation off an
// existing pod, whose schedulerName the API server will not let us change.
func (h *webhook) handleUpdate(req admission.Request, pod *corev1.Pod) admission.Response {
	oldPod := &corev1.Pod{}
	if err := h.decoder.DecodeRaw(req.OldObject, oldPod); err != nil {
		klog.Errorf("Failed to decode old object: %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	annotation, changed := changedSchedulerOwnedAnnotation(oldPod, pod)
	if !changed {
		return admission.Allowed("no scheduler-owned annotation changed")
	}
	// HAMi rewrites these itself once a pod is admitted: the scheduler patches
	// them as it filters and binds, and a device plugin rewrites them as each
	// container gets its devices. Those writers are service accounts and they
	// only ever touch a pod this scheduler was asked to place, so a request
	// failing either test is not one of them. A vendor's device plugin can run
	// in its own namespace, which is why the account itself is not pinned here.
	if !strings.HasPrefix(req.UserInfo.Username, serviceAccountPrefix) {
		return denySchedulerOwnedAnnotation(pod, req.UserInfo.Username, annotation)
	}
	if len(config.SchedulerName) > 0 && pod.Spec.SchedulerName != config.SchedulerName {
		return denySchedulerOwnedAnnotation(pod, req.UserInfo.Username, annotation)
	}
	return admission.Allowed("scheduler-owned annotation changed by a HAMi component")
}

func denySchedulerOwnedAnnotation(pod *corev1.Pod, username, annotation string) admission.Response {
	klog.Warningf(template+" - Denying update as %s writes %s", pod.Namespace, pod.Name, pod.UID, username, annotation)
	return admission.Denied(fmt.Sprintf("annotation %s is written by the scheduler and cannot be set by %s", annotation, username))
}

// schedulerOwnedAnnotationKeys lists the annotations the scheduler writes after
// a pod is admitted, together with the per-device keys each backend fills in.
func schedulerOwnedAnnotationKeys() []string {
	keys := []string{
		util.AssignedNodeAnnotations,
		util.BindTimeAnnotations,
		util.DeviceBindPhase,
	}
	for _, perDevice := range []map[string]string{device.InRequestDevices, device.SupportDevices} {
		for _, key := range perDevice {
			keys = append(keys, key)
		}
	}
	return keys
}

// schedulerOwnedAnnotation reports an annotation the scheduler writes after a
// pod is admitted. A pod that already carries one at create did not get it from
// the scheduler: it was either written by hand or copied from a scheduled pod's
// manifest, and in both cases the device plugin would act on it as though the
// scheduler had decided it.
func schedulerOwnedAnnotation(pod *corev1.Pod) (string, bool) {
	for _, key := range schedulerOwnedAnnotationKeys() {
		if _, ok := pod.Annotations[key]; ok {
			return key, true
		}
	}
	return "", false
}

// changedSchedulerOwnedAnnotation reports a scheduler-owned annotation the
// update adds, rewrites or drops.
func changedSchedulerOwnedAnnotation(oldPod, newPod *corev1.Pod) (string, bool) {
	for _, key := range schedulerOwnedAnnotationKeys() {
		if oldPod.Annotations[key] != newPod.Annotations[key] {
			return key, true
		}
	}
	return "", false
}

func privilegedContainerName(pod *corev1.Pod) (string, bool) {
	for _, ctr := range pod.Spec.InitContainers {
		if isPrivilegedContainer(&ctr) {
			return ctr.Name, true
		}
	}
	for _, ctr := range pod.Spec.Containers {
		if isPrivilegedContainer(&ctr) {
			return ctr.Name, true
		}
	}
	return "", false
}

func isPrivilegedContainer(ctr *corev1.Container) bool {
	return ctr.SecurityContext != nil &&
		ctr.SecurityContext.Privileged != nil &&
		*ctr.SecurityContext.Privileged
}

func fitResourceQuota(pod *corev1.Pod) bool {
	for deviceName, dev := range device.GetDevices() {
		resourceNames := dev.GetResourceNames()
		if len(resourceNames.ResourceMemoryName) == 0 && len(resourceNames.ResourceCoreName) == 0 {
			continue
		}

		// Ask the backend what the pod is requesting rather than reading the
		// container spec here. It applies its own memory factor, defaults and
		// template rounding, which is what the scheduler later records as used,
		// so this keeps admission and the scheduler on the same numbers.
		var appMemoryReq, appCoresReq int64
		for i := range pod.Spec.Containers {
			req := dev.GenerateResourceRequests(&pod.Spec.Containers[i])
			if req.Nums == 0 {
				continue
			}
			appMemoryReq += int64(req.Memreq) * int64(req.Nums)
			appCoresReq += int64(req.Coresreq) * int64(req.Nums)
		}

		var initPeakMemoryReq, initPeakCoresReq int64
		var sidecarMemoryReq, sidecarCoresReq int64
		for i := range pod.Spec.InitContainers {
			c := &pod.Spec.InitContainers[i]
			req := dev.GenerateResourceRequests(c)
			if req.Nums == 0 {
				continue
			}
			mem := int64(req.Memreq) * int64(req.Nums)
			cores := int64(req.Coresreq) * int64(req.Nums)
			if util.IsSidecarContainer(c) {
				sidecarMemoryReq += mem
				sidecarCoresReq += cores
				initPeakMemoryReq = max(initPeakMemoryReq, sidecarMemoryReq)
				initPeakCoresReq = max(initPeakCoresReq, sidecarCoresReq)
				continue
			}
			initPeakMemoryReq = max(initPeakMemoryReq, sidecarMemoryReq+mem)
			initPeakCoresReq = max(initPeakCoresReq, sidecarCoresReq+cores)
		}

		memoryReq := max(initPeakMemoryReq, sidecarMemoryReq+appMemoryReq)
		coresReq := max(initPeakCoresReq, sidecarCoresReq+appCoresReq)
		if memoryReq == 0 && coresReq == 0 {
			continue
		}

		klog.V(5).Infof("Checking quota for device %s: memory %d, cores %d, factor %d", deviceName, memoryReq, coresReq, resourceNames.MemoryFactor)
		if !device.GetLocalCache().FitQuota(pod.Namespace, memoryReq, resourceNames.MemoryFactor, coresReq, deviceName) {
			klog.Infof(template+" - Denying admission", pod.Namespace, pod.Name, pod.UID)
			return false
		}
	}
	return true
}
