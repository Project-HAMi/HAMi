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
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/tools/cache"
)

type LeaderCallbacks struct {
	// Callbacks run after CurrentState has published the new state. A direct
	// term-to-term transition calls OnStoppedLeading before OnStartedLeading;
	// both callbacks observe the new term.
	// OnStartedLeading is called when starts leading.
	OnStartedLeading func()
	// OnStoppedLeading is called when stops leading.
	OnStoppedLeading func()
}

// LeadershipState is a point-in-time view of local leadership. Term is an
// opaque, process-local generation that starts at one and advances whenever
// this instance enters a new observed leadership term. A former leader keeps
// its last term while IsLeader is false; zero means no term has been observed.
type LeadershipState struct {
	IsLeader bool
	Term     uint64
}

type LeaderManager interface {
	IsLeader() bool

	cache.ResourceEventHandler
}

type TermAwareLeaderManager interface {
	LeaderManager
	CurrentState() LeadershipState
}

var _ TermAwareLeaderManager = &leaderManager{}

type leaderManager struct {
	hostname          string
	resourceName      string
	resourceNamespace string

	leaseLock       sync.RWMutex
	observedLease   *coordinationv1.Lease
	observedTime    time.Time
	term            uint64
	now             func() time.Time
	notifiedLeading bool

	callbacks LeaderCallbacks

	cache.FilteringResourceEventHandler
}

func NewLeaderManager(hostname, namespace, name string, callbacks LeaderCallbacks) *leaderManager {
	m := &leaderManager{
		hostname:          hostname,
		resourceName:      name,
		resourceNamespace: namespace,
		callbacks:         callbacks,
		now:               time.Now,
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

func (m *leaderManager) setObservedRecordAt(lease *coordinationv1.Lease, observedAt time.Time) {
	m.observedLease = lease

	if lease == nil {
		m.observedTime = time.Time{}
	} else {
		m.observedTime = observedAt
	}
}

func (m *leaderManager) setObservedRecord(lease *coordinationv1.Lease) {
	m.setObservedRecordAt(lease, m.now())
}

func (m *leaderManager) observe(lease *coordinationv1.Lease, forceRefresh bool) {
	now := m.now()
	m.leaseLock.Lock()
	previousState := m.currentStateLocked(now)
	previousHolder := holderIdentity(m.observedLease)
	if lease == nil {
		m.setObservedRecordAt(nil, now)
	} else if forceRefresh || m.observedLease == nil || !apiequality.Semantic.DeepEqual(m.observedLease.Spec, lease.Spec) {
		m.setObservedRecordAt(lease, now)
	} else {
		// Informer resync and metadata-only updates must not extend the lease.
		m.observedLease = lease
	}
	currentState := m.currentStateLocked(now)
	currentHolder := holderIdentity(lease)

	// kube-scheduler includes a UUID in HolderIdentity. A changed full
	// identity for the same hostname is therefore a new leadership term.
	holderChanged := previousState.IsLeader && currentState.IsLeader && previousHolder != currentHolder
	termChanged := currentState.IsLeader && (!previousState.IsLeader || holderChanged)
	if termChanged {
		m.term++
	}
	stoppedLeading := m.notifiedLeading && (!currentState.IsLeader || termChanged)
	startedLeading := currentState.IsLeader && (!m.notifiedLeading || termChanged)
	m.notifiedLeading = currentState.IsLeader
	m.leaseLock.Unlock()

	if stoppedLeading && m.callbacks.OnStoppedLeading != nil {
		m.callbacks.OnStoppedLeading()
	}
	if startedLeading && m.callbacks.OnStartedLeading != nil {
		m.callbacks.OnStartedLeading()
	}
}

// onAdd notifies if we are the leader when lease is created.
func (m *leaderManager) onAdd(obj any) {
	lease, ok := obj.(*coordinationv1.Lease)
	if !ok {
		return
	}
	m.observe(lease, true)
}

// onUpdate notifies when leadership changes or an expired lease becomes valid
// again. oldObj is validated because informer update handlers must receive two
// Lease objects, but the manager's locked observed state is authoritative.
func (m *leaderManager) onUpdate(oldObj, newObj any) {
	if _, ok := oldObj.(*coordinationv1.Lease); !ok {
		return
	}
	newLease, ok := newObj.(*coordinationv1.Lease)
	if !ok {
		return
	}
	m.observe(newLease, false)
}

func (m *leaderManager) onDelete(obj any) {
	if objectToLease(obj) == nil {
		return
	}
	m.observe(nil, true)
}

func (m *leaderManager) isHolderOf(lease *coordinationv1.Lease) bool {
	// Only kube-scheduler's `hostname + "_" + UUID` identity format is
	// supported; requiring the separator avoids hostname-prefix collisions.
	if lease == nil || lease.Spec.HolderIdentity == nil {
		return false
	}
	return strings.HasPrefix(*lease.Spec.HolderIdentity, m.hostname+"_")
}

func holderIdentity(lease *coordinationv1.Lease) string {
	if lease == nil || lease.Spec.HolderIdentity == nil {
		return ""
	}
	return *lease.Spec.HolderIdentity
}

func (m *leaderManager) isLeaseValid(now time.Time) bool {
	if m.observedLease == nil || m.observedLease.Spec.LeaseDurationSeconds == nil {
		return false
	}
	return m.observedTime.Add(time.Second * time.Duration(*m.observedLease.Spec.LeaseDurationSeconds)).After(now)
}

func (m *leaderManager) currentStateLocked(now time.Time) LeadershipState {
	return LeadershipState{
		IsLeader: m.isHolderOf(m.observedLease) && m.isLeaseValid(now),
		Term:     m.term,
	}
}

func (m *leaderManager) CurrentState() LeadershipState {
	m.leaseLock.RLock()
	defer m.leaseLock.RUnlock()
	return m.currentStateLocked(m.now())
}

func (m *leaderManager) IsLeader() bool {
	return m.CurrentState().IsLeader
}

type dummyLeaderManager struct {
	elected bool
	cache.ResourceEventHandlerFuncs
}

var _ TermAwareLeaderManager = &dummyLeaderManager{}

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

func (d *dummyLeaderManager) CurrentState() LeadershipState {
	if !d.elected {
		return LeadershipState{}
	}
	return LeadershipState{IsLeader: true, Term: 1}
}
