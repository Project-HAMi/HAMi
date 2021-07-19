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
    "4pd.io/k8s-vgpu/pkg/device-plugin/checkpoint"
    "4pd.io/k8s-vgpu/pkg/device-plugin/config"
    "4pd.io/k8s-vgpu/pkg/k8sutil"
    "4pd.io/k8s-vgpu/pkg/util"
    "context"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/fields"
    "k8s.io/apimachinery/pkg/labels"
    k8stypes "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/informers"
    "k8s.io/client-go/kubernetes"
    listerscorev1 "k8s.io/client-go/listers/core/v1"
    "k8s.io/klog/v2"
    "time"
)

type PodManager struct {
    kubeClient kubernetes.Interface
    podLister  listerscorev1.PodLister
    stopCh     chan struct{}

    cp *checkpoint.Checkpoint
}

func (m *PodManager) Start() {
    m.stopCh = make(chan struct{})
    kubeClient, err := k8sutil.NewClient()
    check(err)
    m.kubeClient = kubeClient
    //selector := fields.SelectorFromSet(fields.Set{"spec.nodeName": config.NodeName, "status.phase": "Pending"})
    selector := fields.SelectorFromSet(fields.Set{"spec.nodeName": config.NodeName})
    informerFactory := informers.NewSharedInformerFactoryWithOptions(
        m.kubeClient,
        time.Hour*1,
        informers.WithTweakListOptions(func(options *metav1.ListOptions) {
            options.FieldSelector = selector.String()
        }))
    m.podLister = informerFactory.Core().V1().Pods().Lister()
    informerFactory.Start(m.stopCh)
    m.cp, err = checkpoint.NewCheckpoint()
    check(err)
}

func (m *PodManager) Stop() {
    close(m.stopCh)
    m.cp = nil
}

//func resourceEqual(a, b []int) bool {
//    if len(a) != len(b) {
//        return false
//    }
//    for i := 0; i < len(a); i++ {
//        if a[i] != b[i] {
//            return false
//        }
//    }
//    return true
//}

func (m *PodManager) getCandidatePods() ([]*corev1.Pod, error) {
    //pods, err := m.podLister.Pods(corev1.NamespaceAll).List(labels.Everything())
    selector := fields.SelectorFromSet(fields.Set{"spec.nodeName": config.NodeName, "status.phase": "Pending"})
    pods, err := m.kubeClient.CoreV1().Pods(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{
        FieldSelector: selector.String(),
    })
    if err != nil {
        return nil, err
    }

    var pendingPods []*corev1.Pod
    for _, pod := range pods.Items {
        if k8sutil.AllContainersCreated(&pod) {
            continue
        }
        _, ok := pod.Annotations[util.AssignedTimeAnnotations]
        if !ok {
            continue
        }
        pendingPods = append(pendingPods, pod.DeepCopy())
    }
    if len(pendingPods) > 1 {
        klog.Warningf("pending pods > 1")
    } else if len(pendingPods) == 0 {
        klog.Errorf("not found any pending pod")
        return nil, nil
    }
    return pendingPods, nil
}

func (m *PodManager) getDevices(resourceNums []int) ([][]string, error) {
    pending, err := m.getCandidatePods()
    if err != nil {
        return nil, err
    }
    cps, err := m.cp.GetCheckpoint()
    if err != nil {
        return nil, err
    }
    m.debugCheckpoint(cps)

    for _, pod := range pending {
        ids, ok := pod.Annotations[util.AssignedIDsAnnotations]
        if !ok {
            continue
        }
        pd := util.DecodePodDevices(ids)
        if len(pd) != len(pod.Spec.Containers) {
            klog.Errorf("pod %v/%v annotations mismatch", pod.Namespace, pod.Name)
            continue
        }
        var unused []int
        containers, assigned := cps[pod.UID]
        for i, c := range pod.Spec.Containers {
            if assigned {
                _, ok := containers[c.Name]
                if ok {
                    // TODO: check pd[i] == xxx
                    klog.Infof("container %v already assigned, skip", c.Name)
                    continue
                }
            }
            unused = append(unused, i)
        }

        var res [][]string
        for _, n := range resourceNums {
            for k, i := range unused {
                if n == len(pd[i]) {
                    res = append(res, pd[i])
                    unused = append(unused[:k], unused[k+1:]...)
                    break
                }
            }
        }
        if len(res) == len(resourceNums) {
            return res, nil
        }
    }
    return nil, nil
}

func (m *PodManager) debugCheckpoint(cps map[k8stypes.UID]map[string]util.ContainerDevices) {
    if !util.DebugMode {
        return
    }
    pods, _ := m.podLister.List(labels.Everything())
    for _, pod := range pods {
        podCP, ok := cps[pod.UID]
        if !ok {
            continue
        }
        ids, ok := pod.Annotations[util.AssignedIDsAnnotations]
        if !ok {
            continue
        }
        podDev := util.DecodePodDevices(ids)
        if len(podDev) != len(pod.Spec.Containers) {
            klog.Errorf("pod %v/%v annotations mismatch", pod.Namespace, pod.Name)
            continue
        }
        for i := 0; i < len(podDev); i++ {
            contDev, ok := podCP[pod.Spec.Containers[i].Name]
            if !ok {
                continue
            }
            if len(contDev) != len(podDev[i]) {
                klog.Errorf("pod %v/%v container %v mismatch", pod.Namespace, pod.Name, pod.Spec.Containers[i].Name)
                continue
            }
            for j := 0; j < len(contDev); j++ {
                if contDev[j] != podDev[i][j] {
                    klog.Errorf("pod %v/%v container %v mismatch", pod.Namespace, pod.Name, pod.Spec.Containers[i].Name)
                }
            }
        }
    }
}
