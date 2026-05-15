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

import (
	"context" // Added for the new test
	"runtime"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1" // Added for the new test
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func Test_LockNode(t *testing.T) {
	client.KubeClient = fake.NewClientset()

	// Use a short retry timeout so tests that expect lock contention complete quickly.
	originalRetryTimeout := NodeLockRetryTimeout
	NodeLockRetryTimeout = 500 * time.Millisecond
	defer func() {
		NodeLockRetryTimeout = originalRetryTimeout
	}()

	type args struct {
		nodeName func() string
		lockname string
		pods     *corev1.Pod
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "node not found",
			args: args{
				nodeName: func() string {
					return "node"
				},
				pods: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "node has been locked by another pod",
			args: args{
				nodeName: func() string {
					name := "worker-1"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
							Annotations: map[string]string{
								NodeLockKey: GenerateNodeLockKeyByPod(&corev1.Pod{
									ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "other-ns"},
								}),
							},
						},
					}, metav1.CreateOptions{})
					// The lock-holding pod must exist to avoid dangling detection.
					client.KubeClient.CoreV1().Pods("other-ns").Create(context.TODO(), &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "other-ns"},
					}, metav1.CreateOptions{})
					return name
				},
				pods: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "node lock is invalid",
			args: args{
				nodeName: func() string {
					name := "worker-2"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
							Annotations: map[string]string{
								NodeLockKey: "lock",
							},
						},
					}, metav1.CreateOptions{})
					return name
				},
				pods: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "successfully set node lock",
			args: args{
				nodeName: func() string {
					name := "worker-3"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{}},
					}, metav1.CreateOptions{})
					return name
				},
				pods: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := LockNode(tt.args.nodeName(), tt.args.lockname, tt.args.pods); (err != nil) != tt.wantErr {
				t.Errorf("LockNode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLockNodeWithTimeout(t *testing.T) {
	client.KubeClient = fake.NewClientset()

	// Set a custom timeout for testing.
	originalTimeout := NodeLockTimeout
	NodeLockTimeout = time.Minute * 2
	defer func() {
		NodeLockTimeout = originalTimeout
	}()

	// Use a short retry timeout so the test completes quickly.
	originalRetryTimeout := NodeLockRetryTimeout
	NodeLockRetryTimeout = 500 * time.Millisecond
	defer func() {
		NodeLockRetryTimeout = originalRetryTimeout
	}()

	nodeName := "test-node-timeout"

	// Create a node with a fresh lock (should not be expired).
	freshLockTime := time.Now().Format(time.RFC3339)
	testNamespace := "test-ns"
	testPodName := "test-pod"
	lockValue := freshLockTime + NodeLockSep + testNamespace + NodeLockSep + testPodName

	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: lockValue,
			},
		},
	}, metav1.CreateOptions{})

	// Pod must exist to avoid dangling node lock.
	client.KubeClient.CoreV1().Pods(testNamespace).Create(context.TODO(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodName,
			Namespace: testNamespace,
		},
	}, metav1.CreateOptions{})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-pod",
			Namespace: "new-ns",
		},
	}

	start := time.Now()
	err := LockNode(nodeName, "", pod)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected error but got nil")
	}

	// Should have retried for roughly NodeLockRetryTimeout before giving up.
	if elapsed < 400*time.Millisecond {
		t.Errorf("LockNode returned too quickly (%v), expected retry for ~%v", elapsed, NodeLockRetryTimeout)
	}

	if !strings.Contains(err.Error(), "locked by a valid pod") {
		t.Errorf("Expected errNodeLocked sentinel, got: %v", err)
	}
}

func TestLockNodeWithDangling(t *testing.T) {
	client.KubeClient = fake.NewClientset()

	// Set a custom timeout for testing
	originalTimeout := NodeLockTimeout
	NodeLockTimeout = time.Minute * 2
	defer func() {
		NodeLockTimeout = originalTimeout
	}()

	nodeName := "test-node-timeout"

	// Create a node with a fresh lock (should not be expired)
	freshLockTime := time.Now().Format(time.RFC3339)
	testNamespace := "test-ns"
	testPodName := "test-pod"
	lockValue := freshLockTime + NodeLockSep + testNamespace + NodeLockSep + testPodName

	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: lockValue,
			},
		},
	}, metav1.CreateOptions{})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-pod",
			Namespace: "new-ns",
		},
	}

	// Try to lock the node again - this should pass and release the old dangling lock
	if err := LockNode(nodeName, "", pod); err != nil {
		t.Fatal("Expected nil but got error")
	}
}

