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
