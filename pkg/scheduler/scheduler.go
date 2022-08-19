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
	"sort"
	"strconv"
	"time"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/k8sutil"
	"4pd.io/k8s-vgpu/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
)

type Scheduler struct {
	nodeManager
	podManager

	stopCh       chan struct{}
	kubeClient   kubernetes.Interface
	podLister    listerscorev1.PodLister
	nodeLister   listerscorev1.NodeLister
	cachedstatus map[string]*NodeUsage
}

func NewScheduler() *Scheduler {
	klog.Infof("New Scheduler")
	s := &Scheduler{
		stopCh:       make(chan struct{}),
		cachedstatus: make(map[string]*NodeUsage),
	}
	s.nodeManager.init()
	s.podManager.init()
	return s
}

func check(err error) {
	if err != nil {
		klog.Fatal(err)
	}
}

func (s *Scheduler) onAddPod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		klog.Errorf("unknown add object type")
		return
	}
	nodeID, ok := pod.Annotations[util.AssignedNodeAnnotations]
	if !ok {
		return
	}
	ids, ok := pod.Annotations[util.AssignedIDsAnnotations]
	if !ok {
		return
	}
	if k8sutil.IsPodInTerminatedState(pod) {
		s.delPod(pod)
		return
	}
	podDev := util.DecodePodDevices(ids)
	s.addPod(pod, nodeID, podDev)
}

func (s *Scheduler) onUpdatePod(_, newObj interface{}) {
	s.onAddPod(newObj)
}

func (s *Scheduler) onDelPod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		klog.Errorf("unknown add object type")
		return
	}
	_, ok = pod.Annotations[util.AssignedNodeAnnotations]
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
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	s.nodeLister = informerFactory.Core().V1().Nodes().Lister()

	informer := informerFactory.Core().V1().Pods().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAddPod,
		UpdateFunc: s.onUpdatePod,
		DeleteFunc: s.onDelPod,
	})

	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
}

//func (s *Scheduler) assignedNode(pod *corev1.Pod) string {
//    if node, ok := pod.ObjectMeta.Annotations[util.AssignedNodeAnnotations]; ok {
//        return node
//    }
//    return ""
//}
func (s *Scheduler) Register(stream api.DeviceService_RegisterServer) error {
	var nodeID string
	for {
		req, err := stream.Recv()
		if err != nil {
			/* Need to update */
			s.delNode(nodeID)
			klog.Infof("node %v leave, %v", nodeID, err)
			_ = stream.SendAndClose(&api.RegisterReply{})
			return err
		}
		klog.V(3).Infof("device register %v", req.String())
		nodeID = req.GetNode()
		nodeInfo := &NodeInfo{}
		nodeInfo.ID = nodeID
		nodeInfo.Devices = make([]DeviceInfo, len(req.Devices))
		for i := 0; i < len(req.Devices); i++ {
			nodeInfo.Devices[i] = DeviceInfo{
				ID:     req.Devices[i].GetId(),
				Count:  req.Devices[i].GetCount(),
				Devmem: req.Devices[i].GetDevmem(),
				Type:   req.Devices[i].GetType(),
				Health: req.Devices[i].GetHealth(),
			}
		}
		if s.nodes[nodeID] != nil {
			klog.Infoln("before=", s.nodes[nodeID].Devices)
		}
		s.addNode(nodeID, nodeInfo)
		klog.Infof("node %v come node info=%v total=%v", nodeID, nodeInfo, s.nodes[nodeID].Devices)
	}
}

// InspectAllNodesUsage is used by metrics monitor
func (s *Scheduler) InspectAllNodesUsage() *map[string]*NodeUsage {
	return &s.cachedstatus
}

func (s *Scheduler) getNodesUsage(nodes *[]string) (*map[string]*NodeUsage, map[string]string, error) {
	nodeMap := make(map[string]*NodeUsage)
	failedNodes := make(map[string]string)
	for _, nodeID := range *nodes {
		node, err := s.GetNode(nodeID)
		if err != nil {
			klog.Errorf("get node %v device error, %v", nodeID, err)
			failedNodes[nodeID] = "node unregisterd"
			continue
		}

		nodeInfo := &NodeUsage{}
		for _, d := range node.Devices {
			nodeInfo.Devices = append(nodeInfo.Devices, &DeviceUsage{
				Id:        d.ID,
				Used:      0,
				Count:     d.Count,
				Usedmem:   0,
				Totalmem:  d.Devmem,
				Usedcores: 0,
				Type:      d.Type,
				Health:    d.Health,
			})
		}
		nodeMap[nodeID] = nodeInfo
	}
	for _, p := range s.pods {
		node, ok := nodeMap[p.NodeID]
		if !ok {
			continue
		}
		for _, ds := range p.Devices {
			for _, udevice := range ds {
				for _, d := range node.Devices {
					if d.Id == udevice.UUID {
						d.Used++
						d.Usedmem += udevice.Usedmem
						d.Usedcores += udevice.Usedcores
					}
				}
			}
		}
		klog.V(5).Infof("usage: pod %v assigned %v %v", p.Name, p.NodeID, p.Devices)
	}
	s.cachedstatus = nodeMap
	return &nodeMap, failedNodes, nil
}

