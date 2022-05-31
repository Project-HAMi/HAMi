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

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/util"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type podInfo struct {
	Namespace string
	Name      string
	Uid       k8stypes.UID
	NodeID    string
	Devices   util.PodDevices
	CtrIDs    []string
}

type containerInfo struct {
	podUID k8stypes.UID
	ctrIdx int
}

type podManager struct {
	pods       map[k8stypes.UID]*podInfo
	containers map[string]containerInfo
	mutex      sync.Mutex
}

func (m *podManager) init() {
	m.pods = make(map[k8stypes.UID]*podInfo)
	m.containers = make(map[string]containerInfo)
}

func (m *podManager) addPod(pod *corev1.Pod, nodeID string, devices util.PodDevices) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	pi, ok := m.pods[pod.UID]
	if !ok {
		pi = &podInfo{Name: pod.Name, Uid: pod.UID}
		m.pods[pod.UID] = pi
		pi.Namespace = pod.Namespace
		pi.Name = pod.Name
		pi.Uid = pod.UID
		pi.NodeID = nodeID
		pi.Devices = devices
		klog.Info(pod.Name + "Added")
		pi.CtrIDs = make([]string, len(pod.Spec.Containers))
		for i := 0; i < len(pod.Spec.Containers); i++ {
			c := &pod.Spec.Containers[i]
			if i >= len(devices) {
				klog.Errorf("len(device) != len(containers)")
				continue
			}
			for _, env := range c.Env {
				if env.Name == api.ContainerUID {
					m.containers[env.Value] = containerInfo{
						podUID: pod.UID,
						ctrIdx: i,
					}
					pi.CtrIDs[i] = env.Value
					break
				}
			}
			if len(pi.CtrIDs[i]) == 0 {
				klog.Errorf("not found container uid in container %v/%v/%v", pod.Namespace, pod.Name, c.Name)
			}
		}
	}
}

func (m *podManager) delPod(pod *corev1.Pod) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	pi, ok := m.pods[pod.UID]
	if ok {
		for _, id := range pi.CtrIDs {
			delete(m.containers, id)
		}
		klog.Infof(pi.Name + " deleted")
		delete(m.pods, pod.UID)
	}
}

func (m *podManager) GetScheduledPods() (map[k8stypes.UID]*podInfo, error) {
	return m.pods, nil
}

func (m *podManager) getContainerByUUID(uuid string) (podInfo, int, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	c, ok := m.containers[uuid]
	if !ok {
		return podInfo{}, 0, fmt.Errorf("not found container %v", uuid)
	}
	pi, ok := m.pods[c.podUID]
	if !ok {
		return podInfo{}, 0, fmt.Errorf("not found pod %v", c.podUID)
	}
	return *pi, c.ctrIdx, nil
}
