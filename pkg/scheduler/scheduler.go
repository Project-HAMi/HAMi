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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/k8sutil"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	stopCh     chan struct{}
	kubeClient kubernetes.Interface
	podLister  listerscorev1.PodLister
	nodeLister listerscorev1.NodeLister
	//Node status returned by filter
	cachedstatus map[string]*NodeUsage
	nodeNotify   chan struct{}
	//Node Overview
	overviewstatus map[string]*NodeUsage
}

func NewScheduler() *Scheduler {
	klog.Info("New Scheduler")
	s := &Scheduler{
		stopCh:       make(chan struct{}),
		cachedstatus: make(map[string]*NodeUsage),
		nodeNotify:   make(chan struct{}, 1),
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

func (s *Scheduler) onUpdateNode(_, newObj interface{}) {
	s.nodeNotify <- struct{}{}
}

func (s *Scheduler) onDelNode(obj interface{}) {
	s.nodeNotify <- struct{}{}
}

func (s *Scheduler) onAddNode(obj interface{}) {
	s.nodeNotify <- struct{}{}
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
	if k8sutil.IsPodInTerminatedState(pod) {
		s.delPod(pod)
		return
	}
	podDev, _ := util.DecodePodDevices(util.SupportDevices, pod.Annotations)
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
	informerFactory.Core().V1().Nodes().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAddNode,
		UpdateFunc: s.onUpdateNode,
		DeleteFunc: s.onDelNode,
	})
	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)

}

func (s *Scheduler) Stop() {
	close(s.stopCh)
}

func (s *Scheduler) RegisterFromNodeAnnotations() {
	klog.V(5).Infoln("Scheduler into RegisterFromNodeAnnotations")
	nodeInfoCopy := make(map[string]*util.NodeInfo)
	ticker := time.NewTicker(time.Second * 15)
	for {
		select {
		case <-s.nodeNotify:
		case <-ticker.C:
		case <-s.stopCh:
			return
		}
		nodes, err := s.nodeLister.List(labels.Everything())
		if err != nil {
			klog.Errorln("nodes list failed", err.Error())
			continue
		}
		nodeNames := []string{}
		for _, val := range nodes {
			nodeNames = append(nodeNames, val.Name)
			for devhandsk, devInstance := range device.GetDevices() {
				health, needUpdate := devInstance.CheckHealth(devhandsk, val)
				if !health {
					_, ok := s.nodes[val.Name]
					if ok {
						_, ok = nodeInfoCopy[devhandsk]
						if ok && nodeInfoCopy[devhandsk] != nil {
							s.rmNodeDevice(val.Name, nodeInfoCopy[devhandsk])
							klog.Infof("node %v device %s:%v leave, %v remaining devices:%v", val.Name, devhandsk, nodeInfoCopy[devhandsk], err, s.nodes[val.Name].Devices)

							err := devInstance.NodeCleanUp(val.Name)
							if err != nil {
								klog.ErrorS(err, "markAnnotationsToDeleteFailed")
							}
							continue
						}
					}
				}
				if !needUpdate {
					continue
				}
				_, ok := util.HandshakeAnnos[devhandsk]
				if ok {
					tmppat := make(map[string]string)
					tmppat[util.HandshakeAnnos[devhandsk]] = "Requesting_" + time.Now().Format("2006.01.02 15:04:05")
					klog.Infoln("New timestamp=", util.HandshakeAnnos[devhandsk], tmppat[util.HandshakeAnnos[devhandsk]])
					n, err := util.GetNode(val.Name)
					if err != nil {
						klog.Errorln("get node failed", err.Error())
						continue
					}
					util.PatchNodeAnnotations(n, tmppat)
				}

				nodedevices, err := devInstance.GetNodeDevices(*val)
				if err != nil {
					continue
				}
				nodeInfo := &util.NodeInfo{}
				nodeInfo.ID = val.Name
				nodeInfo.Devices = make([]util.DeviceInfo, 0)
				for _, deviceinfo := range nodedevices {
					found := false
					_, ok := s.nodes[val.Name]
					if ok {
						for i1, val1 := range s.nodes[val.Name].Devices {
							if strings.Compare(val1.ID, deviceinfo.Id) == 0 {
								found = true
								s.nodes[val.Name].Devices[i1].Devmem = deviceinfo.Devmem
								s.nodes[val.Name].Devices[i1].Devcore = deviceinfo.Devcore
								break
							}
						}
					}
					if !found {
						nodeInfo.Devices = append(nodeInfo.Devices, util.DeviceInfo{
							ID:      deviceinfo.Id,
							Index:   uint(deviceinfo.Index),
							Count:   deviceinfo.Count,
							Devmem:  deviceinfo.Devmem,
							Devcore: deviceinfo.Devcore,
							Type:    deviceinfo.Type,
							Numa:    deviceinfo.Numa,
							Health:  deviceinfo.Health,
						})
					}
				}
				s.addNode(val.Name, nodeInfo)
				nodeInfoCopy[devhandsk] = nodeInfo
				if s.nodes[val.Name] != nil && len(nodeInfo.Devices) > 0 {
					klog.Infof("node %v device %s come node info=%v total=%v", val.Name, devhandsk, nodeInfoCopy[devhandsk], s.nodes[val.Name].Devices)
				}
			}
		}
		_, _, err = s.getNodesUsage(&nodeNames, nil)
		if err != nil {
			klog.Errorln("get node usage failed", err.Error())
		}
	}
}

