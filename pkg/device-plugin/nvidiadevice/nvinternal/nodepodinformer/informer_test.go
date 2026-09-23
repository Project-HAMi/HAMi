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

package nodepodinformer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"
)

func TestInformerListFailsClosedUntilSynced(t *testing.T) {
	informer, err := New(fake.NewSimpleClientset(), "node-a")
	require.NoError(t, err)
	_, err = informer.List()
	require.ErrorIs(t, err, ErrNotSynced)
}

func TestInformerStartsOnceAndUsesNodeFieldSelector(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "default"},
		Spec:       corev1.PodSpec{NodeName: "node-a"},
	})
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		listAction := action.(ktesting.ListAction)
		// The reactor runs on the informer goroutine; use a nonfatal assertion.
		assert.Equal(t, "spec.nodeName=node-a", listAction.GetListRestrictions().Fields.String())
		return false, nil, nil
	})

	informer, err := New(client, "node-a")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	informer.Start(ctx)
	informer.Start(ctx)

	require.Eventually(t, func() bool {
		pods, err := informer.List()
		return err == nil && len(pods) == 1 && pods[0].Name == "pod-a"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestNewValidatesInputs(t *testing.T) {
	_, err := New(nil, "node-a")
	require.Error(t, err)
	_, err = New(fake.NewSimpleClientset(), "")
	require.Error(t, err)
}

func TestListFreshRejectsUnsyncedInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	informer, err := New(client, "node-a")
	require.NoError(t, err)
	_, err = informer.ListFresh(t.Context())
	require.ErrorIs(t, err, ErrNotSynced)
	require.Empty(t, client.Actions())
}

func TestListFreshConfirmsPodsMissingFromSyncedCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	informer, err := New(client, "node-a")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	informer.Start(ctx)
	require.Eventually(t, informer.informer.HasSynced, time.Second, time.Millisecond)
	cancel()
	informer.factory.Shutdown()
	// Freeze the informer at an empty, already-synced snapshot, as when a
	// watch stops delivering events. The API subsequently gains a new Pod.
	_, err = client.CoreV1().Pods("default").Create(t.Context(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "new-pod", Namespace: "default", UID: "new-uid"},
		Spec:       corev1.PodSpec{NodeName: "node-a"},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	cached, err := informer.List()
	require.NoError(t, err)
	require.Empty(t, cached)
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		list, ok := action.(interface{ GetListOptions() metav1.ListOptions })
		require.True(t, ok)
		options := list.GetListOptions()
		require.Equal(t, "spec.nodeName=node-a", options.FieldSelector)
		require.Empty(t, options.ResourceVersion, "do not request stale resourceVersion=0 reads")
		require.Empty(t, options.ResourceVersionMatch)
		return false, nil, nil
	})
	live, err := informer.ListFresh(t.Context())
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, "new-uid", string(live[0].UID))
	failure := errors.New("API unavailable")
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, failure
	})
	live, err = informer.ListFresh(t.Context())
	require.ErrorIs(t, err, failure)
	require.Nil(t, live, "never fall back to the stale empty snapshot")
}

func TestListFreshHonorsCancellation(t *testing.T) {
	informer, err := New(fake.NewSimpleClientset(), "node-a")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	informer.Start(ctx)
	require.Eventually(t, informer.informer.HasSynced, time.Second, time.Millisecond)
	cancel()
	informer.factory.Shutdown()
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	informer.client, err = kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	require.NoError(t, err)
	requestCtx, cancelRequest := context.WithCancel(t.Context())
	defer cancelRequest()
	done := make(chan error, 1)
	go func() { _, err := informer.ListFresh(requestCtx); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("confirmation request was not sent")
	}
	cancelRequest()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("API confirmation ignored cancellation")
	}
	timeoutCtx, cancelTimeout := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancelTimeout()
	pods, err := informer.ListFresh(timeoutCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, pods)
}
