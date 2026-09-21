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
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	coordinationv1 "k8s.io/client-go/listers/coordination/v1"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	extenderv1 "k8s.io/kube-scheduler/extender/v1"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	metrics "github.com/Project-HAMi/HAMi/pkg/metrics"
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
	//Node Overview
	overviewstatus map[string]*NodeUsage
	// printedLog records the nodes whose devices have already been logged at
	// info level, so a re-registration logs at V(5) instead. It is pruned when
	// a node is deleted, both to keep it bounded under node churn and so a node
	// that returns under the same name is logged as newly added again.
	printedLog    map[string]bool
	eventRecorder record.EventRecorder
	started       uint32 // 0 = false, 1 = true

	lock   sync.RWMutex
	synced atomic.Bool

	// allocLock serializes reservation mutations between Filter, the NUMA
	// refit (RefitNumaAllocation), and pod updates that release init-container
	// usage. kube-scheduler already serializes Filter calls per scheduling
	// cycle, so in the common path this adds no contention; it exists so these
	// paths cannot observe or produce half-applied accounting.
	allocLock sync.Mutex

	allocationMetrics     *metrics.SchedulerOutcomeMetrics
	allocationMetricsOnce sync.Once
}

// NewScheduler initializes the scheduler's managers and outcome metrics.
func NewScheduler() *Scheduler {
	klog.InfoS("Initializing HAMi scheduler")
	s := &Scheduler{
		stopCh:            make(chan struct{}),
		overviewstatus:    make(map[string]*NodeUsage),
		printedLog:        make(map[string]bool),
		nodeNotify:        make(chan struct{}, 1),
		leaderNotify:      make(chan struct{}, 1),
		started:           0,
		allocationMetrics: metrics.NewSchedulerOutcomeMetrics(),
	}
	s.nodeManager = newNodeManager()
	s.podManager = device.NewPodManager()
	s.quotaManager = device.NewQuotaManager()
	s.leaderManager = leaderelection.NewDummyLeaderManager(true)
	if config.LeaderElect {
		callbacks := leaderelection.LeaderCallbacks{
			OnStartedLeading: func() {
				select {
				case s.leaderNotify <- struct{}{}:
				default:
				}
			},
			OnStoppedLeading: func() {
				s.synced.Store(false)
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

// GetAllocationMetrics returns the scheduler's bounded-cardinality outcome metrics.
func (s *Scheduler) GetAllocationMetrics() *metrics.SchedulerOutcomeMetrics {
	s.allocationMetricsOnce.Do(func() {
		if s.allocationMetrics == nil {
			s.allocationMetrics = metrics.NewSchedulerOutcomeMetrics()
		}
	})
	return s.allocationMetrics
}

// deviceTypeForRequests returns the single requested device type, or a bounded
// fallback when the request contains zero or multiple device types.
func deviceTypeForRequests(requests device.PodDeviceRequests) string {
	types := make(map[string]struct{})
	for _, container := range requests {
		for deviceType := range container {
			types[deviceType] = struct{}{}
		}
	}
	if len(types) == 0 {
		return "unknown"
	}
	if len(types) > 1 {
		return "mixed"
	}
	for deviceType := range types {
		return deviceType
	}
	return "unknown"
}

// deviceTypeForPodDevices returns the single allocated device type, or a
// bounded fallback when the allocation contains zero or multiple types.
func deviceTypeForPodDevices(devices device.PodDevices) string {
	types := make(map[string]struct{})
	for deviceType := range devices {
		types[deviceType] = struct{}{}
	}
	if len(types) == 0 {
		return "unknown"
	}
	if len(types) > 1 {
		return "mixed"
	}
	for deviceType := range types {
		return deviceType
	}
	return "unknown"
}

// podAllocationType returns the device type recorded for a cached pod.
func (s *Scheduler) podAllocationType(pod *corev1.Pod) string {
	if s.podManager == nil {
		return "unknown"
	}
	if pi, ok := s.podManager.GetPod(pod); ok {
		return deviceTypeForPodDevices(pi.Devices)
	}
	return "unknown"
}

// bindFailureReason maps internal bind errors to the bounded metric vocabulary.
func bindFailureReason(err error) metrics.SchedulerFailureReason {
	if errors.Is(err, nodelockutil.ErrNodeLockContention) {
		return metrics.FailureReasonLock
	}
	if errors.Is(err, errBindAnnotationPatch) {
		return metrics.FailureReasonAnnotationPatch
	}
	return metrics.FailureReasonBind
}

// errBindAnnotationPatch marks annotation patch failures during binding.
var errBindAnnotationPatch = errors.New("bind annotation patch")

func (s *Scheduler) doNodeNotify() {
	select {
	case s.nodeNotify <- struct{}{}:
	default:
	}
}

func (s *Scheduler) onAddPod(obj any) {
	pod, ok := obj.(*corev1.Pod)
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
		if pi, ok := s.podManager.TakeAndDeletePod(pod); ok {
			s.quotaManager.RmUsage(pod, pi.Devices)
		}
		return
	}
	if util.IsPodTerminating(pod) {
		// A terminating pod still holds its devices. When it is already
		// cached, refresh the object; when it is not (the informer's initial
		// sync after a scheduler restart replays it as an add), fall through
		// so its usage is accounted instead of silently dropped.
		if _, cached := s.podManager.GetPod(pod); cached {
			klog.V(5).InfoS("Pod is terminating but holding locks, preserving cache", "pod", pod.Name)
			s.podManager.UpdatePod(pod)
			return
		}
	}

	rawDevices, err := device.DecodePodDevices(device.SupportDevices, pod.Annotations)
	if err != nil {
		klog.ErrorS(err, "failed to decode pod devices", "pod", klog.KObj(pod))
		return
	}

	effectiveDevices := device.CollapseInitContainerUsage(pod, rawDevices)

	if s.podManager.AddPod(pod, nodeID, effectiveDevices) {
		s.quotaManager.AddUsage(pod, effectiveDevices)
	}
}

func (s *Scheduler) onUpdatePod(oldObj, newObj any) {
	newPod, ok := newObj.(*corev1.Pod)
	if !ok {
		return
	}

	klog.V(5).InfoS("Pod updated", "pod", klog.KObj(newPod))

	if _, ok := newPod.Annotations[util.AssignedNodeAnnotations]; !ok {
		return
	}

	if util.IsPodInTerminatedState(newPod) {
		if pi, ok := s.podManager.TakeAndDeletePod(newPod); ok {
			s.quotaManager.RmUsage(newPod, pi.Devices)
		}
		return
	}

	if util.IsPodTerminating(newPod) {
		// Same as onAddPod: a resync update for a terminating pod that is
		// missing from the cache must be accounted, not dropped.
		if _, cached := s.podManager.GetPod(newPod); !cached {
			s.onAddPod(newPod)
			return
		}
		s.podManager.UpdatePod(newPod)
		return
	}

	// RefitNumaAllocation reads the release flag, devices, and quota as one
	// accounting snapshot. Keep the normal update and the one-time init-usage
	// transition in the same critical section so a refit cannot commit from a
	// stale pre-release snapshot after this handler records steady-state usage.
	s.allocLock.Lock()
	defer s.allocLock.Unlock()

	pi, exists := s.podManager.GetPod(newPod)
	if !exists {
		s.onAddPod(newPod)
		return
	}

	s.podManager.UpdatePod(newPod)

	if !pi.InitContainerResourceReleased && util.AllNonSidecarInitContainersSucceeded(newPod) {
		rawDevices, err := device.DecodePodDevices(device.SupportDevices, newPod.Annotations)
		if err != nil {
			klog.ErrorS(err, "failed to decode pod devices during shrink", "pod", klog.KObj(newPod))
			return
		}

		steadyStateDevices := device.SteadyStateDeviceUsage(newPod, rawDevices)

		oldDevices, ok := s.podManager.UpdatePodDevice(newPod, steadyStateDevices)
		if ok {
			s.quotaManager.ReplaceUsage(newPod, oldDevices, steadyStateDevices)
			klog.InfoS("Non-sidecar init containers completed, shrunk usage to steady state",
				"pod", klog.KObj(newPod),
				"oldUsage", oldDevices,
				"newUsage", steadyStateDevices,
			)
		}
	}
}

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
			klog.V(4).InfoS("Received tombstone for non-pod object on pod delete", "type", fmt.Sprintf("%T", t.Obj))
			return
		}
	default:
		klog.Errorf("Received unknown object type on pod delete")
		return
	}

	// Delete notifications can contain incomplete Pod objects. The cached
	// allocation, keyed by the immutable UID, is the cleanup source of truth.
	if pi, ok := s.podManager.TakeAndDeletePod(pod); ok {
		s.quotaManager.RmUsage(pod, pi.Devices)
	}
}

