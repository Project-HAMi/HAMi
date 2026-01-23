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
<<<<<<< HEAD
	"fmt"
	"maps"
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

<<<<<<< HEAD
<<<<<<< HEAD
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
=======
=======
	"4pd.io/k8s-vgpu/pkg/device"
>>>>>>> 21785f7 (update to v2.3.2)
	"4pd.io/k8s-vgpu/pkg/k8sutil"
	"4pd.io/k8s-vgpu/pkg/util"
	"4pd.io/k8s-vgpu/pkg/util/nodelock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
<<<<<<< HEAD
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
	"k8s.io/apimachinery/pkg/labels"
>>>>>>> c7a3893 (Remake this repo to HAMi)
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	coordinationv1 "k8s.io/client-go/listers/coordination/v1"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"
<<<<<<< HEAD
<<<<<<< HEAD

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/leaderelection"
	nodelockutil "github.com/Project-HAMi/HAMi/pkg/util/nodelock"
)

const (
	defaultResync    = 1 * time.Hour
	syncedPollPeriod = 100 * time.Millisecond
=======
	"k8s.io/kubernetes/pkg/scheduler/framework"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
=======
>>>>>>> c7a3893 (Remake this repo to HAMi)
)

type Scheduler struct {
	*nodeManager
	podManager    *device.PodManager
	quotaManager  *device.QuotaManager
	leaderManager leaderelection.LeaderManager

	stopCh       chan struct{}
	nodeNotify   chan struct{}
	leaderNotify chan struct{}

	kubeClient  kubernetes.Interface
	podLister   listerscorev1.PodLister
	nodeLister  listerscorev1.NodeLister
	quotaLister listerscorev1.ResourceQuotaLister
	leaseLister coordinationv1.LeaseLister
	//Node status returned by filter
	cachedstatus map[string]*NodeUsage
	//Node Overview
	overviewstatus map[string]*NodeUsage
	eventRecorder  record.EventRecorder
	started        uint32 // 0 = false, 1 = true

	lock   sync.RWMutex
	synced bool
}

func NewScheduler() *Scheduler {
	klog.InfoS("Initializing HAMi scheduler")
	s := &Scheduler{
		stopCh:       make(chan struct{}),
		cachedstatus: make(map[string]*NodeUsage),
		nodeNotify:   make(chan struct{}, 1),
		leaderNotify: make(chan struct{}, 1),
		started:      0,
		synced:       false,
	}
	s.nodeManager = newNodeManager()
	s.podManager = device.NewPodManager()
	s.quotaManager = device.NewQuotaManager()
	// Use dummy leader manager when leaderElect is disabled
	// This ensures IsLeader() always returns true and synced will not be set to false
	s.leaderManager = leaderelection.NewDummyLeaderManager(true)
	if config.LeaderElect {
		callbacks := leaderelection.LeaderCallbacks{
			OnStartedLeading: func() {
				s.leaderNotify <- struct{}{}
			},
			OnStoppedLeading: func() {
				s.lock.Lock()
				defer s.lock.Unlock()
				s.synced = false
			},
		}
		s.leaderManager = leaderelection.NewLeaderManager(config.HostName, config.LeaderElectResourceNamespace, config.LeaderElectResourceName, callbacks)
	}
	klog.V(2).InfoS("Scheduler initialized successfully")
	return s
}

func (s *Scheduler) GetQuotaManager() *device.QuotaManager {
	return s.quotaManager
}

func (s *Scheduler) GetPodManager() *device.PodManager {
	return s.podManager
}

func (s *Scheduler) GetLeaderManager() leaderelection.LeaderManager {
	return s.leaderManager
}

func (s *Scheduler) doNodeNotify() {
	select {
	case s.nodeNotify <- struct{}{}:
	default:
	}
}

