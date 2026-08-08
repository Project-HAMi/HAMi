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
	"sync"

	k8stypes "k8s.io/apimachinery/pkg/types"
)

type refMutex struct {
	sync.Mutex
	refCount int
}

type podLockManager struct {
	mu    sync.Mutex
	locks map[k8stypes.UID]*refMutex
}

func newPodLockManager() *podLockManager {
	return &podLockManager{
		locks: make(map[k8stypes.UID]*refMutex),
	}
}

func (m *podLockManager) Lock(uid k8stypes.UID) {
	m.mu.Lock()
	rm, exists := m.locks[uid]
	if !exists {
		rm = &refMutex{}
		m.locks[uid] = rm
	}
	rm.refCount++
	m.mu.Unlock()

	rm.Lock()
}

func (m *podLockManager) Unlock(uid k8stypes.UID) {
	m.mu.Lock()
	rm, exists := m.locks[uid]
	if !exists {
		m.mu.Unlock()
		return
	}
	rm.Unlock()
	rm.refCount--
	if rm.refCount == 0 {
		delete(m.locks, uid)
	}
	m.mu.Unlock()
}
