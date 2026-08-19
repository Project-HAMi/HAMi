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

package cambricon

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

func Test_GetNodeDevices(t *testing.T) {
	config := CambriconConfig{
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
	}
	InitMLUDevice(config)
	tests := []struct {
		name string
		args corev1.Node
		want []*device.DeviceInfo
	}{
		{
			name: "test with valid configuration",
			args: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						"cambricon.com/mlu.smlu.vcore":   *resource.NewQuantity(1, resource.DecimalSI),
						"cambricon.com/mlu.smlu.vmemory": *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			},
			want: []*device.DeviceInfo{
				{
					Index:        0,
					ID:           "test-cambricon-mlu-0",
					Count:        int32(100),
					Devmem:       int32(25600),
					Devcore:      int32(100),
					Type:         CambriconMLUDevice,
					Numa:         0,
					Health:       true,
					DeviceVendor: CambriconMLUCommonWord,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := CambriconDevices{}
			result, err := dev.GetNodeDevices(test.args)
			if err != nil {
				assert.NoError(t, err)
			}
			for k, v := range test.want {
				assert.Equal(t, v, result[k])
			}
		})
	}
}

func Test_MutateAdmission(t *testing.T) {
	config := CambriconConfig{
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
		ResourceCountName:  "cambricon.com/mlu",
	}
	InitMLUDevice(config)
	tests := []struct {
		name string
		args struct {
			ctr corev1.Container
			pod corev1.Pod
		}
		want bool
		err  error
	}{
		{
			name: "set to resources limits",
			args: struct {
				ctr corev1.Container
				pod corev1.Pod
			}{
				ctr: corev1.Container{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"cambricon.com/mlu": resource.MustParse("1"),
						},
					},
				},
				pod: corev1.Pod{},
			},
			want: true,
			err:  nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := CambriconDevices{}
			result, _ := dev.MutateAdmission(&test.args.ctr, &test.args.pod)
			assert.Equal(t, result, test.want)
		})
	}
}

func Test_ParseConfig(t *testing.T) {
	tests := []struct {
		name string
		args flag.FlagSet
	}{
		{
			name: "test",
			args: flag.FlagSet{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ParseConfig(&test.args)
		})
	}
}

func Test_checkType(t *testing.T) {
	dev := CambriconDevices{}
	tests := []struct {
		name string
		args struct {
			annos map[string]string
			d     device.DeviceUsage
			n     device.ContainerDeviceRequest
		}
		want1 bool
		want2 bool
	}{
		{
			name: "the same type",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
					Type: dev.CommonWord(),
				},
			},
			want1: true,
			want2: true,
		},
		{
			name: "the different type",
			args: struct {
				annos map[string]string
				d     device.DeviceUsage
				n     device.ContainerDeviceRequest
			}{
				annos: map[string]string{},
				d:     device.DeviceUsage{},
				n: device.ContainerDeviceRequest{
					Type: "TEST",
				},
			},
			want1: false,
			want2: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result1, result2, _ := dev.checkType(test.args.annos, test.args.d, test.args.n)
			assert.Equal(t, result1, test.want1)
			assert.Equal(t, result2, test.want2)
		})
	}
}

