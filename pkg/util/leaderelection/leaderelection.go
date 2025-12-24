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

package leaderelection

import (
	"strings"
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/client-go/tools/cache"
)

type LeaderManager interface {
	IsLeader() bool

	// Notify when just elected as leader
	LeaderNotifyChan() <-chan struct{}

	cache.ResourceEventHandler
}

var _ LeaderManager = &leaderManager{}

type leaderManager struct {
	hostname          string
	resourceName      string
	resourceNamespace string

	leaseLock     sync.RWMutex
	observedLease *coordinationv1.Lease
	observedTime  time.Time
	leaderNotify  chan struct{}

	cache.FilteringResourceEventHandler
}

func NewLeaderManager(hostname, namespace, name string) *leaderManager {
	m := &leaderManager{
		hostname:          hostname,
		resourceName:      name,
		resourceNamespace: namespace,
		leaderNotify:      make(chan struct{}, 1),
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

func (m *leaderManager) setObservedRecord(lease *coordinationv1.Lease) {
	m.observedLease = lease

	if lease == nil {
		m.observedTime = time.Time{}
	} else {
		m.observedTime = time.Now()
	}
}

// onAdd notifies if we are the leader when lease is created.
func (m *leaderManager) onAdd(obj interface{}) {
	lease, ok := obj.(*coordinationv1.Lease)
	if !ok {
		return
	}

	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()

	m.setObservedRecord(lease)
	// Notify if we are the leader from the very begging
	if m.isHolderOf(lease) {
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

	m.setObservedRecord(newLease)
	// Notify if we have been elected to become the leader
	if m.isHolderOf(newLease) {
		oldLease, ok := oldObj.(*coordinationv1.Lease)
		if !ok {
			return
		}

		if !m.isHolderOf(oldLease) {
			m.leaderNotify <- struct{}{}
		}
	}
}

func (m *leaderManager) onDelete(obj interface{}) {
	// Do nothing on delete
	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()

	m.setObservedRecord(nil)
}

func (m *leaderManager) isHolderOf(lease *coordinationv1.Lease) bool {
	// kube-scheduler lease id take format of `hostname + "_" + string(uuid.NewUUID())`
	return lease.Spec.HolderIdentity != nil && strings.HasPrefix(*lease.Spec.HolderIdentity, m.hostname)
}

func (m *leaderManager) isLeaseValid(now time.Time) bool {
	return m.observedTime.Add(time.Second * time.Duration(*m.observedLease.Spec.LeaseDurationSeconds)).After(now)
}

func (m *leaderManager) IsLeader() bool {
	m.leaseLock.RLock()
	defer m.leaseLock.RUnlock()

	if m.observedLease == nil {
		return false
	}

	// TODO: should we check valid lease here?
	return m.isHolderOf(m.observedLease)
}

func (m *leaderManager) LeaderNotifyChan() <-chan struct{} {
	return m.leaderNotify
}

type dummyLeaderManager struct {
	elected bool
	cache.ResourceEventHandlerFuncs

	leaderNotify chan struct{}
}

var _ LeaderManager = &dummyLeaderManager{}

// NewDummyLeaderManager creates a dummy leader manager which will not change its elected state during its lifetime.
// It will always return the elected state passed in the constructor when calling IsLeader() and you will never get notified by it's channel.
//
// This is useful when disabling leader-election.
func NewDummyLeaderManager(elected bool) *dummyLeaderManager {
	notifyCh := make(chan struct{}, 1)
	// dummyLeaderManager will not notify because the elected state is fixed
	close(notifyCh)
	return &dummyLeaderManager{
		elected:      elected,
		leaderNotify: notifyCh,
	}
}

func (d *dummyLeaderManager) IsLeader() bool {
	return d.elected
}

func (d *dummyLeaderManager) LeaderNotifyChan() <-chan struct{} {
	return d.leaderNotify
}