// onDelNode handles node delete events. It removes any in-memory per-node
// lock bookkeeping and per-device health bookkeeping to avoid unbounded growth
// when nodes are removed by autoscalers or administratively.
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
	// Clear per-device health bookkeeping for the deleted node.
	// Devices that track per-node state (e.g. NvidiaGPUDevices) implement
	// NodeDeleted to prune their internal maps; others are a no-op.
	for _, devInstance := range device.GetDevices() {
		if nd, ok := devInstance.(*nvidia.NvidiaGPUDevices); ok {
			nd.NodeDeleted(nodeName)
		}
	}
}

// cleanupNodeUsage removes the node from the overviewstatus and printedLog maps
// to ensure metrics no longer report data for deleted nodes, and that a node
// recreated under the same name is logged as newly added rather than updated.
func (s *Scheduler) cleanupNodeUsage(nodeID string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok := s.overviewstatus[nodeID]; ok {
		delete(s.overviewstatus, nodeID)
		klog.V(4).InfoS("Removed node from overviewstatus", "node", nodeID)
	}
	delete(s.printedLog, nodeID)
}

func (s *Scheduler) onAddQuota(obj any) {
	quota, ok := obj.(*corev1.ResourceQuota)
	if !ok {
		klog.Errorf("unknown add object type")
		return
	}
	s.quotaManager.AddQuota(quota)
}

// onUpdateQuota applies both halves of the change under a single lock. Doing
// this as a delete followed by an add leaves the limits zeroed in between, and
// FitQuota reads a zero limit as no limit, so quota goes unenforced for the gap.
func (s *Scheduler) onUpdateQuota(oldObj, newObj any) {
	oldQuota, ok := asResourceQuota(oldObj)
	if !ok {
		return
	}
	newQuota, ok := asResourceQuota(newObj)
	if !ok {
		return
	}
	s.quotaManager.UpdateQuota(oldQuota, newQuota)
}

func (s *Scheduler) onDelQuota(obj any) {
	quota, ok := asResourceQuota(obj)
	if !ok {
		return
	}
	s.quotaManager.DelQuota(quota)
}

// asResourceQuota unwraps an informer object, including the tombstone a delete
// carries when the watch missed the event.
func asResourceQuota(obj any) (*corev1.ResourceQuota, bool) {
	switch t := obj.(type) {
	case *corev1.ResourceQuota:
		return t, true
	case cache.DeletedFinalStateUnknown:
		quota, ok := t.Obj.(*corev1.ResourceQuota)
		if !ok {
			klog.Errorf("resource quota tombstone contained object of type %T", t.Obj)
			return nil, false
		}
		return quota, true
	default:
		klog.Errorf("unknown resource quota object type %T", obj)
		return nil, false
	}
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
		return fmt.Errorf("failed to register pod event handler: %w", err)
	}
	nodeEventHandlerRegistration, err := informerFactory.Core().V1().Nodes().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(_ any) { s.doNodeNotify() },
		DeleteFunc: s.onDelNode,
	})
	if err != nil {
		return fmt.Errorf("failed to register node event handler: %w", err)
	}
	resourceQuotaEventHandlerRegistration, err := informerFactory.Core().V1().ResourceQuotas().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.onAddQuota,
		UpdateFunc: s.onUpdateQuota,
		DeleteFunc: s.onDelQuota,
	})
	if err != nil {
		return fmt.Errorf("failed to register resource quota event handler: %w", err)
	}

	informerFactory.Start(s.stopCh)
	informerFactory.WaitForCacheSync(s.stopCh)
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
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
}

