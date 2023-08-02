/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package scheduler

import (
	"fmt"
	"strings"
	"sync"

	"k8s.io/klog/v2"
)

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

type DeviceUsage struct {
	Id        string
	Index     uint
	Used      int32
	Count     int32
	Usedmem   int32
	Totalmem  int32
	Totalcore int32
	Usedcores int32
	Type      string
	Health    bool
}

type DeviceUsageList []*DeviceUsage

type NodeUsage struct {
	Devices DeviceUsageList
}

type nodeManager struct {
	nodes map[string]*NodeInfo
	mutex sync.Mutex
}

func (m *nodeManager) init() {
	m.nodes = make(map[string]*NodeInfo)
}

func (m *nodeManager) addNode(nodeID string, nodeInfo *NodeInfo) {
	if nodeInfo == nil || len(nodeInfo.Devices) == 0 {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	_, ok := m.nodes[nodeID]
	if ok {
		tmp := make([]DeviceInfo, 0, len(m.nodes[nodeID].Devices)+len(nodeInfo.Devices))
		tmp = append(tmp, m.nodes[nodeID].Devices...)
		tmp = append(tmp, nodeInfo.Devices...)
		m.nodes[nodeID].Devices = tmp
	} else {
		m.nodes[nodeID] = nodeInfo
	}
}

func (m *nodeManager) rmNodeDevice(nodeID string, nodeInfo *NodeInfo) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	_, ok := m.nodes[nodeID]
	if ok {
		if m.nodes[nodeID].Devices == nil || len(m.nodes[nodeID].Devices) == 0 {
			return
		}
		klog.Infoln("before rm:", m.nodes[nodeID].Devices, "needs remove", nodeInfo.Devices)
		tmp := make([]DeviceInfo, 0, len(m.nodes[nodeID].Devices)-len(nodeInfo.Devices))
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
		klog.Infoln("Rm Devices res:", m.nodes[nodeID].Devices)
	}
}

func (m *nodeManager) GetNode(nodeID string) (*NodeInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if n, ok := m.nodes[nodeID]; ok {
		return n, nil
	}
	return &NodeInfo{}, fmt.Errorf("node %v not found", nodeID)
}

func (m *nodeManager) ListNodes() (map[string]*NodeInfo, error) {
	return m.nodes, nil
}