func Test_GenerateResourceRequests(t *testing.T) {
	tests := []struct {
		name string
		args corev1.Container
		want device.ContainerDeviceRequest
	}{
		{
			name: "don't set to limits and request",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits:   corev1.ResourceList{},
					Requests: corev1.ResourceList{},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums: 0,
			},
		},
		{
			name: "resourcecoresname,resourcecountname and resourcememoryname set to limits and request",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
						"cambricon.com/mlu.smlu.vcore":   resource.MustParse("2"),
					},
					Requests: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
						"cambricon.com/mlu.smlu.vcore":   resource.MustParse("2"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             CambriconMLUDevice,
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(2),
			},
		},
		{
			name: "resourcememoryname don't set to limits and requests",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
					},
					Requests: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("1000"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             CambriconMLUDevice,
				Memreq:           int32(256000),
				MemPercentagereq: int32(0),
				Coresreq:         int32(100),
			},
		},
		{
			name: "resourcecoresname don't set to limits and requests",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":            resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vcore": resource.MustParse("2"),
					},
					Requests: corev1.ResourceList{
						"cambricon.com/mlu":            resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vcore": resource.MustParse("2"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             int32(1),
				Type:             CambriconMLUDevice,
				Memreq:           int32(0),
				MemPercentagereq: int32(100),
				Coresreq:         int32(2),
			},
		},
		{
			name: "memory overflowing int32 is rejected, not truncated to zero",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("16Gi"),
			name: "zero count must not silently bypass quota",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu": resource.MustParse("0"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "decimal-form memory request is rejected, not treated as zero",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu":              resource.MustParse("1"),
						"cambricon.com/mlu.smlu.vmemory": resource.MustParse("16.0Gi"),
			name: "negative count must be rejected",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu": resource.MustParse("-1"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
		{
			name: "max int32 count is accepted",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu": resource.MustParse("2147483647"),
					},
				},
			},
			want: device.ContainerDeviceRequest{
				Nums:             2147483647,
				Type:             CambriconMLUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         100,
			},
		},
		{
			name: "count above max int32 is rejected",
			args: corev1.Container{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						"cambricon.com/mlu": resource.MustParse("2147483648"),
					},
				},
			},
			want: device.ContainerDeviceRequest{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := CambriconConfig{
				ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
				ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
				ResourceCountName:  "cambricon.com/mlu",
			}
			InitMLUDevice(config)
			dev := CambriconDevices{}
			result := dev.GenerateResourceRequests(&test.args)
			assert.Equal(t, test.want, result)
		})
	}
}

func Test_PatchAnnotations(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			annoinput map[string]string
			pd        device.PodDevices
		}
		want map[string]string
	}{
		{
			name: "exist device",
			args: struct {
				annoinput map[string]string
				pd        device.PodDevices
			}{
				annoinput: map[string]string{},
				pd: device.PodDevices{
					CambriconMLUDevice: device.PodSingleDevice{
						[]device.ContainerDevice{
							{
								Idx:       0,
								UUID:      "device-0",
								Type:      "MLU",
								Usedcores: 1,
								Usedmem:   256000,
							},
						},
					},
				},
			},
			want: map[string]string{
				"CAMBRICON_DSMLU_ASSIGNED":                  "false",
				"CAMBRICON_DSMLU_PROFILE":                   "0_1_1000",
				"hami.io/cambricon-mlu-devices-to-allocate": "device-0,MLU,256000,1:;",
				"hami.io/cambricon-mlu-devices-allocated":   "device-0,MLU,256000,1:;",
			},
		},
		{
			name: "no device",
			args: struct {
				annoinput map[string]string
				pd        device.PodDevices
			}{
				annoinput: map[string]string{},
				pd:        device.PodDevices{},
			},
			want: map[string]string{},
		},
		{
			// First container has no MLU (padded empty), real device in the second; used to panic on devlist[0][0].
			name: "leading container without device",
			args: struct {
				annoinput map[string]string
				pd        device.PodDevices
			}{
				annoinput: map[string]string{},
				pd: device.PodDevices{
					CambriconMLUDevice: device.PodSingleDevice{
						[]device.ContainerDevice{},
						[]device.ContainerDevice{
							{
								Idx:       0,
								UUID:      "device-0",
								Type:      "MLU",
								Usedcores: 1,
								Usedmem:   256000,
							},
						},
					},
				},
			},
			want: map[string]string{
				"CAMBRICON_DSMLU_ASSIGNED":                  "false",
				"CAMBRICON_DSMLU_PROFILE":                   "0_1_1000",
				"hami.io/cambricon-mlu-devices-to-allocate": ";device-0,MLU,256000,1:;",
				"hami.io/cambricon-mlu-devices-allocated":   ";device-0,MLU,256000,1:;",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dev := CambriconDevices{}
			result := dev.PatchAnnotations(&corev1.Pod{}, &test.args.annoinput, test.args.pd)
			assert.Equal(t, len(test.want), len(result), "Expected length of result to match want")
			for k, v := range test.want {
				assert.Equal(t, v, result[k], "pod add annotation key [%s], values is [%s]", k, result[k])
			}
		})
	}
}