<<<<<<< HEAD
func (s *Scheduler) onAddPod(obj any) {
	pod, ok := obj.(*corev1.Pod)
=======
func (s *Scheduler) onAddPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	if !ok {
		klog.ErrorS(fmt.Errorf("invalid pod object"), "Failed to process pod addition")
		return
	}
	klog.V(5).InfoS("Pod added", "pod", pod.Name, "namespace", pod.Namespace)
	nodeID, ok := pod.Annotations[util.AssignedNodeAnnotations]
	if !ok {
		return
	}
	if util.IsPodInTerminatedState(pod) {
		pi, ok := s.podManager.GetPod(pod)
		if ok {
			s.quotaManager.RmUsage(pod, pi.Devices)
		}
		s.podManager.DelPod(pod)
		return
	}
	podDev, _ := device.DecodePodDevices(device.SupportDevices, pod.Annotations)
	if s.podManager.AddPod(pod, nodeID, podDev) {
		s.quotaManager.AddUsage(pod, podDev)
	}
<<<<<<< HEAD
=======
	podDev, _ := util.DecodePodDevices(ids)
	s.addPod(pod, nodeID, podDev)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

func (s *Scheduler) onUpdatePod(_, newObj any) {
	s.onAddPod(newObj)
}

<<<<<<< HEAD
func (s *Scheduler) onDelPod(obj any) {
	var pod *corev1.Pod
	var ok bool

	switch t := obj.(type) {
	case *corev1.Pod:
		pod = t
		klog.V(4).InfoS("Pod deleted, cleaning up cache", "pod", pod.Namespace+"/"+pod.Name)
	case cache.DeletedFinalStateUnknown:
		if pod, ok = t.Obj.(*corev1.Pod); ok {
			klog.V(4).InfoS("Pod tombstone deleted, cleaning up cache", "pod", t.Key)
		} else {
			klog.Errorf("Received tombstone for non-pod object on pod delete")
		}
	default:
		klog.Errorf("Received unknown object type on pod delete")
=======
func (s *Scheduler) onDelPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("unknown add object type")
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		return
	}

	_, ok = pod.Annotations[util.AssignedNodeAnnotations]
	if !ok {
		return
	}
	pi, ok := s.podManager.GetPod(pod)
	if ok {
		s.quotaManager.RmUsage(pod, pi.Devices)
		s.podManager.DelPod(pod)
	}
}

// onDelNode handles node delete events. It removes any in-memory per-node
// lock bookkeeping to avoid unbounded growth when nodes are removed by
// autoscalers or administratively.
func (s *Scheduler) onDelNode(obj any) {
	// Ensure downstream consumers are notified regardless of decoding success
	defer s.doNodeNotify()

	var nodeName string
	switch t := obj.(type) {
	case *corev1.Node:
		nodeName = t.Name
		klog.V(4).InfoS("Node deleted, cleaning up nodelock", "node", nodeName)
	case cache.DeletedFinalStateUnknown:
		if n, ok := t.Obj.(*corev1.Node); ok {
			nodeName = n.Name
			klog.V(4).InfoS("Node tombstone deleted, cleaning up nodelock", "node", nodeName)
		} else {
			klog.V(5).InfoS("Received tombstone for non-node object on delete")
			return
		}
	default:
		klog.V(5).InfoS("Received unknown object type on node delete")
		return
	}

	nodelockutil.CleanupNodeLock(nodeName)
	s.rmNode(nodeName)
	s.cleanupNodeUsage(nodeName)
}

// cleanupNodeUsage removes the node from overviewstatus and cachedstatus maps
// to ensure metrics no longer report data for deleted nodes.
func (s *Scheduler) cleanupNodeUsage(nodeID string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok := s.overviewstatus[nodeID]; ok {
		delete(s.overviewstatus, nodeID)
		klog.V(4).InfoS("Removed node from overviewstatus", "node", nodeID)
	}
	if _, ok := s.cachedstatus[nodeID]; ok {
		delete(s.cachedstatus, nodeID)
		klog.V(4).InfoS("Removed node from cachedstatus", "node", nodeID)
	}
}

func (s *Scheduler) onAddQuota(obj any) {
	quota, ok := obj.(*corev1.ResourceQuota)
	if !ok {
		klog.Errorf("unknown add object type")
		return
	}
	s.quotaManager.AddQuota(quota)
}

func (s *Scheduler) onUpdateQuota(oldObj, newObj any) {
	s.onDelQuota(oldObj)
	s.onAddQuota(newObj)
}

func (s *Scheduler) onDelQuota(obj any) {
	quota, ok := obj.(*corev1.ResourceQuota)
	if !ok {
		klog.Errorf("unknown del object type")
		return
	}
	s.quotaManager.DelQuota(quota)
}

func (s *Scheduler) Start() error {
	klog.InfoS("Starting HAMi scheduler components")
	s.kubeClient = client.GetClient()
	informerFactory := informers.NewSharedInformerFactoryWithOptions(s.kubeClient, defaultResync)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	s.nodeLister = informerFactory.Core().V1().Nodes().Lister()
	s.quotaLister = informerFactory.Core().V1().ResourceQuotas().Lister()

	podEventHandlerRegistration, err := informerFactory.Core().V1().Pods().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAddPod,
		UpdateFunc: s.onUpdatePod,
		DeleteFunc: s.onDelPod,
	})
	if err != nil {
		return fmt.Errorf("failed to register pod event handler: %v", err)
	}
	nodeEventHandlerRegistration, err := informerFactory.Core().V1().Nodes().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ any) { s.doNodeNotify() },
		DeleteFunc: s.onDelNode,
	})
	if err != nil {
		return fmt.Errorf("failed to register node event handler: %v", err)
	}
	resourceQuotaEventHandlerRegistration, err := informerFactory.Core().V1().ResourceQuotas().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAddQuota,
		UpdateFunc: s.onUpdateQuota,
		DeleteFunc: s.onDelQuota,
	})
	if err != nil {
		return fmt.Errorf("failed to register resource quota event handler: %v", err)
	}

	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)
