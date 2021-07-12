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

package service

import (
    "context"
    "encoding/json"
    "fmt"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    k8stypes "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/tools/cache"
    "sort"
    "strconv"
    "strings"
    "sync"
    "time"

    "4pd.io/k8s-vgpu/pkg/k8sutil"
    "4pd.io/k8s-vgpu/pkg/util"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/client-go/informers"
    "k8s.io/client-go/kubernetes"
    listerscorev1 "k8s.io/client-go/listers/core/v1"
    "k8s.io/klog/v2"
    extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

type DeviceUsage struct {
    id     string
    used   int32
    count  int32
    health bool
}

type DeviceUsageList []*DeviceUsage

type NodeUsage struct {
    devices  DeviceUsageList
    creating bool
}

type NodeScore struct {
    nodeID  string
    devices [][]string
    score   float32
}

type NodeScoreList []*NodeScore

type podInfo struct {
    name     string
    uid      k8stypes.UID
    nodeID   string
    devices  [][]string
    creating bool
}

type Scheduler struct {
    //nodes map[string]NodeUsage
    //mutex sync.Mutex
    pods  map[k8stypes.UID]*podInfo
    mutex sync.Mutex

    stopCh     chan struct{}
    kubeClient kubernetes.Interface
    //podLister  listerscorev1.PodLister
    nodeLister listerscorev1.NodeLister

    deviceService *DeviceService
}

func NewScheduler(deviceService *DeviceService) *Scheduler {
    return &Scheduler{
        stopCh:        make(chan struct{}),
        deviceService: deviceService,
    }
}

func check(err error) {
    if err != nil {
        klog.Fatal(err)
    }
}

func (l DeviceUsageList) Len() int {
    return len(l)
}

func (l DeviceUsageList) Swap(i, j int) {
    l[i], l[j] = l[j], l[i]
}

func (l DeviceUsageList) Less(i, j int) bool {
    return l[i].count-l[i].used < l[j].count-l[j].used
}

func (l NodeScoreList) Len() int {
    return len(l)
}

func (l NodeScoreList) Swap(i, j int) {
    l[i], l[j] = l[j], l[i]
}

func (l NodeScoreList) Less(i, j int) bool {
    return l[i].score < l[j].score
}

//func (s *Scheduler) Name() string {
//    return s.name.String()
//}

func (s *Scheduler) addPod(pod *corev1.Pod, nodeID string, devices [][]string) {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    if k8sutil.IsPodInTerminatedState(pod) {
        delete(s.pods, pod.UID)
        return
    }
    pi, ok := s.pods[pod.UID]
    if !ok {
        pi = &podInfo{name: pod.Name, uid: pod.UID}
        s.pods[pod.UID] = pi
    }
    pi.nodeID = nodeID
    pi.devices = devices
    pi.creating = !k8sutil.IdPodCreated(pod)
}

func (s *Scheduler) delPod(pod *corev1.Pod) {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    delete(s.pods, pod.UID)
}

func (s *Scheduler) onAddPod(pod *corev1.Pod) {
    nodeID, ok := pod.Annotations[util.AssignedNodeAnnotations]
    if !ok {
        return
    }
    ids, ok := pod.Annotations[util.AssignedIDsAnnotations]
    if !ok {
        return
    }
    var devices [][]string
    for _, c := range strings.Split(ids, ";") {
        devices = append(devices, strings.Split(c, ","))
    }
    s.addPod(pod, nodeID, devices)
}

func (s *Scheduler) onDelPod(pod *corev1.Pod) {
    _, ok := pod.Annotations[util.AssignedNodeAnnotations]
    if !ok {
        return
    }
    s.delPod(pod)
}

func (s *Scheduler) Start() {
    kubeClient, err := k8sutil.NewClient()
    check(err)
    s.kubeClient = kubeClient
    informerFactory := informers.NewSharedInformerFactoryWithOptions(s.kubeClient, time.Hour*1)
    //s.podLister = informerFactory.Core().V1().Pods().Lister()
    //s.nodeLister = informerFactory.Core().V1().Nodes().Lister()

    s.pods = make(map[k8stypes.UID]*podInfo)
    informer := informerFactory.Core().V1().Pods().Informer()
    informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc: func(obj interface{}) {
            if pod, ok := obj.(*corev1.Pod); ok {
                s.onAddPod(pod)
            } else {
                klog.Errorf("unknown obj")
            }
        },
        UpdateFunc: func(oldObj, newObj interface{}) {
            if pod, ok := newObj.(*corev1.Pod); ok {
                s.onAddPod(pod)
            } else {
                klog.Errorf("unknown obj")
            }
        },
        DeleteFunc: func(obj interface{}) {
            if pod, ok := obj.(*corev1.Pod); ok {
                s.onDelPod(pod)
            } else {
                klog.Errorf("unknown obj")
            }
        },
    })

    informerFactory.Start(s.stopCh)
    informerFactory.WaitForCacheSync(s.stopCh)
}

func (s *Scheduler) Stop() {
    close(s.stopCh)
}

func (s *Scheduler) assignedNode(pod *corev1.Pod) string {
    if node, ok := pod.ObjectMeta.Annotations[util.AssignedNodeAnnotations]; ok {
        return node
    }
    return ""
}