func Test_setNodeLock(t *testing.T) {
	client.KubeClient = fake.NewClientset()
	k8sClient := client.GetClient()
	if k8sClient == nil {
		t.Skip("no k8s client available")
	}

	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "node-set-lock",
			Annotations: map[string]string{},
		},
	}

	ctx := context.Background()
	_, err := k8sClient.CoreV1().Nodes().Create(ctx, &node, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create node: %v", err)
	}
	defer k8sClient.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{})

	dev := CambriconDevices{}
	err = dev.setNodeLock(&node)
	if err != nil {
		t.Errorf("did not expect error but got %v", err)
	}

	// Verify the annotation was set on the apiserver.
	fetchedNode, err := k8sClient.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to fetch node: %v", err)
	}
	if _, ok := fetchedNode.Annotations[DsmluLockTime]; !ok {
		t.Error("Expected node to be locked but it wasn't")
	}
}

// Setup function to initialize resources for each test case.
func setupTest(t *testing.T) (*corev1.Node, *corev1.Pod, func(), *fake.Clientset) {
	ctx := context.Background()

	clientset := fake.NewClientset()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceName(MLUResourceCount):  resource.MustParse("2"),
				corev1.ResourceName(MLUResourceMemory): resource.MustParse("4096"),
				corev1.ResourceName(MLUResourceCores):  resource.MustParse("200"),
			},
		},
	}
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "test-container",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceName(MLUResourceCount):  resource.MustParse("1"),
						corev1.ResourceName(MLUResourceMemory): resource.MustParse("2048"),
						corev1.ResourceName(MLUResourceCores):  resource.MustParse("100"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceName(MLUResourceCount):  resource.MustParse("1"),
						corev1.ResourceName(MLUResourceMemory): resource.MustParse("2048"),
						corev1.ResourceName(MLUResourceCores):  resource.MustParse("100"),
					},
				},
			}},
		},
	}

	config := CambriconConfig{
		ResourceCountName:  MLUResourceCount,
		ResourceMemoryName: MLUResourceMemory,
		ResourceCoreName:   MLUResourceCores,
	}
	InitMLUDevice(config)

	_, err := clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	return node, pod, func() {
		clientset.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{})
	}, clientset
}

