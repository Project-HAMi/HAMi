/*
*
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
*
*/

package nvidia

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/NVIDIA/go-nvlib/pkg/nvlib/device"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// P2PLinkType defines the link information between two devices.
type P2PLinkType uint

// The following constants define the nature of a link between two devices.
// These include peer-2-peer and NVLink information.
const (
	P2PLinkUnknown P2PLinkType = iota
	P2PLinkCrossCPU
	P2PLinkSameCPU
	P2PLinkHostBridge
	P2PLinkMultiSwitch
	P2PLinkSingleSwitch
	P2PLinkSameBoard
	SingleNVLINKLink
	TwoNVLINKLinks
	ThreeNVLINKLinks
	FourNVLINKLinks
	FiveNVLINKLinks
	SixNVLINKLinks
	SevenNVLINKLinks
	EightNVLINKLinks
	NineNVLINKLinks
	TenNVLINKLinks
	ElevenNVLINKLinks
	TwelveNVLINKLinks
	ThirteenNVLINKLinks
	FourteenNVLINKLinks
	FifteenNVLINKLinks
	SixteenNVLINKLinks
	SeventeenNVLINKLinks
	EighteenNVLINKLinks
)

// String returns the string representation of the P2PLink type.
func (l P2PLinkType) String() string {
	switch l {
	case P2PLinkCrossCPU:
		return "P2PLinkCrossCPU"
	case P2PLinkSameCPU:
		return "P2PLinkSameCPU"
	case P2PLinkHostBridge:
		return "P2PLinkHostBridge"
	case P2PLinkMultiSwitch:
		return "P2PLinkMultiSwitch"
	case P2PLinkSingleSwitch:
		return "P2PLinkSingleSwitch"
	case P2PLinkSameBoard:
		return "P2PLinkSameBoard"
	case SingleNVLINKLink:
		return "SingleNVLINKLink"
	case TwoNVLINKLinks:
		return "TwoNVLINKLinks"
	case ThreeNVLINKLinks:
		return "ThreeNVLINKLinks"
	case FourNVLINKLinks:
		return "FourNVLINKLinks"
	case FiveNVLINKLinks:
		return "FiveNVLINKLinks"
	case SixNVLINKLinks:
		return "SixNVLINKLinks"
	case SevenNVLINKLinks:
		return "SevenNVLINKLinks"
	case EightNVLINKLinks:
		return "EightNVLINKLinks"
	case NineNVLINKLinks:
		return "NineNVLINKLinks"
	case TenNVLINKLinks:
		return "TenNVLINKLinks"
	case ElevenNVLINKLinks:
		return "ElevenNVLINKLinks"
	case TwelveNVLINKLinks:
		return "TwelveNVLINKLinks"
	case ThirteenNVLINKLinks:
		return "ThirteenNVLINKLinks"
	case FourteenNVLINKLinks:
		return "FourteenNVLINKLinks"
	case FifteenNVLINKLinks:
		return "FifteenNVLINKLinks"
	case SixteenNVLINKLinks:
		return "SixteenNVLINKLinks"
	case SeventeenNVLINKLinks:
		return "SeventeenNVLINKLinks"
	case EighteenNVLINKLinks:
		return "EighteenNVLINKLinks"
	default:
		return fmt.Sprintf("UNKNOWN (%v)", uint(l))
	}
}

// GetP2PLink gets the peer-to-peer connectivity between two devices.
func GetP2PLink(dev1 device.Device, dev2 device.Device) (P2PLinkType, error) {
	level, ret := dev1.GetTopologyCommonAncestor(dev2)
	if !errors.Is(ret, nvml.SUCCESS) {
		return P2PLinkUnknown, fmt.Errorf("failed to get commmon anscestor: %v", ret)
	}

	switch level {
	case nvml.TOPOLOGY_INTERNAL:
		return P2PLinkSameBoard, nil
	case nvml.TOPOLOGY_SINGLE:
		return P2PLinkSingleSwitch, nil
	case nvml.TOPOLOGY_MULTIPLE:
		return P2PLinkMultiSwitch, nil
	case nvml.TOPOLOGY_HOSTBRIDGE:
		return P2PLinkHostBridge, nil
	case nvml.TOPOLOGY_NODE: // NVML_TOPOLOGY_CPU was renamed NVML_TOPOLOGY_NODE
		return P2PLinkSameCPU, nil
	case nvml.TOPOLOGY_SYSTEM:
		return P2PLinkCrossCPU, nil

	}

	return P2PLinkUnknown, fmt.Errorf("unknown topology level: %v", level)
}