<<<<<<< HEAD
	cache.WaitForCacheSync(s.stopCh, podEventHandlerRegistration.HasSynced, nodeEventHandlerRegistration.HasSynced, resourceQuotaEventHandlerRegistration.HasSynced)

	if config.LeaderElect {
		leaseInformerFactory := informers.NewSharedInformerFactoryWithOptions(s.kubeClient, defaultResync, informers.WithNamespace(config.LeaderElectResourceNamespace))
		s.leaseLister = leaseInformerFactory.Coordination().V1().Leases().Lister()

		leaseEventHandlerRegistration, err := leaseInformerFactory.Coordination().V1().Leases().Informer().AddEventHandler(s.leaderManager)
		if err != nil {
			return fmt.Errorf("failed to register lease event handler: %w", err)
		}
		leaseInformerFactory.Start(s.stopCh)
		leaseInformerFactory.WaitForCacheSync(s.stopCh)
		cache.WaitForCacheSync(s.stopCh, leaseEventHandlerRegistration.HasSynced)
	}

	s.addAllEventHandlers()
	atomic.StoreUint32(&s.started, 1)
	return nil
=======

>>>>>>> 21785f7 (update to v2.3.2)
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
}

<<<<<<< HEAD
func (s *Scheduler) RegisterFromNodeAnnotations() {
	klog.InfoS("Entering RegisterFromNodeAnnotations")
	defer klog.InfoS("Exiting RegisterFromNodeAnnotations")

	labelSelector := labels.Set(config.NodeLabelSelector).AsSelector()
	klog.InfoS("Using label selector for list nodes", "selector", labelSelector.String())

	ticker := time.NewTicker(time.Second * 15)
	defer ticker.Stop()
	printedLog := map[string]bool{}
=======
func (s *Scheduler) RegisterFromNodeAnnotatons() error {
	klog.V(5).Infoln("Scheduler into RegisterFromNodeAnnotations")
	nodeInfoCopy := make(map[string]*NodeInfo)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	for {
<<<<<<< HEAD
		select {
		case <-s.nodeNotify:
			klog.V(5).InfoS("Received node notification")
		case <-s.leaderNotify:
			klog.V(5).InfoS("Received leaderElection notification. We are just elected to leader")
		case <-ticker.C:
			klog.V(5).InfoS("Ticker triggered")
		case <-s.stopCh:
			klog.InfoS("Received stop signal, exiting RegisterFromNodeAnnotations")
			return
		}
<<<<<<< HEAD
		if atomic.LoadUint32(&s.started) == 0 {
			klog.V(5).InfoS("Scheduler not started yet, skipping ...")
			continue
=======
		for _, val := range nodes.Items {
=======
		nodes, err := s.nodeLister.List(labels.Everything())
		if err != nil {
			klog.Errorln("nodes list failed", err.Error())
			return err
		}
		nodeNames := []string{}
		for _, val := range nodes {
			nodeNames = append(nodeNames, val.Name)
>>>>>>> c7a3893 (Remake this repo to HAMi)
			for devhandsk, devreg := range device.KnownDevice {
				_, ok := val.Annotations[devreg]
				if !ok {
					continue
				}
<<<<<<< HEAD
				nodedevices := util.DecodeNodeDevices(val.Annotations[devreg])
				if len(nodedevices) == 0 {
=======
				nodedevices, err := util.DecodeNodeDevices(val.Annotations[devreg])
				if err != nil {
					klog.ErrorS(err, "failed to decode node devices", "node", val.Name, "device annotation", val.Annotations[devreg])
>>>>>>> c7a3893 (Remake this repo to HAMi)
					continue
				}
				if len(nodedevices) == 0 {
					klog.InfoS("no node gpu device found", "node", val.Name, "device annotation", val.Annotations[devreg])
					continue
				}
				klog.V(5).InfoS("nodes device information", "node", val.Name, "nodedevices", util.EncodeNodeDevices(nodedevices))
				handshake := val.Annotations[devhandsk]
				if strings.Contains(handshake, "Requesting") {
					formertime, _ := time.Parse("2006.01.02 15:04:05", strings.Split(handshake, "_")[1])
					if time.Now().After(formertime.Add(time.Second * 60)) {
						_, ok := s.nodes[val.Name]
						if ok {
							s.rmNodeDevice(val.Name, nodeInfoCopy[devhandsk])
							klog.Infof("node %v device %s:%v leave, %v remaining devices:%v", val.Name, devhandsk, nodeInfoCopy[devhandsk], err, s.nodes[val.Name].Devices)

<<<<<<< HEAD
							tmppat := make(map[string]string)
							tmppat[devhandsk] = "Deleted_" + time.Now().Format("2006.01.02 15:04:05")
							n, err := util.GetNode(val.Name)
							if err != nil {
								klog.Errorln("get node failed", err.Error())
=======
								tmppat := make(map[string]string)
								tmppat[devhandsk] = "Deleted_" + time.Now().Format("2006.01.02 15:04:05")
								n, err := util.GetNode(val.Name)
								if err != nil {
									klog.Errorln("get node failed", err.Error())
									continue
								}
								util.PatchNodeAnnotations(n, tmppat)
								continue
>>>>>>> c7a3893 (Remake this repo to HAMi)
							}
							util.PatchNodeAnnotations(n, tmppat)
							continue
						}
					}
					continue
				} else if strings.Contains(handshake, "Deleted") {
					continue
				} else {
					tmppat := make(map[string]string)
					tmppat[devhandsk] = "Requesting_" + time.Now().Format("2006.01.02 15:04:05")
					n, err := util.GetNode(val.Name)
					if err != nil {
						klog.Errorln("get node failed", err.Error())
						continue
					}
					util.PatchNodeAnnotations(n, tmppat)
				}
				nodeInfo := &NodeInfo{}
				nodeInfo.ID = val.Name
				nodeInfo.Devices = make([]DeviceInfo, 0)
				for index, deviceinfo := range nodedevices {
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
						nodeInfo.Devices = append(nodeInfo.Devices, DeviceInfo{
							ID:      deviceinfo.Id,
							Index:   uint(index),
							Count:   deviceinfo.Count,
							Devmem:  deviceinfo.Devmem,
							Devcore: deviceinfo.Devcore,
							Type:    deviceinfo.Type,
							Health:  deviceinfo.Health,
						})
					}
				}
				s.addNode(val.Name, nodeInfo)
				nodeInfoCopy[devhandsk] = nodeInfo
				if s.nodes[val.Name] != nil && nodeInfo != nil && len(nodeInfo.Devices) > 0 {
					klog.Infof("node %v device %s come node info=%v total=%v", val.Name, devhandsk, nodeInfoCopy[devhandsk], s.nodes[val.Name].Devices)
				}
			}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		}
<<<<<<< HEAD
		s.register(labelSelector, printedLog)
=======
		_, _, err = s.getNodesUsage(&nodeNames, nil)
		if err != nil {
			klog.Errorln("get node usage failed", err.Error())
			return err
		}
		time.Sleep(time.Second * 15)
>>>>>>> c7a3893 (Remake this repo to HAMi)
	}
}

<<<<<<< HEAD
func (s *Scheduler) register(labelSelector labels.Selector, printedLog map[string]bool) {
	// Lock here to avoid setting s.synced to false, when we lost leadership, while doing register.
	// 1. lost leadership before register: synced will set to false in callbacks, and register will be skipped because IsLeader() returns false
	// 2. lost leadership during or after register: synced will set to true after finishing register, and callback will set it to false again after lock is acquired by callback
	s.lock.Lock()
	defer s.lock.Unlock()
	// Only do registration when we are leader
	if !s.leaderManager.IsLeader() {
		klog.V(5).InfoS("Scheduler is not leader yet, skipping ...")
		return
	}
	rawNodes, err := s.nodeLister.List(labelSelector)
	if err != nil {
		klog.ErrorS(err, "Failed to list nodes with selector", "selector", labelSelector.String())
		return
	}
	klog.V(5).InfoS("Listed nodes", "nodeCount", len(rawNodes))
	var nodeNames []string
	for _, val := range rawNodes {
		nodeNames = append(nodeNames, val.Name)
		klog.V(5).InfoS("Processing node", "nodeName", val.Name)

		for devhandsk, devInstance := range device.GetDevices() {
			klog.V(5).InfoS("Checking device health", "nodeName", val.Name, "deviceVendor", devhandsk)

			nodedevices, err := devInstance.GetNodeDevices(*val)
			if err != nil {
				klog.V(5).InfoS("Failed to get node devices", "nodeName", val.Name, "deviceVendor", devhandsk)
				continue
			}

			health, needUpdate := devInstance.CheckHealth(devhandsk, val)
			klog.V(5).InfoS("Device health check result", "nodeName", val.Name, "deviceVendor", devhandsk, "health", health, "needUpdate", needUpdate)

			if !health {
				klog.Warning("Device is unhealthy, cleaning up node", "nodeName", val.Name, "deviceVendor", devhandsk)
				err := devInstance.NodeCleanUp(val.Name)
				if err != nil {
					klog.ErrorS(err, "Node cleanup failed", "nodeName", val.Name, "deviceVendor", devhandsk)
				}

				s.rmNodeDevices(val.Name, devhandsk)
				continue
			}
			if !needUpdate {
				klog.V(5).InfoS("No update needed for device", "nodeName", val.Name, "deviceVendor", devhandsk)
				continue
			}
			nodeInfo := &device.NodeInfo{}
			nodeInfo.ID = val.Name
			nodeInfo.Node = val
			klog.V(5).InfoS("Fetching node devices", "nodeName", val.Name, "deviceVendor", devhandsk)
			nodeInfo.Devices = make(map[string][]device.DeviceInfo, 0)
			for _, deviceinfo := range nodedevices {
				nodeInfo.Devices[deviceinfo.DeviceVendor] = append(nodeInfo.Devices[deviceinfo.DeviceVendor], *deviceinfo)
			}
			s.addNode(val.Name, nodeInfo)
			if s.nodes[val.Name] != nil && len(nodeInfo.Devices) > 0 {
				if printedLog[val.Name] {
					klog.V(5).InfoS("Node device updated", "nodeName", val.Name, "deviceVendor", devhandsk, "nodeInfo", nodeInfo, "totalDevices", s.nodes[val.Name].Devices)
				} else {
					klog.InfoS("Node device added", "nodeName", val.Name, "deviceVendor", devhandsk, "nodeInfo", nodeInfo, "totalDevices", s.nodes[val.Name].Devices)
					printedLog[val.Name] = true
				}
			}
		}
	}
	_, _, err = s.getNodesUsage(&nodeNames, nil)
	if err != nil {
		klog.ErrorS(err, "Failed to get node usage", "nodeNames", nodeNames)
		return
	}

	// Set synced to true only after getNodeUsage() succeeds
	s.synced = true
}

func (s *Scheduler) WaitForCacheSync(ctx context.Context) bool {
	err := wait.PollUntilContextCancel(ctx, syncedPollPeriod, true, func(context.Context) (done bool, err error) {
		s.lock.RLock()
		defer s.lock.RUnlock()
		return s.synced, nil
	})
	if err != nil {
		klog.ErrorS(err, "failed to poll until context cancel")
		return false
	}

	return true
}

// InspectAllNodesUsage is used by metrics monitor.
func (s *Scheduler) InspectAllNodesUsage() *map[string]*NodeUsage {
	return &s.overviewstatus
}

<<<<<<< HEAD
// returns all nodes and its device memory usage, and we filter it with nodeSelector, taints, nodeAffinity
// unschedulerable and nodeName.
func (s *Scheduler) getNodesUsage(nodes *[]string, task *corev1.Pod) (*map[string]*NodeUsage, map[string]string, error) {
	overallnodeMap := make(map[string]*NodeUsage)
	cachenodeMap := make(map[string]*NodeUsage)
	failedNodes := make(map[string]string)
	allNodes, err := s.ListNodes()
	if err != nil {
		return &overallnodeMap, failedNodes, err
=======
// InspectAllNodesUsage is used by metrics monitor
func (s *Scheduler) InspectAllNodesUsage() *map[string]*NodeUsage {
	return &s.cachedstatus
}

// GenerateNodeMapAndSlice returns the nodeMap and nodeSlice generated from ssn
func GenerateNodeMapAndSlice(nodes []*v1.Node) map[string]*framework.NodeInfo {
	nodeMap := make(map[string]*framework.NodeInfo)
	for _, node := range nodes {
		nodeInfo := framework.NewNodeInfo()
		nodeInfo.SetNode(node)
		nodeMap[node.Name] = nodeInfo
	}
	return nodeMap
}

=======
>>>>>>> c7a3893 (Remake this repo to HAMi)
// returns all nodes and its device memory usage, and we filter it with nodeSelector, taints, nodeAffinity
// unschedulerable and nodeName
func (s *Scheduler) getNodesUsage(nodes *[]string, task *v1.Pod) (*map[string]*NodeUsage, map[string]string, error) {
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
			nodeInfo.Devices = append(nodeInfo.Devices, &util.DeviceUsage{
				Id:        d.ID,
				Index:     d.Index,
				Used:      0,
				Count:     d.Count,
				Usedmem:   0,
				Totalmem:  d.Devmem,
				Totalcore: d.Devcore,
				Usedcores: 0,
				Type:      d.Type,
				Health:    d.Health,
			})
		}
		nodeMap[nodeID] = nodeInfo
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	}

	for _, node := range allNodes {
		nodeInfo := &NodeUsage{}
		userGPUPolicy := util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, task)
		nodeInfo.Node = node.Node
		nodeInfo.Devices = policy.DeviceUsageList{
			Policy:      userGPUPolicy,
			DeviceLists: make([]*policy.DeviceListsScore, 0),
		}
		for _, k := range node.Devices {
			for _, d := range k {
				nodeInfo.Devices.DeviceLists = append(nodeInfo.Devices.DeviceLists, &policy.DeviceListsScore{
					Score: 0,
					Device: &device.DeviceUsage{
						ID:        d.ID,
						Index:     d.Index,
						Used:      0,
						Count:     d.Count,
						Usedmem:   0,
						Totalmem:  d.Devmem,
						Totalcore: d.Devcore,
						Usedcores: 0,
						MigUsage: device.MigInUse{
							Index:     0,
							UsageList: make(device.MIGS, 0),
						},
						MigTemplate: d.MIGTemplate,
						Mode:        d.Mode,
						Type:        d.Type,
						Numa:        d.Numa,
						Health:      d.Health,
						PodInfos:    make([]*device.PodInfo, 0),
						CustomInfo:  maps.Clone(d.CustomInfo),
					},
				})
			}
		}
		overallnodeMap[node.ID] = nodeInfo
	}

	podsInfo := s.podManager.ListPodsInfo()
	for _, p := range podsInfo {
		node, ok := overallnodeMap[p.NodeID]
		if !ok {
			klog.V(5).InfoS("pod allocated unknown node resources",
				"pod", klog.KRef(p.Namespace, p.Name), "nodeID", p.NodeID)
			continue
		}
		for _, podsingleds := range p.Devices {
			for _, ctrdevs := range podsingleds {
				for _, udevice := range ctrdevs {
					for _, d := range node.Devices.DeviceLists {
						deviceID := udevice.UUID
						if strings.Contains(deviceID, "[") {
							deviceID = strings.Split(deviceID, "[")[0]
						}
						if d.Device.ID == deviceID {
							d.Device.Used++
							d.Device.Usedmem += udevice.Usedmem
							d.Device.Usedcores += udevice.Usedcores
							d.Device.PodInfos = append(d.Device.PodInfos, p)

							if strings.Contains(udevice.UUID, "[") {
								if strings.Compare(d.Device.Mode, "hami-core") == 0 {
									klog.Errorf("found a mig task running on a hami-core GPU\n")
									d.Device.Health = false
									continue
								}
								tmpIdx, Instance, _ := device.ExtractMigTemplatesFromUUID(udevice.UUID)
								if len(d.Device.MigUsage.UsageList) == 0 {
									device.PlatternMIG(&d.Device.MigUsage, d.Device.MigTemplate, tmpIdx)
								}
								d.Device.MigUsage.UsageList[Instance].InUse = true
								klog.V(5).Infoln("add mig usage", d.Device.MigUsage, "template=", d.Device.MigTemplate, "uuid=", d.Device.ID)
							}
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
			// The identified node does not have a gpu device, so the log here has no practical meaning,increase log priority.
			klog.V(5).InfoS("node unregistered", "node", nodeID, "error", err)
			failedNodes[nodeID] = "node unregistered"
			continue
		}
		cachenodeMap[node.ID] = overallnodeMap[node.ID]
	}
	s.cachedstatus = cachenodeMap
	return &cachenodeMap, failedNodes, nil
}

func (s *Scheduler) getPodUsage() (map[string]device.PodUseDeviceStat, error) {
	podUsageStat := make(map[string]device.PodUseDeviceStat)
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
			podUsageStat[nodeName] = device.PodUseDeviceStat{
				TotalPod:     1,
				UseDevicePod: podUseDeviceNum,
			}
		} else {
			exist := podUsageStat[nodeName]
			podUsageStat[nodeName] = device.PodUseDeviceStat{
				TotalPod:     exist.TotalPod + 1,
				UseDevicePod: exist.UseDevicePod + podUseDeviceNum,
			}
		}
	}
	return podUsageStat, nil
}

func (s *Scheduler) Bind(args extenderv1.ExtenderBindingArgs) (*extenderv1.ExtenderBindingResult, error) {
	klog.InfoS("Attempting to bind pod to node", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
	var res *extenderv1.ExtenderBindingResult
<<<<<<< HEAD

	binding := &corev1.Binding{
=======
	binding := &v1.Binding{
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		ObjectMeta: metav1.ObjectMeta{Name: args.PodName, UID: args.PodUID},
		Target:     v1.ObjectReference{Kind: "Node", Name: args.Node},
	}
	current, err := s.kubeClient.CoreV1().Pods(args.PodNamespace).Get(context.Background(), args.PodName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to get pod", "pod", args.PodName, "namespace", args.PodNamespace)
		return &extenderv1.ExtenderBindingResult{Error: err.Error()}, err
	}
<<<<<<< HEAD
	klog.InfoS("Trying to get the target node for pod", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
	node, err := s.kubeClient.CoreV1().Nodes().Get(context.Background(), args.Node, metav1.GetOptions{})
=======
	err = nodelock.LockNode(args.Node)
>>>>>>> 21785f7 (update to v2.3.2)
	if err != nil {
		klog.ErrorS(err, "Failed to get node", "node", args.Node)
		s.recordScheduleBindingResultEvent(current, EventReasonBindingFailed, []string{}, fmt.Errorf("failed to get node %s", args.Node))
		res = &extenderv1.ExtenderBindingResult{Error: err.Error()}
		return res, nil
	}

	tmppatch := map[string]string{
		util.DeviceBindPhase:     "allocating",
		util.BindTimeAnnotations: strconv.FormatInt(time.Now().Unix(), 10),
	}

	for _, val := range device.GetDevices() {
		err = val.LockNode(node, current)
		if err != nil {
			klog.ErrorS(err, "Failed to lock node", "node", args.Node, "device", val)
			goto ReleaseNodeLocks
		}
	}

	err = util.PatchPodAnnotations(current, tmppatch)
	if err != nil {
		klog.ErrorS(err, "Failed to patch pod annotations", "pod", klog.KObj(current))
		goto ReleaseNodeLocks
	}

	err = s.kubeClient.CoreV1().Pods(args.PodNamespace).Bind(context.Background(), binding, metav1.CreateOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to bind pod", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
		goto ReleaseNodeLocks
	}

	s.recordScheduleBindingResultEvent(current, EventReasonBindingSucceed, []string{args.Node}, nil)
	klog.InfoS("Successfully bound pod to node", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
	return &extenderv1.ExtenderBindingResult{Error: ""}, nil

ReleaseNodeLocks:
	klog.InfoS("Release node locks", "node", args.Node)
	for _, val := range device.GetDevices() {
		val.ReleaseNodeLock(node, current)
	}
	s.recordScheduleBindingResultEvent(current, EventReasonBindingFailed, []string{}, err)
	return &extenderv1.ExtenderBindingResult{Error: err.Error()}, nil
}

func (s *Scheduler) Filter(args extenderv1.ExtenderArgs) (*extenderv1.ExtenderFilterResult, error) {
<<<<<<< HEAD
	klog.InfoS("Starting schedule filter process", "pod", args.Pod.Name, "uuid", args.Pod.UID, "namespace", args.Pod.Namespace)
	resourceReqs := device.Resourcereqs(args.Pod)
	resourceReqTotal := 0
	for _, n := range resourceReqs {
=======
	klog.InfoS("begin schedule filter", "pod", args.Pod.Name, "uuid", args.Pod.UID, "namespaces", args.Pod.Namespace)
	nums := k8sutil.Resourcereqs(args.Pod)
	total := 0
	for _, n := range nums {
>>>>>>> c7a3893 (Remake this repo to HAMi)
		for _, k := range n {
			resourceReqTotal += int(k.Nums)
		}
	}
<<<<<<< HEAD
	if resourceReqTotal == 0 {
		klog.V(1).InfoS("Pod does not request any resources",
			"pod", args.Pod.Name)
		s.recordScheduleFilterResultEvent(args.Pod, EventReasonFilteringFailed, "", fmt.Errorf("does not request any resource"))
=======
	if total == 0 {
		klog.V(1).Infof("pod %v not find resource", args.Pod.Name)
>>>>>>> 21785f7 (update to v2.3.2)
		return &extenderv1.ExtenderFilterResult{
			NodeNames:   args.NodeNames,
			FailedNodes: nil,
			Error:       "",
		}, nil
	}
	s.podManager.DelPod(args.Pod)
	nodeUsage, failedNodes, err := s.getNodesUsage(args.NodeNames, args.Pod)
	if err != nil {
		s.recordScheduleFilterResultEvent(args.Pod, EventReasonFilteringFailed, "", err)
		return nil, err
	}
	if len(failedNodes) != 0 {
<<<<<<< HEAD
		klog.V(5).InfoS("Nodes failed during usage retrieval",
			"nodes", failedNodes)
	}
	nodeScores, err := s.calcScore(nodeUsage, resourceReqs, args.Pod, failedNodes)
=======
		klog.V(5).InfoS("getNodesUsage failed nodes", "nodes", failedNodes)
	}
	nodeScores, err := calcScore(nodeUsage, &failedNodes, nums, annos, args.Pod)
>>>>>>> c7a3893 (Remake this repo to HAMi)
	if err != nil {
		err := fmt.Errorf("calcScore failed %v for pod %v", err, args.Pod.Name)
		s.recordScheduleFilterResultEvent(args.Pod, EventReasonFilteringFailed, "", err)
		return nil, err
	}
	if len((*nodeScores).NodeList) == 0 {
		klog.V(4).InfoS("No available nodes meet the required scores",
			"pod", args.Pod.Name)
		s.recordScheduleFilterResultEvent(args.Pod, EventReasonFilteringFailed, "", fmt.Errorf("no available node, %d nodes do not meet", len(*args.NodeNames)))
		return &extenderv1.ExtenderFilterResult{
			FailedNodes: failedNodes,
		}, nil
	}
	klog.V(4).Infoln("nodeScores_len=", len((*nodeScores).NodeList))
	sort.Sort(nodeScores)
	m := (*nodeScores).NodeList[len((*nodeScores).NodeList)-1]
	klog.InfoS("Scheduling pod to node",
		"podNamespace", args.Pod.Namespace,
		"podName", args.Pod.Name,
		"nodeID", m.NodeID,
		"devices", m.Devices)
	annotations := make(map[string]string)
	annotations[util.AssignedNodeAnnotations] = m.NodeID
	annotations[util.AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)

	for _, val := range device.GetDevices() {
		val.PatchAnnotations(args.Pod, &annotations, m.Devices)
	}

	if s.podManager.AddPod(args.Pod, m.NodeID, m.Devices) {
		s.quotaManager.AddUsage(args.Pod, m.Devices)
	}
	err = util.PatchPodAnnotations(args.Pod, annotations)
	if err != nil {
		s.recordScheduleFilterResultEvent(args.Pod, EventReasonFilteringFailed, "", err)
		s.podManager.DelPod(args.Pod)
		return nil, err
	}
	successMsg := genSuccessMsg(len(*args.NodeNames), m.NodeID, nodeScores.NodeList)
	s.recordScheduleFilterResultEvent(args.Pod, EventReasonFilteringSucceed, successMsg, nil)
	res := extenderv1.ExtenderFilterResult{NodeNames: &[]string{m.NodeID}}
	return &res, nil
}

func genSuccessMsg(totalNodes int, target string, nodes []*policy.NodeScore) string {
	successMsg := "find fit node(%s), %d nodes not fit, %d nodes fit(%s)"
	var scores []string
	for _, no := range nodes {
		scores = append(scores, fmt.Sprintf("%s:%.2f", no.NodeID, no.Score))
	}
	score := strings.Join(scores, ",")
	return fmt.Sprintf(successMsg, target, totalNodes-len(nodes), len(nodes), score)
}