func Test_LockNode(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantErr     bool
	}{
		{
			name:        "node is not locked",
			annotations: map[string]string{},
			wantErr:     false,
		},
		{
			name: "node is already locked within 2 minutes",
			annotations: map[string]string{
				DsmluLockTime: time.Now().Add(-time.Minute).Format(time.RFC3339),
			},
			wantErr: true,
		},
		{
			name: "lock time expired (more than 2 minutes)",
			annotations: map[string]string{
				DsmluLockTime: time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			wantErr: false,
		},
		{
			name: "invalid lock time format",
			annotations: map[string]string{
				DsmluLockTime: "invalid-format",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, pod, teardown, clientset := setupTest(t)
			client.KubeClient = clientset
			defer teardown()

			// Set up the node with the specified annotations.
			node.Annotations = tt.annotations

			dev := InitMLUDevice(CambriconConfig{
				ResourceCountName:  MLUResourceCount,
				ResourceMemoryName: MLUResourceMemory,
				ResourceCoreName:   MLUResourceCores,
			})

			err := dev.LockNode(node, pod)
			if (err != nil) != tt.wantErr {
				t.Errorf("LockNode() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Optionally check if the node was correctly patched with the lock annotation.
			if !tt.wantErr {
				fetchedNode, _ := clientset.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
				if _, ok := fetchedNode.Annotations[DsmluLockTime]; !ok && !tt.wantErr {
					t.Error("Expected node to be locked but it wasn't")
				}
			}
		})
	}
}

func Test_ReleaseNodeLock(t *testing.T) {
	clientset := fake.NewClientset()
	client.KubeClient = clientset
	ctx := context.Background()

	tests := []struct {
		name              string
		nodeName          string
		annotations       map[string]string
		callerAnnotations map[string]string
		skipCreate        bool
		wantErr           bool
	}{
		{
			name:        "no annotation — lock not present",
			nodeName:    "node-rel-01",
			annotations: nil,
			wantErr:     false,
		},
		{
			name:     "annotation without lock key",
			nodeName: "node-rel-02",
			annotations: map[string]string{
				"test": "test123",
			},
			wantErr: false,
		},
		{
			name:     "lock present — successfully released",
			nodeName: "node-rel-03",
			annotations: map[string]string{
				DsmluLockTime: time.Now().Format(time.RFC3339),
			},
			wantErr: false,
		},
		{
			name:     "stale caller — lock on server but not on caller node",
			nodeName: "node-rel-04",
			annotations: map[string]string{
				DsmluLockTime: time.Now().Format(time.RFC3339),
			},
			callerAnnotations: map[string]string{},
			wantErr:           false,
		},
		{
			name:       "node not found",
			nodeName:   "node-rel-nonexistent",
			skipCreate: true,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.skipCreate {
				node := &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:        tt.nodeName,
						Annotations: tt.annotations,
					},
				}
				_, err := clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("failed to create node %s: %v", tt.nodeName, err)
				}
				defer clientset.CoreV1().Nodes().Delete(ctx, tt.nodeName, metav1.DeleteOptions{})
			}

			callerNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.nodeName,
				},
			}
			if tt.callerAnnotations != nil {
				callerNode.Annotations = tt.callerAnnotations
			} else if tt.annotations != nil {
				callerNode.Annotations = tt.annotations
			}

			dev := CambriconDevices{}
			err := dev.ReleaseNodeLock(callerNode, &corev1.Pod{})
			if (err != nil) != tt.wantErr {
				t.Errorf("ReleaseNodeLock() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && !tt.skipCreate && tt.annotations != nil {
				if _, ok := tt.annotations[DsmluLockTime]; ok {
					fetched, ferr := clientset.CoreV1().Nodes().Get(ctx, tt.nodeName, metav1.GetOptions{})
					if ferr != nil {
						t.Fatalf("failed to fetch node: %v", ferr)
					}
					if _, ok := fetched.Annotations[DsmluLockTime]; ok {
						t.Error("Expected lock annotation to be removed but it's still present")
					}
				}
			}
		})
	}

	// Retry/conflict tests using PrependReactor.
	t.Run("patch error — retry succeeds", func(t *testing.T) {
		cs := fake.NewClientset()
		client.KubeClient = cs

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-retry-01",
				Annotations: map[string]string{
					DsmluLockTime: time.Now().Format(time.RFC3339),
				},
			},
		}
		_, err := cs.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create node: %v", err)
		}
		defer cs.CoreV1().Nodes().Delete(ctx, "node-retry-01", metav1.DeleteOptions{})

		patchFailCount := 0
		cs.PrependReactor("patch", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			patchFailCount++
			if patchFailCount <= 2 {
				return true, nil, apierrors.NewInternalError(fmt.Errorf("patch error"))
			}
			return false, nil, nil
		})

		dev := CambriconDevices{}
		err = dev.ReleaseNodeLock(node, &corev1.Pod{})
		if err != nil {
			t.Errorf("ReleaseNodeLock() unexpected error: %v", err)
		}
		if patchFailCount < 2 {
			t.Errorf("expected at least 2 patch errors before success, got %d", patchFailCount)
		}

		fetched, _ := cs.CoreV1().Nodes().Get(ctx, "node-retry-01", metav1.GetOptions{})
		if _, ok := fetched.Annotations[DsmluLockTime]; ok {
			t.Error("Expected lock annotation to be removed after retries")
		}
	})

	t.Run("lock already removed during retry", func(t *testing.T) {
		cs := fake.NewClientset()
		client.KubeClient = cs

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-retry-02",
				Annotations: map[string]string{
					DsmluLockTime: time.Now().Format(time.RFC3339),
				},
			},
		}
		_, err := cs.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create node: %v", err)
		}
		defer cs.CoreV1().Nodes().Delete(ctx, "node-retry-02", metav1.DeleteOptions{})

		cs.PrependReactor("patch", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewInternalError(fmt.Errorf("patch error"))
		})

		getCount := 0
		cs.PrependReactor("get", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			getCount++
			if getCount == 2 {
				return true, &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "node-retry-02",
						Annotations: map[string]string{},
					},
				}, nil
			}
			return false, nil, nil
		})

		dev := CambriconDevices{}
		err = dev.ReleaseNodeLock(node, &corev1.Pod{})
		if err != nil {
			t.Errorf("ReleaseNodeLock() unexpected error: %v", err)
		}
	})

	t.Run("re-get fails during retry", func(t *testing.T) {
		cs := fake.NewClientset()
		client.KubeClient = cs

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-retry-03",
				Annotations: map[string]string{
					DsmluLockTime: time.Now().Format(time.RFC3339),
				},
			},
		}
		_, err := cs.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create node: %v", err)
		}
		defer cs.CoreV1().Nodes().Delete(ctx, "node-retry-03", metav1.DeleteOptions{})

		cs.PrependReactor("patch", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewInternalError(fmt.Errorf("patch error"))
		})
		getCount := 0
		cs.PrependReactor("get", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			getCount++
			if getCount == 2 {
				return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "nodes"}, "node-retry-03")
			}
			return false, nil, nil
		})

		dev := CambriconDevices{}
		err = dev.ReleaseNodeLock(node, &corev1.Pod{})
		if err == nil {
			t.Error("ReleaseNodeLock() expected error but got nil")
		}
	})
}

