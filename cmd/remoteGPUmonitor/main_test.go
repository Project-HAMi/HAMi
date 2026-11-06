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

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	remote "github.com/Project-HAMi/HAMi/pkg/device/remotegpu"
	"github.com/Project-HAMi/HAMi/pkg/util"
)

func TestRunRequiresNodeName(t *testing.T) {
	err := run("", "http://127.0.0.1:9/metrics", ":0")
	if err == nil || err.Error() != "node name is required" {
		t.Fatalf("got %v", err)
	}
}

func TestReadyzReportsCacheSync(t *testing.T) {
	ready := false
	mux := newServeMux(func() bool { return ready }, prometheus.NewRegistry())
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz before sync: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
	ready = true
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz after sync: %d", rec.Code)
	}
}

func TestTrimPodKeepsCollectorFields(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "worker",
			Namespace:       "ns",
			UID:             types.UID("uid-1"),
			ResourceVersion: "7",
			Labels:          map[string]string{"app": "keep-me-out"},
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: "cpu-node",
				remote.AllocatedAnnos:        "allocated",
				"unrelated":                  "drop",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "cpu-node",
			InitContainers: []corev1.Container{{
				Name:          "setup",
				RestartPolicy: &always,
				Env: []corev1.EnvVar{
					{Name: "CUDA_DEVICE_MEMORY_LIMIT", Value: "1024m"},
					{Name: "OTHER", Value: "secret"},
				},
				Image: "busybox",
			}},
			Containers: []corev1.Container{{Name: "a", Image: "workload"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: "setup",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 0,
					Message:  "done",
				}},
			}},
			PodIP: "10.0.0.1",
		},
	}
	got := trimPod(pod)
	if got.Name != "worker" || got.Namespace != "ns" || got.UID != "uid-1" || got.ResourceVersion != "7" {
		t.Fatalf("identity: %+v", got.ObjectMeta)
	}
	if got.Annotations["unrelated"] != "" || got.Annotations[util.AssignedNodeAnnotations] != "cpu-node" || got.Annotations[remote.AllocatedAnnos] != "allocated" {
		t.Fatalf("annotations: %v", got.Annotations)
	}
	if len(got.Labels) != 0 || got.Spec.Containers[0].Image != "" || got.Status.PodIP != "" {
		t.Fatal("kept fields the collector does not read")
	}
	if got.Spec.NodeName != "cpu-node" || got.Status.Phase != corev1.PodRunning {
		t.Fatalf("placement: %s %s", got.Spec.NodeName, got.Status.Phase)
	}
	if len(got.Spec.InitContainers[0].Env) != 1 || got.Spec.InitContainers[0].Env[0].Value != "1024m" || got.Spec.InitContainers[0].RestartPolicy == nil {
		t.Fatalf("init container: %+v", got.Spec.InitContainers[0])
	}
	if got.Status.InitContainerStatuses[0].State.Terminated.ExitCode != 0 || got.Status.InitContainerStatuses[0].State.Terminated.Message != "" {
		t.Fatalf("init status: %+v", got.Status.InitContainerStatuses[0])
	}
}
