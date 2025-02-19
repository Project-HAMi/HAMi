package lm

import (
	"testing"

	"github.com/stretchr/testify/require"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource"
	rt "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/resource/testing"
)

func TestMigCapabilityLabeler(t *testing.T) {
	testCases := []struct {
		description    string
		devices        []resource.Device
		expectedError  bool
		expectedLabels map[string]string
	}{
		{
			description: "no devices returns empty labels",
		},
		{
			description: "single non-mig capable device returns mig.capable as false",
			devices: []resource.Device{
				rt.NewFullGPU(),
			},
			expectedLabels: map[string]string{
				"nvidia.com/mig.capable": "false",
			},
		},
		{
			description: "multiple non-mig capable devices returns mig.capable as false",
			devices: []resource.Device{
				rt.NewFullGPU(),
				rt.NewFullGPU(),
			},
			expectedLabels: map[string]string{
				"nvidia.com/mig.capable": "false",
			},
		},
		{
			description: "single mig capable device returns mig.capable as true",
			devices: []resource.Device{
				rt.NewMigEnabledDevice(),
			},
			expectedLabels: map[string]string{
				"nvidia.com/mig.capable": "true",
			},
		},
		{
			description: "one mig capable device among multiple returns mig.capable as true",
			devices: []resource.Device{
				rt.NewFullGPU(),
				rt.NewMigEnabledDevice(),
			},
			expectedLabels: map[string]string{
				"nvidia.com/mig.capable": "true",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			nvmlMock := rt.NewManagerMockWithDevices(tc.devices...)

			migCapabilityLabeler, _ := newMigCapabilityLabeler(nvmlMock)

			labels, err := migCapabilityLabeler.Labels()
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.EqualValues(t, tc.expectedLabels, labels)
		})
	}
}

func TestSharingLabeler(t *testing.T) {
	testCases := []struct {
		description    string
		manager        resource.Manager
		config         *spec.Config
		expectedLabels map[string]string
		expectedError  error
	}{
		{
			description: "nil config",
			expectedLabels: map[string]string{
				"nvidia.com/mps.capable": "false",
			},
		},
		{
			description: "empty config",
			config:      &spec.Config{},
			expectedLabels: map[string]string{
				"nvidia.com/mps.capable": "false",
			},
		},
		{
			description: "config with timeslicing replicas",
			config: &spec.Config{
				Sharing: spec.Sharing{
					TimeSlicing: spec.ReplicatedResources{
						Resources: []spec.ReplicatedResource{
							{
								Replicas: 2,
							},
						},
					},
				},
			},
			expectedLabels: map[string]string{
				"nvidia.com/mps.capable": "false",
			},
		},
		{
			description: "config with no mps replicas",
			config: &spec.Config{
				Sharing: spec.Sharing{
					MPS: &spec.ReplicatedResources{
						Resources: []spec.ReplicatedResource{
							{
								Replicas: 1,
							},
						},
					},
				},
			},
			expectedLabels: map[string]string{
				"nvidia.com/mps.capable": "false",
			},
		},
		{
			description: "config with mps replicas no-mig-devices",
			manager: &resource.ManagerMock{
				GetDevicesFunc: func() ([]resource.Device, error) {
					devices := []resource.Device{
						&resource.DeviceMock{
							IsMigEnabledFunc: func() (bool, error) {
								return false, nil
							},
						},
					}
					return devices, nil
				},
			},
			config: &spec.Config{
				Sharing: spec.Sharing{
					MPS: &spec.ReplicatedResources{
						Resources: []spec.ReplicatedResource{
							{
								Replicas: 2,
							},
						},
					},
				},
			},
			expectedLabels: map[string]string{
				"nvidia.com/mps.capable": "true",
			},
		},
		{
			description: "config with mps replicas mig-devices",
			manager: &resource.ManagerMock{
				GetDevicesFunc: func() ([]resource.Device, error) {
					devices := []resource.Device{
						&resource.DeviceMock{
							IsMigEnabledFunc: func() (bool, error) {
								return true, nil
							},
						},
					}
					return devices, nil
				},
			},
			config: &spec.Config{
				Sharing: spec.Sharing{
					MPS: &spec.ReplicatedResources{
						Resources: []spec.ReplicatedResource{
							{
								Replicas: 2,
							},
						},
					},
				},
			},
			expectedError:  errMPSSharingNotSupported,
			expectedLabels: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			labels, err := newSharingLabeler(tc.manager, tc.config)
			require.ErrorIs(t, err, tc.expectedError)
			if tc.expectedError != nil {
				require.Nil(t, labels)
			} else {
				require.EqualValues(t, tc.expectedLabels, labels)
			}
		})
	}
}

func TestGPUModeLabeler(t *testing.T) {
	testCases := []struct {
		description    string
		devices        []resource.Device
		expectedError  bool
		expectedLabels map[string]string
	}{
		{
			description: "single device with compute PCI class",
			devices: []resource.Device{
				rt.NewDeviceWithPCIClassMock(0x030000),
			},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.mode": "graphics",
			},
		},
		{
			description: "single device with graphics PCI class",
			devices: []resource.Device{
				rt.NewDeviceWithPCIClassMock(0x030200),
			},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.mode": "compute",
			},
		},
		{
			description: "single device with switch PCI class",
			devices: []resource.Device{
				rt.NewDeviceWithPCIClassMock(0x068000),
			},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.mode": "unknown",
			},
		},
		{
			description: "multiple device have same graphics PCI class",
			devices: []resource.Device{
				rt.NewDeviceWithPCIClassMock(0x030200),
				rt.NewDeviceWithPCIClassMock(0x030200),
				rt.NewDeviceWithPCIClassMock(0x030200),
			},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.mode": "compute",
			},
		},
		{
			description: "multiple device have same compute PCI class",
			devices: []resource.Device{
				rt.NewDeviceWithPCIClassMock(0x030000),
				rt.NewDeviceWithPCIClassMock(0x030000),
				rt.NewDeviceWithPCIClassMock(0x030000),
			},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.mode": "graphics",
			},
		},
		{
			description: "multiple device with some with graphics and others with compute PCI class",
			devices: []resource.Device{
				rt.NewDeviceWithPCIClassMock(0x030000),
				rt.NewDeviceWithPCIClassMock(0x030200),
				rt.NewDeviceWithPCIClassMock(0x030000),
			},
			expectedLabels: map[string]string{
				"nvidia.com/gpu.mode": "unknown",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {

			gpuModeLabeler, _ := newGPUModeLabeler(tc.devices)

			labels, err := gpuModeLabeler.Labels()
			if tc.expectedError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.EqualValues(t, tc.expectedLabels, labels)
		})
	}
}
