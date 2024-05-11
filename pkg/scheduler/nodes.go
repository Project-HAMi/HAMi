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

	"github.com/Project-HAMi/HAMi/pkg/scheduler/policy"
	"github.com/Project-HAMi/HAMi/pkg/util"

	"k8s.io/klog/v2"
)

type NodeUsage struct {
	Devices policy.DeviceUsageList
}

type nodeManager struct {
	nodes map[string]*util.NodeInfo
	mutex sync.RWMutex
}

func (m *nodeManager) init() {
	m.nodes = make(map[string]*util.NodeInfo)
}

func (m *nodeManager) addNode(nodeID string, nodeInfo *util.NodeInfo) {
	if nodeInfo == nil || len(nodeInfo.Devices) == 0 {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	_, ok := m.nodes[nodeID]
	if ok {
		tmp := make([]util.DeviceInfo, 0, len(m.nodes[nodeID].Devices)+len(nodeInfo.Devices))
		tmp = append(tmp, m.nodes[nodeID].Devices...)
		tmp = append(tmp, nodeInfo.Devices...)
		m.nodes[nodeID].Devices = tmp
	} else {
		m.nodes[nodeID] = nodeInfo
	}
}

func (m *nodeManager) rmNodeDevice(nodeID string, nodeInfo *util.NodeInfo) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	_, ok := m.nodes[nodeID]
	if ok {
		if len(m.nodes[nodeID].Devices) == 0 {
			delete(m.nodes, nodeID)
			return
		}
		klog.V(5).Infoln("before rm:", m.nodes[nodeID].Devices, "needs remove", nodeInfo.Devices)
		tmp := make([]util.DeviceInfo, 0, len(m.nodes[nodeID].Devices)-len(nodeInfo.Devices))
		for _, val := range m.nodes[nodeID].Devices {
			found := false
			for _, rmval := range nodeInfo.Devices {
				if strings.Compare(val.ID, rmval.ID) == 0 {
					found = true
					break
				}
			}
			if !found && len(val.ID) > 0 {
				tmp = append(tmp, val)
			}
		}
		m.nodes[nodeID].Devices = tmp
		if len(m.nodes[nodeID].Devices) == 0 {
			delete(m.nodes, nodeID)
			return
		}
		klog.V(5).Infoln("Rm Devices res:", m.nodes[nodeID].Devices)
	}
}

func (m *nodeManager) GetNode(nodeID string) (*util.NodeInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if n, ok := m.nodes[nodeID]; ok {
		return n, nil
	}
	return &util.NodeInfo{}, fmt.Errorf("node %v not found", nodeID)
}

func (m *nodeManager) ListNodes() (map[string]*util.NodeInfo, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.nodes, nil
}