func TestReleaseNodeLock(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	type args struct {
		nodeName func() string
		lockname string
		pod      *corev1.Pod
		timeout  bool
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "node not found",
			args: args{
				nodeName: func() string {
					return "node"
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "node is not lock",
			args: args{
				nodeName: func() string {
					name := "worker-1"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{}},
					}, metav1.CreateOptions{})
					return name
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "node lock is not set by this pod",
			args: args{
				nodeName: func() string {
					name := "worker-2"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{
							NodeLockKey: GenerateNodeLockKeyByPod(&corev1.Pod{
								ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "namespace"},
							}),
						}},
					}, metav1.CreateOptions{})
					return name
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "successfully release node lock",
			args: args{
				nodeName: func() string {
					name := "worker-3"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{
							NodeLockKey: GenerateNodeLockKeyByPod(&corev1.Pod{
								ObjectMeta: metav1.ObjectMeta{Name: "hami", Namespace: "hami-ns"},
							}),
						}},
					}, metav1.CreateOptions{})
					return name
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hami",
						Namespace: "hami-ns",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ReleaseNodeLock(tt.args.nodeName(), tt.args.lockname, tt.args.pod, tt.args.timeout); (err != nil) != tt.wantErr {
				t.Errorf("ReleaseNodeLock() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConcurrentNodeLocks verifies that locks on different nodes can be acquired concurrently.
func TestConcurrentNodeLocks(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	nodeLocks = newNodeLockManager()

	prevProcs := runtime.GOMAXPROCS(0)
	targetProcs := max(runtime.NumCPU(), 2)
	runtime.GOMAXPROCS(targetProcs)
	defer runtime.GOMAXPROCS(prevProcs)

	makePod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "test-ns",
			},
		}
	}

	for _, nodeName := range []string{"node-a", "node-b"} {
		_, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        nodeName,
				Annotations: map[string]string{},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create node %s: %v", nodeName, err)
		}
	}

	// Holding node-a's lock must not block locking node-b.
	nodeALock := nodeLocks.getLock("node-a")
	nodeALock.Lock()

	podB := makePod("pod-b")
	nodeBResult := make(chan error, 1)
	go func() {
		nodeBResult <- LockNode("node-b", "", podB)
	}()

	select {
	case err := <-nodeBResult:
		if err != nil {
			t.Fatalf("LockNode for node-b failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("LockNode for node-b blocked by unrelated node lock")
	}

	nodeALock.Unlock()

	// Clean up node-b lock to avoid leaking state for subsequent checks.
	if err := ReleaseNodeLock("node-b", "", podB, false); err != nil {
		t.Fatalf("ReleaseNodeLock for node-b failed: %v", err)
	}

	// Holding node-a's lock should block another lock attempt on the same node until released.
	nodeALock.Lock()

	podA := makePod("pod-a")
	nodeAResult := make(chan error, 1)
	go func() {
		nodeAResult <- LockNode("node-a", "", podA)
	}()

	select {
	case err := <-nodeAResult:
		t.Fatalf("LockNode for node-a should block while mutex held, got err=%v", err)
	case <-time.After(100 * time.Millisecond):
		// Expected path: still waiting for the per-node lock.
	}

	nodeALock.Unlock()

	if err := <-nodeAResult; err != nil {
		t.Fatalf("LockNode for node-a failed after releasing lock: %v", err)
	}

	if err := ReleaseNodeLock("node-a", "", podA, false); err != nil {
		t.Fatalf("ReleaseNodeLock for node-a failed: %v", err)
	}
}

// TestCleanupNodeLockOnNodeDelete ensures CleanupNodeLock removes the entry
// and a subsequent getLock allocates a fresh mutex instance.
func TestCleanupNodeLockOnNodeDelete(t *testing.T) {
	// Reset manager state for this test
	nodeLocks = newNodeLockManager()

	first := nodeLocks.getLock("to-be-deleted")
	if first == nil {
		t.Fatalf("expected non-nil mutex from getLock")
	}

	// Trigger cleanup as if node was removed by autoscaler
	CleanupNodeLock("to-be-deleted")

	second := nodeLocks.getLock("to-be-deleted")
	if second == nil {
		t.Fatalf("expected non-nil mutex from getLock after cleanup")
	}

	if first == second {
		t.Fatalf("expected a new mutex instance after cleanup, got the same pointer")
	}
}

func TestGeneratePodNamespaceName(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		sep      string
		expected string
	}{
		{
			name: "Test with valid pod and separator",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			},
			sep:      "-",
			expected: "test-namespace-test-pod",
		},
		{
			name: "Test with empty separator",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			},
			sep:      "",
			expected: "test-namespacetest-pod",
		},
		{
			name: "Test with special characters in separator",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			},
			sep:      "@@@",
			expected: "test-namespace@@@test-pod",
		},
		{
			name:     "Test with nil pod",
			pod:      nil,
			sep:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GeneratePodNamespaceName(tt.pod, tt.sep)
			if result != tt.expected {
				t.Errorf("GeneratePodNamespaceName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestLockNodeRetrySuccess verifies that LockNode retries and succeeds when a
// lock is released by the holder before the retry timeout expires.
func TestLockNodeRetrySuccess(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	nodeLocks = newNodeLockManager()

	originalRetryTimeout := NodeLockRetryTimeout
	NodeLockRetryTimeout = 5 * time.Second
	defer func() {
		NodeLockRetryTimeout = originalRetryTimeout
	}()

	nodeName := "retry-success-node"
	holderPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "holder-pod", Namespace: "holder-ns"},
	}
	waiterPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "waiter-pod", Namespace: "waiter-ns"},
	}

	// Create node and holder pod.
	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
	}, metav1.CreateOptions{})
	client.KubeClient.CoreV1().Pods("holder-ns").Create(context.TODO(), holderPod, metav1.CreateOptions{})

	// Holder acquires the lock.
	if err := LockNode(nodeName, "", holderPod); err != nil {
		t.Fatalf("Holder failed to acquire lock: %v", err)
	}

	// Release the lock after a short delay so the waiter can acquire it.
	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := ReleaseNodeLock(nodeName, "", holderPod, false); err != nil {
			t.Errorf("Holder failed to release lock: %v", err)
		}
	}()

	// Waiter should retry and eventually succeed.
	start := time.Now()
	if err := LockNode(nodeName, "", waiterPod); err != nil {
		t.Fatalf("Waiter failed to acquire lock after retry: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 400*time.Millisecond {
		t.Errorf("Waiter acquired lock too quickly (%v), expected some retry delay", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Waiter took too long (%v), expected lock within ~1-2s", elapsed)
	}
}

// TestLockNodeIdempotent verifies that LockNode succeeds immediately when the
// lock is already held by the same pod (idempotent behavior).
func TestLockNodeIdempotent(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	nodeLocks = newNodeLockManager()

	nodeName := "idempotent-node"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "my-pod", Namespace: "my-ns"},
	}

	client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
	}, metav1.CreateOptions{})

	// First lock.
	if err := LockNode(nodeName, "", pod); err != nil {
		t.Fatalf("First LockNode failed: %v", err)
	}

	// Second lock by the same pod should succeed immediately.
	start := time.Now()
	if err := LockNode(nodeName, "", pod); err != nil {
		t.Fatalf("Second LockNode (idempotent) failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("Idempotent lock took too long (%v), should be immediate", elapsed)
	}
}

