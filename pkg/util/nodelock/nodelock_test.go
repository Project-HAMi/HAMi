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
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func TestSetNodeLockPreservesConcurrentLockAfterConflict(t *testing.T) {
	nodeLocks = newNodeLockManager()
	nodeName := "node-set-conflict"
	podA := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns"}}
	podB := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns"}}
	holderB := GenerateNodeLockKeyByPod(podB)
	clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
	client.KubeClient = clientSet

	getCalls := 0
	clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		getCalls++
		if getCalls < 3 {
			return false, nil, nil
		}
		return true, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:        nodeName,
			Annotations: map[string]string{NodeLockKey: holderB},
		}}, nil
	})
	patchCalls := 0
	clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		patchCalls++
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, nodeName, errors.New("simulated concurrent lock"))
	})

	err := SetNodeLock(nodeName, "", podA)
	if !IsNodeLockContention(err) {
		t.Fatalf("SetNodeLock() error = %v, want node lock contention", err)
	}
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got := node.Annotations[NodeLockKey]; got != holderB {
		t.Fatalf("node lock = %q, want concurrent holder %q", got, holderB)
	}
	if patchCalls != 1 {
		t.Fatalf("patch calls = %d, want 1", patchCalls)
	}
}

func TestSetNodeLockSucceedsAfterLostPatchResponse(t *testing.T) {
	nodeLocks = newNodeLockManager()
	nodeName := "node-set-lost-response"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns"}}
	lockStr := "2026-08-01T06:00:00Z,ns,pod-a"
	clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
	client.KubeClient = clientSet

	getCalls := 0
	clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		getCalls++
		if getCalls < 3 {
			return false, nil, nil
		}
		return true, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:        nodeName,
			Annotations: map[string]string{NodeLockKey: lockStr},
		}}, nil
	})
	patchCalls := 0
	clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		patchCalls++
		return true, nil, errors.New("simulated lost patch response")
	})

	if err := SetNodeLock(nodeName, "", pod); err != nil {
		t.Fatalf("SetNodeLock() error = %v, want nil", err)
	}
	if patchCalls != 1 {
		t.Fatalf("patch calls = %d, want 1", patchCalls)
	}
}

func TestReleaseNodeLockPreservesConcurrentLockAfterConflict(t *testing.T) {
	nodeLocks = newNodeLockManager()
	nodeName := "node-release-conflict"
	podA := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns"}}
	podB := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns"}}
	holderA := GenerateNodeLockKeyByPod(podA)
	holderB := GenerateNodeLockKeyByPod(podB)
	clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:        nodeName,
		Annotations: map[string]string{NodeLockKey: holderA},
	}})
	client.KubeClient = clientSet

	getCalls := 0
	clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		getCalls++
		if getCalls < 3 {
			return false, nil, nil
		}
		return true, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:        nodeName,
			Annotations: map[string]string{NodeLockKey: holderB},
		}}, nil
	})
	patchCalls := 0
	clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		patchCalls++
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, nodeName, errors.New("simulated concurrent lock"))
	})

	if err := ReleaseNodeLock(nodeName, "", podA, false); err != nil {
		t.Fatalf("ReleaseNodeLock() error = %v, want nil", err)
	}
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got := node.Annotations[NodeLockKey]; got != holderB {
		t.Fatalf("node lock = %q, want concurrent holder %q", got, holderB)
	}
	if patchCalls != 1 {
		t.Fatalf("patch calls = %d, want 1", patchCalls)
	}
}

