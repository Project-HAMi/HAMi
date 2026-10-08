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

package scheduler

import (
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/device/amd"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func TestNumaBindingRequested(t *testing.T) {
	pod := func(annos map[string]string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: annos}}
	}
	assert.Equal(t, false, numaBindingRequested(nil))
	assert.Equal(t, false, numaBindingRequested(pod(nil)))
	assert.Equal(t, true, numaBindingRequested(pod(map[string]string{nvidia.NumaBind: "true"})))
	assert.Equal(t, true, numaBindingRequested(pod(map[string]string{amd.AMDNumaBind: "true"})))
	assert.Equal(t, false, numaBindingRequested(pod(map[string]string{amd.AMDNumaBind: "false"})))
	assert.Equal(t, false, numaBindingRequested(pod(map[string]string{amd.AMDNumaBind: "maybe"})))
}
