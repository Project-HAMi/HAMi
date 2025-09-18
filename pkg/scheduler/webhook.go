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
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

const template = "Processing admission hook for pod %v/%v, UID: %v"

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
	if len(pod.Spec.Containers) == 0 {
		klog.Warningf(template+" - Denying admission as pod has no containers", pod.Namespace, pod.Name, pod.UID)
		return admission.Denied("pod has no containers")
	}
	if pod.Spec.SchedulerName != "" &&
		pod.Spec.SchedulerName != corev1.DefaultSchedulerName || !config.ForceOverwriteDefaultScheduler &&
		(len(config.SchedulerName) == 0 || pod.Spec.SchedulerName != config.SchedulerName) {
		klog.Infof(template+" - Pod already has different scheduler assigned", req.Namespace, req.Name, req.UID)
		return admission.Allowed("pod already has different scheduler assigned")
	}
	klog.Infof(template, pod.Namespace, pod.Name, pod.UID)
	hasResource := false
	for idx, ctr := range pod.Spec.Containers {
		c := &pod.Spec.Containers[idx]
		if ctr.SecurityContext != nil {
			if ctr.SecurityContext.Privileged != nil && *ctr.SecurityContext.Privileged {
				klog.Warningf(template+" - Denying admission as container %s is privileged", pod.Namespace, pod.Name, pod.UID, c.Name)
				continue
			}
		}
		for _, val := range device.GetDevices() {
			found, err := val.MutateAdmission(c, pod)
			if err != nil {
				klog.Errorf("validating pod failed:%s", err.Error())
				return admission.Errored(http.StatusInternalServerError, err)
			}
			hasResource = hasResource || found
		}
		ensureNvidiaExclusiveCoreDefault(c)
	}

	if !hasResource {
		klog.Infof(template+" - Allowing admission for pod: no resource found", pod.Namespace, pod.Name, pod.UID)
		//return admission.Allowed("no resource found")
	} else if len(config.SchedulerName) > 0 {
		pod.Spec.SchedulerName = config.SchedulerName
		if pod.Spec.NodeName != "" {
			klog.Infof(template+" - Pod already has node assigned", pod.Namespace, pod.Name, pod.UID)
			return admission.Denied("pod has node assigned")
		}
	}
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		klog.Errorf(template+" - Failed to marshal pod, error: %v", pod.Namespace, pod.Name, pod.UID, err)
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func ensureNvidiaExclusiveCoreDefault(ctr *corev1.Container) {
	if ctr == nil {
		return
	}

	gpuResourceName := config.NvidiaResourceCountName
	if gpuResourceName == "" {
		return
	}

	gpuResource := corev1.ResourceName(gpuResourceName)
	if !resourcePresent(ctr, gpuResource) {
		return
	}

	coreResourceName := config.NvidiaResourceCoreName
	if coreResourceName == "" {
		return
	}

	coreResource := corev1.ResourceName(coreResourceName)
	if resourcePresent(ctr, coreResource) {
		return
	}

	exclusive := false
	if pct, ok := resourceValue(ctr, corev1.ResourceName(config.NvidiaResourceMemoryPercentageName)); ok {
		exclusive = pct == 100
	} else if config.NvidiaResourceMemoryName == "" {
		exclusive = true
	} else if _, ok := resourceValue(ctr, corev1.ResourceName(config.NvidiaResourceMemoryName)); !ok {
		exclusive = true
	}

	if !exclusive {
		return
	}

	if ctr.Resources.Limits == nil {
		ctr.Resources.Limits = corev1.ResourceList{}
	}
	ctr.Resources.Limits[coreResource] = *resource.NewQuantity(100, resource.BinarySI)
}

func resourceValue(ctr *corev1.Container, name corev1.ResourceName) (int64, bool) {
	if name == "" {
		return 0, false
	}
	if qty, ok := ctr.Resources.Limits[name]; ok {
		return qty.Value(), true
	}
	if qty, ok := ctr.Resources.Requests[name]; ok {
		return qty.Value(), true
	}
	return 0, false
}

func resourcePresent(ctr *corev1.Container, name corev1.ResourceName) bool {
	if ctr == nil || name == "" {
		return false
	}
	if _, ok := ctr.Resources.Limits[name]; ok {
		return true
	}
	if _, ok := ctr.Resources.Requests[name]; ok {
		return true
	}
	return false
}