func TestReleaseNodeLockPreservesReplacedLegacyLockAfterConflict(t *testing.T) {
	nodeLocks = newNodeLockManager()
	nodeName := "node-release-legacy-conflict"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns"}}
	initialLock := "2026-08-01T06:00:00Z"
	replacedLock := "2026-08-01T06:00:01Z"
	clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:        nodeName,
		Annotations: map[string]string{NodeLockKey: initialLock},
	}})
	client.KubeClient = clientSet

	getCalls := 0
	clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		getCalls++
		if getCalls < 3 {
			return false, nil, nil
		}
		return true, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:        nodeName,
			Annotations: map[string]string{NodeLockKey: replacedLock},
		}}, nil
	})
	patchCalls := 0
	clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		patchCalls++
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, nodeName, errors.New("simulated concurrent legacy lock"))
	})

	if err := ReleaseNodeLock(nodeName, "", pod, false); err != nil {
		t.Fatalf("ReleaseNodeLock() error = %v, want nil", err)
	}
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got := node.Annotations[NodeLockKey]; got != replacedLock {
		t.Fatalf("node lock = %q, want concurrent legacy lock %q", got, replacedLock)
	}
	if patchCalls != 1 {
		t.Fatalf("patch calls = %d, want 1", patchCalls)
	}
}

func TestReleaseNodeLockReleasesRestampedLockForSamePod(t *testing.T) {
	nodeLocks = newNodeLockManager()
	nodeName := "node-release-restamped"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns"}}
	initialLock := "2026-07-29T13:00:00Z,ns,pod-a"
	restampedLock := "2026-07-29T13:00:01Z,ns,pod-a"
	clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:        nodeName,
		Annotations: map[string]string{NodeLockKey: initialLock},
	}})
	client.KubeClient = clientSet

	getCalls := 0
	clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		getCalls++
		if getCalls != 3 {
			return false, nil, nil
		}
		return true, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:        nodeName,
			Annotations: map[string]string{NodeLockKey: restampedLock},
		}}, nil
	})
	patchCalls := 0
	clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
		patchCalls++
		if patchCalls == 1 {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, nodeName, errors.New("simulated restamp"))
		}
		return false, nil, nil
	})

	if err := ReleaseNodeLock(nodeName, "", pod, false); err != nil {
		t.Fatalf("ReleaseNodeLock() error = %v, want nil", err)
	}
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if _, ok := node.Annotations[NodeLockKey]; ok {
		t.Fatalf("node lock = %q, want removed", node.Annotations[NodeLockKey])
	}
	if patchCalls != 2 {
		t.Fatalf("patch calls = %d, want 2", patchCalls)
	}
}

