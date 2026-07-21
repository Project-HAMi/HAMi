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

	safecast "github.com/ccoveille/go-safecast/v2"
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
		return P2PLinkUnknown, fmt.Errorf("failed to get common ancestor: %v", ret)
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
	// Direct GPU <-> GPU: match remote PCI bus IDs.
	pciInfos, err := getAllNvLinkRemotePciInfo(dev1)
	if err != nil {
		return P2PLinkUnknown, fmt.Errorf("failed to get nvlink remote pci info: %v", err)
	}

	dev2PciInfo, ret := dev2.GetPciInfo()
	if !errors.Is(ret, nvml.SUCCESS) {
		return P2PLinkUnknown, fmt.Errorf("failed to get pci info: %v", ret)
	}
	dev2BusID := PciInfo(dev2PciInfo).BusID()

	direct := nvlinkCountToType(countMatchingLinks(pciInfos, dev2BusID))
	if direct != P2PLinkUnknown {
		return direct, nil
	}

	// NVSwitch: remote PCI is the switch, not the peer GPU.
	// Both GPUs must connect through NVSwitch; the link count is the
	// minimum active count across the pair.
	links1, viaSwitch1 := countNvSwitchLinks(dev1)
	links2, viaSwitch2 := countNvSwitchLinks(dev2)
	if viaSwitch1 && viaSwitch2 {
		n := min(links1, links2)
		return nvlinkCountToType(n), nil
	}

	return P2PLinkUnknown, nil
}

// getAllNvLinkRemotePciInfo returns the PCI info for all devices attached to the specified device by an NVLink.
func getAllNvLinkRemotePciInfo(dev device.Device) ([]PciInfo, error) {
	var pciInfos []PciInfo
	for i := range nvml.NVLINK_MAX_LINKS {
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

// countMatchingLinks counts how many remote PCI entries match busID.
func countMatchingLinks(pciInfos []PciInfo, busID string) int {
	n := 0
	for _, pci := range pciInfos {
		if pci.BusID() == busID {
			n++
		}
	}
	return n
}

// countNvSwitchLinks returns the count of enabled NVLinks whose remote is
// an NVSwitch, and whether the device has any such links at all.
func countNvSwitchLinks(dev device.Device) (count int, viaSwitch bool) {
	for i := range nvml.NVLINK_MAX_LINKS {
		state, ret := dev.GetNvLinkState(i)
		if !errors.Is(ret, nvml.SUCCESS) || state != nvml.FEATURE_ENABLED {
			continue
		}
		deviceType, ret := dev.GetNvLinkRemoteDeviceType(i)
		if errors.Is(ret, nvml.SUCCESS) && deviceType == nvml.NVLINK_DEVICE_TYPE_SWITCH {
			count++
		}
	}
	return count, count > 0
}

// nvlinkCountToType converts a count to the corresponding P2PLinkType.
func nvlinkCountToType(n int) P2PLinkType {
	// ponytail: linear scan, array would be cleaner but it's 18 items.
	types := []P2PLinkType{
		P2PLinkUnknown,
		SingleNVLINKLink, TwoNVLINKLinks, ThreeNVLINKLinks,
		FourNVLINKLinks, FiveNVLINKLinks, SixNVLINKLinks,
		SevenNVLINKLinks, EightNVLINKLinks, NineNVLINKLinks,
		TenNVLINKLinks, ElevenNVLINKLinks, TwelveNVLINKLinks,
		ThirteenNVLINKLinks, FourteenNVLINKLinks, FifteenNVLINKLinks,
		SixteenNVLINKLinks, SeventeenNVLINKLinks, EighteenNVLINKLinks,
	}
	if n < 1 || n >= len(types) {
		return P2PLinkUnknown
	}
	return types[n]
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
	affinity, err := safecast.Convert[uint](node)
	if err != nil {
		return nil
	}
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