func (s *Scheduler) RegisterFromNodeAnnotations() {
	klog.InfoS("Entering RegisterFromNodeAnnotations")
	defer klog.InfoS("Exiting RegisterFromNodeAnnotations")

	labelSelector := labels.Set(config.NodeLabelSelector).AsSelector()
	klog.InfoS("Using label selector for list nodes", "selector", labelSelector.String())

	ticker := time.NewTicker(time.Second * 15)
	defer ticker.Stop()
	for {
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
		if atomic.LoadUint32(&s.started) == 0 {
			klog.V(5).InfoS("Scheduler not started yet, skipping ...")
			continue
		}
		s.register(labelSelector)
	}
}

func (s *Scheduler) register(labelSelector labels.Selector) {
	// Lock here to avoid setting s.synced to false, when we lost leadership, while doing register.
	// 1. lost leadership before register: synced will set to false in callbacks, and register will be skipped because IsLeader() returns false
	// 2. lost leadership during or after register: synced will set to true after finishing register, and callback will set it to false again after lock is acquired by callback
	s.lock.Lock()
	defer s.lock.Unlock()

	// Only do registration when we are leader.
	isLeader := s.leaderManager.IsLeader()
	if isLeader {
		s.updateSchedulerLabel()
	} else {
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
				klog.V(5).InfoS("Failed to get node devices", "nodeName", val.Name, "deviceVendor", devhandsk, "error", err)
			}

			health, needUpdate := devInstance.CheckHealth(devhandsk, val)
			klog.V(5).InfoS("Device health check result", "nodeName", val.Name, "deviceVendor", devhandsk, "health", health, "needUpdate", needUpdate)

			if !health {
				existingNode, getNodeErr := s.GetNode(val.Name)
				if getNodeErr != nil {
					klog.V(5).InfoS("Skipping device cleanup for node not present in scheduler cache", "nodeName", val.Name, "deviceVendor", devhandsk)
					continue
				}
				if _, ok := existingNode.Devices[devhandsk]; !ok {
					klog.V(5).InfoS("Skipping device cleanup for vendor not present in scheduler cache", "nodeName", val.Name, "deviceVendor", devhandsk)
					continue
				}
				// klog.Warning does plain fmt.Print-style concatenation of its arguments -
				// klog v2 has no structured WarningS variant. Passing alternating
				// "key", value pairs to it (as if it were InfoS/ErrorS) produces a garbled,
				// unstructured log line instead of the intended structured fields. Use
				// ErrorS (nil error is fine here; this is a detected condition, not a Go
				// error) to match the structured logging used throughout the rest of this
				// file.
				klog.ErrorS(nil, "Device is unhealthy, cleaning up node", "nodeName", val.Name, "deviceVendor", devhandsk)
				err := devInstance.NodeCleanUp(val.Name)
				if err != nil {
					klog.ErrorS(err, "Node cleanup failed", "nodeName", val.Name, "deviceVendor", devhandsk)
				}

				s.rmNodeDevices(val.Name, devhandsk)
				continue
			}
			if err != nil {
				continue
			}
			// GetNodeDevices succeeded but reported zero devices: the vendor plugin
			// is healthy but no longer advertising devices on this node. Remove any
			// stale entry so the scheduler does not keep offering capacity that no
			// longer exists.
			if len(nodedevices) == 0 {
				if existingNode, getNodeErr := s.GetNode(val.Name); getNodeErr == nil {
					if _, ok := existingNode.Devices[devhandsk]; ok {
						klog.InfoS("Vendor reports zero devices, removing stale cache entry", "nodeName", val.Name, "deviceVendor", devhandsk)
						s.rmNodeDevices(val.Name, devhandsk)
					}
				}
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
			// Log the locally built nodeInfo; reading it back from s.nodes raced with onDelNode->rmNode.
			if len(nodeInfo.Devices) > 0 {
				if s.printedLog[val.Name] {
					klog.V(5).InfoS("Node device updated", "nodeName", val.Name, "deviceVendor", devhandsk, "nodeInfo", nodeInfo)
				} else {
					klog.InfoS("Node device added", "nodeName", val.Name, "deviceVendor", devhandsk, "nodeInfo", nodeInfo)
					s.printedLog[val.Name] = true
				}
			}
		}
	}
	_, overallnodeMap, _, err := s.getNodesUsage(&nodeNames, nil)
	if err != nil {
		klog.ErrorS(err, "Failed to get node usage", "nodeNames", nodeNames)
		return
	}
	s.overviewstatus = *overallnodeMap

	// Set synced to true only after getNodeUsage() succeeds
	s.synced.Store(true)
}

