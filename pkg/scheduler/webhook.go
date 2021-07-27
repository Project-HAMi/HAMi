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
    "fmt"
    "net/http"

    "4pd.io/k8s-vgpu/pkg/api"
    "4pd.io/k8s-vgpu/pkg/k8sutil"
    "4pd.io/k8s-vgpu/pkg/scheduler/config"
    "4pd.io/k8s-vgpu/pkg/util"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/runtime"
    clientgoscheme "k8s.io/client-go/kubernetes/scheme"
    "k8s.io/klog/v2"
    "k8s.io/klog/v2/klogr"
    "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type webhook struct {
    decoder *admission.Decoder
}

func NewWebHook() (*admission.Webhook, error) {
    schema := runtime.NewScheme()
    if err := clientgoscheme.AddToScheme(schema); err != nil {
        return nil, err
    }
    decoder, err := admission.NewDecoder(schema)
    if err != nil {
        return nil, err
    }
    wh := &admission.Webhook{Handler: &webhook{decoder: decoder}}
    _ = wh.InjectLogger(klogr.New())
    return wh, nil
}

func (h *webhook) Handle(_ context.Context, req admission.Request) admission.Response {
    pod := &corev1.Pod{}
    err := h.decoder.Decode(req, pod)
    if err != nil {
        return admission.Errored(http.StatusBadRequest, err)
    }
    if len(pod.Spec.Containers) == 0 {
        return admission.Denied("pod has no containers")
    }
    klog.V(1).Infof("hook %v pod %v/%v", req.UID, req.Namespace, req.Name)
    nums := k8sutil.ResourceNums(pod, corev1.ResourceName(util.ResourceName))
    total := 0
    // use request uid
    uid := req.UID
    for i := 0; i < len(nums); i++ {
        if nums[i] == 0 {
            continue
        }
        total += nums[i]
        c := &pod.Spec.Containers[i]
        c.Env = append(c.Env, corev1.EnvVar{
            Name:  api.ContainerUID,
            Value: fmt.Sprintf("%v/%v", uid, c.Name),
        })
    }
    if total == 0 {
        return admission.Allowed(fmt.Sprintf("no resource %v", util.ResourceName))
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
