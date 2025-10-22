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

package nodelock

import "sync"

// ResetNodeLocksForTest clears the in-memory node lock registry. Intended for tests.
func ResetNodeLocksForTest() {
	nodeLocks = newNodeLockManager()
}

// EnsureNodeLockForTest makes sure the internal state contains an entry for the provided node.
// It is noop if the node already exists. Intended for tests.
func EnsureNodeLockForTest(nodeName string) {
	nodeLocks.mu.Lock()
	if _, ok := nodeLocks.locks[nodeName]; !ok {
		nodeLocks.locks[nodeName] = &sync.Mutex{}
	}
	nodeLocks.mu.Unlock()
}

// NodeLockCountForTest reports how many node-specific locks are being tracked. Intended for tests.
func NodeLockCountForTest() int {
	nodeLocks.mu.Lock()
	defer nodeLocks.mu.Unlock()
	return len(nodeLocks.locks)
}