func (s *Scheduler) updateSchedulerLabel() {
	schedulerSelector := labels.Set(map[string]string{util.HAMiComponentLabel: util.HAMiComponentScheduler}).AsSelector()
	schedulerPods, err := s.podLister.Pods(os.Getenv("POD_NAMESPACE")).List(schedulerSelector)
	if err != nil {
		klog.ErrorS(err, "Failed to list hami scheduler pods from lister",
			"namespace", os.Getenv("POD_NAMESPACE"),
			"selector", schedulerSelector.String(),
		)
		return
	}

	for idx := range schedulerPods {
		pod := schedulerPods[idx]
		if pod.Name == os.Getenv("POD_NAME") {
			// The pod is leader, apply the leader role label to it.
			if pod.Labels == nil || pod.Labels[util.HAMiRoleLabel] != util.HAMiRoleLabelValueLeader {
				err := util.PatchPodLabels(
					pod.Namespace,
					pod.Name,
					map[string]string{util.HAMiRoleLabel: util.HAMiRoleLabelValueLeader},
				)
				if err != nil {
					klog.ErrorS(err, "Failed to patch the leader label to hami scheduler pod",
						"namespace", pod.Namespace,
						"pod", pod.Name,
					)
				} else {
					klog.V(4).InfoS("Successfully patched leader label to hami scheduler pod",
						"namespace", pod.Namespace,
						"pod", pod.Name,
					)
				}
			}
		} else {
			// The pod is not the leader, apply the follower role label to it.
			if pod.Labels == nil || pod.Labels[util.HAMiRoleLabel] != util.HAMiRoleLabelValueFollower {
				err := util.PatchPodLabels(
					pod.Namespace,
					pod.Name,
					map[string]string{util.HAMiRoleLabel: util.HAMiRoleLabelValueFollower},
				)
				if err != nil {
					klog.ErrorS(err, "Failed to patch leader label from pod",
						"namespace", pod.Namespace,
						"pod", pod.Name,
					)
				} else {
					klog.V(4).InfoS("Successfully patched follower label to hami scheduler pod",
						"namespace", pod.Namespace,
						"pod", pod.Name,
					)
				}
			}
		}
	}
}

// IsSynced returns true when the scheduler's internal node/device cache has
// completed at least one successful sync cycle and is ready to serve requests.
// It uses atomic lock-free reads and is safe to call from Prometheus Collect callbacks
// without contending on the scheduler's write lock during cache refreshes.
func (s *Scheduler) IsSynced() bool {
	return s.synced.Load()
}

func (s *Scheduler) WaitForCacheSync(ctx context.Context) bool {
	err := wait.PollUntilContextCancel(ctx, syncedPollPeriod, true, func(context.Context) (done bool, err error) {
		return s.synced.Load(), nil
	})
	if err != nil {
		klog.ErrorS(err, "failed to poll until context cancel")
		return false
	}

	return true
}

// InspectAllNodesUsage is used by metrics monitor.
func (s *Scheduler) InspectAllNodesUsage() *map[string]*NodeUsage {
	s.lock.RLock()
	defer s.lock.RUnlock()

	snapshot := make(map[string]*NodeUsage, len(s.overviewstatus))
	for nodeID, usage := range s.overviewstatus {
		snapshot[nodeID] = usage.DeepCopy()
	}
	return &snapshot
}

// numaBindingRequested reports whether the pod requests numa-bind affinity.
func numaBindingRequested(task *corev1.Pod) bool {
	if task == nil {
		return false
	}
	v, ok := task.Annotations[nvidia.NumaBind]
	if !ok {
		return false
	}
	enforce, err := strconv.ParseBool(v)
	return err == nil && enforce
}

func buildNodeUsage(node *device.NodeInfo, task *corev1.Pod) *NodeUsage {
	userGPUPolicy := util.GetGPUSchedulerPolicyByPod(device.GPUSchedulerPolicy, task)
	nodeUsage := &NodeUsage{
		Node:     node.Node,
		NodeInfo: node,
		Devices: policy.DeviceUsageList{
			Policy:      userGPUPolicy,
			NumaBind:    numaBindingRequested(task),
			DeviceLists: make([]*policy.DeviceListsScore, 0),
		},
	}
	for _, vendorDevices := range node.Devices {
		for _, d := range vendorDevices {
			nodeUsage.Devices.DeviceLists = append(nodeUsage.Devices.DeviceLists, &policy.DeviceListsScore{
				Score: 0,
				Device: &device.DeviceUsage{
					ID:           d.ID,
					Index:        d.Index,
					Used:         0,
					Count:        d.Count,
					Usedmem:      0,
					Totalmem:     d.Devmem,
					Totalcore:    d.Devcore,
					Usedcores:    0,
					MigProfiles:  d.MIGProfiles,
					Mode:         d.Mode,
					Type:         d.Type,
					DeviceVendor: d.DeviceVendor,
					Numa:         d.Numa,
					Health:       d.Health,
					PodInfos:     make([]*device.PodInfo, 0),
					CustomInfo:   maps.Clone(d.CustomInfo),
				},
			})
		}
	}
	return nodeUsage
}

func buildTransientNodeInfo(node *corev1.Node) (*device.NodeInfo, error) {
	nodeInfo := &device.NodeInfo{
		ID:      node.Name,
		Node:    node.DeepCopy(),
		Devices: make(map[string][]device.DeviceInfo),
	}
	for _, devInstance := range device.GetDevices() {
		nodedevices, err := devInstance.GetNodeDevices(*node)
		if err != nil || len(nodedevices) == 0 {
			continue
		}
		for _, deviceInfo := range nodedevices {
			nodeInfo.Devices[deviceInfo.DeviceVendor] = append(nodeInfo.Devices[deviceInfo.DeviceVendor], *deviceInfo)
		}
	}
	if len(nodeInfo.Devices) == 0 {
		return nil, fmt.Errorf("node unregistered")
	}
	return nodeInfo, nil
}

func nodeNamesLen(nodeNames *[]string) int {
	if nodeNames == nil {
		return 0
	}
	return len(*nodeNames)
}

func nodeListLen(nodes *corev1.NodeList) int {
	if nodes == nil {
		return 0
	}
	return len(nodes.Items)
}

func migAllocationUsage(allocation nvidia.MigAllocation) device.MigAllocation {
	usage := device.MigAllocation{
		Profile: allocation.Profile, Placement: allocation.Placement,
		MigUUID:      allocation.MigUUID,
		RuntimeReady: allocation.MigUUID != "" && allocation.GPUInstanceID != nil && allocation.ComputeInstanceID != nil,
	}
	if allocation.GPUInstanceID != nil {
		usage.GPUInstanceID = *allocation.GPUInstanceID
	}
	if allocation.ComputeInstanceID != nil {
		usage.ComputeInstanceID = *allocation.ComputeInstanceID
	}
	return usage
}

