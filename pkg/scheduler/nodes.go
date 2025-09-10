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
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"

	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/enflame"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/iluvatar"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

var vendorNoUseAnnoKeyMap = map[string][]string{
	nvidia.GPUNoUseUUID:        {nvidia.NvidiaGPUDevice},
	cambricon.MLUNoUseUUID:     {cambricon.CambriconMLUDevice},
	hygon.DCUNoUseUUID:         {hygon.HygonDCUDevice},
	iluvatar.IluvatarNoUseUUID: {iluvatar.IluvatarGPUDevice},
	enflame.EnflameNoUseUUID:   {enflame.EnflameGPUDevice},
	mthreads.MthreadsNoUseUUID: {mthreads.MthreadsGPUDevice},
	metax.MetaxNoUseUUID:       {metax.MetaxGPUDevice, metax.MetaxSGPUDevice},
}

type NodeUsage struct {
	Node    *corev1.Node
	Devices policy.DeviceUsageList
}

type nodeManager struct {
	nodes map[string]*device.NodeInfo
	mutex sync.RWMutex
}

func newNodeManager() *nodeManager {
	return &nodeManager{
		nodes: make(map[string]*device.NodeInfo),
	}
}

func (m *nodeManager) addNode(nodeID string, nodeInfo *device.NodeInfo) {
	if nodeInfo == nil || len(nodeInfo.Devices) == 0 {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	_, ok := m.nodes[nodeID]
	if ok {
		if len(nodeInfo.Devices) > 0 {
			tmp := make([]device.DeviceInfo, 0, len(nodeInfo.Devices))
			devices := device.GetDevices()
			deviceType := ""
			for _, val := range devices {
				if strings.Contains(nodeInfo.Devices[0].Type, val.CommonWord()) {
					deviceType = val.CommonWord()
				}
			}
			for _, val := range m.nodes[nodeID].Devices {
				if !strings.Contains(val.Type, deviceType) {
					tmp = append(tmp, val)
				}
			}
			m.nodes[nodeID].Devices = tmp
			m.nodes[nodeID].Devices = append(m.nodes[nodeID].Devices, nodeInfo.Devices...)
		}
		m.nodes[nodeID].Node = nodeInfo.Node
	} else {
		m.nodes[nodeID] = nodeInfo
	}
	m.nodes[nodeID].Devices = rmDeviceByNodeAnnotation(m.nodes[nodeID])
}

func rmDeviceByNodeAnnotation(nodeInfo *device.NodeInfo) []device.DeviceInfo {
	vendorWithDisableGPUUUIDMap := make(map[string]map[string]bool)
	if nodeInfo.Node != nil && nodeInfo.Node.Annotations != nil {
		for annoKey, vendors := range vendorNoUseAnnoKeyMap {
			klog.V(5).Infof("Current annokey is %s, and vendor is %v", annoKey, vendors)
			if value, ok := nodeInfo.Node.Annotations[annoKey]; ok {
				disableGPUUUIDList := strings.Split(value, ",")
				klog.V(5).Infof("Disable gpu uuid list is: %v", disableGPUUUIDList)
				for _, disableGPUUUID := range disableGPUUUIDList {
					if id := strings.TrimSpace(disableGPUUUID); id != "" {
						for _, vendor := range vendors {
							if vendorWithDisableGPUUUIDMap[vendor] == nil {
								newVendorMap := make(map[string]bool)
								newVendorMap[disableGPUUUID] = true
								vendorWithDisableGPUUUIDMap[vendor] = newVendorMap
							} else {
								vendorWithDisableGPUUUIDMap[vendor][disableGPUUUID] = true
							}
						}
					}
				}
			}
		}
	}
	if len(vendorWithDisableGPUUUIDMap) == 0 {
		return nodeInfo.Devices
	}
	tmp := make([]device.DeviceInfo, 0, len(nodeInfo.Devices))
	for _, d := range nodeInfo.Devices {
		removeFlag := false
		if disableGPUUUIDMap, ok := vendorWithDisableGPUUUIDMap[d.DeviceVendor]; ok {
			if ok := disableGPUUUIDMap[d.ID]; ok {
				klog.V(5).Infof("Disable gpu uuid is : %s", d.ID)
				removeFlag = true
			}
		}
		if !removeFlag {
			tmp = append(tmp, d)
		}
	}
	return tmp
}

func (m *nodeManager) rmNodeDevices(nodeID string, deviceVendor string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	nodeInfo := m.nodes[nodeID]
	if nodeInfo == nil {
		return
	}

	devices := make([]device.DeviceInfo, 0)
	for _, val := range nodeInfo.Devices {
		if val.DeviceVendor != deviceVendor {
			devices = append(devices, val)
		}
	}

	if len(devices) == 0 {
		delete(m.nodes, nodeID)
	} else {
		nodeInfo.Devices = devices
	}
	klog.InfoS("Removing device from node", "nodeName", nodeID, "deviceVendor", deviceVendor, "remainingDevices", devices)
}

func (m *nodeManager) GetNode(nodeID string) (*device.NodeInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if n, ok := m.nodes[nodeID]; ok {
		return n, nil
	}
	return &device.NodeInfo{}, fmt.Errorf("node %v not found", nodeID)
}

func (m *nodeManager) ListNodes() (map[string]*device.NodeInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.nodes, nil
}
