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

	"4pd.io/k8s-vgpu/pkg/util"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_getNodesUsage(t *testing.T) {
	nodeMage := nodeManager{}
	nodeMage.init()
	nodeMage.addNode("node1", &NodeInfo{
		ID: "node1",
		Devices: []DeviceInfo{
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
		[]util.ContainerDevice{
			{
				Idx:       0,
				UUID:      "GPU0",
				Usedmem:   100,
				Usedcores: 10,
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
	assert := assert.New(t)
	assert.Equal(1, len(*cachenodeMap), "unexpected cachenodeMap length")
	v, ok := (*cachenodeMap)["node1"]
	assert.True(ok, "node1 should exist in cachenodeMap")
	assert.Equal(2, len(v.Devices), "unexpected Devices length")
	assert.Equal(int32(2), v.Devices[0].Used, "unexpected Used")
	assert.Equal(int32(200), v.Devices[0].Usedmem, "unexpected Usedmem")
	assert.Equal(int32(20), v.Devices[0].Usedcores, "unexpected Usedcores")
}