// returns all nodes and its device memory usage, and we filter it with nodeSelector, taints, nodeAffinity
// unschedulerable and nodeName.
func (s *Scheduler) getNodesUsage(nodes *[]string, task *corev1.Pod) (*map[string]*NodeUsage, *map[string]*NodeUsage, map[string]string, error) {
	overallnodeMap := make(map[string]*NodeUsage)
	cachenodeMap := make(map[string]*NodeUsage)
	failedNodes := make(map[string]string)
	allNodes, err := s.ListNodes()
	if err != nil {
		return &overallnodeMap, &overallnodeMap, failedNodes, err
	}

	for _, node := range allNodes {
		overallnodeMap[node.ID] = buildNodeUsage(node, task)
	}

	podsInfo := s.podManager.ListPodsInfo()
	for _, p := range podsInfo {
		allocationsByGPU := map[string][]nvidia.MigAllocation{}
		if slotRaw, ok := p.Annotations[nvidia.MigAllocationsAnnotation]; ok {
			if allocations, err := nvidia.DecodeMigAllocations(slotRaw); err == nil {
				for _, allocation := range allocations {
					allocationsByGPU[allocation.GPUUUID] = append(allocationsByGPU[allocation.GPUUUID], allocation)
				}
			}
		}
		node, ok := overallnodeMap[p.NodeID]
		if !ok {
			klog.V(5).InfoS("pod allocated unknown node resources",
				"pod", klog.KRef(p.Namespace, p.Name), "nodeID", p.NodeID)
			continue
		}
		for _, podsingleds := range p.Devices {
			for _, ctrdevs := range podsingleds {
				for _, udevice := range ctrdevs {
					matched := false
					for _, d := range node.Devices.DeviceLists {
						deviceID := udevice.UUID
						if d.Device.ID == deviceID {
							matched = true
							// Raw entries carry no slot count; clamp to at least one.
							slots := max(udevice.Slots, 1)
							d.Device.Used += slots
							d.Device.Usedmem += udevice.Usedmem
							d.Device.Usedcores += udevice.Usedcores
							d.Device.PodInfos = append(d.Device.PodInfos, p)

							if allocations := allocationsByGPU[udevice.UUID]; len(allocations) > 0 {
								if strings.Compare(d.Device.Mode, "hami-core") == 0 {
									klog.Errorf("found a mig task running on a hami-core GPU\n")
									d.Device.Health = false
									continue
								}
								allocation := allocations[0]
								allocationsByGPU[udevice.UUID] = allocations[1:]
								d.Device.MigAllocationsInUse = append(d.Device.MigAllocationsInUse, migAllocationUsage(allocation))
								continue
							}
							if d.Device.Mode == nvidia.MigMode {
								klog.ErrorS(nil, "MIG Pod lacks a matching profile/placement reservation", "pod", klog.KRef(p.Namespace, p.Name), "gpuUUID", udevice.UUID)
								d.Device.Health = false
							}
						}
					}
					if !matched {
						klog.ErrorS(nil, "pod allocated unknown or stale device resources", "pod", klog.KRef(p.Namespace, p.Name), "nodeID", p.NodeID, "gpuUUID", udevice.UUID)
					}
				}
			}
		}
		for gpuUUID, allocations := range allocationsByGPU {
			if len(allocations) == 0 {
				continue
			}
			matched := false
			for _, d := range node.Devices.DeviceLists {
				if d.Device.ID != gpuUUID {
					continue
				}
				matched = true
				if d.Device.Mode != nvidia.MigMode {
					klog.ErrorS(nil, "unconsumed MIG reservations reference a non-MIG device", "pod", klog.KRef(p.Namespace, p.Name), "gpuUUID", gpuUUID, "reservations", len(allocations))
					d.Device.Health = false
					break
				}
				for _, allocation := range allocations {
					d.Device.MigAllocationsInUse = append(d.Device.MigAllocationsInUse, migAllocationUsage(allocation))
				}
				klog.InfoS("restored MIG reservations missing from cached Pod devices", "pod", klog.KRef(p.Namespace, p.Name), "gpuUUID", gpuUUID, "reservations", len(allocations))
				break
			}
			if !matched {
				klog.ErrorS(nil, "unconsumed MIG reservations reference an unknown device", "pod", klog.KRef(p.Namespace, p.Name), "gpuUUID", gpuUUID, "reservations", len(allocations))
				for _, d := range node.Devices.DeviceLists {
					if d.Device.Mode == nvidia.MigMode {
						d.Device.Health = false
					}
				}
			}
		}
		klog.V(5).Infof("usage: pod %v assigned %v %v", p.Name, p.NodeID, p.Devices)
	}
	if nodes == nil {
		return &cachenodeMap, &overallnodeMap, failedNodes, nil
	}
	for _, nodeID := range *nodes {
		node, err := s.GetNode(nodeID)
		if err != nil {
			// The identified node does not have a gpu device, so the log here has no practical meaning,increase log priority.
			klog.V(5).InfoS("node unregistered", "node", nodeID, "error", err)
			failedNodes[nodeID] = "node unregistered"
			continue
		}
		usage, ok := overallnodeMap[node.ID]
		if !ok {
			klog.V(5).InfoS("node usage not found in snapshot", "node", nodeID)
			failedNodes[nodeID] = "node usage unavailable"
			continue
		}
		cachenodeMap[node.ID] = usage
	}
	return &cachenodeMap, &overallnodeMap, failedNodes, nil
}

