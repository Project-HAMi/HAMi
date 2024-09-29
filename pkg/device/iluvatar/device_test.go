package iluvatar

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Project-HAMi/HAMi/pkg/api"
)

func TestGetNodeDevices(t *testing.T) {
	IluvatarResourceCores = "iluvatar.ai/MR-V100.vCore"
	IluvatarResourceMemory = "iluvatar.ai/MR-V100.vMem"

	tests := []struct {
		name     string
		node     corev1.Node
		expected []*api.DeviceInfo
		err      error
	}{
		{
			name: "Test with valid node",
			node: corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName(IluvatarResourceCores):  *resource.NewQuantity(100, resource.DecimalSI),
						corev1.ResourceName(IluvatarResourceMemory): *resource.NewQuantity(128, resource.DecimalSI),
					},
				},
			},
			expected: []*api.DeviceInfo{
				{
					Index:   0,
					ID:      "test-iluvatar-0",
					Count:   100,
					Devmem:  32768,
					Devcore: 100,
					Type:    IluvatarGPUDevice,
					Numa:    0,
					Health:  true,
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dev := &IluvatarDevices{}
			got, err := dev.GetNodeDevices(tt.node)
			if (err != nil) != (tt.err != nil) {
				t.Errorf("GetNodeDevices() error = %v, expected %v", err, tt.err)
				return
			}

			if len(got) != len(tt.expected) {
				t.Errorf("GetNodeDevices() got %d devices, expected %d", len(got), len(tt.expected))
				return
			}

			for i, device := range got {
				if device.Index != tt.expected[i].Index {
					t.Errorf("Expected index %d, got %d", tt.expected[i].Index, device.Index)
				}
				if device.ID != tt.expected[i].ID {
					t.Errorf("Expected id %s, got %s", tt.expected[i].ID, device.ID)
				}
				if device.Devcore != tt.expected[i].Devcore {
					t.Errorf("Expected devcore %d, got %d", tt.expected[i].Devcore, device.Devcore)
				}
				if device.Devmem != tt.expected[i].Devmem {
					t.Errorf("Expected cevmem %d, got %d", tt.expected[i].Devmem, device.Devmem)
				}
			}
		})
	}
}
