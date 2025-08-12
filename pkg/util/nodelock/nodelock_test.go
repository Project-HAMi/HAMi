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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func Test_LockNode(t *testing.T) {
	client.KubeClient = fake.NewSimpleClientset()
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
			name: "node has been locked",
			args: args{
				nodeName: func() string {
					name := "worker-1"
					client.KubeClient.CoreV1().Nodes().Create(context.TODO(), &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: name,
							Annotations: map[string]string{
								NodeLockKey: GenerateNodeLockKeyByPod(&corev1.Pod{
									ObjectMeta: metav1.ObjectMeta{Name: "hami", Namespace: "hami-ns"},
								}),
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
	client.KubeClient = fake.NewSimpleClientset()

	// Set a custom timeout for testing
	originalTimeout := NodeLockTimeout
	NodeLockTimeout = time.Minute * 2
	defer func() {
		NodeLockTimeout = originalTimeout
	}()

	nodeName := "test-node-timeout"

	// Create a node with a fresh lock (should not be expired)
	freshLockTime := time.Now().Format(time.RFC3339)
	lockValue := freshLockTime + NodeLockSep + "test-ns" + NodeLockSep + "test-pod"

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

func TestReleaseNodeLock(t *testing.T) {
	client.KubeClient = fake.NewSimpleClientset()
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