func (s *Scheduler) getSimulationNodesUsage(nodes *corev1.NodeList, task *corev1.Pod) (*map[string]*NodeUsage, map[string]string, error) {
	candidateNodes := make(map[string]*NodeUsage)
	failedNodes := make(map[string]string)
	if nodes == nil {
		klog.V(3).InfoS("Simulation node usage requested with nil node list",
			"pod", klog.KObj(task))
		return &candidateNodes, failedNodes, nil
	}
	klog.V(3).InfoS("Building simulation node usage from request nodes",
		"pod", klog.KObj(task),
		"nodesLen", len(nodes.Items))
	for i := range nodes.Items {
		node := &nodes.Items[i]
		nodeInfo, err := buildTransientNodeInfo(node)
		if err != nil {
			klog.V(4).InfoS("Simulation node rejected during transient node construction",
				"pod", klog.KObj(task),
				"node", node.Name,
				"reason", err.Error())
			failedNodes[node.Name] = err.Error()
			continue
		}
		candidateNodes[node.Name] = buildNodeUsage(nodeInfo, task)
	}
	klog.V(3).InfoS("Built simulation node usage",
		"pod", klog.KObj(task),
		"candidateNodes", len(candidateNodes),
		"failedNodes", len(failedNodes))
	return &candidateNodes, failedNodes, nil
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

func (s *Scheduler) cleanupStalePodAllocation(pod *corev1.Pod) {
	if pi, ok := s.podManager.TakeAndDeletePod(pod); ok && len(pi.Devices) > 0 {
		s.quotaManager.RmUsage(pod, pi.Devices)
	}
}

func (s *Scheduler) lockAllDevices(node *corev1.Node, pod *corev1.Pod) error {
	devs := device.GetDevices()
	keys := make([]string, 0, len(devs))
	for k := range devs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	acquired := make([]device.Devices, 0, len(keys))
	for _, k := range keys {
		val := devs[k]
		if err := val.LockNode(node, pod); err != nil {
			for _, locked := range slices.Backward(acquired) {
				if relErr := locked.ReleaseNodeLock(node, pod); relErr != nil {
					klog.ErrorS(relErr, "Failed to release node lock during rollback", "node", node.Name, "pod", klog.KObj(pod))
				}
			}
			return err
		}
		acquired = append(acquired, val)
	}
	return nil
}

func (s *Scheduler) releaseAllDevices(node *corev1.Node, pod *corev1.Pod) {
	devs := device.GetDevices()
	keys := make([]string, 0, len(devs))
	for k := range devs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := devs[k]
		if err := val.ReleaseNodeLock(node, pod); err != nil {
			klog.ErrorS(err, "Failed to release node lock", "node", node.Name, "pod", klog.KObj(pod))
		}
	}
}

