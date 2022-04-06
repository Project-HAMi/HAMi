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
	"strings"

	"4pd.io/k8s-vgpu/pkg/util"
	"k8s.io/klog/v2"
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

func checkGPUtype(annos map[string]string, cardtype string) bool {
	inuse, ok := annos[util.GPUInUse]
	if ok {
		for _, val := range strings.Split(inuse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
				return true
			}
		}
		return false
	}
	nouse, ok := annos[util.GPUNoUse]
	if ok {
		for _, val := range strings.Split(nouse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
				return false
			}
		}
		return true
	}
	return true
}

func calcScore(nodes *map[string]*NodeUsage, errMap *map[string]string, nums []util.ContainerDeviceRequest, annos map[string]string) (*NodeScoreList, error) {
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
				klog.Info("Scoring pod ", n.Memreq, ":", n.MemPercentagereq, ":", n.Coresreq, ":", n.Nums)
				if n.MemPercentagereq != 101 {
					n.Memreq = node.Devices[i].Totalmem * n.MemPercentagereq / 100
				}
				if node.Devices[i].Totalmem-node.Devices[i].Usedmem < n.Memreq {
					continue
				}
				if 100-node.Devices[i].Usedcores < n.Coresreq {
					continue
				}
				// Coresreq=100 indicates it want this card exclusively
				if n.Coresreq == 100 && node.Devices[i].Used > 0 {
					continue
				}
				// You can't allocate core=0 job to an already full GPU
				if node.Devices[i].Usedcores == 100 && n.Coresreq == 0 {
					continue
				}
				if !checkGPUtype(annos, node.Devices[i].Type) {
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