func (s *Scheduler) Bind(args extenderv1.ExtenderBindingArgs) (*extenderv1.ExtenderBindingResult, error) {
	klog.InfoS("Bind", "pod", args.PodName, "namespace", args.PodNamespace, "podUID", args.PodUID, "node", args.Node)
	var err error
	var res *extenderv1.ExtenderBindingResult
	binding := &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: args.PodName, UID: args.PodUID},
		Target:     corev1.ObjectReference{Kind: "Node", Name: args.Node},
	}
	current, err := s.kubeClient.CoreV1().Pods(args.PodNamespace).Get(context.Background(), args.PodName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Get pod failed")
	}
	err = util.LockNode(args.Node)
	if err != nil {
		klog.ErrorS(err, "Failed to lock node", "node", args.Node)
	}
	//defer util.ReleaseNodeLock(args.Node)

	tmppatch := make(map[string]string)
	tmppatch[util.DeviceBindPhase] = "allocating"
	tmppatch[util.BindTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)

	err = util.PatchPodAnnotations(current, tmppatch)
	if err != nil {
		klog.ErrorS(err, "patch pod annotation failed")
	}
	if err = s.kubeClient.CoreV1().Pods(args.PodNamespace).Bind(context.Background(), binding, metav1.CreateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to bind pod", "pod", args.PodName, "namespace", args.PodNamespace, "podUID", args.PodUID, "node", args.Node)
	}
	if err == nil {
		res = &extenderv1.ExtenderBindingResult{
			Error: "",
		}
	} else {
		res = &extenderv1.ExtenderBindingResult{
			Error: err.Error(),
		}
	}
	klog.Infoln("After Binding Process")
	return res, nil
}

func (s *Scheduler) Filter(args extenderv1.ExtenderArgs) (*extenderv1.ExtenderFilterResult, error) {
	klog.Infof("schedule pod %v/%v[%v]", args.Pod.Namespace, args.Pod.Name, args.Pod.UID)
	nums := k8sutil.Resourcereqs(args.Pod)
	total := 0
	for _, n := range nums {
		for _, k := range n {
			total += int(k.Nums)
		}
	}
	if total == 0 {
		klog.V(1).Infof("pod %v not find resource %v or %v", args.Pod.Name, util.ResourceName, util.MLUResourceCount)
		return &extenderv1.ExtenderFilterResult{
			NodeNames:   args.NodeNames,
			FailedNodes: nil,
			Error:       "",
		}, nil
	}
	annos := args.Pod.Annotations
	s.delPod(args.Pod)
	nodeUsage, failedNodes, err := s.getNodesUsage(args.NodeNames)
	if err != nil {
		return nil, err
	}
	nodeScores, err := calcScore(nodeUsage, &failedNodes, nums, annos)
	if err != nil {
		return nil, err
	}
	if len(*nodeScores) == 0 {
		return &extenderv1.ExtenderFilterResult{
			FailedNodes: failedNodes,
		}, nil
	}
	sort.Sort(nodeScores)
	m := (*nodeScores)[len(*nodeScores)-1]
	klog.Infof("schedule %v/%v to %v %v", args.Pod.Namespace, args.Pod.Name, m.nodeID, m.devices)
	annotations := make(map[string]string)
	annotations[util.AssignedNodeAnnotations] = m.nodeID
	annotations[util.AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)
	annotations[util.AssignedIDsAnnotations] = util.EncodePodDevices(m.devices)
	annotations[util.AssignedIDsToAllocateAnnotations] = annotations[util.AssignedIDsAnnotations]
	s.addPod(args.Pod, m.nodeID, m.devices)
	err = util.PatchPodAnnotations(args.Pod, annotations)
	if err != nil {
		s.delPod(args.Pod)
		return nil, err
	}
	res := extenderv1.ExtenderFilterResult{NodeNames: &[]string{m.nodeID}}
	return &res, nil
}

// addGPUIndexPatch returns the patch adding GPU index
//func addGPUIndexPatch() string {
//	return fmt.Sprintf(`[{"op": "add", "path": "/spec/containers/0/env/-", "value":{"name":"asdf","value":"tttt"}}]`)
//}
