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

func checkType(annos map[string]string, d DeviceUsage, n util.ContainerDeviceRequest) bool {
	if !strings.Contains(d.Type, n.Type) {
		return false
	}
	if strings.Compare(n.Type, util.NvidiaGPUDevice) == 0 {
		return checkGPUtype(annos, d.Type)
	}
	if strings.Compare(n.Type, util.CambriconMLUDevice) == 0 {
		if !strings.Contains(d.Type, "370") && n.Memreq != 0 {
			return false
		}
		if strings.Contains(d.Type, "370") && n.Memreq == 0 && d.Used > 0 {
			return false
		}
		return true
	}
	klog.Infof("Unrecognized device", n.Type)
	return false
}

func calcScore(nodes *map[string]*NodeUsage, errMap *map[string]string, nums [][]util.ContainerDeviceRequest, annos map[string]string) (*NodeScoreList, error) {
	res := make(NodeScoreList, 0, len(*nodes))
	for nodeID, node := range *nodes {
		viewStatus(*node)
		dn := len(node.Devices)
		score := NodeScore{nodeID: nodeID, score: 0}
		for _, n := range nums {
			sums := 0
			for _, k := range n {
				sums += int(k.Nums)
			}
			if sums == 0 {
				score.devices = append(score.devices, util.ContainerDevices{})
				continue
			}
			devs := make([]util.ContainerDevice, 0, sums)
			fit := true
			total := int32(0)
			free := int32(0)
			for _, k := range n {
				if int(k.Nums) > dn {
					fit = false
					break
				}
				sort.Sort(node.Devices)
				//If this node has no devices available
				if node.Devices[dn-int(k.Nums)].Count <= node.Devices[dn-int(k.Nums)].Used {
					fit = false
					break
				}
				//devs := make([]string, 0, n)
				klog.Infoln("Allocating device for container request", k)
				for i := len(node.Devices) - 1; i >= 0; i-- {
					klog.Info("Scoring pod ", k.Memreq, ":", k.MemPercentagereq, ":", k.Coresreq, ":", k.Nums, "i", i, "device:", node.Devices[i].Id)
					if node.Devices[i].Count <= node.Devices[i].Used {
						continue
					}
					if k.MemPercentagereq != 101 && k.Memreq == 0 {
						k.Memreq = node.Devices[i].Totalmem * k.MemPercentagereq / 100
					}
					if node.Devices[i].Totalmem-node.Devices[i].Usedmem < k.Memreq {
						continue
					}
					if 100-node.Devices[i].Usedcores < k.Coresreq {
						continue
					}
					// Coresreq=100 indicates it want this card exclusively
					if k.Coresreq == 100 && node.Devices[i].Used > 0 {
						continue
					}
					// You can't allocate core=0 job to an already full GPU
					if node.Devices[i].Usedcores == 100 && k.Coresreq == 0 {
						continue
					}
					if !checkType(annos, *node.Devices[i], k) {
						continue
					}
					total += node.Devices[i].Count
					free += node.Devices[i].Count - node.Devices[i].Used
					if k.Nums > 0 {
						klog.Infoln("device", node.Devices[i].Id, "fitted")
						k.Nums--
						node.Devices[i].Used++
						node.Devices[i].Usedmem += k.Memreq
						node.Devices[i].Usedcores += k.Coresreq
						devs = append(devs, util.ContainerDevice{
							UUID:      node.Devices[i].Id,
							Type:      k.Type,
							Usedmem:   k.Memreq,
							Usedcores: k.Coresreq,
						})
					}
					if k.Nums == 0 {
						break
					}
				}
				if k.Nums > 0 {
					fit = false
					break
				}
			}
			if fit {
				score.devices = append(score.devices, devs)
				score.score += float32(free) / float32(total)
				score.score += float32(dn - int(sums))
			} else {
				break
			}
		}
		if len(score.devices) == len(nums) {
			res = append(res, &score)
		}
	}
	return &res, nil
}
