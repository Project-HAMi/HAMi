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
	"sort"

	"4pd.io/k8s-vgpu/pkg/util"
)

type NodeScore struct {
	nodeID  string
	devices util.PodDevices
	score   float32
}

type NodeScoreList []*NodeScore

func (l DeviceUsageList) Len() int {
	return len(l)
}

func (l DeviceUsageList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l DeviceUsageList) Less(i, j int) bool {
	return l[i].Count-l[i].Used < l[j].Count-l[j].Used
}

func (l NodeScoreList) Len() int {
	return len(l)
}

func (l NodeScoreList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l NodeScoreList) Less(i, j int) bool {
	return l[i].score < l[j].score
}

func viewStatus(usage NodeUsage) {
	fmt.Println("viewing status")
	for _, val := range usage.Devices {
		fmt.Println(val)
	}
}

func calcScore(nodes *map[string]*NodeUsage, errMap *map[string]string, nums []util.ContainerDeviceRequest) (*NodeScoreList, error) {
	res := make(NodeScoreList, 0, len(*nodes))
	for nodeID, node := range *nodes {
		viewStatus(*node)
		dn := len(node.Devices)
		score := NodeScore{nodeID: nodeID, score: 0}
		for _, n := range nums {
			if n.Nums == 0 {
				score.devices = append(score.devices, util.ContainerDevices{})
				continue
			}
			if int(n.Nums) > dn {
				break
			}
			sort.Sort(node.Devices)
			if node.Devices[dn-int(n.Nums)].Count <= node.Devices[dn-int(n.Nums)].Used {
				break
			}
			total := int32(0)
			free := int32(0)
			//devs := make([]string, 0, n)
			devs := make([]util.ContainerDevice, 0, n.Nums)
			countremains := 1
			for i := len(node.Devices) - 1; i >= 0; i-- {
				if node.Devices[i].Count <= node.Devices[i].Used {
					countremains = 0
					break
				}
				if node.Devices[i].Totalmem-node.Devices[i].Usedmem < n.Memreq {
					continue
				}
				if 100-node.Devices[i].Usedcores <= n.Coresreq {
					continue
				}
				total += node.Devices[i].Count
				free += node.Devices[i].Count - node.Devices[i].Used
				if n.Nums > 0 {
					n.Nums--
					node.Devices[i].Used++
					node.Devices[i].Usedmem += n.Memreq
					node.Devices[i].Usedcores += n.Coresreq
					devs = append(devs, util.ContainerDevice{
						UUID:      node.Devices[i].Id,
						Usedmem:   n.Memreq,
						Usedcores: n.Coresreq,
					})
				}
			}
			if countremains == 0 || n.Nums > 0 {
				break
			}
			score.devices = append(score.devices, devs)
			score.score += float32(free) / float32(total)
			score.score += float32(dn - int(n.Nums))
		}
		if len(score.devices) == len(nums) {
			res = append(res, &score)
		}
	}
	return &res, nil
}