// InspectAllNodesUsage is used by metrics monitor.
func (s *Scheduler) InspectAllNodesUsage() *map[string]*NodeUsage {
	return &s.overviewstatus
}

// returns all nodes and its device memory usage, and we filter it with nodeSelector, taints, nodeAffinity
// unschedulerable and nodeName.
func (s *Scheduler) getNodesUsage(nodes *[]string, task *corev1.Pod) (*map[string]*NodeUsage, map[string]string, error) {
	overallnodeMap := make(map[string]*NodeUsage)
	cachenodeMap := make(map[string]*NodeUsage)
	failedNodes := make(map[string]string)
	//for _, nodeID := range *nodes {
	allNodes, err := s.ListNodes()
	if err != nil {
		return &overallnodeMap, failedNodes, err
	}

	for _, node := range allNodes {
		nodeInfo := &NodeUsage{}
		userGPUPolicy := config.GPUSchedulerPolicy
		if task != nil && task.Annotations != nil {
			if value, ok := task.Annotations[policy.GPUSchedulerPolicyAnnotationKey]; ok {
				userGPUPolicy = value
			}
		}
		nodeInfo.Devices = policy.DeviceUsageList{
			Policy:      userGPUPolicy,
			DeviceLists: make([]*policy.DeviceListsScore, 0),
		}
		for _, d := range node.Devices {
			nodeInfo.Devices.DeviceLists = append(nodeInfo.Devices.DeviceLists, &policy.DeviceListsScore{
				Score: 0,
				Device: &util.DeviceUsage{
					ID:        d.ID,
					Index:     d.Index,
					Used:      0,
					Count:     d.Count,
					Usedmem:   0,
					Totalmem:  d.Devmem,
					Totalcore: d.Devcore,
					Usedcores: 0,
					Type:      d.Type,
					Numa:      d.Numa,
					Health:    d.Health,
				},
			})
		}
		overallnodeMap[node.ID] = nodeInfo
	}

	for _, p := range s.pods {
		node, ok := overallnodeMap[p.NodeID]
		if !ok {
			continue
		}
		for _, podsingleds := range p.Devices {
			for _, ctrdevs := range podsingleds {
				for _, udevice := range ctrdevs {
					for _, d := range node.Devices.DeviceLists {
						if d.Device.ID == udevice.UUID {
							d.Device.Used++
							d.Device.Usedmem += udevice.Usedmem
							d.Device.Usedcores += udevice.Usedcores
						}
					}
				}
			}
		}
		klog.V(5).Infof("usage: pod %v assigned %v %v", p.Name, p.NodeID, p.Devices)
	}
	s.overviewstatus = overallnodeMap
	for _, nodeID := range *nodes {
		node, err := s.GetNode(nodeID)
		if err != nil {
			klog.Warningf("get node %v device error, %v", nodeID, err)
			failedNodes[nodeID] = "node unregisterd"
			continue
		}
		cachenodeMap[node.ID] = overallnodeMap[node.ID]
	}
	s.cachedstatus = cachenodeMap
	return &cachenodeMap, failedNodes, nil
}