func Test_LockNode(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	type args struct {
		nodeName func(t *testing.T) string
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
				nodeName: func(t *testing.T) string {
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
				nodeName: func(t *testing.T) string {
					name := "worker-1"
					if _, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
							Annotations: map[string]string{
								NodeLockKey: GenerateNodeLockKeyByPod(&corev1.Pod{
									ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "other-ns"},
								}),
							},
						},
					}, metav1.CreateOptions{}); err != nil {
						t.Fatalf("failed to create node fixture: %v", err)
					}
					// The lock holder ("other-pod"/"other-ns") must exist and not be
					// dangling, otherwise LockNode treats it as stale and takes over.
					if _, err := client.KubeClient.CoreV1().Pods("other-ns").Create(context.TODO(), &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "other-ns"},
					}, metav1.CreateOptions{}); err != nil {
						t.Fatalf("failed to create lock-holder pod fixture: %v", err)
					}
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
			name: "node has been locked by another pod in the same namespace",
			args: args{
				nodeName: func(t *testing.T) string {
					name := "worker-1b"
					if _, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
							Annotations: map[string]string{
								NodeLockKey: GenerateNodeLockKeyByPod(&corev1.Pod{
									ObjectMeta: metav1.ObjectMeta{Name: "other-pod-same-ns", Namespace: "hami-ns"},
								}),
							},
						},
					}, metav1.CreateOptions{}); err != nil {
						t.Fatalf("failed to create node fixture: %v", err)
					}
					// Same namespace as the requester below, but a different pod
					// name: exercises ns == pods.Namespace (true) with
					// previousPodName == pods.Name (false), distinct from both the
					// "another pod" case above (both false) and the reentrant
					// same-pod case (both true).
					if _, err := client.KubeClient.CoreV1().Pods("hami-ns").Create(context.TODO(), &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{Name: "other-pod-same-ns", Namespace: "hami-ns"},
					}, metav1.CreateOptions{}); err != nil {
						t.Fatalf("failed to create lock-holder pod fixture: %v", err)
					}
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
				nodeName: func(t *testing.T) string {
					name := "worker-2"
					if _, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
							Annotations: map[string]string{
								NodeLockKey: "lock",
							},
						},
					}, metav1.CreateOptions{}); err != nil {
						t.Fatalf("failed to create node fixture: %v", err)
					}
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
				nodeName: func(t *testing.T) string {
					name := "worker-3"
					if _, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{}},
					}, metav1.CreateOptions{}); err != nil {
						t.Fatalf("failed to create node fixture: %v", err)
					}
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
			if err := LockNode(tt.args.nodeName(t), tt.args.lockname, tt.args.pods); (err != nil) != tt.wantErr {
				t.Errorf("LockNode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLockNodeWithTimeout(t *testing.T) {
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

	// Pod must exist to avoid dangling node lock
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

	// Try to lock the node again - this should trigger line 130
	err := LockNode(nodeName, "", pod)

	// Verify the error contains the NodeLockTimeout value
	if err == nil {
		t.Fatal("Expected error but got nil")
	}

	expectedError := "has been locked within 2m0s"
	if !strings.Contains(err.Error(), expectedError) {
		t.Errorf("Expected error to contain '%s', but got: %v", expectedError, err)
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

// TestLockNodeReentrantSamePod covers lockAllDevices' actual call pattern:
// it calls LockNode once per device vendor a pod requests resources from, so
// a pod requesting e.g. both nvidia.com/gpu and cambricon.com/vmlu locks the
// same node twice for itself in a row. The second call must succeed instead
// of contending with the pod's own still-valid lock, or such a pod could
// never become schedulable.
func TestLockNodeReentrantSamePod(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	nodeLocks = newNodeLockManager()
	nodeName := "multi-vendor-node"
	_, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "multi-vendor-pod", Namespace: "test-ns"}}
	// The requesting pod must actually exist for this test to exercise the
	// intended live-ownership scenario. Without it, the first LockNode call
	// still succeeds, but only because it's setting a fresh lock, not
	// because the reentrancy branch has verified anything about a real,
	// live pod - so a real dangling-lock path could be masked instead.
	if _, err := client.KubeClient.CoreV1().Pods("test-ns").Create(context.TODO(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	if err := LockNode(nodeName, "nvidia", pod); err != nil {
		t.Fatalf("first LockNode call (simulating the nvidia backend) failed: %v", err)
	}
	if err := LockNode(nodeName, "cambricon", pod); err != nil {
		t.Fatalf("second LockNode call for the same pod (simulating the cambricon backend) should succeed, got: %v", err)
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
		name          string
		args          args
		wantErr       bool
		checkNodeLock bool
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
			wantErr:       false,
			checkNodeLock: true,
		},
		{
			name: "node lock is legacy timestamp format",
			args: args{
				nodeName: func() string {
					name := "worker-5"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: map[string]string{
							NodeLockKey: "2026-07-03T15:35:10+08:00",
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
			wantErr:       false,
			checkNodeLock: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeName := tt.args.nodeName()
			if err := ReleaseNodeLock(nodeName, tt.args.lockname, tt.args.pod, tt.args.timeout); (err != nil) != tt.wantErr {
				t.Errorf("ReleaseNodeLock() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.checkNodeLock {
				node, err := client.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("failed to get node %s after releasing lock: %v", nodeName, err)
				}
				if _, ok := node.Annotations[NodeLockKey]; ok {
					t.Errorf("expected %s annotation to be removed from node %s", NodeLockKey, nodeName)
				}
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

// TestSetNodeLockRaceIsRetryable covers the narrow race window where two
// callers both observe an unlocked node in LockNode's outer check and then
// genuinely race on SetNodeLock's per-node mutex to actually claim the lock.
func TestSetNodeLockRaceIsRetryable(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	nodeLocks = newNodeLockManager()
	nodeName := "race-node"
	_, err := client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}
	podA := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "test-ns"}}
	podB := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "test-ns"}}
	raceLock := nodeLocks.getLock(nodeName)
	raceLock.Lock()
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pod := range []*corev1.Pod{podA, podB} {
		wg.Add(1)
		go func(pod *corev1.Pod) {
			defer wg.Done()
			results <- LockNode(nodeName, "", pod)
		}(pod)
	}
	time.Sleep(50 * time.Millisecond)
	raceLock.Unlock()
	wg.Wait()
	close(results)
	var successes, contentions int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case IsNodeLockContention(err):
			contentions++
		default:
			t.Fatalf("unexpected error from LockNode: %v", err)
		}
	}
	if successes != 1 || contentions != 1 {
		t.Fatalf("expected exactly 1 winner and 1 retryable contention out of 2 concurrent LockNode calls, got successes=%d contentions=%d", successes, contentions)
	}
}

// TestCleanupNodeLockOnNodeDelete ensures CleanupNodeLock removes the entry
// and a subsequent getLock allocates a fresh mutex instance.
func TestCleanupNodeLockOnNodeDelete(t *testing.T) {
	nodeLocks = newNodeLockManager()
	first := nodeLocks.getLock("to-be-deleted")
	if first == nil {
		t.Fatalf("expected non-nil mutex from getLock")
	}
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

func TestSetupNodeLockTimeout(t *testing.T) {
	original := NodeLockTimeout
	t.Cleanup(func() { NodeLockTimeout = original })

	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"empty env uses default", "", original},
		{"valid duration sets timeout", "10m", 10 * time.Minute},
		{"invalid duration keeps default", "notaduration", original},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			NodeLockTimeout = original
			t.Setenv("HAMI_NODELOCK_EXPIRE", tt.env)
			setupNodeLockTimeout()
			if NodeLockTimeout != tt.want {
				t.Errorf("got %v, want %v", NodeLockTimeout, tt.want)
			}
		})
	}
}

