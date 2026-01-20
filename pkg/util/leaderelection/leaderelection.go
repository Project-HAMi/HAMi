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

type LeaderCallbacks struct {
	// OnStartedLeading is called when starts leading
	OnStartedLeading func()
	// OnStoppedLeading is called when stops leading
	OnStoppedLeading func()
}

type LeaderManager interface {
	IsLeader() bool

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

	callbacks LeaderCallbacks

	cache.FilteringResourceEventHandler
}

func NewLeaderManager(hostname, namespace, name string, callbacks LeaderCallbacks) *leaderManager {
	m := &leaderManager{
		hostname:          hostname,
		resourceName:      name,
		resourceNamespace: namespace,
		callbacks:         callbacks,
	}

	m.FilteringResourceEventHandler = cache.FilteringResourceEventHandler{
		FilterFunc: func(obj any) bool {
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

func objectToLease(obj any) *coordinationv1.Lease {
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
func (m *leaderManager) onAdd(obj any) {
	lease, ok := obj.(*coordinationv1.Lease)
	if !ok {
		return
	}

	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()

	m.setObservedRecord(lease)
	// Notify if we are the leader from the very begging
	if m.isHolderOf(lease) && m.callbacks.OnStartedLeading != nil {
		m.callbacks.OnStartedLeading()
	}
}

// onUpdate notifies when we have been elected as leader.
func (m *leaderManager) onUpdate(oldObj, newObj any) {
	newLease, ok := newObj.(*coordinationv1.Lease)
	if !ok {
		return
	}
	oldLease, ok := oldObj.(*coordinationv1.Lease)
	if !ok {
		return
	}

	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()
	m.setObservedRecord(newLease)

	// Notify if we have been elected to become the leader
	if !m.isHolderOf(oldLease) && m.isHolderOf(newLease) {
		if m.callbacks.OnStartedLeading != nil {
			m.callbacks.OnStartedLeading()
		}
	} else if m.isHolderOf(oldLease) && !m.isHolderOf(newLease) {
		if m.callbacks.OnStoppedLeading != nil {
			m.callbacks.OnStoppedLeading()
		}
	}
}

func (m *leaderManager) onDelete(obj any) {
	// Do nothing on delete
	m.leaseLock.Lock()
	defer m.leaseLock.Unlock()

	m.setObservedRecord(nil)
	if m.callbacks.OnStoppedLeading != nil {
		m.callbacks.OnStoppedLeading()
	}
}

func (m *leaderManager) isHolderOf(lease *coordinationv1.Lease) bool {
	// kube-scheduler lease id take format of `hostname + "_" + string(uuid.NewUUID())`
	if lease == nil || lease.Spec.HolderIdentity == nil {
		return false
	}
	return strings.HasPrefix(*lease.Spec.HolderIdentity, m.hostname)
}

func (m *leaderManager) isLeaseValid(now time.Time) bool {
	if m.observedLease == nil || m.observedLease.Spec.LeaseDurationSeconds == nil {
		return false
	}
	return m.observedTime.Add(time.Second * time.Duration(*m.observedLease.Spec.LeaseDurationSeconds)).After(now)
}

func (m *leaderManager) IsLeader() bool {
	m.leaseLock.RLock()
	defer m.leaseLock.RUnlock()

	if m.observedLease == nil {
		return false
	}

	return m.isHolderOf(m.observedLease) && m.isLeaseValid(time.Now())
}

type dummyLeaderManager struct {
	elected bool
	cache.ResourceEventHandlerFuncs
}

var _ LeaderManager = &dummyLeaderManager{}

// NewDummyLeaderManager creates a dummy leader manager which will not change its elected state during its lifetime.
// It will always return the elected state passed in the constructor when calling IsLeader() and you will never get notified by it's channel.
//
// This is useful when disabling leader-election.
func NewDummyLeaderManager(elected bool) *dummyLeaderManager {
	return &dummyLeaderManager{
		elected: elected,
	}
}

func (d *dummyLeaderManager) IsLeader() bool {
	return d.elected
}