func TestDevices_Fit(t *testing.T) {
	config := CambriconConfig{
		ResourceCountName:  "cambricon.com/mlu",
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
	}
	dev := InitMLUDevice(config)

	tests := []struct {
		name       string
		devices    []*device.DeviceUsage
		request    device.ContainerDeviceRequest
		annos      map[string]string
		wantFit    bool
		wantLen    int
		wantDevIDs []string
		wantReason string
	}{
		{
			name: "fit success",
			devices: []*device.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      CambriconMLUDevice,
					Health:    true,
				},
				{
					ID:        "dev-1",
					Index:     0,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      CambriconMLUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-1"},
			wantReason: "",
		},
		{
			name: "fit fail: memory not enough",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientMemory",
		},
		{
			name: "fit fail: core not enough",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1024,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardInsufficientCore",
		},
		{
			name: "fit fail: type mismatch",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Health:    true,
				Type:      CambriconMLUDevice,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Type:             "OtherType",
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTypeMismatch",
		},
		{
			name: "fit fail: user assign use uuid mismatch",
			devices: []*device.DeviceUsage{{
				ID:        "dev-1",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{"cambricon.com/use-gpuuuid": "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: user assign no use uuid match",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{"cambricon.com/nouse-gpuuuid": "dev-0"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardUuidMismatch",
		},
		{
			name: "fit fail: card overused",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      100,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardTimeSlicingExhausted",
		},
		{
			name: "fit success: but core limit can't exceed 100",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         120,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "",
		},
		{
			name: "fit fail:  card exclusively",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         100,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 ExclusiveDeviceAllocateConflict",
		},
		{
			name: "fit fail:  CardComputeUnitsExhausted",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 100,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         0,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardComputeUnitsExhausted",
		},
		{
			name: "fit fail:  AllocatedCardsInsufficientRequest",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 10,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             2,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    1,
			wantDevIDs: []string{},
			wantReason: "1/1 AllocatedCardsInsufficientRequest",
		},
		{
			name: "fit success:  memory percentage",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      20,
				Count:     100,
				Usedmem:   0,
				Totalmem:  1280,
				Totalcore: 100,
				Usedcores: 10,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    true,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           0,
				MemPercentagereq: 10,
				Coresreq:         20,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    true,
			wantLen:    1,
			wantDevIDs: []string{"dev-0"},
			wantReason: "",
		},
		{
			name: "mutex policy rejects used device",
			devices: []*device.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      1,
					Count:     100,
					Usedmem:   0,
					Totalmem:  128,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      CambriconMLUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{"hami.io/gpu-scheduler-policy": "mutex"},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 ExclusiveDeviceAllocateConflict",
		},
		{
			name: "fit fail: partial allocation AllocatedCardsInsufficientRequest for multiple cards",
			devices: []*device.DeviceUsage{
				{
					ID:        "dev-0",
					Index:     0,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  1280,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      CambriconMLUDevice,
					Health:    true,
				},
				{
					ID:        "dev-1",
					Index:     1,
					Used:      0,
					Count:     100,
					Usedmem:   0,
					Totalmem:  1280,
					Totalcore: 100,
					Usedcores: 0,
					Numa:      0,
					Type:      CambriconMLUDevice,
					Health:    true,
				},
			},
			request: device.ContainerDeviceRequest{
				Nums:             3,
				Memreq:           512,
				MemPercentagereq: 0,
				Coresreq:         20,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    2,
			wantDevIDs: []string{},
			wantReason: "2/2 AllocatedCardsInsufficientRequest",
		},
		{
			name: "fit fail: CardNotHealth",
			devices: []*device.DeviceUsage{{
				ID:        "dev-0",
				Index:     0,
				Used:      0,
				Count:     100,
				Usedmem:   0,
				Totalmem:  128,
				Totalcore: 100,
				Usedcores: 0,
				Numa:      0,
				Type:      CambriconMLUDevice,
				Health:    false,
			}},
			request: device.ContainerDeviceRequest{
				Nums:             1,
				Memreq:           64,
				MemPercentagereq: 0,
				Coresreq:         50,
				Type:             CambriconMLUDevice,
			},
			annos:      map[string]string{},
			wantFit:    false,
			wantLen:    0,
			wantDevIDs: []string{},
			wantReason: "1/1 CardNotHealth",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocated := &device.PodDevices{}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: test.annos,
				},
			}
			fit, result, reason := dev.Fit(test.devices, test.request, pod, &device.NodeInfo{}, allocated)
			if fit != test.wantFit {
				t.Errorf("Fit: got %v, want %v", fit, test.wantFit)
			}
			if len(result[CambriconMLUDevice]) != test.wantLen {
				t.Errorf("expected len: %d, got len %d", test.wantLen, len(result[CambriconMLUDevice]))
			}
			if test.wantFit {
				for idx, id := range test.wantDevIDs {
					if id != result[CambriconMLUDevice][idx].UUID {
						t.Errorf("expected device id: %s, got device id %s", id, result[CambriconMLUDevice][idx].UUID)
					}
				}
			}

			if reason != test.wantReason {
				t.Errorf("expected reason: %s, got reason: %s", test.wantReason, reason)
			}
		})
	}
}

// TestDevices_Fit_ResourceQuotaWholeCardRequest reproduces
// https://github.com/Project-HAMi/HAMi/issues/2468: a pod that omits the
// memory resource (which resolves to a MemPercentagereq of 100, i.e. a whole
// card) carried a Memreq of 0, and Fit only checked device capacity, never
// the namespace ResourceQuota. That let a single pod consume an entire card's
// worth of quota while never being charged against it, so the ResourceQuota
// was left permanently unusable for every other pod in the namespace. This
// asserts Fit now denies the same request once it is resolved against the
// quota, matching how a request carrying an equivalent explicit Memreq
// already behaved.
func TestDevices_Fit_ResourceQuotaWholeCardRequest(t *testing.T) {
	config := CambriconConfig{
		ResourceCountName:  "cambricon.com/mlu",
		ResourceMemoryName: "cambricon.com/mlu.smlu.vmemory",
		ResourceCoreName:   "cambricon.com/mlu.smlu.vcore",
	}
	dev := InitMLUDevice(config)
	device.DevicesMap = map[string]device.Devices{CambriconMLUDevice: dev}

	ns := "cambricon-quota-test-ns"
	qm := device.NewQuotaManager()
	qm.AddQuota(&corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "mlu-quota", Namespace: ns},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceName("limits.cambricon.com/mlu.smlu.vmemory"): resource.MustParse("50"),
			},
		},
	})
	t.Cleanup(func() {
		qm.DelQuota(&corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{Name: "mlu-quota", Namespace: ns},
			Spec: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{
					corev1.ResourceName("limits.cambricon.com/mlu.smlu.vmemory"): resource.MustParse("50"),
				},
			},
		})
	})

	devices := []*device.DeviceUsage{{
		ID:        "dev-0",
		Index:     0,
		Used:      0,
		Count:     100,
		Usedmem:   0,
		Totalmem:  40960,
		Totalcore: 100,
		Usedcores: 0,
		Numa:      0,
		Type:      CambriconMLUDevice,
		Health:    true,
	}}
	// No Memreq set: this is what GenerateResourceRequests returns for a pod
	// that omits the memory field entirely, i.e. an implicit whole-card ask.
	request := device.ContainerDeviceRequest{
		Nums:             1,
		Memreq:           0,
		MemPercentagereq: 100,
		Coresreq:         10,
		Type:             CambriconMLUDevice,
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns}}

	fit, _, reason := dev.Fit(devices, request, pod, &device.NodeInfo{}, &device.PodDevices{})
	if fit {
		t.Errorf("expected whole-card request exceeding the namespace's ResourceQuota to be denied, but Fit admitted it (reason=%q)", reason)
	}
	if reason != "1/1 ResourceQuotaNotFit" {
		t.Errorf("expected reason %q, got %q", "1/1 ResourceQuotaNotFit", reason)
	}
}