func Test_NodeLock_ContentionBackoffAndTimeout(t *testing.T) {
	nodeLocks = newNodeLockManager()
	nodeName := "contention-node"
	clientSet := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
	})
	client.KubeClient = clientSet

	t.Run("Context Cancellation Halts Retry Backoff", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		clientSet := fake.NewClientset(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
		})
		client.KubeClient = clientSet

		// Prepend a reactor to patch that simulates persistent transient API failure
		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated transient API error")
		})

		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-waiter", Namespace: "ns"}}
		errChan := make(chan error, 1)

		go func() {
			errChan <- SetNodeLockWithContext(ctx, nodeName, "", pod)
		}()

		select {
		case err := <-errChan:
			elapsed := time.Since(start)
			if err == nil {
				t.Fatalf("Expected error due to context cancellation, got nil")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Expected context.DeadlineExceeded during retries, got %v", err)
			}
			if elapsed > 300*time.Millisecond {
				t.Fatalf("Cancellation was not observed promptly: took %v (expected ~50ms)", elapsed)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("SetNodeLockWithContext hung after context deadline")
		}
	})

	t.Run("High Lock Contention Goroutine Safety", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		clientSet = fake.NewClientset(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
		})
		client.KubeClient = clientSet

		numGoroutines := 10
		pods := make([]*corev1.Pod, numGoroutines)
		for i := range numGoroutines {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("pod-%d", i), Namespace: "ns"}}
			_, err := clientSet.CoreV1().Pods("ns").Create(context.Background(), pod, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create pod fixture: %v", err)
			}
			pods[i] = pod
		}

		startGate := make(chan struct{})
		doneHolding := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, numGoroutines)

		for i := range numGoroutines {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				pod := pods[idx]
				<-startGate
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				defer cancel()
				err := LockNodeWithContext(ctx, nodeName, "", pod)
				errs <- err
				if err == nil {
					<-doneHolding
					_ = ReleaseNodeLockWithContext(context.Background(), nodeName, "", pod, false)
				}
			}(i)
		}

		close(startGate)

		var successCount int
		var failureCount int
		for range numGoroutines {
			err := <-errs
			if err == nil {
				successCount++
			} else {
				failureCount++
				if !IsNodeLockContention(err) {
					t.Errorf("Expected ErrNodeLockContention, got: %v", err)
				}
			}
		}

		close(doneHolding)
		wg.Wait()

		if successCount != 1 {
			t.Fatalf("Expected exactly 1 successful lock acquisition, got %d", successCount)
		}
		if failureCount != numGoroutines-1 {
			t.Fatalf("Expected %d failed lock attempts, got %d", numGoroutines-1, failureCount)
		}
	})

	t.Run("TryLockNode Non-blocking Attempt", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		clientSet = fake.NewClientset(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName, Annotations: map[string]string{}},
		})
		client.KubeClient = clientSet

		podA := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "try-pod-a", Namespace: "ns"}}
		if err := TryLockNode(nodeName, "", podA); err != nil {
			t.Fatalf("TryLockNode should succeed when node is unlocked: %v", err)
		}

		if _, err := clientSet.CoreV1().Pods("ns").Create(context.Background(), podA, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Failed to create pod fixture: %v", err)
		}

		podB := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "try-pod-b", Namespace: "ns"}}
		if err := TryLockNode(nodeName, "", podB); err == nil {
			t.Fatalf("TryLockNode should fail when node is locked by another pod")
		} else if !IsNodeLockContention(err) {
			t.Fatalf("Expected NodeLockContention error, got: %v", err)
		}
	})
}

