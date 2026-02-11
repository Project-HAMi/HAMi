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
	"github.com/Project-HAMi/HAMi/pkg/device/ascend"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/device/hygon"
	"github.com/Project-HAMi/HAMi/pkg/device/kunlun"
	"github.com/Project-HAMi/HAMi/pkg/device/metax"
	"github.com/Project-HAMi/HAMi/pkg/device/mthreads"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
)

var vendorNoUseAnnoKeyMap = map[string][]string{
	nvidia.GPUNoUseUUID:        {nvidia.NvidiaGPUDevice},
	cambricon.MLUNoUseUUID:     {cambricon.CambriconMLUDevice},
	hygon.DCUNoUseUUID:         {hygon.HygonDCUDevice},
	mthreads.MthreadsNoUseUUID: {mthreads.MthreadsGPUDevice},
	metax.MetaxNoUseUUID:       {metax.MetaxGPUDevice, metax.MetaxSGPUDevice},
	kunlun.KunlunNoUseUUID:     {kunlun.KunlunGPUDevice},
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
			for vendor := range nodeInfo.Devices {
				m.nodes[nodeID].Devices[vendor] = nodeInfo.Devices[vendor]
			}
		}
		m.nodes[nodeID].Node = nodeInfo.Node
	} else {
		m.nodes[nodeID] = nodeInfo
	}
	m.nodes[nodeID].Devices = rmDeviceByNodeAnnotation(m.nodes[nodeID])
}

func rmDeviceByNodeAnnotation(nodeInfo *device.NodeInfo) map[string][]device.DeviceInfo {
	if nodeInfo == nil {
		return nil
	}
	vendorWithDisableGPUUUIDMap := make(map[string]map[string]bool)
	if nodeInfo.Node != nil && nodeInfo.Node.Annotations != nil {
		// Process known vendor annotations
		for annoKey, vendors := range vendorNoUseAnnoKeyMap {
			klog.V(5).Infof("Current annokey is %s, and vendor is %v", annoKey, vendors)
			if value, ok := nodeInfo.Node.Annotations[annoKey]; ok {
				disableGPUUUIDList := strings.Split(value, ",")
				klog.V(5).Infof("Disable gpu uuid list is: %v", disableGPUUUIDList)
				for _, disableGPUUUID := range disableGPUUUIDList {
					if id := strings.TrimSpace(disableGPUUUID); id != "" {
						for _, vendor := range vendors {
							if vendorWithDisableGPUUUIDMap[vendor] == nil {
								vendorWithDisableGPUUUIDMap[vendor] = make(map[string]bool)
							}
							vendorWithDisableGPUUUIDMap[vendor][id] = true
						}
					}
				}
			}
		}
		// Process Ascend device annotations dynamically
		// Ascend devices use format: hami.io/no-use-{CommonWord}-uuid
		for annoKey, value := range nodeInfo.Node.Annotations {
			if strings.HasPrefix(annoKey, ascend.AscendNoUseUUIDPrefix) && strings.HasSuffix(annoKey, ascend.AscendNoUseUUIDSuffix) {
				klog.V(5).Infof("Processing Ascend annotation: %s", annoKey)
				disableGPUUUIDList := strings.Split(value, ",")
				klog.V(5).Infof("Disable Ascend device uuid list is: %v", disableGPUUUIDList)
				// Extract the device type from the annotation key
				// Format: hami.io/no-use-{DeviceType}-uuid
				deviceType := strings.TrimPrefix(annoKey, ascend.AscendNoUseUUIDPrefix)
				deviceType = strings.TrimSuffix(deviceType, ascend.AscendNoUseUUIDSuffix)
				for _, disableGPUUUID := range disableGPUUUIDList {
					if id := strings.TrimSpace(disableGPUUUID); id != "" {
						if vendorWithDisableGPUUUIDMap[deviceType] == nil {
							vendorWithDisableGPUUUIDMap[deviceType] = make(map[string]bool)
						}
						vendorWithDisableGPUUUIDMap[deviceType][id] = true
					}
				}
			}
		}
	}
	if len(vendorWithDisableGPUUUIDMap) == 0 {
		return nodeInfo.Devices
	}
	newDeviceMap := make(map[string][]device.DeviceInfo)
	for deviceName, deviceList := range nodeInfo.Devices {
		newDeviceList := make([]device.DeviceInfo, 0, len(deviceList))
		for _, d := range deviceList {
			if disableGPUUUIDMap, ok := vendorWithDisableGPUUUIDMap[d.DeviceVendor]; ok {
				if disabled := disableGPUUUIDMap[d.ID]; disabled {
					klog.V(5).Infof("Disable gpu uuid is : %s", d.ID)
					continue
				}
			}
			newDeviceList = append(newDeviceList, d)
		}
		newDeviceMap[deviceName] = newDeviceList
	}
	return newDeviceMap
}

func (m *nodeManager) rmNodeDevices(nodeID string, deviceVendor string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	nodeInfo := m.nodes[nodeID]
	if nodeInfo == nil {
		return
	}
	delete(m.nodes[nodeID].Devices, deviceVendor)
	if len(m.nodes[nodeID].Devices) == 0 {
		delete(m.nodes, nodeID)
	}
	klog.InfoS("Removing device from node", "nodeName", nodeID, "deviceVendor", deviceVendor)
}

func (m *nodeManager) rmNode(nodeID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.nodes[nodeID]; ok {
		delete(m.nodes, nodeID)
		klog.InfoS("Removing node from nodeManager", "nodeName", nodeID)
	}
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
	nodesCopy := make(map[string]*device.NodeInfo, len(m.nodes))
	for nodeID, nodeInfo := range m.nodes {
		if nodeInfo == nil || nodeInfo.Node == nil {
			klog.Warningf("ListNodes nodes copy step skip node(%s) because of nil NodeInfo or NodeInfo.Node", nodeID)
			continue
		}
		nodeInfoCopy := &device.NodeInfo{
			ID:      nodeInfo.ID,
			Node:    nodeInfo.Node.DeepCopy(),
			Devices: make(map[string][]device.DeviceInfo),
		}
		for k, v := range nodeInfo.Devices {
			nodeInfoCopy.Devices[k] = make([]device.DeviceInfo, len(v))
			copy(nodeInfoCopy.Devices[k], v)
		}
		nodesCopy[nodeID] = nodeInfoCopy
	}
	return nodesCopy, nil
}
