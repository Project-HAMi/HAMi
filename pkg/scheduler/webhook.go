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
	decoder *admission.Decoder
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
		klog.Warningf(template+" - Denying admission as pod has no containers", req.Namespace, req.Name, req.UID)
		return admission.Denied("pod has no containers")
	}
	// First, check if pod has GPU resources to determine processing strategy
	hasGPUResource := false
	for _, ctr := range pod.Spec.Containers {
		for resourceName := range ctr.Resources.Limits {
			if resourceName == "nvidia.com/gpu" || resourceName == "hami.io/gpu" {
				hasGPUResource = true
				break
			}
		}
		if hasGPUResource {
			break
		}
	}

	// Skip processing only if pod explicitly specifies a non-default, non-HAMi scheduler AND has no GPU resources
	// For GPU pods, we need to process them to add appropriate scheduling constraints
	if !hasGPUResource && pod.Spec.SchedulerName != "" &&
		pod.Spec.SchedulerName != "default-scheduler" &&
		(len(config.SchedulerName) == 0 || pod.Spec.SchedulerName != config.SchedulerName) {
		klog.Infof(template+" - Pod has no GPU resources and different scheduler assigned", req.Namespace, req.Name, req.UID)
		return admission.Allowed("pod has no GPU resources and different scheduler assigned")
	}
	klog.Infof(template, req.Namespace, req.Name, req.UID)
	hasResource := false
	for idx, ctr := range pod.Spec.Containers {
		c := &pod.Spec.Containers[idx]
		if ctr.SecurityContext != nil {
			if ctr.SecurityContext.Privileged != nil && *ctr.SecurityContext.Privileged {
				klog.Warningf(template+" - Denying admission as container %s is privileged", req.Namespace, req.Name, req.UID, c.Name)
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
	}

	if !hasResource {
		klog.Infof(template+" - Allowing admission for pod: no resource found", req.Namespace, req.Name, req.UID)
		//return admission.Allowed("no resource found")
	} else {
		// Check if user explicitly wants to skip HAMi scheduler
		skipHAMI := false
		if pod.Annotations != nil {
			if _, exists := pod.Annotations["hami.io/skip-scheduler"]; exists {
				skipHAMI = true
				klog.Infof(template+" - Pod has hami.io/skip-scheduler annotation, will not modify scheduler", req.Namespace, req.Name, req.UID)
			}
		}

		// Determine scheduling strategy based on current scheduler and user preference
		currentScheduler := pod.Spec.SchedulerName
		if currentScheduler == "" {
			currentScheduler = corev1.DefaultSchedulerName
		}

		if !skipHAMI && len(config.SchedulerName) > 0 && currentScheduler == corev1.DefaultSchedulerName {
			// Default behavior: GPU pods with default-scheduler automatically use HAMi scheduler
			pod.Spec.SchedulerName = config.SchedulerName
			klog.Infof(template+" - Pod with GPU resources automatically switched to HAMi scheduler", req.Namespace, req.Name, req.UID)
		} else if currentScheduler != config.SchedulerName {
			// Pod has GPU resources but is using a different scheduler (or user opted out)
			// Add NodeAffinity to avoid HAMi-managed nodes
			klog.Infof(template+" - Pod has GPU resources but using non-HAMi scheduler (%s), adding NodeAffinity to avoid HAMi nodes", req.Namespace, req.Name, req.UID, currentScheduler)

			// Create NodeAffinity to avoid nodes with hami.io/node-nvidia-register annotation
			if pod.Spec.Affinity == nil {
				pod.Spec.Affinity = &corev1.Affinity{}
			}
			if pod.Spec.Affinity.NodeAffinity == nil {
				pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
			}
			if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
				pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
			}

			// Add term to avoid HAMi-managed nodes
			avoidHAMITerm := corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      "hami.io/node-nvidia-register",
						Operator: corev1.NodeSelectorOpDoesNotExist,
					},
				},
			}

			pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms =
				append(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, avoidHAMITerm)
		}

		if pod.Spec.NodeName != "" {
			klog.Infof(template+" - Pod already has node assigned", req.Namespace, req.Name, req.UID)
			return admission.Denied("pod has node assigned")
		}
	}
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		klog.Errorf(template+" - Failed to marshal pod, error: %v", req.Namespace, req.Name, req.UID, err)
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}