func TestLockMutexWithContext_Cancelled(t *testing.T) {
	nodeLocks = newNodeLockManager()
	nodeName := "mutex-cancel-node"
	mu := nodeLocks.getLock(nodeName)
	mu.Lock()
	defer mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := lockMutexWithContext(ctx, mu)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestSetNodeLockWithContext_Coverage(t *testing.T) {
	t.Run("nil context uses background context", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-nil-ctx-set"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		if err := SetNodeLockWithContext(context.TODO(), nodeName, "", pod); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("mutex lock failure when context is canceled", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-mutex-fail-set"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		mu := nodeLocks.getLock(nodeName)
		mu.Lock()
		defer mu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := SetNodeLockWithContext(ctx, nodeName, "", pod)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("initial node get error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-get-fail-set"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset()
		client.KubeClient = clientSet

		clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated initial get error")
		})

		err := SetNodeLockWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !strings.Contains(err.Error(), "simulated initial get error") {
			t.Fatalf("expected initial get error, got %v", err)
		}
	})

	t.Run("retry loop node get error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-retry-get-fail-set"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		getCalls := 0
		clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			getCalls++
			if getCalls > 1 {
				return true, nil, errors.New("simulated retry get error")
			}
			return false, nil, nil
		})
		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, nodeName, errors.New("conflict"))
		})

		err := SetNodeLockWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !strings.Contains(err.Error(), "simulated retry get error") {
			t.Fatalf("expected retry get error, got %v", err)
		}
	})

	t.Run("canceled context during retry", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-retry-cancel-set"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated transient error requiring retry")
		})

		err := SetNodeLockWithContext(ctx, nodeName, "", pod)
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	})
}

