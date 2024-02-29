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
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/util"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_getNodesUsage(t *testing.T) {
	nodeMage := nodeManager{}
	nodeMage.init()
	nodeMage.addNode("node1", &util.NodeInfo{
		ID: "node1",
		Devices: []util.DeviceInfo{
			{
				ID:      "GPU0",
				Index:   0,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Health:  true,
			},
			{
				ID:      "GPU1",
				Index:   1,
				Count:   10,
				Devmem:  1024,
				Devcore: 100,
				Numa:    1,
				Health:  true,
			},
		},
	})
	podDevces := util.PodDevices{
		"NVIDIA": util.PodSingleDevice{
			[]util.ContainerDevice{
				{
					Idx:       0,
					UUID:      "GPU0",
					Usedmem:   100,
					Usedcores: 10,
				},
			},
		},
	}
	podMap := podManager{}
	podMap.init()
	podMap.addPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "1111",
			Name:      "test1",
			Namespace: "default",
		},
	}, "node1", podDevces)
	podMap.addPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "2222",
			Name:      "test2",
			Namespace: "default",
		},
	}, "node1", podDevces)
	s := Scheduler{
		nodeManager: nodeMage,
		podManager:  podMap,
	}
	nodes := make([]string, 0)
	nodes = append(nodes, "node1")
	cachenodeMap, _, err := s.getNodesUsage(&nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(*cachenodeMap), 1)
	v, ok := (*cachenodeMap)["node1"]
	assert.Equal(t, ok, true)
	assert.Equal(t, len(v.Devices), 2)
	assert.Equal(t, v.Devices[0].Used, int32(2))
	assert.Equal(t, v.Devices[0].Usedmem, int32(200))
	assert.Equal(t, v.Devices[0].Usedcores, int32(20))
}
