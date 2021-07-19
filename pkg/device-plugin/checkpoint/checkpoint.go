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

package checkpoint

import (
    "4pd.io/k8s-vgpu/pkg/util"
    "k8s.io/apimachinery/pkg/types"
    "k8s.io/klog/v2"
    pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
    "k8s.io/kubernetes/pkg/kubelet/checkpointmanager"
    "k8s.io/kubernetes/pkg/kubelet/cm/devicemanager/checkpoint"
)

const kubeletDeviceManagerCheckpoint = "kubelet_internal_checkpoint"

type Checkpoint struct {
    manager checkpointmanager.CheckpointManager
}

func NewCheckpoint() (*Checkpoint, error) {
    m, err := checkpointmanager.NewCheckpointManager(pluginapi.DevicePluginPath)
    if err != nil {
        return nil, err
    }
    return &Checkpoint{manager: m}, nil
}

func (m *Checkpoint) GetCheckpoint() (map[types.UID]map[string]util.ContainerDevices, error) {
    registeredDevs := make(map[string][]string)
    devEntries := make([]checkpoint.PodDevicesEntry, 0)
    cp := checkpoint.New(devEntries, registeredDevs)
    err := m.manager.GetCheckpoint(kubeletDeviceManagerCheckpoint, cp)
    if err != nil {
        klog.Errorf("read checkpoint error, %v", err)
        return nil, err
    }
    res := map[types.UID]map[string]util.ContainerDevices{}
    podDevices, _ := cp.GetData()
    for _, pde := range podDevices {
        if pde.ResourceName != util.ResourceName {
            continue
        }
        allocResp := &pluginapi.ContainerAllocateResponse{}
        err = allocResp.Unmarshal(pde.AllocResp)
        if err != nil {
            klog.Errorf("Error: unmarshal container allocate response failed")
            continue
        }
        klog.V(5).Infof("checkpoint pod %v container %v alloc %v resp %v", pde.PodUID, pde.ContainerName, pde.DeviceIDs, allocResp)
        s, ok := allocResp.Annotations[util.AssignedIDsAnnotations]
        if !ok {
            klog.Errorf("pod %v container %v not found device ids", pde.PodUID, pde.ContainerName)
            continue
        }
        cd := util.DecodeContainerDevices(s)
        _, ok = res[types.UID(pde.PodUID)]
        if !ok {
            res[types.UID(pde.PodUID)] = map[string]util.ContainerDevices{pde.ContainerName: cd}
        } else {
            res[types.UID(pde.PodUID)][pde.ContainerName] = cd
        }
    }
    return res, nil
}