func TestReleaseNodeLockWithContext_Coverage(t *testing.T) {
	t.Run("nil pod returns error", func(t *testing.T) {
		err := ReleaseNodeLockWithContext(context.Background(), "node", "", nil, false)
		if err == nil || !strings.Contains(err.Error(), "pod is nil") {
			t.Fatalf("expected pod is nil error, got %v", err)
		}
	})

	t.Run("nil context uses background context", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-nil-ctx-rel"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: "2026-08-01T00:00:00Z,ns1,pod1",
			},
		}})
		client.KubeClient = clientSet

		if err := ReleaseNodeLockWithContext(context.TODO(), nodeName, "", pod, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("mutex lock failure when context is canceled", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-mutex-fail-rel"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		mu := nodeLocks.getLock(nodeName)
		mu.Lock()
		defer mu.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := ReleaseNodeLockWithContext(ctx, nodeName, "", pod, false)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("initial node get error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-get-fail-rel"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset()
		client.KubeClient = clientSet

		clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated get error")
		})

		err := ReleaseNodeLockWithContext(context.Background(), nodeName, "", pod, false)
		if err == nil || !strings.Contains(err.Error(), "simulated get error") {
			t.Fatalf("expected get error, got %v", err)
		}
	})

	t.Run("retry loop node get error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-retry-get-fail-rel"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: "2026-08-01T00:00:00Z,ns1,pod1",
			},
		}})
		client.KubeClient = clientSet

		getCalls := 0
		clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			getCalls++
			if getCalls > 1 {
				return true, nil, errors.New("simulated retry get error")
			}
			return false, nil, nil
		})
		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated patch retry error")
		})

		err := ReleaseNodeLockWithContext(context.Background(), nodeName, "", pod, false)
		if err == nil || !strings.Contains(err.Error(), "simulated retry get error") {
			t.Fatalf("expected retry get error, got %v", err)
		}
	})

	t.Run("retry loop annotation missing concurrently", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-deleted-ann-rel"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: "2026-08-01T00:00:00Z,ns1,pod1",
			},
		}})
		client.KubeClient = clientSet

		getCalls := 0
		clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			getCalls++
			if getCalls > 1 {
				return true, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				}}, nil
			}
			return false, nil, nil
		})
		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated patch retry error")
		})

		err := ReleaseNodeLockWithContext(context.Background(), nodeName, "", pod, false)
		if err != nil {
			t.Fatalf("expected nil error when annotation removed concurrently, got %v", err)
		}
	})

	t.Run("canceled context during retry", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-retry-cancel-rel"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: "2026-08-01T00:00:00Z,ns1,pod1",
			},
		}})
		client.KubeClient = clientSet

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated transient patch error requiring retry")
		})

		err := ReleaseNodeLockWithContext(ctx, nodeName, "", pod, false)
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	})

	t.Run("retry exhausted returns formatted error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-exhausted-rel"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: "2026-08-01T00:00:00Z,ns1,pod1",
			},
		}})
		client.KubeClient = clientSet

		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("persistent patch failure")
		})

		err := ReleaseNodeLockWithContext(context.Background(), nodeName, "", pod, false)
		if err == nil || !strings.Contains(err.Error(), "failed to release node lock") {
			t.Fatalf("expected failed to release node lock error, got %v", err)
		}
	})
}

func TestLockNodeWithContext_Coverage(t *testing.T) {
	t.Run("nil context uses background context", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-nil-ctx-lock"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		if err := LockNodeWithContext(context.TODO(), nodeName, "", pod); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("expired lock triggers takeover", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-expired-lock"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "new-pod", Namespace: "new-ns"}}
		oldLockTime := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
		expiredLockStr := fmt.Sprintf("%s,old-ns,old-pod", oldLockTime)
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: expiredLockStr,
			},
		}})
		client.KubeClient = clientSet

		if err := LockNodeWithContext(context.Background(), nodeName, "", pod); err != nil {
			t.Fatalf("expected success taking over expired lock, got %v", err)
		}
	})

	t.Run("pod get returns non-NotFound error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-pod-get-err"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		freshLockTime := time.Now().Format(time.RFC3339)
		lockStr := fmt.Sprintf("%s,other-ns,other-pod", freshLockTime)
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: lockStr,
			},
		}})
		client.KubeClient = clientSet

		clientSet.PrependReactor("get", "pods", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated pod API error")
		})

		err := LockNodeWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !strings.Contains(err.Error(), "simulated pod API error") {
			t.Fatalf("expected pod API error, got %v", err)
		}
	})

	t.Run("release failure during expired/dangling lock takeover", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-release-takeover-err"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		oldLockTime := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
		expiredLockStr := fmt.Sprintf("%s,old-ns,old-pod", oldLockTime)
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Annotations: map[string]string{
				NodeLockKey: expiredLockStr,
			},
		}})
		client.KubeClient = clientSet

		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated patch error during release")
		})

		err := LockNodeWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !strings.Contains(err.Error(), "failed to release node lock") {
			t.Fatalf("expected release failure error, got %v", err)
		}
	})
}