func (s *Scheduler) getUsage(nodes *[]string) (*map[string]*NodeUsage, error) {
    nodeMap := make(map[string]*NodeUsage)
    for _, nodeID := range *nodes {
        node, err := s.deviceService.GetNode(nodeID)
        if err != nil {
            klog.Errorf("get node %v device error, %v", nodeID, err)
            continue
        }

        nodeInfo := &NodeUsage{}
        for _, d := range node.Devices {
            nodeInfo.devices = append(nodeInfo.devices, &DeviceUsage{
                id:     d.ID,
                used:   0,
                count:  d.Count,
                health: d.Health,
            })
        }
        nodeMap[nodeID] = nodeInfo
    }
    //podList, err := s.kubeClient.CoreV1().Pods(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
    ////pods, err := s.podLister.Pods(corev1.NamespaceAll).List(labels.Everything())
    //if err != nil {
    //    klog.Errorf("list pods error, %v", err)
    //    return nil, err
    //}
    for _, p := range s.pods {
        //if k8sutil.IsPodInTerminatedState(p) {
        //    continue
        //}
        //nodeID, ok := p.Annotations[util.AssignedNodeAnnotations]
        //if !ok {
        //    continue
        //}
        //ids, ok := p.Annotations[util.AssignedIDsAnnotations]
        //if !ok {
        //    continue
        //}
        node, ok := nodeMap[p.nodeID]
        if !ok {
            continue
        }
        if p.creating {
            node.creating = true
        }
        for _, ds := range p.devices {
            for _, deviceID := range ds {
                for _, d := range node.devices {
                    if d.id == deviceID {
                        d.used++
                    }
                }
            }
        }
        klog.V(5).Infof("usage: pod %v assigned %v %v", p.name, p.nodeID, p.devices)
    }
    return &nodeMap, nil
}

func calcScore(nodes *map[string]*NodeUsage, counts []int) (*NodeScoreList, error) {
    res := make(NodeScoreList, 0, len(*nodes))
    for nodeID, node := range *nodes {
        if node.creating {
            continue
        }
        dn := len(node.devices)
        score := NodeScore{nodeID: nodeID, score: 0}
        for _, n := range counts {
            if n > dn {
                break
            }
            sort.Sort(node.devices)
            if node.devices[dn-n].count <= node.devices[dn-n].used {
                continue
            }
            total := int32(0)
            free := int32(0)
            devs := make([]string, 0, n)
            for i := len(node.devices) - 1; i >= 0; i-- {
                total += node.devices[i].count
                free += node.devices[i].count - node.devices[i].used
                if n > 0 {
                    n--
                    node.devices[i].used++
                    devs = append(devs, node.devices[i].id)
                }
            }
            score.devices = append(score.devices, devs)
            score.score += float32(free) / float32(total)
            score.score += float32(dn - n)
        }
        if len(score.devices) == len(counts) {
            res = append(res, &score)
        }
    }
    return &res, nil
}

func (s *Scheduler) Filter(args extenderv1.ExtenderArgs) (*extenderv1.ExtenderFilterResult, error) {
    klog.Infof("schedule pod %v[%v]", args.Pod.Name, args.Pod.UID)
    counts := k8sutil.ResourceCounts(args.Pod, util.ResourceName)
    if len(counts) < 1 {
        klog.Infof("pod %v not find resource %v", args.Pod.Name, util.ResourceName)
        return &extenderv1.ExtenderFilterResult{
            NodeNames:   args.NodeNames,
            FailedNodes: nil,
            Error:       "",
        }, nil
    }
    //pod, err := s.podLister.Pods(args.Pod.Namespace).Get(args.Pod.Name)
    //if err != nil {
    //    return nil, err
    //}
    //if pod.UID != args.Pod.UID {
    //    return nil, fmt.Errorf("pod %v uid not match", pod.Name)
    //}
    nodeUsage, err := s.getUsage(args.NodeNames)
    if err != nil {
        return nil, err
    }
    nodeScores, err := calcScore(nodeUsage, counts)
    if err != nil {
        return nil, err
    }
    if len(*nodeScores) == 0 {
        failedNodes := make(map[string]string)
        for _, v := range *args.NodeNames {
            failedNodes[v] = fmt.Sprintf("no suitable vgpu")
        }
        return &extenderv1.ExtenderFilterResult{
            FailedNodes: failedNodes,
        }, nil
    }
    sort.Sort(nodeScores)
    m := (*nodeScores)[len(*nodeScores)-1]
    klog.Infof("schedule %v to %v %v", args.Pod.Name, m.nodeID, m.devices)
    annotations := make(map[string]string)
    annotations[util.AssignedNodeAnnotations] = m.nodeID
    annotations[util.AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)
    strs := make([]string, 0, len(m.devices))
    for _, v := range m.devices {
        strs = append(strs, strings.Join(v, ","))
    }
    annotations[util.AssignedIDsAnnotations] = strings.Join(strs, ";")
    err = s.patchPodAnnotations(args.Pod, annotations)
    if err != nil {
        return nil, err
    }
    s.addPod(args.Pod, m.nodeID, m.devices)
    res := extenderv1.ExtenderFilterResult{NodeNames: &[]string{m.nodeID}}
    return &res, nil
}

func (s *Scheduler) patchPodAnnotations(pod *corev1.Pod, annotations map[string]string) error {
    type patchMetadata struct {
        Annotations map[string]string `json:"annotations,omitempty"`
    }
    type patchPod struct {
        Metadata patchMetadata `json:"metadata"`
        //Spec     patchSpec     `json:"spec,omitempty"`
    }

    p := patchPod{}
    p.Metadata.Annotations = annotations

    bytes, err := json.Marshal(p)
    if err != nil {
        return err
    }
    _, err = s.kubeClient.CoreV1().Pods(pod.Namespace).
        Patch(context.Background(), pod.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
    if err != nil {
        klog.Infof("patch pod %v failed, %v", pod.Name, err)
    }
    return err
}
