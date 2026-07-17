/*
Copyright 2026 The HAMi Authors.

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
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

// Test_updateSchedulerLabel_PatchErrorDoesNotExit verifies that a failing leader
// label patch is logged and does not terminate the scheduler. If updateSchedulerLabel
// still called klog.Fatalf, os.Exit would kill the test binary before the assertion,
// failing the whole package; reaching require.Positive proves it returned instead.
func Test_updateSchedulerLabel_PatchErrorDoesNotExit(t *testing.T) {
	const ns, name = "kube-system", "hami-scheduler-0"
	t.Setenv("POD_NAMESPACE", ns)
	t.Setenv("POD_NAME", name)

	// Leader pod with the component label but no role label, so the leader-patch branch runs.
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: ns,
		Labels:    map[string]string{util.HAMiComponentLabel: util.HAMiComponentScheduler},
	}}

	fakeClient := fake.NewClientset()
	var patched atomic.Int32
	fakeClient.PrependReactor("patch", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		patched.Add(1)
		return true, nil, fmt.Errorf("the server was unable to return a response in time")
	})
	origKubeClient := client.KubeClient
	client.KubeClient = fakeClient
	t.Cleanup(func() { client.KubeClient = origKubeClient })

	s := NewScheduler()
	s.kubeClient = fakeClient
	informerFactory := informers.NewSharedInformerFactoryWithOptions(fakeClient, 0)
	s.podLister = informerFactory.Core().V1().Pods().Lister()
	require.NoError(t, informerFactory.Core().V1().Pods().Informer().GetIndexer().Add(pod))

	s.updateSchedulerLabel()

	require.Positive(t, patched.Load(), "the leader label patch should have been attempted")
}

// failingPodLister is a PodLister whose List always fails, used to exercise the
// list-error branch of updateSchedulerLabel.
type failingPodLister struct{ err error }

func (l failingPodLister) List(labels.Selector) ([]*corev1.Pod, error) { return nil, l.err }
func (l failingPodLister) Pods(string) listerscorev1.PodNamespaceLister {
	return failingPodNamespaceLister(l)
}

type failingPodNamespaceLister struct{ err error }

func (l failingPodNamespaceLister) List(labels.Selector) ([]*corev1.Pod, error) { return nil, l.err }
func (l failingPodNamespaceLister) Get(string) (*corev1.Pod, error)             { return nil, l.err }

// Test_updateSchedulerLabel_ListErrorDoesNotExit verifies that a failing pod
// list is logged and does not terminate the scheduler. Reaching the end of the
// test proves updateSchedulerLabel returned instead of calling os.Exit.
func Test_updateSchedulerLabel_ListErrorDoesNotExit(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "kube-system")

	s := NewScheduler()
	s.podLister = failingPodLister{err: fmt.Errorf("the server was unable to return a response in time")}

	s.updateSchedulerLabel()
}
