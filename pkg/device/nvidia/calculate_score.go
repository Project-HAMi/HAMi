/**
# Copyright 2024 NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package nvidia

import (
	"errors"
	"fmt"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// Device represents a GPU device as reported by NVML, including all of its
// Point-to-Point link information.
type Device struct {
	nvlibDevice
	Index int
	Links map[int][]P2PLink
}

// DeviceList stores an ordered list of devices.
type DeviceList []*Device

// Filter filters out the selected devices from the list.
// Note that the specified uuids must exist in the list of devices.
func (d DeviceList) Filter(uuids []string) (DeviceList, error) {
	var filtered DeviceList
	for _, uuid := range uuids {
		for _, device := range d {
			if device.UUID == uuid {
				filtered = append(filtered, device)
				break
			}
		}
		if len(filtered) == 0 || filtered[len(filtered)-1].UUID != uuid {
			return nil, fmt.Errorf("no device with uuid: %v", uuid)
		}
	}

	return filtered, nil
}

// P2PLink represents a Point-to-Point link between two GPU devices. The link
// is between the Device struct this struct is embedded in and the GPU Device
// contained in the P2PLink struct itself.
type P2PLink struct {
	GPU  *Device
	Type P2PLinkType
}

type nvlibDevice struct {
	device.Device
	// The previous binding implementation used to cache specific device properties.
	// These should be considered deprecated and the functions associated with device.Device
	// should be used instead.
	UUID string
	PCI  struct {
		BusID string
	}
	CPUAffinity *uint
}

// deviceListBuilder stores the options required to build a list of linked devices.
type deviceListBuilder struct {
	nvmllib   nvml.Interface
	devicelib device.Interface
}

// NewDevices creates a list of Devices from all available nvml.Devices using the specified options.
func NewDevices() (DeviceList, error) {
	o := &deviceListBuilder{}
	o.nvmllib = nvml.New()
	o.devicelib = device.New(o.nvmllib)
	return o.build()
}

// build uses the configured options to build a DeviceList.
func (o *deviceListBuilder) build() (DeviceList, error) {
	if err := o.nvmllib.Init(); !errors.Is(err, nvml.SUCCESS) {
		return nil, fmt.Errorf("error calling nvml.Init: %v", err)
	}
	defer func() {
		_ = o.nvmllib.Shutdown()
	}()

	nvmlDevices, err := o.devicelib.GetDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %v", err)
	}

	var devices DeviceList
	for i, d := range nvmlDevices {
		device, err := newDevice(i, d)
		if err != nil {
			return nil, fmt.Errorf("failed to construct linked device: %v", err)
		}
		devices = append(devices, device)
	}

	for i, d1 := range nvmlDevices {
		for j, d2 := range nvmlDevices {
			if i != j {
				p2plink, err := GetP2PLink(d1, d2)
				if err != nil {
					return nil, fmt.Errorf("error getting P2PLink for devices (%v, %v): %v", i, j, err)
				}
				if p2plink != P2PLinkUnknown {
					devices[i].Links[j] = append(devices[i].Links[j], P2PLink{devices[j], p2plink})
				}

				nvlink, err := GetNVLink(d1, d2)
				if err != nil {
					return nil, fmt.Errorf("error getting NVLink for devices (%v, %v): %v", i, j, err)
				}
				if nvlink != P2PLinkUnknown {
					devices[i].Links[j] = append(devices[i].Links[j], P2PLink{devices[j], nvlink})
				}
			}
		}
	}

	return devices, nil
}

// newDevice constructs a Device for the specified index and nvml Device.
func newDevice(i int, d device.Device) (*Device, error) {
	uuid, ret := d.GetUUID()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get device uuid: %v", ret)
	}
	pciInfo, ret := d.GetPciInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		return nil, fmt.Errorf("failed to get device pci info: %v", ret)
	}

	device := Device{
		nvlibDevice: nvlibDevice{
			Device:      d,
			UUID:        uuid,
			PCI:         struct{ BusID string }{BusID: PciInfo(pciInfo).BusID()},
			CPUAffinity: PciInfo(pciInfo).CPUAffinity(),
		},
		Index: i,
		Links: make(map[int][]P2PLink),
	}

	return &device, nil
}

type ListDeviceScore []DeviceScore

type DeviceScore struct {
	UUID string `json:"uuid"`
	// Score is record and other gpu communications score,
	// the value bigger communications bandwidth the higher.
	Score map[string]int `json:"score"`
}

func CalculateGPUScore(available []string) (ListDeviceScore, error) {
	linkedDevices, err := NewDevices()
	if err != nil {
		return nil, err
	}
	requiredDevices, err := linkedDevices.Filter(available)
	if err != nil {
		return nil, err
	}
	score := calculateGPUScore(requiredDevices)
	return score, nil
}

func calculateGPUScore(devices []*Device) ListDeviceScore {
	ds := make(ListDeviceScore, len(devices))
	for indexI, gpuI := range devices {
		score := make(map[string]int)
		for indexJ, gpuJ := range devices {
			if indexI == indexJ {
				continue
			}
			score[gpuJ.UUID] = calculateGPUPairScore(gpuI, gpuJ)
		}
		ds[indexI] = DeviceScore{
			Score: score,
			UUID:  gpuI.UUID,
		}
	}
	return ds
}

func calculateGPUPairScore(gpu0 *Device, gpu1 *Device) int {
	if gpu0 == nil || gpu1 == nil {
		return 0
	}

	if gpu0 == gpu1 {
		return 0
	}

	if len(gpu0.Links[gpu1.Index]) != len(gpu1.Links[gpu0.Index]) {
		err := fmt.Errorf("internal error in bestEffort GPU allocator: all P2PLinks between 2 GPUs should be bidirectional")
		panic(err)
	}

	score := 0

	for _, link := range gpu0.Links[gpu1.Index] {
		switch link.Type {
		case P2PLinkCrossCPU:
			score += 10
		case P2PLinkSameCPU:
			score += 20
		case P2PLinkHostBridge:
			score += 30
		case P2PLinkMultiSwitch:
			score += 40
		case P2PLinkSingleSwitch:
			score += 50
		case P2PLinkSameBoard:
			score += 60
		case SingleNVLINKLink:
			score += 100
		case TwoNVLINKLinks:
			score += 200
		case ThreeNVLINKLinks:
			score += 300
		case FourNVLINKLinks:
			score += 400
		case FiveNVLINKLinks:
			score += 500
		case SixNVLINKLinks:
			score += 600
		case SevenNVLINKLinks:
			score += 700
		case EightNVLINKLinks:
			score += 800
		case NineNVLINKLinks:
			score += 900
		case TenNVLINKLinks:
			score += 1000
		case ElevenNVLINKLinks:
			score += 1100
		case TwelveNVLINKLinks:
			score += 1200
		case ThirteenNVLINKLinks:
			score += 1300
		case FourteenNVLINKLinks:
			score += 1400
		case FifteenNVLINKLinks:
			score += 1500
		case SixteenNVLINKLinks:
			score += 1600
		case SeventeenNVLINKLinks:
			score += 1700
		case EighteenNVLINKLinks:
			score += 1800
		}
	}

	return score
}