func (s *Scheduler) acquireNodeLocks(node *corev1.Node, pod *corev1.Pod) error {
	if !util.IsPodGroupMember(pod) || config.NodeLockRetryTimeout <= 0 {
		return s.lockAllDevices(node, pod)
	}

	deadline := time.Now().Add(config.NodeLockRetryTimeout)
	for {
		err := s.lockAllDevices(node, pod)
		if err == nil {
			return nil
		}
		if !nodelockutil.IsNodeLockContention(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %v waiting for node %s to be unlocked: %w",
				config.NodeLockRetryTimeout, node.Name, nodelockutil.ErrNodeLockContention)
		}
		select {
		case <-s.stopCh:
			return fmt.Errorf("scheduler shutting down while waiting for node lock: %w", nodelockutil.ErrNodeLockContention)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// verifyBindTarget rejects a bind request that does not describe the live pod
// it names. Everything below it acts on privileged state: node locks, an
// annotation patch and the binding itself, all driven by fields the caller
// supplied.
func (s *Scheduler) verifyBindTarget(args extenderv1.ExtenderBindingArgs, current *corev1.Pod) error {
	// An absent UID is refused rather than waved through, otherwise omitting
	// the field is all it takes to skip the identity check below.
	if args.PodUID == "" {
		return fmt.Errorf("bind request for pod %s/%s carries no UID", args.PodNamespace, args.PodName)
	}
	if current.UID != args.PodUID {
		return fmt.Errorf("pod %s/%s has UID %s, bind request carries UID %s",
			args.PodNamespace, args.PodName, current.UID, args.PodUID)
	}
	if current.Spec.NodeName != "" {
		return fmt.Errorf("pod %s/%s is already assigned to node %s",
			args.PodNamespace, args.PodName, current.Spec.NodeName)
	}
	// The node the filter phase picked is the only node this pod may be bound
	// to. Binding it elsewhere would leave the reservation on the scheduled
	// node while the pod consumes devices on another.
	if pi, ok := s.podManager.GetPod(current); ok && pi.NodeID != "" && pi.NodeID != args.Node {
		return fmt.Errorf("pod %s/%s was scheduled to node %s, bind request names node %s",
			args.PodNamespace, args.PodName, pi.NodeID, args.Node)
	}
	return nil
}

// Bind validates and commits a reserved device allocation to the target node.
func (s *Scheduler) Bind(args extenderv1.ExtenderBindingArgs) (*extenderv1.ExtenderBindingResult, error) {
	klog.InfoS("Attempting to bind pod to node", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
	var res *extenderv1.ExtenderBindingResult

	binding := &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: args.PodName, UID: args.PodUID},
		Target:     corev1.ObjectReference{Kind: "Node", Name: args.Node},
	}

	current, err := s.podLister.Pods(args.PodNamespace).Get(args.PodName)
	if err != nil {
		klog.ErrorS(err, "Failed to get pod from cache", "pod", args.PodName, "namespace", args.PodNamespace)
		s.cleanupStalePodAllocation(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       args.PodUID,
				Name:      args.PodName,
				Namespace: args.PodNamespace,
			},
		})
		s.GetAllocationMetrics().ObserveAllocationFailure("bind", "unknown", metrics.FailureReasonLookup)
		return &extenderv1.ExtenderBindingResult{Error: err.Error()}, err
	}

	if err := s.verifyBindTarget(args, current); err != nil {
		klog.ErrorS(err, "Rejecting bind request", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
		s.GetAllocationMetrics().ObserveAllocationFailure("bind", s.podAllocationType(current), metrics.FailureReasonIdentity)
		return &extenderv1.ExtenderBindingResult{Error: err.Error()}, nil
	}

	klog.InfoS("Trying to get the target node for pod", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)

	node, err := s.nodeLister.Get(args.Node)
	if err != nil {
		klog.ErrorS(err, "Failed to get node from cache", "node", args.Node)
		s.recordScheduleBindingResultEvent(current, EventReasonBindingFailed, []string{}, fmt.Errorf("failed to get node %s", args.Node))
		deviceType := s.podAllocationType(current)
		s.cleanupStalePodAllocation(current)
		s.GetAllocationMetrics().ObserveAllocationFailure("bind", deviceType, metrics.FailureReasonLookup)
		res = &extenderv1.ExtenderBindingResult{Error: err.Error()}
		return res, nil
	}

	tmppatch := map[string]string{
		util.DeviceBindPhase:     "allocating",
		util.BindTimeAnnotations: strconv.FormatInt(time.Now().Unix(), 10),
	}

	fail := func(e error) (*extenderv1.ExtenderBindingResult, error) {
		klog.InfoS("Release node locks", "node", args.Node)
		s.releaseAllDevices(node, current)
		reason := metrics.FailureReasonBind
		if e != nil {
			reason = bindFailureReason(e)
		}
		s.GetAllocationMetrics().ObserveAllocationFailure("bind", s.podAllocationType(current), reason)
		s.GetAllocationMetrics().ObserveBindRollback("bind", s.podAllocationType(current), reason)
		s.recordScheduleBindingResultEvent(current, EventReasonBindingFailed, []string{}, e)
		errStr := ""
		if e != nil {
			errStr = e.Error()
		}
		return &extenderv1.ExtenderBindingResult{Error: errStr}, nil
	}

	if err = s.acquireNodeLocks(node, current); err != nil {
		klog.ErrorS(err, "Failed to lock node", "node", args.Node, "pod", klog.KObj(current))
		return fail(err)
	}

	if err = util.PatchPodAnnotations(current, tmppatch); err != nil {
		klog.ErrorS(err, "Failed to patch pod annotations", "pod", klog.KObj(current))
		return fail(fmt.Errorf("%w: %v", errBindAnnotationPatch, err))
	}

	if err = s.kubeClient.CoreV1().Pods(args.PodNamespace).Bind(context.Background(), binding, metav1.CreateOptions{}); err != nil {
		klog.ErrorS(err, "Failed to bind pod", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
		return fail(err)
	}

	s.recordScheduleBindingResultEvent(current, EventReasonBindingSucceed, []string{args.Node}, nil)
	klog.InfoS("Successfully bound pod to node", "pod", args.PodName, "namespace", args.PodNamespace, "node", args.Node)
	return &extenderv1.ExtenderBindingResult{Error: ""}, nil
}

// authoritativePod resolves the pod an extender request names and refuses the
// request unless the live object still matches the identity the request
// carries and is still unscheduled. /filter and /bind authenticate no caller,
// so the pod in the request body is input, not evidence: a caller is free to
// pair a running pod's identity with a spec of its own, and the device
// reservation and annotation patch further down would act on it.
func (s *Scheduler) authoritativePod(claimed *corev1.Pod) (*corev1.Pod, error) {
	if s.podLister != nil {
		live, err := s.podLister.Pods(claimed.Namespace).Get(claimed.Name)
		if err == nil {
			return matchesClaimedPod(claimed, live)
		}
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		// A pod created moments ago may not have reached the informer yet, so
		// a cache miss falls through to the API server rather than being
		// trusted or rejected on its own.
	}
	live, err := s.kubeClient.CoreV1().Pods(claimed.Namespace).Get(context.Background(), claimed.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return matchesClaimedPod(claimed, live)
}

func matchesClaimedPod(claimed, live *corev1.Pod) (*corev1.Pod, error) {
	// An absent UID is refused rather than waved through, otherwise omitting
	// the field is all it takes to skip the identity check below.
	if claimed.UID == "" {
		return nil, fmt.Errorf("filter request for pod %s/%s carries no UID", claimed.Namespace, claimed.Name)
	}
	if live.UID != claimed.UID {
		return nil, fmt.Errorf("pod %s/%s has UID %s, filter request carries UID %s",
			claimed.Namespace, claimed.Name, live.UID, claimed.UID)
	}
	if live.Spec.NodeName != "" {
		return nil, fmt.Errorf("pod %s/%s is already assigned to node %s",
			claimed.Namespace, claimed.Name, live.Spec.NodeName)
	}
	return live, nil
}

// Filter selects a node and reserves devices for a scheduler extender request.
func (s *Scheduler) Filter(args extenderv1.ExtenderArgs) (*extenderv1.ExtenderFilterResult, error) {
	klog.InfoS("Starting schedule filter process", "pod", args.Pod.Name, "uuid", args.Pod.UID, "namespace", args.Pod.Namespace)

	// Simulation callers describe pods that need not exist, and the path they
	// take reserves nothing, so only the scheduling path resolves the pod.
	pod := args.Pod
	if args.Nodes == nil {
		live, err := s.authoritativePod(args.Pod)
		if err != nil {
			klog.ErrorS(err, "Rejecting filter request", "pod", klog.KObj(args.Pod))
			return nil, err
		}
		pod = live
	}

	resourceReqs := device.Resourcereqs(pod)

	hasHAMiResource := false

	for _, reqMap := range resourceReqs {
		if len(reqMap) > 0 {
			hasHAMiResource = true
			break
		}
	}

	if !hasHAMiResource {
		klog.V(1).InfoS("Pod does not request any resources", "pod", pod.Name)
		// Simulation callers such as the cluster autoscaler send Nodes
		// instead of NodeNames; echo both back so a pod without HAMi
		// resources keeps every candidate node on either protocol shape.
		return &extenderv1.ExtenderFilterResult{
			Nodes:       args.Nodes,
			NodeNames:   args.NodeNames,
			FailedNodes: nil,
			Error:       "",
		}, nil
	}
	if args.Nodes != nil {
		return s.filterSimulation(args, resourceReqs)
	}

	s.allocLock.Lock()
	defer s.allocLock.Unlock()

	if pi, ok := s.podManager.TakeAndDeletePod(pod); ok {
		s.quotaManager.RmUsage(pod, pi.Devices)
	}
	nodeUsage, _, failedNodes, err := s.getNodesUsage(args.NodeNames, pod)
	if err != nil {
		s.GetAllocationMetrics().ObserveAllocationFailure("filter", deviceTypeForRequests(resourceReqs), metrics.FailureReasonInternal)
		s.recordScheduleFilterResultEvent(pod, EventReasonFilteringFailed, "", err)
		return nil, err
	}
	if len(failedNodes) != 0 {
		klog.V(5).InfoS("Nodes failed during usage retrieval", "nodes", failedNodes)
	}
	nodeScores, err := s.calcScore(nodeUsage, resourceReqs, pod, failedNodes)
	if err != nil {
		err := fmt.Errorf("calcScore failed %v for pod %v", err, pod.Name)
		s.GetAllocationMetrics().ObserveAllocationFailure("filter", deviceTypeForRequests(resourceReqs), metrics.FailureReasonInternal)
		s.recordScheduleFilterResultEvent(pod, EventReasonFilteringFailed, "", err)
		return nil, err
	}
	if len((*nodeScores).NodeList) == 0 {
		klog.V(4).InfoS("No available nodes meet the required scores", "pod", pod.Name)
		s.GetAllocationMetrics().ObserveAllocationFailure("filter", deviceTypeForRequests(resourceReqs), metrics.FailureReasonNoFit)
		s.recordScheduleFilterResultEvent(pod, EventReasonFilteringFailed, "", fmt.Errorf("no available node, %d nodes do not meet", len(*args.NodeNames)))
		return &extenderv1.ExtenderFilterResult{
			FailedNodes: failedNodes,
		}, nil
	}
	klog.V(4).Infoln("nodeScores_len=", len((*nodeScores).NodeList))
	sort.Sort(nodeScores)
	m := (*nodeScores).NodeList[len((*nodeScores).NodeList)-1]
	klog.InfoS("Scheduling pod to node",
		"podNamespace", pod.Namespace,
		"podName", pod.Name,
		"nodeID", m.NodeID,
		"devices", m.Devices)
	annotations := make(map[string]string)
	annotations[util.AssignedNodeAnnotations] = m.NodeID
	annotations[util.AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)

	for _, val := range device.GetDevices() {
		val.PatchAnnotations(pod, &annotations, m.Devices)
	}

	rawDevices := m.Devices
	effectiveDevices := device.CollapseInitContainerUsage(pod, rawDevices)
	if args.Nodes == nil {
		added := s.podManager.AddPod(pod, m.NodeID, effectiveDevices)
		if added {
			s.quotaManager.AddUsage(pod, effectiveDevices) // use collapsed
		}
		err = util.PatchPodAnnotations(pod, annotations)
		if err != nil {
			s.GetAllocationMetrics().ObserveAllocationFailure("filter", deviceTypeForRequests(resourceReqs), metrics.FailureReasonAnnotationPatch)
			s.recordScheduleFilterResultEvent(pod, EventReasonFilteringFailed, "", err)
			if added {
				s.quotaManager.RmUsage(pod, effectiveDevices)
			}
			s.podManager.DelPod(pod)
			return nil, err
		}
	}

	s.GetAllocationMetrics().ObserveAllocation("filter", deviceTypeForRequests(resourceReqs))
	successMsg := genSuccessMsg(len(*args.NodeNames), m.NodeID, nodeScores.NodeList)
	s.recordScheduleFilterResultEvent(pod, EventReasonFilteringSucceed, successMsg, nil)
	res := extenderv1.ExtenderFilterResult{NodeNames: &[]string{m.NodeID}}
	return &res, nil
}

func (s *Scheduler) filterSimulation(args extenderv1.ExtenderArgs, resourceReqs device.PodDeviceRequests) (*extenderv1.ExtenderFilterResult, error) {
	klog.V(2).InfoS("Entering simulation filter path",
		"pod", klog.KObj(args.Pod),
		"nodesLen", nodeListLen(args.Nodes))
	nodeUsage, failedNodes, err := s.getSimulationNodesUsage(args.Nodes, args.Pod)
	if err != nil {
		return nil, err
	}
	klog.V(3).InfoS("Collected simulation node usage for filtering",
		"pod", klog.KObj(args.Pod),
		"candidateNodes", len(*nodeUsage),
		"failedNodes", len(failedNodes))
	nodeScores, err := s.calcScoreWithOptions(nodeUsage, resourceReqs, args.Pod, failedNodes, false, true)
	if err != nil {
		return nil, fmt.Errorf("calcScore failed %v for pod %v", err, args.Pod.Name)
	}
	if len(nodeScores.NodeList) == 0 {
		klog.V(3).InfoS("Simulation filter found no fit nodes",
			"pod", klog.KObj(args.Pod),
			"failedNodes", failedNodes)
		return &extenderv1.ExtenderFilterResult{
			FailedNodes: failedNodes,
		}, nil
	}
	sort.Sort(nodeScores)
	bestNodeID := nodeScores.NodeList[len(nodeScores.NodeList)-1].NodeID
	filteredNodes := make([]corev1.Node, 0, 1)
	for i := range args.Nodes.Items {
		if args.Nodes.Items[i].Name == bestNodeID {
			filteredNodes = append(filteredNodes, *args.Nodes.Items[i].DeepCopy())
			break
		}
	}
	klog.V(2).InfoS("Simulation filter selected best node",
		"pod", klog.KObj(args.Pod),
		"selectedNode", bestNodeID,
		"filteredNodesLen", len(filteredNodes))
	return &extenderv1.ExtenderFilterResult{
		Nodes: &corev1.NodeList{
			Items: filteredNodes,
		},
		FailedNodes: failedNodes,
	}, nil
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
