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
	"sync"

<<<<<<< HEAD
	corev1 "k8s.io/api/core/v1"
=======
	"4pd.io/k8s-vgpu/pkg/util"
>>>>>>> 21785f7 (update to v2.3.2)
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
)

<<<<<<< HEAD
=======
type DeviceInfo struct {
	ID      string
	Index   uint
	Count   int32
	Devmem  int32
	Devcore int32
	Type    string
	Health  bool
}

type NodeInfo struct {
	ID      string
	Devices []DeviceInfo
}

type DeviceUsageList []*util.DeviceUsage

>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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
