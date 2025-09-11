/*
Copyright 2025 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kunlun

import (
	"sort"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/device"
)

type FitFn func(device *device.DeviceUsage, request device.ContainerDeviceRequest) bool

func parseUsage(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, fitFn FitFn) []int {
	usage := []int{}
	for _, val := range devices {
		if fitFn(val, request) {
			usage = append(usage, int(val.Index))
		}
	}
	return usage
}

func addidx(temp []int, value int) []int {
	for _, val := range temp {
		if val == value {
			return temp
		}
	}
	temp = append(temp, value)
	return temp
}

func getvalue(t int) int {
	if t == 4 {
		return 0
	}
	if t == 1 {
		return 2
	}
	return 1
}

func countbubble(t []int) int {
	left := 0
	right := 0
	for _, val := range t {
		if val < 4 {
			left++
		} else {
			right++
		}
	}
	if left == 0 && right == 0 {
		return 1
	}
	return getvalue(left) + getvalue(right)
}

func calcscore(p []int, c []int) float32 {
	sort.Slice(p, func(i, j int) bool {
		return i < j
	})
	sort.Slice(c, func(i, j int) bool {
		return i < j
	})
	prev := countbubble(p)
	cur := countbubble(c)
	klog.V(5).Infoln("Score kunlun num prev=", prev, "cur=", cur)
	switch cur - prev {
	case -1:
		return 3000
	case 0:
		return 2000
	case 1:
		return 1000
	case 2:
		return 0
	}
	return 1000
}

func parseInterconnection() [][]int {
	var interconnection [][]int
	pairs := strings.Split(InterGroupConnection, ",")
	for _, pair := range pairs {
		lw, _ := strconv.Atoi(strings.Split(pair, "-")[0])
		rw, _ := strconv.Atoi(strings.Split(pair, "-")[1])
		interconnection = append(interconnection, []int{lw, rw})
	}
	pairs = strings.Split(GroupConnection, ",")
	for _, pair := range pairs {
		lw, _ := strconv.Atoi(strings.Split(pair, "-")[0])
		rw, _ := strconv.Atoi(strings.Split(pair, "-")[1])
		interconnection = append(interconnection, []int{lw, rw})
	}
	return interconnection
}

func parseInterconnection2() [][]int {
	var interconnection2 [][]int
	for group := range strings.SplitSeq(InterGroupConnection2, ",") {
		values := strings.Split(group, "-")
		connect := make([]int, 4)
		for i, value := range values {
			v, _ := strconv.Atoi(value)
			connect[i] = v
		}
		interconnection2 = append(interconnection2, connect)
	}
	return interconnection2
}

func interconnect(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, fitFn FitFn) []int {
	count := int(request.Nums)
	if count == 2 {
		for _, val := range devices {
			if !fitFn(val, request) {
				continue
			}
			for _, val2 := range devices {
				if !fitFn(val2, request) || val2.Index == val.Index {
					continue
				}
				for p := range strings.SplitSeq(InterGroupConnection, ",") {
					lw, _ := strconv.Atoi(strings.Split(p, "-")[0])
					rw, _ := strconv.Atoi(strings.Split(p, "-")[1])
					klog.V(5).InfoS("interconnect", "lw", lw, "rw", rw, "left device", val.Index, "right device", val2.Index)
					if lw == int(val.Index) && rw == int(val2.Index) || lw == int(val2.Index) && rw == int(val.Index) {
						return []int{int(val.Index), int(val2.Index)}
					}
				}
			}
		}
	}
	if count == 4 {
		unused := parseUsage(devices, request, fitFn)
		interconnect2 := parseInterconnection2()
		if len(unused) == 4 || len(unused) == 5 {
			for _, c := range interconnect2 {
				if canMeet(unused, c) {
					return c
				}
			}
		}
		if len(unused) == 6 {
			ret := []int{}
			for _, c := range interconnect2 {
				if canMeet(unused, c) {
					ret = c
					delta := delta(unused, c)
					for _, val := range parseInterconnection() {
						if canMeet(delta, val) {
							return ret
						}
					}
				}
			}
			return ret
		}
	}
	return []int{}
}

func canMeet(have, want []int) bool {
	mp := make(map[int]bool)
	for _, v := range have {
		mp[v] = true
	}
	for _, v := range want {
		if !mp[v] {
			return false
		}
	}
	return true
}

func delta(have, want []int) []int {
	var ret []int
	mp := make(map[int]bool)
	for _, v := range want {
		mp[v] = true
	}
	for _, v := range have {
		if !mp[v] {
			ret = append(ret, v)
		}
	}
	return ret
}

func devicepick(devices []*device.DeviceUsage, start int, request device.ContainerDeviceRequest, fitFn FitFn) []int {
	count := int(request.Nums)
	res := []int{}
	for t := start; t < 8; t++ {
		if fitFn(devices[t], request) {
			res = append(res, int(devices[t].Index))
			if len(res) == count {
				return res
			}
		}
	}
	return res
}

func graghSelect(devices []*device.DeviceUsage, request device.ContainerDeviceRequest, fitFn FitFn) []int {
	count := int(request.Nums)
	leftwing := 0
	rightwing := 0
	for idx, val := range devices {
		klog.Infoln("graph select val=", *val)
		if idx < 4 {
			if fitFn(val, request) {
				leftwing++
			}
		} else {
			if fitFn(val, request) {
				rightwing++
			}
		}
	}
	oddorder := []int{1, 3, 2, 4}
	switch count {
	case 8:
		{
			if leftwing+rightwing == count {
				return []int{0, 1, 2, 3, 4, 5, 6, 7}
			}
			return []int{}
		}
	case 1, 2, 4:
		{
			if leftwing >= count || rightwing >= count {
				for slots := count; slots <= 4; slots++ {
					num := slots
					if count%2 == 1 {
						num = oddorder[slots-1]
					}
					klog.Infoln("slots=", slots, "num=", num, "leftwing=", leftwing, "==", rightwing)
					if leftwing == num {
						return devicepick(devices, 0, request, fitFn)
					}
					if rightwing == num {
						return devicepick(devices, 4, request, fitFn)
					}
				}
			}
			return interconnect(devices, request, fitFn)
		}
	}
	return []int{}
}
