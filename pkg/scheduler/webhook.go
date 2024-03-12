/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package scheduler

import (
	"context"
	"encoding/json"
	"k8s.io/klog/v2"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
)

type webhook struct {
	decoder *admission.Decoder
}

func NewWebHook() (*admission.Webhook, error) {
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
		klog.Warning("Denying admission as pod has no containers")
		return admission.Denied("pod has no containers")
	}
	klog.Infof("Processing hook for pod %v/%v, UID: %v", req.Namespace, req.Name, req.UID)
	hasResource := false
	for idx, ctr := range pod.Spec.Containers {
		c := &pod.Spec.Containers[idx]
		if ctr.SecurityContext != nil {
			if ctr.SecurityContext.Privileged != nil && *ctr.SecurityContext.Privileged {
				klog.Infof("Denying admission as container %s is privileged", c.Name)
				continue
			}
		}
		for _, val := range device.GetDevices() {
			hasResource = hasResource || val.MutateAdmission(c)
		}
	}

	if !hasResource {
		klog.Infof("Allowing admission for pod: %v/%v", req.Namespace, req.Name)
		return admission.Allowed("no resource found")
	}
	if len(config.SchedulerName) > 0 {
		pod.Spec.SchedulerName = config.SchedulerName
	}
	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}