// GetNVLink gets the number of NVLinks between the specified devices.
func GetNVLink(dev1 device.Device, dev2 device.Device) (P2PLinkType, error) {
	pciInfos, err := getAllNvLinkRemotePciInfo(dev1)
	if err != nil {
		return P2PLinkUnknown, fmt.Errorf("failed to get nvlink remote pci info: %v", err)
	}

	dev2PciInfo, ret := dev2.GetPciInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		return P2PLinkUnknown, fmt.Errorf("failed to get pci info: %v", ret)
	}
	dev2BusID := PciInfo(dev2PciInfo).BusID()

	nvlink := P2PLinkUnknown
	for _, pciInfo := range pciInfos {
		if pciInfo.BusID() != dev2BusID {
			continue
		}
		switch nvlink {
		case P2PLinkUnknown:
			nvlink = SingleNVLINKLink
		case SingleNVLINKLink:
			nvlink = TwoNVLINKLinks
		case TwoNVLINKLinks:
			nvlink = ThreeNVLINKLinks
		case ThreeNVLINKLinks:
			nvlink = FourNVLINKLinks
		case FourNVLINKLinks:
			nvlink = FiveNVLINKLinks
		case FiveNVLINKLinks:
			nvlink = SixNVLINKLinks
		case SixNVLINKLinks:
			nvlink = SevenNVLINKLinks
		case SevenNVLINKLinks:
			nvlink = EightNVLINKLinks
		case EightNVLINKLinks:
			nvlink = NineNVLINKLinks
		case NineNVLINKLinks:
			nvlink = TenNVLINKLinks
		case TenNVLINKLinks:
			nvlink = ElevenNVLINKLinks
		case ElevenNVLINKLinks:
			nvlink = TwelveNVLINKLinks
		case TwelveNVLINKLinks:
			nvlink = ThirteenNVLINKLinks
		case ThirteenNVLINKLinks:
			nvlink = FourteenNVLINKLinks
		case FourteenNVLINKLinks:
			nvlink = FifteenNVLINKLinks
		case FifteenNVLINKLinks:
			nvlink = SixteenNVLINKLinks
		case SixteenNVLINKLinks:
			nvlink = SeventeenNVLINKLinks
		case SeventeenNVLINKLinks:
			nvlink = EighteenNVLINKLinks
		}
	}
	// TODO(klueska): Handle NVSwitch semantics

	return nvlink, nil
}

// getAllNvLinkRemotePciInfo returns the PCI info for all devices attached to the specified device by an NVLink.
func getAllNvLinkRemotePciInfo(dev device.Device) ([]PciInfo, error) {
	var pciInfos []PciInfo
	for i := 0; i < nvml.NVLINK_MAX_LINKS; i++ {
		state, ret := dev.GetNvLinkState(i)
		if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) || errors.Is(ret, nvml.ERROR_INVALID_ARGUMENT) {
			continue
		}
		if !errors.Is(ret, nvml.SUCCESS) {
			return nil, fmt.Errorf("failed to get nvlink state: %v", ret)
		}
		if state != nvml.FEATURE_ENABLED {
			continue
		}
		pciInfo, ret := dev.GetNvLinkRemotePciInfo(i)
		if errors.Is(ret, nvml.ERROR_NOT_SUPPORTED) || errors.Is(ret, nvml.ERROR_INVALID_ARGUMENT) {
			continue
		}
		if !errors.Is(ret, nvml.SUCCESS) {
			return nil, fmt.Errorf("failed to get remote pci info: %v", ret)
		}
		pciInfos = append(pciInfos, PciInfo(pciInfo))
	}

	return pciInfos, nil
}

// PciInfo is a type alias to nvml.PciInfo to allow for functions to be defined on the type.
type PciInfo nvml.PciInfo

// BusID provides a utility function that returns the string representation of the bus ID.
// Note that the []int8 slice member is named BusId.
func (p PciInfo) BusID() string {
	var bytes []byte
	for _, b := range p.BusId {
		if byte(b) == '\x00' {
			break
		}
		bytes = append(bytes, byte(b))
	}
	id := strings.ToLower(string(bytes))

	if id != "0000" {
		id = strings.TrimPrefix(id, "0000")
	}
	return id
}

// CPUAffinity returns the CPU affinity associated with a specified PCI device.
// If NUMA information is not available, this returns nil.
func (p PciInfo) CPUAffinity() *uint {
	node := p.NumaNode()
	if node < 0 {
		return nil
	}
	affinity := uint(node)
	return &affinity
}

// NumaNode returns the numa node associates with a PCI device.
// If numa is unsupported, -1 is returned.
func (p PciInfo) NumaNode() int64 {
	// Read the numa_node file associated with the PCI Device Info
	b, err := os.ReadFile(fmt.Sprintf("/sys/bus/pci/devices/%s/numa_node", p.BusID()))
	if err != nil {
		return -1
	}
	node, err := strconv.ParseInt(string(bytes.TrimSpace(b)), 10, 64)
	if err != nil {
		return -1
	}
	return node
}