func TestTryLockNodeWithContext_Coverage(t *testing.T) {
	t.Run("nil context uses background context", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-nil-ctx-try"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		if err := TryLockNodeWithContext(context.TODO(), nodeName, "", pod); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("in-memory mutex locked returns contention error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-in-memory-locked"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		mu := nodeLocks.getLock(nodeName)
		mu.Lock()
		defer mu.Unlock()

		err := TryLockNodeWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !IsNodeLockContention(err) {
			t.Fatalf("expected ErrNodeLockContention, got %v", err)
		}
	})

	t.Run("node get error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-get-fail-try"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset()
		client.KubeClient = clientSet

		clientSet.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated get node error")
		})

		err := TryLockNodeWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !strings.Contains(err.Error(), "simulated get node error") {
			t.Fatalf("expected get node error, got %v", err)
		}
	})

	t.Run("patch conflict error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-patch-conflict-try"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "nodes"}, nodeName, errors.New("simulated conflict"))
		})

		err := TryLockNodeWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !IsNodeLockContention(err) || !strings.Contains(err.Error(), "patch conflict") {
			t.Fatalf("expected patch conflict contention error, got %v", err)
		}
	})

	t.Run("patch general error", func(t *testing.T) {
		nodeLocks = newNodeLockManager()
		nodeName := "node-patch-gen-err-try"
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "ns1"}}
		clientSet := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}})
		client.KubeClient = clientSet

		clientSet.PrependReactor("patch", "nodes", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("simulated generic patch error")
		})

		err := TryLockNodeWithContext(context.Background(), nodeName, "", pod)
		if err == nil || !strings.Contains(err.Error(), "simulated generic patch error") {
			t.Fatalf("expected generic patch error, got %v", err)
		}
	})
}

func TestParseNodeLock_Malformed(t *testing.T) {
	_, _, _, err := ParseNodeLock("part1,part2")
	if err == nil || !strings.Contains(err.Error(), "malformed lock annotation: expected 3 parts, got 2") {
		t.Fatalf("expected malformed lock annotation error, got %v", err)
	}
}

func TestGenerateNodeLockKeyByPod_NilPod(t *testing.T) {
	key := GenerateNodeLockKeyByPod(nil)
	if key == "" {
		t.Fatalf("expected non-empty key for nil pod")
	}
	if strings.Contains(key, NodeLockSep) {
		t.Fatalf("expected timestamp-only key without separator for nil pod, got %q", key)
	}
}

func TestTestHelpers(t *testing.T) {
	ResetNodeLocksForTest()
	if count := NodeLockCountForTest(); count != 0 {
		t.Fatalf("expected 0 locks after reset, got %d", count)
	}
	EnsureNodeLockForTest("node1")
	if count := NodeLockCountForTest(); count != 1 {
		t.Fatalf("expected 1 lock after ensure, got %d", count)
	}
	EnsureNodeLockForTest("node1")
	if count := NodeLockCountForTest(); count != 1 {
		t.Fatalf("expected 1 lock after no-op ensure, got %d", count)
	}
}