func (s *Scheduler) getPodUsage() (map[string]PodUseDeviceStat, error) {
	podUsageStat := make(map[string]PodUseDeviceStat)
	pods, err := s.podLister.List(labels.NewSelector())
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodSucceeded {
			continue
		}
		podUseDeviceNum := 0
		if v, ok := pod.Annotations[util.DeviceBindPhase]; ok && v == util.DeviceBindSuccess {
			podUseDeviceNum = 1
		}
		nodeName := pod.Spec.NodeName
		if _, ok := podUsageStat[nodeName]; !ok {
			podUsageStat[nodeName] = PodUseDeviceStat{
				TotalPod:     1,
				UseDevicePod: podUseDeviceNum,
			}
		} else {
			exist := podUsageStat[nodeName]
			podUsageStat[nodeName] = PodUseDeviceStat{
				TotalPod:     exist.TotalPod + 1,
				UseDevicePod: exist.UseDevicePod + podUseDeviceNum,
			}
		}
	}
	return podUsageStat, nil
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

	node, err := s.kubeClient.CoreV1().Nodes().Get(context.Background(), args.Node, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to get node", "node", args.Node)
		res = &extenderv1.ExtenderBindingResult{
			Error: err.Error(),
		}
		return res, nil
	}

	tmppatch := make(map[string]string)
	for _, val := range device.GetDevices() {
		err = val.LockNode(node, current)
		if err != nil {
			goto RelaseNodeLocks
		}
	}
	/*
		err = nodelock.LockNode(args.Node)
		if err != nil {
			klog.ErrorS(err, "Failed to lock node", "node", args.Node)
			res = &extenderv1.ExtenderBindingResult{
				Error: err.Error(),
			}
			return res, nil
		}*/
	//defer util.ReleaseNodeLock(args.Node)

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
		klog.Infoln("After Binding Process")
		return res, nil
	}
RelaseNodeLocks:
	klog.InfoS("bind failed", "err", err.Error())
	for _, val := range device.GetDevices() {
		val.ReleaseNodeLock(node, current)
	}
	return &extenderv1.ExtenderBindingResult{
		Error: err.Error(),
	}, nil
}

func (s *Scheduler) Filter(args extenderv1.ExtenderArgs) (*extenderv1.ExtenderFilterResult, error) {
	klog.InfoS("begin schedule filter", "pod", args.Pod.Name, "uuid", args.Pod.UID, "namespaces", args.Pod.Namespace)
	nums := k8sutil.Resourcereqs(args.Pod)
	total := 0
	for _, n := range nums {
		for _, k := range n {
			total += int(k.Nums)
		}
	}
	if total == 0 {
		klog.V(1).Infof("pod %v not find resource", args.Pod.Name)
		return &extenderv1.ExtenderFilterResult{
			NodeNames:   args.NodeNames,
			FailedNodes: nil,
			Error:       "",
		}, nil
	}
	annos := args.Pod.Annotations
	s.delPod(args.Pod)
	nodeUsage, failedNodes, err := s.getNodesUsage(args.NodeNames, args.Pod)
	if err != nil {
		return nil, err
	}
	if len(failedNodes) != 0 {
		klog.V(5).InfoS("getNodesUsage failed nodes", "nodes", failedNodes)
	}
	nodeScores, err := s.calcScore(nodeUsage, nums, annos, args.Pod)
	if err != nil {
		klog.Infoln("err=", err.Error())
		return nil, err
	}
	if len((*nodeScores).NodeList) == 0 {
		return &extenderv1.ExtenderFilterResult{
			FailedNodes: failedNodes,
		}, nil
	}
	klog.V(4).Infoln("nodeScores_len=", len((*nodeScores).NodeList))
	sort.Sort(nodeScores)
	m := (*nodeScores).NodeList[len((*nodeScores).NodeList)-1]
	klog.Infof("schedule %v/%v to %v %v", args.Pod.Namespace, args.Pod.Name, m.NodeID, m.Devices)
	annotations := make(map[string]string)
	annotations[util.AssignedNodeAnnotations] = m.NodeID
	annotations[util.AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)

	for _, val := range device.GetDevices() {
		val.PatchAnnotations(&annotations, m.Devices)
	}

	//InRequestDevices := util.EncodePodDevices(util.InRequestDevices, m.devices)
	//supportDevices := util.EncodePodDevices(util.SupportDevices, m.devices)
	//maps.Copy(annotations, InRequestDevices)
	//maps.Copy(annotations, supportDevices)
	s.addPod(args.Pod, m.NodeID, m.Devices)
	err = util.PatchPodAnnotations(args.Pod, annotations)
	if err != nil {
		s.delPod(args.Pod)
		return nil, err
	}
	res := extenderv1.ExtenderFilterResult{NodeNames: &[]string{m.NodeID}}
	return &res, nil
}
