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

package remotegpu

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

func TestReconcileSessionStubs_RecoversTerminalAndOutdatedPods(t *testing.T) {
	const desiredImage = "alpine/socat:new"
	tests := []struct {
		name    string
		mutate  func(*corev1.Pod)
		replace bool
	}{
		{name: "evicted", replace: true, mutate: func(p *corev1.Pod) {
			p.Status.Phase, p.Status.Reason = corev1.PodFailed, "Evicted"
		}},
		{name: "completed", replace: true, mutate: func(p *corev1.Pod) { p.Status.Phase = corev1.PodSucceeded }},
		{name: "old image", replace: true, mutate: func(p *corev1.Pod) { p.Spec.Containers[0].Image = "alpine/socat:old" }},
		{name: "pending old image", replace: true, mutate: func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodPending
			p.Spec.Containers[0].Image = "alpine/socat:old"
		}},
		{name: "missing relay", replace: true, mutate: func(p *corev1.Pod) { p.Spec.Containers[0].Name = "other" }},
		{name: "running"},
		{name: "pending", mutate: func(p *corev1.Pod) { p.Status.Phase = corev1.PodPending }},
		{name: "unknown phase", mutate: func(p *corev1.Pod) { p.Status.Phase = corev1.PodUnknown }},
		{name: "no phase yet", mutate: func(p *corev1.Pod) { p.Status.Phase = "" }},
		{name: "kubelet restarting the container", mutate: func(p *corev1.Pod) {
			p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "relay", RestartCount: 4,
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
		}},
		{name: "service account injected secrets", mutate: func(p *corev1.Pod) {
			p.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "service-account-registry"}}
		}},
		{name: "injected sidecar has another image", mutate: func(p *corev1.Pod) {
			p.Spec.Containers = append([]corev1.Container{{Name: "sidecar", Image: "sidecar:latest"}}, p.Spec.Containers...)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub, err := sessionStub("kube-system", "gpu-a", "10.0.0.5:14833", desiredImage, nil)
			assert.NilError(t, err)
			stub.UID, stub.Status.Phase = types.UID("listed-relay"), corev1.PodRunning
			if tt.mutate != nil {
				tt.mutate(stub)
			}
			c := sessionScene(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, stub)
			cfg := testConfig()
			cfg.SessionImage = desiredImage
			dev := InitRemoteGPUDevice(cfg)
			t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })
			c.ClearActions()

			dev.ReconcileSessionStubs(context.Background())
			got := stubsIn(t, c)
			assert.Equal(t, len(got), 1)
			var creates, deletes int
			for _, a := range c.Actions() {
				switch a.GetVerb() {
				case "create":
					creates++
				case "delete":
					deletes++
					del, ok := a.(k8stesting.DeleteAction)
					if !ok {
						t.Fatalf("expected DeleteAction, got %T", a)
					}
					pre := del.GetDeleteOptions().Preconditions
					assert.Assert(t, pre != nil && pre.UID != nil && *pre.UID == stub.UID)
				}
			}
			if tt.replace {
				assert.Equal(t, deletes, 1)
				assert.Equal(t, creates, 1)
				assert.Assert(t, got[0].UID != stub.UID, "the obsolete relay was retained")
				assert.Equal(t, got[0].Spec.Containers[0].Image, desiredImage)
				assert.Equal(t, stubEndpoint(&got[0]), "10.0.0.5:14833")
			} else {
				assert.Equal(t, deletes, 0)
				assert.Equal(t, creates, 0)
				assert.DeepEqual(t, got[0], *stub)
			}
		})
	}
}

func TestReconcileSessionStubs_WaitsForGracefulRecovery(t *testing.T) {
	stub, err := sessionStub("kube-system", "gpu-a", "10.0.0.5:14833", "alpine/socat:old", nil)
	assert.NilError(t, err)
	stub.UID, stub.Status.Phase = types.UID("outdated-relay"), corev1.PodRunning
	c := sessionScene(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, stub)
	cfg := testConfig()
	cfg.SessionImage = "alpine/socat:test"
	dev := InitRemoteGPUDevice(cfg)
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })

	// Keep the old Pod in the tracker until its graceful deletion completes.
	deletes := 0
	c.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		deletes++
		return true, nil, nil
	})
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, deletes, 1)
	assert.Equal(t, stubsIn(t, c)[0].UID, stub.UID)

	terminating := stub.DeepCopy()
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	assert.NilError(t, c.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), terminating, "kube-system"))
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, deletes, 1, "an in-progress deletion is not requested again")
	assert.Equal(t, stubsIn(t, c)[0].UID, stub.UID)

	assert.NilError(t, c.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "kube-system", stub.Name))
	dev.ReconcileSessionStubs(context.Background())
	got := stubsIn(t, c)
	assert.Equal(t, len(got), 1)
	assert.Assert(t, got[0].UID != stub.UID)
	assert.Equal(t, got[0].Spec.Containers[0].Image, cfg.SessionImage)
}

func TestReconcileSessionStubs_PreservesLeaderReplacement(t *testing.T) {
	stub, err := sessionStub("kube-system", "gpu-a", "10.0.0.5:14833", "alpine/socat:test", nil)
	assert.NilError(t, err)
	stub.UID, stub.Status.Phase = types.UID("evicted-relay"), corev1.PodFailed
	c := sessionScene(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, stub)
	cfg := testConfig()
	cfg.SessionImage = "alpine/socat:test"
	dev := InitRemoteGPUDevice(cfg)
	t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })

	replacement := stub.DeepCopy()
	replacement.UID, replacement.Status.Phase = types.UID("new-leader-relay"), corev1.PodRunning
	deletes := 0
	c.PrependReactor("delete", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		deletes++
		del, ok := a.(k8stesting.DeleteAction)
		if !ok {
			t.Fatalf("expected DeleteAction, got %T", a)
		}
		pre := del.GetDeleteOptions().Preconditions
		assert.Assert(t, pre != nil && pre.UID != nil && *pre.UID == stub.UID)
		// Another leader has already created a new Pod under the fixed name.
		assert.NilError(t, c.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "kube-system", stub.Name))
		assert.NilError(t, c.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), replacement, "kube-system"))
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, stub.Name, nil)
	})
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, deletes, 1)
	assert.Equal(t, stubsIn(t, c)[0].UID, replacement.UID)
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, deletes, 1)
	assert.DeepEqual(t, stubsIn(t, c)[0], *replacement)
}
