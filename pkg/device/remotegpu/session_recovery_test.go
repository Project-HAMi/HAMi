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
	"fmt"
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

// TestReconcileSessionStubs_PreservesAdmissionRewrittenImage checks convergence
// after image admission and replacement when the requested image changes.
func TestReconcileSessionStubs_PreservesAdmissionRewrittenImage(t *testing.T) {
	const (
		requestedImage = "alpine/socat:test"
		rewrittenImage = "registry.example/socat@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	for _, scene := range []string{"new relay", "legacy unchanged image", "legacy rewritten image"} {
		t.Run(scene, func(t *testing.T) {
			var seeds []*corev1.Pod
			if scene != "new relay" {
				stub, err := sessionStub("kube-system", "gpu-a", "10.0.0.5:14833", requestedImage, nil)
				assert.NilError(t, err)
				delete(stub.Annotations, sessionImageAnno)
				stub.UID, stub.Status.Phase = types.UID("legacy-relay"), corev1.PodRunning
				if scene == "legacy rewritten image" {
					stub.Spec.Containers[0].Image = rewrittenImage
				}
				seeds = append(seeds, stub)
			}
			c := sessionScene(t, []corev1.Node{lupineNode("gpu-a", "10.0.0.5", "", []*device.DeviceInfo{gpu("GPU-1", 40000)})}, seeds...)
			cfg := testConfig()
			cfg.SessionImage = requestedImage
			dev := InitRemoteGPUDevice(cfg)
			t.Cleanup(func() { InitRemoteGPUDevice(testConfig()) })

			creates, deletes := 0, 0
			pods := corev1.SchemeGroupVersion.WithResource("pods")
			c.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
				create, ok := a.(k8stesting.CreateAction)
				if !ok {
					t.Fatalf("expected CreateAction, got %T", a)
				}
				created, ok := create.GetObject().(*corev1.Pod)
				if !ok {
					t.Fatalf("expected Pod, got %T", create.GetObject())
				}
				admitted := created.DeepCopy()
				creates++
				admitted.UID = types.UID(fmt.Sprintf("admitted-relay-%d", creates))
				// Different requested versions deliberately map to the same image.
				admitted.Spec.Containers[0].Image = rewrittenImage
				assert.NilError(t, c.Tracker().Create(pods, admitted, admitted.Namespace))
				return true, admitted, nil
			})
			c.PrependReactor("delete", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
				deletes++
				del, ok := a.(k8stesting.DeleteAction)
				if !ok {
					t.Fatalf("expected DeleteAction, got %T", a)
				}
				listed, err := c.Tracker().Get(pods, del.GetNamespace(), del.GetName())
				assert.NilError(t, err)
				listedPod, ok := listed.(*corev1.Pod)
				if !ok {
					t.Fatalf("expected Pod, got %T", listed)
				}
				pre := del.GetDeleteOptions().Preconditions
				assert.Assert(t, pre != nil && pre.UID != nil && *pre.UID == listedPod.UID)
				return false, nil, nil
			})

			dev.ReconcileSessionStubs(context.Background())
			got := stubsIn(t, c)
			assert.Equal(t, len(got), 1)
			initialCreates, initialDeletes := 1, 0
			if scene == "legacy unchanged image" {
				initialCreates = 0
			} else {
				assert.Equal(t, got[0].Spec.Containers[0].Image, rewrittenImage)
				assert.Equal(t, got[0].Annotations[sessionImageAnno], requestedImage)
			}
			if scene == "legacy rewritten image" {
				initialDeletes = 1
			}
			assert.Equal(t, creates, initialCreates)
			assert.Equal(t, deletes, initialDeletes)
			original := got[0].DeepCopy()
			for range 3 {
				dev.ReconcileSessionStubs(context.Background())
				got = stubsIn(t, c)
				assert.Equal(t, len(got), 1)
				assert.DeepEqual(t, got[0], *original)
				assert.Equal(t, creates, initialCreates)
				assert.Equal(t, deletes, initialDeletes)
			}

			cfg.SessionImage = "alpine/socat:new"
			dev = InitRemoteGPUDevice(cfg)
			dev.ReconcileSessionStubs(context.Background())
			got = stubsIn(t, c)
			assert.Equal(t, len(got), 1)
			assert.Assert(t, got[0].UID != original.UID, "a requested image change must replace the relay")
			assert.Equal(t, got[0].Spec.Containers[0].Image, rewrittenImage)
			assert.Equal(t, got[0].Annotations[sessionImageAnno], cfg.SessionImage)
			assert.Equal(t, creates, initialCreates+1)
			assert.Equal(t, deletes, initialDeletes+1)
			replacement := got[0].DeepCopy()
			for range 3 {
				dev.ReconcileSessionStubs(context.Background())
				got = stubsIn(t, c)
				assert.Equal(t, len(got), 1)
				assert.DeepEqual(t, got[0], *replacement)
				assert.Equal(t, creates, initialCreates+1)
				assert.Equal(t, deletes, initialDeletes+1)
			}
		})
	}
}

// TestReconcileSessionStubs_RecoversTerminalAndOutdatedPods checks lifecycle and
// configuration recovery without replacing relays for kubelet restarts or
// admission-injected additions.
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
		{name: "failed without an eviction reason", replace: true, mutate: func(p *corev1.Pod) { p.Status.Phase = corev1.PodFailed }},
		{name: "completed", replace: true, mutate: func(p *corev1.Pod) { p.Status.Phase = corev1.PodSucceeded }},
		{name: "old image", replace: true, mutate: func(p *corev1.Pod) {
			p.Spec.Containers[0].Image = "alpine/socat:old"
			p.Annotations[sessionImageAnno] = "alpine/socat:old"
		}},
		{name: "pending old image", replace: true, mutate: func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodPending
			p.Spec.Containers[0].Image = "alpine/socat:old"
			p.Annotations[sessionImageAnno] = "alpine/socat:old"
		}},
		{name: "missing relay", replace: true, mutate: func(p *corev1.Pod) { p.Spec.Containers[0].Name = "other" }},
		{name: "changed endpoint with the current image", replace: true, mutate: func(p *corev1.Pod) {
			p.Spec.Containers[0].Env[0].Value = "10.0.0.5:9999"
		}},
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

// TestReconcileSessionStubs_WaitsForGracefulRecovery checks that the fixed relay
// name is reused only after the old Pod disappears.
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
	creates, deletes := 0, 0
	c.PrependReactor("create", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		creates++
		return false, nil, nil
	})
	c.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		deletes++
		return true, nil, nil
	})
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, deletes, 1)
	assert.Equal(t, creates, 1)
	assert.Equal(t, stubsIn(t, c)[0].UID, stub.UID)

	terminating := stub.DeepCopy()
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	assert.NilError(t, c.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), terminating, "kube-system"))
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, deletes, 1, "an in-progress deletion is not requested again")
	assert.Equal(t, creates, 1, "a terminating relay must not trigger another create")
	assert.Equal(t, stubsIn(t, c)[0].UID, stub.UID)

	assert.NilError(t, c.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "kube-system", stub.Name))
	dev.ReconcileSessionStubs(context.Background())
	assert.Equal(t, creates, 2, "one replacement is created after deletion completes")
	got := stubsIn(t, c)
	assert.Equal(t, len(got), 1)
	assert.Assert(t, got[0].UID != stub.UID)
	assert.Equal(t, got[0].Spec.Containers[0].Image, cfg.SessionImage)
}

// TestReconcileSessionStubs_PreservesLeaderReplacement checks that a stale
// UID-conditioned delete leaves another leader's replacement intact.
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
