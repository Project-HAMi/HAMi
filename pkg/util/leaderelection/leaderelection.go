package leaderelection

import (
	"strings"
	"sync"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/client-go/tools/cache"
)

type LeaderManager interface {
	IsLeader() bool
	LeaderNotifyChan() <-chan struct{}

	cache.ResourceEventHandler
}

var _ LeaderManager = &leaderManager{}

type leaderManager struct {
	hostname          string
	resourceName      string
	resourceNamespace string

	leaseLock    sync.RWMutex
	lease        *coordinationv1.Lease
	leaderNotify chan struct{}

	cache.FilteringResourceEventHandler
}

func NewLeaderManager(hostname, namespace, name string) *leaderManager {
	m := &leaderManager{
		hostname:          hostname,
		resourceName:      name,
		resourceNamespace: namespace,
		leaderNotify:      make(chan struct{}),
	}

	m.FilteringResourceEventHandler = cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			lease := objectToLease(obj)
			if lease == nil {
				return false
			}
			return lease.Name == m.resourceName && lease.Namespace == m.resourceNamespace
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    m.onAdd,
			UpdateFunc: m.onUpdate,
			DeleteFunc: m.onDelete,
		},
	}

	return m
}

func objectToLease(obj interface{}) *coordinationv1.Lease {
	switch t := obj.(type) {
	case *coordinationv1.Lease:
		return t
	case cache.DeletedFinalStateUnknown:
		if lease, ok := t.Obj.(*coordinationv1.Lease); ok {
			return lease
		}
	default:
		return nil
	}
	return nil
}

// onAdd notifies if we are the leader when lease is created.
func (m *leaderManager) onAdd(obj interface{}) {
	lease, ok := obj.(*coordinationv1.Lease)
	if !ok {
		return
	}

	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()

	m.lease = lease
	// Notify if we are the leader from the very begging
	if m.isHolder(lease) {
		m.leaderNotify <- struct{}{}
	}
}

// onUpdate notifies when we have been elected as leader.
func (m *leaderManager) onUpdate(oldObj, newObj interface{}) {
	newLease, ok := newObj.(*coordinationv1.Lease)
	if !ok {
		return
	}

	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()

	m.lease = newLease
	// Notify if we have been elected to become the leader
	if m.isHolder(newLease) {
		oldLease, ok := oldObj.(*coordinationv1.Lease)
		if !ok {
			return
		}

		if !m.isHolder(oldLease) {
			m.leaderNotify <- struct{}{}
		}
	}
}

func (m *leaderManager) onDelete(obj interface{}) {
	// Do nothing on delete
	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()

	m.lease = nil
}

func (m *leaderManager) isHolder(lease *coordinationv1.Lease) bool {
	// kube-scheduler lease id take format of `hostname + "_" + string(uuid.NewUUID())`
	return lease.Spec.HolderIdentity != nil && strings.HasPrefix(*lease.Spec.HolderIdentity, m.hostname)
}

func (m *leaderManager) IsLeader() bool {
	m.leaseLock.RLock()
	defer m.leaseLock.RUnlock()
	return m.isHolder(m.lease)
}

func (m *leaderManager) LeaderNotifyChan() <-chan struct{} {
	return m.leaderNotify
}

type dummyLeaderManager struct {
	elected bool
	cache.ResourceEventHandlerFuncs
}

var _ LeaderManager = &dummyLeaderManager{}

func NewDummyLeaderManager(elected bool) *dummyLeaderManager {
	return &dummyLeaderManager{
		elected: elected,
	}
}

func (d *dummyLeaderManager) IsLeader() bool {
	return d.elected
}

func (d *dummyLeaderManager) LeaderNotifyChan() <-chan struct{} {
	return nil
}
