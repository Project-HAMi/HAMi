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

package service

import (
    "fmt"
    "sync"

    pb "4pd.io/k8s-vgpu/pkg/api"
    "k8s.io/klog/v2"
)

type DeviceInfo struct {
    ID     string
    Count  int32
    Health bool
}

type NodeInfo struct {
    ID      string
    Devices []DeviceInfo
}

type DeviceService struct {
    nodes map[string]NodeInfo
    mutex sync.Mutex
}

func NewDeviceService() *DeviceService {
    return &DeviceService{
        nodes: make(map[string]NodeInfo),
        mutex: sync.Mutex{},
    }
}

func (s *DeviceService) addNode(nodeID string, nodeInfo NodeInfo) {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    s.nodes[nodeID] = nodeInfo
}

func (s *DeviceService) delNode(nodeID string) {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    delete(s.nodes, nodeID)
}

func (s *DeviceService) GetNode(nodeID string) (NodeInfo, error) {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    if n, ok := s.nodes[nodeID]; ok {
        return n, nil
    }
    return NodeInfo{}, fmt.Errorf("node %v not found", nodeID)
}

func (s *DeviceService) Register(stream pb.DeviceService_RegisterServer) error {
    var nodeID string
    for {
        req, err := stream.Recv()
        if err != nil {
            s.delNode(nodeID)
            klog.Infof("node %v leave, %v", nodeID, err)
            stream.SendAndClose(&pb.RegisterReply{})
            return err
        }
        klog.V(3).Infof("device register %v", req.String())
        nodeID = req.GetNode()
        nodeInfo := NodeInfo{}
        nodeInfo.ID = nodeID
        nodeInfo.Devices = make([]DeviceInfo, len(req.Devices))
        for i := 0; i < len(req.Devices); i++ {
            nodeInfo.Devices[i] = DeviceInfo{
                ID:     req.Devices[i].GetId(),
                Count:  req.Devices[i].GetCount(),
                Health: req.Devices[i].GetHealth(),
            }
        }
        s.addNode(nodeID, nodeInfo)
        klog.Infof("node %v come", nodeID)
    }
}
