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
	"sync"
)

type DeviceInfo struct {
	ID     string
	Count  int32
	Devmem int32
	Health bool
}

type NodeInfo struct {
	ID      string
	Devices []DeviceInfo
}

type DeviceUsage struct {
	Id        string
	Used      int32
	Count     int32
	Usedmem   int32
	Totalmem  int32
	Usedcores int32
	Health    bool
}

type DeviceUsageList []*DeviceUsage

type NodeUsage struct {
	Devices DeviceUsageList
}

type nodeManager struct {
	nodes map[string]NodeInfo
	mutex sync.Mutex
}

func (m *nodeManager) init() {
	m.nodes = make(map[string]NodeInfo)
}

func (m *nodeManager) addNode(nodeID string, nodeInfo NodeInfo) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.nodes[nodeID] = nodeInfo
}

func (m *nodeManager) delNode(nodeID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.nodes, nodeID)
}

func (m *nodeManager) GetNode(nodeID string) (NodeInfo, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if n, ok := m.nodes[nodeID]; ok {
		return n, nil
	}
	return NodeInfo{}, fmt.Errorf("node %v not found", nodeID)
}

func (m *nodeManager) ListNodes() (map[string]NodeInfo, error) {
	return m.nodes, nil
}
