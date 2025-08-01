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
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/util/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	NodeLockKey  = "hami.io/mutex.lock"
	MaxLockRetry = 5
	NodeLockSep  = ","
)

var (
	lock sync.Mutex
	// NodeLockTimeout is the global timeout for node locks.
	NodeLockTimeout time.Duration = time.Minute * 5
)

func init() {
	setupNodeLockTimeout()
}

// setupNodeLockTimeout configures the node lock timeout from the environment.
func setupNodeLockTimeout() {
	nodelock := os.Getenv("HAMI_NODELOCK_EXPIRE")
	if nodelock != "" {
		d, err := time.ParseDuration(nodelock)
		if err != nil {
			klog.ErrorS(err, "Failed to parse HAMI_NODELOCK_EXPIRE, using default", "duration", NodeLockTimeout)
		} else {
			NodeLockTimeout = d
			klog.InfoS("Node lock expiration time set from environment variable", "duration", d)
		}
	}
}

func SetNodeLock(nodeName string, lockname string, pods *corev1.Pod) error {
	lock.Lock()
	defer lock.Unlock()
	ctx := context.Background()
	node, err := client.GetClient().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if _, ok := node.Annotations[NodeLockKey]; ok {
		return fmt.Errorf("node %s is locked", nodeName)
	}
	patchData := fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"},"resourceVersion":"%s"}}`, NodeLockKey, GenerateNodeLockKeyByPod(pods), node.ResourceVersion)
	_, err = client.GetClient().CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	for i := 0; i < MaxLockRetry && err != nil; i++ {
		klog.ErrorS(err, "Failed to update node", "node", nodeName, "retry", i)
		time.Sleep(100 * time.Millisecond)
		node, err = client.GetClient().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to get node when retry to update", "node", nodeName)
			continue
		}
		patchData := fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"},"resourceVersion":"%s"}}`, NodeLockKey, GenerateNodeLockKeyByPod(pods), node.ResourceVersion)
		_, err = client.GetClient().CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	}
	if err != nil {
		return fmt.Errorf("setNodeLock exceeds retry count %d", MaxLockRetry)
	}
	klog.InfoS("Node lock set", "node", nodeName, "podName", pods.Name)
	return nil
}

func ReleaseNodeLock(nodeName string, lockname string, pod *corev1.Pod, timeout bool) error {
	lock.Lock()
	defer lock.Unlock()
	ctx := context.Background()
	node, err := client.GetClient().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if lockStr, ok := node.Annotations[NodeLockKey]; !ok {
		return nil
	} else {
		if !strings.Contains(lockStr, pod.Name) && !timeout {
			klog.InfoS("NodeLock is not set by this pod", lockStr, "pod", pod.Name)
			return nil
		}
	}
	patchData := fmt.Sprintf(`{"metadata":{"annotations":{"%s":null},"resourceVersion":"%s"}}`, NodeLockKey, node.ResourceVersion)
	_, err = client.GetClient().CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	for i := 0; i < MaxLockRetry && err != nil; i++ {
		klog.ErrorS(err, "Failed to update node", "node", nodeName, "retry", i)
		time.Sleep(100 * time.Millisecond)
		node, err = client.GetClient().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to get node when retry to update", "node", nodeName)
			continue
		}
		patchData := fmt.Sprintf(`{"metadata":{"annotations":{"%s":null},"resourceVersion":"%s"}}`, NodeLockKey, node.ResourceVersion)
		_, err = client.GetClient().CoreV1().Nodes().Patch(ctx, nodeName, types.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	}
	if err != nil {
		return fmt.Errorf("releaseNodeLock exceeds retry count %d", MaxLockRetry)
	}
	klog.InfoS("Node lock released", "node", nodeName, "podName", pod.Name)
	return nil
}

func LockNode(nodeName string, lockname string, pods *corev1.Pod) error {
	ctx := context.Background()
	node, err := client.GetClient().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if _, ok := node.Annotations[NodeLockKey]; !ok {
		return SetNodeLock(nodeName, lockname, pods)
	}
	lockTime, _, _, err := ParseNodeLock(node.Annotations[NodeLockKey])
	if err != nil {
		return err
	}
	if time.Since(lockTime) > NodeLockTimeout {
		klog.InfoS("Node lock expired", "node", nodeName, "lockTime", lockTime, "timeout", NodeLockTimeout)
		err = ReleaseNodeLock(nodeName, lockname, pods, true)
		if err != nil {
			klog.ErrorS(err, "Failed to release node lock", "node", nodeName)
			return err
		}
		return SetNodeLock(nodeName, lockname, pods)
	}
	return fmt.Errorf("node %s has been locked within %v", nodeName, NodeLockTimeout)
}

func ParseNodeLock(value string) (lockTime time.Time, ns, name string, err error) {
	if !strings.Contains(value, NodeLockSep) {
		lockTime, err = time.Parse(time.RFC3339, value)
		return lockTime, "", "", err
	}
	s := strings.Split(value, NodeLockSep)
	if len(s) != 3 {
		lockTime, err = time.Parse(time.RFC3339, value)
		return lockTime, "", "", err
	}
	lockTime, err = time.Parse(time.RFC3339, s[0])
	return lockTime, s[1], s[2], err
}

func GenerateNodeLockKeyByPod(pods *corev1.Pod) string {
	if pods == nil {
		return time.Now().Format(time.RFC3339)
	}
	ns, name := pods.Namespace, pods.Name
	return fmt.Sprintf("%s%s%s%s%s", time.Now().Format(time.RFC3339), NodeLockSep, ns, NodeLockSep, name)
}
