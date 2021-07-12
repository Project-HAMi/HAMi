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

package device_plugin

import (
    "4pd.io/k8s-vgpu/pkg/device-plugin/config"
    "4pd.io/k8s-vgpu/pkg/k8sutil"
    "4pd.io/k8s-vgpu/pkg/util"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/fields"
    "k8s.io/apimachinery/pkg/labels"
    "k8s.io/client-go/informers"
    "k8s.io/client-go/kubernetes"
    listerscorev1 "k8s.io/client-go/listers/core/v1"
    "k8s.io/klog/v2"
    "strconv"
    "strings"
    "time"
)

type PodManager struct {
    kubeClient kubernetes.Interface
    podLister  listerscorev1.PodLister
    stopCh chan struct{}
}

func (m *PodManager) Start() {
    m.stopCh = make(chan struct{})
    kubeClient, err := k8sutil.NewClient()
    check(err)
    m.kubeClient = kubeClient
    selector := fields.SelectorFromSet(fields.Set{"spec.nodeName": config.NodeName, "status.phase": "Pending"})
    informerFactory := informers.NewSharedInformerFactoryWithOptions(
        m.kubeClient,
        time.Hour*1,
        informers.WithTweakListOptions(func(options *metav1.ListOptions) {
            options.FieldSelector = selector.String()
        }))
    m.podLister = informerFactory.Core().V1().Pods().Lister()
    informerFactory.Start(m.stopCh)
}

func (m *PodManager) Stop() {
    close(m.stopCh)
}

func resourceEqual(a, b []int) bool {
    if len(a) != len(b) {
        return false
    }
    for i := 0; i < len(a); i++ {
        if a[i] != b[i] {
            return false
        }
    }
    return true
}

func (m *PodManager) getCandidatePods(resourceCounts []int) (*corev1.Pod, error) {
    pods, err := m.podLister.Pods(corev1.NamespaceAll).List(labels.Everything())
    if err != nil {
        return nil, err
    }
    var resPod *corev1.Pod
    assignedTime := int64(0)
    for _, pod := range pods {
        if k8sutil.IdPodCreated(pod) {
            continue
        }
        assgnedTimeStr, ok := pod.Annotations[util.AssignedTimeAnnotations]
        if !ok {
            continue
        }
        counts := k8sutil.ResourceCounts(pod, util.ResourceName)
        if !resourceEqual(counts, resourceCounts) {
            continue
        }
        t, err := strconv.ParseInt(assgnedTimeStr, 10, 64)
        if err != nil {
            klog.Errorf("parse assigned time error, %v", assgnedTimeStr)
            t = time.Now().Unix()
        }
        klog.V(3).Infof("candidate pod %v", pod.Name)
        if resPod != nil && assignedTime < t {
            continue
        }
        assignedTime = t
        resPod = pod
    }
    return resPod, nil
}

func getDevices(pod *corev1.Pod) [][]string {
    var res [][]string
    devStr := pod.Annotations[util.AssignedIDsAnnotations]
    for _, v := range strings.Split(devStr, ";") {
        res = append(res, strings.Split(v, ","))
    }
    return res
}