// TestSimulateRetryStorm verifies if the Backoff strategy is using exponential backoff.
func TestSimulateRetryStorm(t *testing.T) {
	tests := []struct {
		name               string
		concurrentRequests int
		maxCollisionsLimit int
	}{
		{
			name:               "DefaultStrategy_Spread_Check",
			concurrentRequests: 50,
			maxCollisionsLimit: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := DefaultStrategy
			retryTimes := make([]time.Duration, 0, tt.concurrentRequests*5)

			t.Logf("Testing Strategy: Steps=%d, Duration=%v, Factor=%v, Jitter=%v",
				strategy.Steps, strategy.Duration, strategy.Factor, strategy.Jitter)

			for range tt.concurrentRequests {
				step := strategy

				for range 3 {
					waitDuration := step.Step()
					retryTimes = append(retryTimes, waitDuration)
				}
			}
			collisionMap := make(map[time.Duration]int)
			for _, d := range retryTimes {
				rounded := d.Round(10 * time.Millisecond)
				collisionMap[rounded]++
			}

			var maxCollisions int
			for duration, count := range collisionMap {
				if count > maxCollisions {
					maxCollisions = count
				}
				if count > 10 {
					t.Logf("INFO: %d requests retrying at ~%v (Potential Thundering Herd)", count, duration)
				}
			}

			if maxCollisions > tt.maxCollisionsLimit {
				t.Errorf("FAIL: Max collisions (%d) exceeded limit (%d). Backoff strategy is not spreading load effectively.", maxCollisions, tt.maxCollisionsLimit)
			} else {
				t.Logf("PASS: Max collisions were %d. Load is well spread.", maxCollisions)
			}
		})
	}
}
