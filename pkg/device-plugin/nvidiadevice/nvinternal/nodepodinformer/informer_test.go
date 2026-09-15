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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
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
