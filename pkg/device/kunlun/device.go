/*
Copyright 2024 The HAMi Authors.

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
	"flag"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Project-HAMi/HAMi/pkg/device/common"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type KunlunDevices struct {
}

const (
	KunlunGPUDevice       = "kunlun"
	KunlunGPUCommonWord   = "kunlun"
	KunlunDeviceSelection = "BAIDU_COM_DEVICE_IDX"
	KunlunUseUUID         = "baidu.com/use-gpuuuid"
	KunlunNoUseUUID       = "baidu.com/nouse-gpuuuid"
	InterGroupConnection  = "0-4,1-5,2-6,3-7"
	InterGroupConnection2 = "0-1-4-5,2-3-6-7,0-2-4-6,1-3-5-7,0-3-4-7,1-2-5-6"
	GroupConnection       = "0-1,0-2,0-3,1-2,1-3,2-3,4-5,4-6,4-7,5-6,5-7,6-7"
)

var (
	KunlunResourceCount string
)

type KunlunConfig struct {
	ResourceCountName string `yaml:"resourceCountName"`
}

func InitKunlunDevice(config KunlunConfig) *KunlunDevices {
	KunlunResourceCount = config.ResourceCountName
	util.SupportDevices[KunlunGPUDevice] = "hami.io/kunlun-allocated"
	return &KunlunDevices{}
}

func (dev *KunlunDevices) CommonWord() string {
	return KunlunGPUCommonWord
}

func ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&KunlunResourceCount, "kunlun-name", "kunlunxin.com/xpu", "kunlunxin resource count")
}

func (dev *KunlunDevices) MutateAdmission(ctr *corev1.Container, p *corev1.Pod) (bool, error) {
	_, ok := ctr.Resources.Limits[corev1.ResourceName(KunlunResourceCount)]
	return ok, nil
}

func (dev *KunlunDevices) GetNodeDevices(n corev1.Node) ([]*util.DeviceInfo, error) {
	nodedevices := []*util.DeviceInfo{}
	i := 0
	cards, ok := n.Status.Capacity.Name(corev1.ResourceName(KunlunResourceCount), resource.DecimalSI).AsInt64()
	if !ok || cards == 0 {
		return []*util.DeviceInfo{}, fmt.Errorf("device not found %s", KunlunResourceCount)
	}
	for int64(i) < cards {
		nodedevices = append(nodedevices, &util.DeviceInfo{
			Index:   uint(i),
			ID:      n.Name + "-kunlun-" + fmt.Sprint(i),
			Count:   100,
			Devmem:  98304,
			Devcore: 100,
			Type:    KunlunGPUDevice,
			Numa:    0,
			Health:  true,
		})
		if int64(i) >= (cards / 2) {
			nodedevices[i].Numa = 1
		}
		i++
	}
	return nodedevices, nil
}

func (dev *KunlunDevices) PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd util.PodDevices) map[string]string {
	devlist, ok := pd[KunlunGPUDevice]
	if ok && len(devlist) > 0 {
		(*annoinput)[util.SupportDevices[KunlunGPUDevice]] = util.EncodePodSingleDevice(devlist)
		for _, dp := range devlist {
			annoKey := KunlunDeviceSelection
			value := ""
			for _, val := range dp {
				value = value + fmt.Sprint(val.Idx) + ","
			}
			if len(value) > 0 {
				(*annoinput)[annoKey] = strings.TrimRight(value, ",")
			}
		}
	}
	return *annoinput
}

func (dev *KunlunDevices) LockNode(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *KunlunDevices) ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error {
	return nil
}

func (dev *KunlunDevices) NodeCleanUp(nn string) error {
	return nil
}

func (dev *KunlunDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool) {
	if strings.Compare(n.Type, KunlunGPUDevice) == 0 {
		return true, false
	}
	return false, false
}

func (dev *KunlunDevices) CheckUUID(annos map[string]string, d util.DeviceUsage) bool {
	userUUID, ok := annos[KunlunUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for Kunlun user uuid [%s], device id is %s", userUUID, d.ID)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		return slices.Contains(userUUIDs, d.ID)
	}

	noUserUUID, ok := annos[KunlunNoUseUUID]
	if ok {
		klog.V(5).Infof("check uuid for Kunlun not user uuid [%s], device id is %s", noUserUUID, d.ID)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		return !slices.Contains(noUserUUIDs, d.ID)
	}
	return true
}

func (dev *KunlunDevices) CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	return true, true
}

func (dev *KunlunDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	klog.Info("Start to count kunlun devices for container ", ctr.Name)
	kunlunResourceCount := corev1.ResourceName(KunlunResourceCount)
	v, ok := ctr.Resources.Limits[kunlunResourceCount]
	if !ok {
		v, ok = ctr.Resources.Requests[kunlunResourceCount]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			klog.Info("Found kunlunxin devices")

			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             KunlunGPUDevice,
				Memreq:           0,
				MemPercentagereq: 100,
				Coresreq:         0,
			}
		}
	}
	return util.ContainerDeviceRequest{}
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

func parseUsage(devices []*util.DeviceUsage) []int {
	usage := []int{}
	for _, val := range devices {
		if val.Used == 0 {
			usage = append(usage, int(val.Index))
		}
	}
	return usage
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
	groups := strings.Split(InterGroupConnection2, ",")
	for _, group := range groups {
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

func interconnect(devices []*util.DeviceUsage, count int) []int {
	if count == 2 {
		for _, val := range devices {
			if val.Used > 0 {
				continue
			}
			for _, val2 := range devices {
				if val2.Used > 0 || val2.Index == val.Index {
					continue
				}
				pairs := strings.Split(InterGroupConnection, ",")
				for _, p := range pairs {
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
		unused := parseUsage(devices)
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

func (dev *KunlunDevices) ScoreNode(node *corev1.Node, podDevices util.PodSingleDevice, previous []*util.DeviceUsage, policy string) float32 {
	current := []int{}
	prev := []int{}
	for _, dev := range previous {
		if !strings.Contains(dev.Type, KunlunGPUDevice) {
			return 0
		}
		if dev.Used > 0 {
			prev = addidx(prev, int(dev.Index))
		}
	}
	for _, ctr := range podDevices {
		for _, val := range ctr {
			if !strings.Contains(val.Type, KunlunGPUDevice) {
				return 0
			}
			current = addidx(current, val.Idx)
		}
	}
	klog.V(3).Infoln("Score kunlun device previous=", prev, "current=", current)
	return calcscore(prev, current)
}

func (dev *KunlunDevices) AddResourceUsage(pod *corev1.Pod, n *util.DeviceUsage, ctr *util.ContainerDevice) error {
	n.Used++
	return nil
}

func devicepick(devices []*util.DeviceUsage, start int, count int) []int {
	res := []int{}
	for t := start; t < 8; t++ {
		if devices[t].Used == 0 {
			res = append(res, int(devices[t].Index))
			if len(res) == count {
				return res
			}
		}
	}
	return res
}

func graghSelect(devices []*util.DeviceUsage, count int) []int {
	leftwing := 0
	rightwing := 0
	for idx, val := range devices {
		klog.Infoln("graph select val=", *val)
		if idx < 4 {
			if val.Used == 0 {
				leftwing++
			}
		} else {
			if val.Used == 0 {
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
						return devicepick(devices, 0, count)
					}
					if rightwing == num {
						return devicepick(devices, 4, count)
					}
				}
			}
			return interconnect(devices, count)
		}
	}
	return []int{}
}

func (kl *KunlunDevices) Fit(devices []*util.DeviceUsage, request util.ContainerDeviceRequest, annos map[string]string, pod *corev1.Pod, allocated *util.PodDevices) (bool, map[string]util.ContainerDevices, string) {
	klog.InfoS("Allocating device for container request", "pod", klog.KObj(pod), "card request", request)
	tmpDevs := make(map[string]util.ContainerDevices)
	reason := make(map[string]int)

	alloc := graghSelect(devices, int(request.Nums))
	if len(alloc) == 0 {
		reason[common.NumaNotFit]++
		klog.V(5).InfoS(common.NumaNotFit, "pod", klog.KObj(pod), "device", devices, "request nums", request.Nums, "numa")
		return false, tmpDevs, common.GenReason(reason, len(reason))
	}

	for _, dev := range alloc {
		for _, val := range devices {
			if val.Index == uint(dev) {
				tmpDevs[request.Type] = append(tmpDevs[request.Type], util.ContainerDevice{
					Idx:       int(val.Index),
					UUID:      val.ID,
					Type:      request.Type,
					Usedmem:   val.Totalmem,
					Usedcores: val.Totalcore,
				})
				break
			}
		}
	}
	return true, tmpDevs, ""
}