func TestDevices_AddResourceUsage(t *testing.T) {
	tests := []struct {
		name        string
		deviceUsage *device.DeviceUsage
		ctr         *device.ContainerDevice
		wantErr     bool
		wantUsage   *device.DeviceUsage
	}{
		{
			name: "test add resource usage",
			deviceUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      0,
				Usedcores: 15,
				Usedmem:   2000,
			},
			ctr: &device.ContainerDevice{
				UUID:      "dev-0",
				Usedcores: 50,
				Usedmem:   1024,
			},
			wantUsage: &device.DeviceUsage{
				ID:        "dev-0",
				Used:      1,
				Usedcores: 65,
				Usedmem:   3024,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &CambriconDevices{}
			if err := dev.AddResourceUsage(&corev1.Pod{}, tt.deviceUsage, tt.ctr); (err != nil) != tt.wantErr {
				t.Errorf("AddResourceUsage() error=%v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if tt.deviceUsage.Usedcores != tt.wantUsage.Usedcores {
					t.Errorf("expected used cores: %d, got used cores %d", tt.wantUsage.Usedcores, tt.deviceUsage.Usedcores)
				}
				if tt.deviceUsage.Usedmem != tt.wantUsage.Usedmem {
					t.Errorf("expected used mem: %d, got used mem %d", tt.wantUsage.Usedmem, tt.deviceUsage.Usedmem)
				}
				if tt.deviceUsage.Used != tt.wantUsage.Used {
					t.Errorf("expected used: %d, got used %d", tt.wantUsage.Used, tt.deviceUsage.Used)
				}
			}
		})
	}
}
