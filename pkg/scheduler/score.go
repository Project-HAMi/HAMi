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
    "4pd.io/k8s-vgpu/pkg/util"
    "sort"
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
    return l[i].count-l[i].used < l[j].count-l[j].used
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

func calcScore(nodes *map[string]*NodeUsage, errMap *map[string]string, nums []int) (*NodeScoreList, error) {
    res := make(NodeScoreList, 0, len(*nodes))
    for nodeID, node := range *nodes {
        dn := len(node.devices)
        score := NodeScore{nodeID: nodeID, score: 0}
        for _, n := range nums {
            if n == 0 {
                score.devices = append(score.devices, []string{})
                continue
            }
            if n > dn {
                break
            }
            sort.Sort(node.devices)
            if node.devices[dn-n].count <= node.devices[dn-n].used {
                continue
            }
            total := int32(0)
            free := int32(0)
            devs := make([]string, 0, n)
            for i := len(node.devices) - 1; i >= 0; i-- {
                total += node.devices[i].count
                free += node.devices[i].count - node.devices[i].used
                if n > 0 {
                    n--
                    node.devices[i].used++
                    devs = append(devs, node.devices[i].id)
                }
            }
            score.devices = append(score.devices, devs)
            score.score += float32(free) / float32(total)
            score.score += float32(dn - n)
        }
        if len(score.devices) == len(nums) {
            res = append(res, &score)
        }
    }
    return &res, nil
}
