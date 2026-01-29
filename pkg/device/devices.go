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

package device

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ccoveille/go-safecast"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/util"
)

type Devices interface {
	CommonWord() string
	MutateAdmission(ctr *corev1.Container, pod *corev1.Pod) (bool, error)
	CheckHealth(devType string, n *corev1.Node) (bool, bool)
	NodeCleanUp(nn string) error
	GetResourceNames() ResourceNames
	GetNodeDevices(n corev1.Node) ([]*DeviceInfo, error)
	LockNode(n *corev1.Node, p *corev1.Pod) error
	ReleaseNodeLock(n *corev1.Node, p *corev1.Pod) error
	GenerateResourceRequests(ctr *corev1.Container) ContainerDeviceRequest
	PatchAnnotations(pod *corev1.Pod, annoinput *map[string]string, pd PodDevices) map[string]string
	ScoreNode(node *corev1.Node, podDevices PodSingleDevice, previous []*DeviceUsage, policy string) float32
	AddResourceUsage(pod *corev1.Pod, n *DeviceUsage, ctr *ContainerDevice) error
	Fit(devices []*DeviceUsage, request ContainerDeviceRequest, pod *corev1.Pod, nodeInfo *NodeInfo, allocated *PodDevices) (bool, map[string]ContainerDevices, string)
}

type MigTemplate struct {
	Name   string `yaml:"name"`
	Core   int32  `yaml:"core"`
	Memory int32  `yaml:"memory"`
	Count  int32  `yaml:"count"`
}

type MigTemplateUsage struct {
	Name   string `json:"name,omitempty"`
	Core   int32  `json:"core,omitempty"`
	Memory int32  `json:"memory,omitempty"`
	InUse  bool   `json:"inuse,omitempty"`
}

type Geometry []MigTemplate

type MIGS []MigTemplateUsage

type MigInUse struct {
	Index     int32
	UsageList MIGS
}

type AllowedMigGeometries struct {
	Models     []string   `yaml:"models"`
	Geometries []Geometry `yaml:"allowedGeometries"`
}

type DeviceUsage struct {
	ID          string
	Index       uint
	Used        int32
	Count       int32
	Usedmem     int32
	Totalmem    int32
	Totalcore   int32
	Usedcores   int32
	Mode        string
	MigTemplate []Geometry
	MigUsage    MigInUse
	Numa        int
	Type        string
	Health      bool
	PodInfos    []*PodInfo
	CustomInfo  map[string]any
}

type DeviceInfo struct {
	ID              string          `json:"id,omitempty"`
	Index           uint            `json:"index,omitempty"`
	Count           int32           `json:"count,omitempty"`
	Devmem          int32           `json:"devmem,omitempty"`
	Devcore         int32           `json:"devcore,omitempty"`
	Type            string          `json:"type,omitempty"`
	Numa            int             `json:"numa,omitempty"`
	Mode            string          `json:"mode,omitempty"`
	MIGTemplate     []Geometry      `json:"migtemplate,omitempty"`
	Health          bool            `json:"health,omitempty"`
	DeviceVendor    string          `json:"devicevendor,omitempty"`
	CustomInfo      map[string]any  `json:"custominfo,omitempty"`
	DevicePairScore DevicePairScore `json:"devicepairscore,omitempty"`
}

type DevicePairScores []DevicePairScore
type DevicePairScore struct {
	ID     string         `json:"uuid,omitempty"`
	Scores map[string]int `json:"score,omitempty"`
}

type NodeInfo struct {
	ID      string
	Node    *corev1.Node
	Devices map[string][]DeviceInfo
}

type ResourceNames struct {
	ResourceCountName  string
	ResourceMemoryName string
	ResourceCoreName   string
}

type ContainerDevice struct {
	// TODO current Idx cannot use, because EncodeContainerDevices method not encode this filed.
	Idx        int
	UUID       string
	Type       string
	Usedmem    int32
	Usedcores  int32
	CustomInfo map[string]any
}

type ContainerDeviceRequest struct {
	Nums             int32
	Type             string
	Memreq           int32
	MemPercentagereq int32
	Coresreq         int32
}

type ContainerDevices []ContainerDevice
type ContainerDeviceRequests map[string]ContainerDeviceRequest

// type ContainerAllDevices map[string]ContainerDevices.
type PodSingleDevice []ContainerDevices
type PodDeviceRequests []ContainerDeviceRequests
type PodDevices map[string]PodSingleDevice

const (
	// OneContainerMultiDeviceSplitSymbol this is when one container use multi device, use : symbol to join device info.
	OneContainerMultiDeviceSplitSymbol = ":"

	// OnePodMultiContainerSplitSymbol this is when one pod having multi container and more than one container use device, use ; symbol to join device info.
	OnePodMultiContainerSplitSymbol = ";"
)

var (
	GPUSchedulerPolicy string
	InRequestDevices   map[string]string
	SupportDevices     map[string]string
	DevicesMap         map[string]Devices
	DevicesToHandle    []string
)

func init() {
	InRequestDevices = make(map[string]string)
	SupportDevices = make(map[string]string)
}

func GetDevices() map[string]Devices {
	return DevicesMap
}

func DecodeNodeDevices(str string) ([]*DeviceInfo, error) {
	if !strings.Contains(str, OneContainerMultiDeviceSplitSymbol) {
		return []*DeviceInfo{}, errors.New("node annotations not decode successfully")
	}
	tmp := strings.Split(str, OneContainerMultiDeviceSplitSymbol)
	var retval []*DeviceInfo
	for _, val := range tmp {
		if strings.Contains(val, ",") {
			items := strings.Split(val, ",")
			if len(items) == 7 || len(items) == 9 {
				count, _ := strconv.ParseInt(items[1], 10, 32)
				devmem, _ := strconv.ParseInt(items[2], 10, 32)
				devcore, _ := strconv.ParseInt(items[3], 10, 32)
				health, _ := strconv.ParseBool(items[6])
				numa, _ := strconv.Atoi(items[5])
				mode := "hami-core"
				index := 0
				if len(items) == 9 {
					index, _ = strconv.Atoi(items[7])
					mode = items[8]
				}
				count32, err := safecast.Convert[int32](count)
				if err != nil {
					return []*DeviceInfo{}, errors.New("node annotations not decode successfully")
				}
				devmem32, err := safecast.Convert[int32](devmem)
				if err != nil {
					return []*DeviceInfo{}, errors.New("node annotations not decode successfully")
				}
				devcore32, err := safecast.Convert[int32](devcore)
				if err != nil {
					return []*DeviceInfo{}, errors.New("node annotations not decode successfully")
				}
				i := DeviceInfo{
					ID:      items[0],
					Count:   count32,
					Devmem:  devmem32,
					Devcore: devcore32,
					Type:    items[4],
					Numa:    numa,
					Health:  health,
					Mode:    mode,
					Index:   uint(index),
				}
				retval = append(retval, &i)
			} else {
				return []*DeviceInfo{}, errors.New("node annotations not decode successfully")
			}
		}
	}
	return retval, nil
}

func DecodePairScores(pairScores string) (*DevicePairScores, error) {
	devicePairScores := &DevicePairScores{}
	if err := json.Unmarshal([]byte(pairScores), devicePairScores); err != nil {
		return nil, err
	}
	return devicePairScores, nil
}

func EncodeNodeDevices(dlist []*DeviceInfo) string {
	builder := strings.Builder{}
	for _, val := range dlist {
		builder.WriteString(val.ID)
		builder.WriteString(",")
		builder.WriteString(strconv.FormatInt(int64(val.Count), 10))
		builder.WriteString(",")
		builder.WriteString(strconv.Itoa(int(val.Devmem)))
		builder.WriteString(",")
		builder.WriteString(strconv.Itoa(int(val.Devcore)))
		builder.WriteString(",")
		builder.WriteString(val.Type)
		builder.WriteString(",")
		builder.WriteString(strconv.Itoa(val.Numa))
		builder.WriteString(",")
		builder.WriteString(strconv.FormatBool(val.Health))
		builder.WriteString(",")
		builder.WriteString(strconv.Itoa(int(val.Index)))
		builder.WriteString(",")
		builder.WriteString(val.Mode)
		builder.WriteString(OneContainerMultiDeviceSplitSymbol)
		//tmp += val.ID + "," + strconv.FormatInt(int64(val.Count), 10) + "," + strconv.Itoa(int(val.Devmem)) + "," + strconv.Itoa(int(val.Devcore)) + "," + val.Type + "," + strconv.Itoa(val.Numa) + "," + strconv.FormatBool(val.Health) + "," + strconv.Itoa(val.Index) + OneContainerMultiDeviceSplitSymbol
	}
	tmp := builder.String()
	klog.V(5).Infof("Encoded node Devices: %s", tmp)
	return tmp
}

// MarshalNodeDevices will only marshal general information, customInfo is neglected.
func MarshalNodeDevices(dlist []*DeviceInfo) string {
	devAnnos := []*DeviceInfo{}
	for _, val := range dlist {
		devAnnos = append(devAnnos, &DeviceInfo{
			ID:      val.ID,
			Count:   val.Count,
			Devmem:  val.Devmem,
			Devcore: val.Devcore,
			Type:    val.Type,
			Numa:    val.Numa,
			Health:  val.Health,
			Index:   val.Index,
			Mode:    val.Mode,
		})
	}
	data, err := json.Marshal(devAnnos)
	if err != nil {
		return ""
	}
	return string(data)
}

func UnMarshalNodeDevices(str string) ([]*DeviceInfo, error) {
	var dlist []*DeviceInfo
	err := json.Unmarshal([]byte(str), &dlist)
	return dlist, err
}

func EncodeContainerDevices(cd ContainerDevices) string {
	tmp := ""
	for _, val := range cd {
		tmp += val.UUID + "," + val.Type + "," + strconv.Itoa(int(val.Usedmem)) + "," + strconv.Itoa(int(val.Usedcores)) + OneContainerMultiDeviceSplitSymbol
	}
	klog.Infof("Encoded container Devices: %s", tmp)
	return tmp
	//return strings.Join(cd, ",")
}

func EncodeContainerDeviceType(cd ContainerDevices, t string) string {
	tmp := ""
	for _, val := range cd {
		if strings.Compare(val.Type, t) == 0 {
			tmp += val.UUID + "," + val.Type + "," + strconv.Itoa(int(val.Usedmem)) + "," + strconv.Itoa(int(val.Usedcores))
		}
		tmp += OneContainerMultiDeviceSplitSymbol
	}
	klog.Infof("Encoded container Certain Device type: %s->%s", t, tmp)
	return tmp
}

func EncodePodSingleDevice(pd PodSingleDevice) string {
	res := ""
	for _, ctrdevs := range pd {
		res = res + EncodeContainerDevices(ctrdevs)
		res = res + OnePodMultiContainerSplitSymbol
	}
	klog.Infof("Encoded pod single devices %s", res)
	return res
}

func EncodePodDevices(checklist map[string]string, pd PodDevices) map[string]string {
	res := map[string]string{}
	for devType, cd := range pd {
		klog.Infoln("devtype=", devType)
		res[checklist[devType]] = EncodePodSingleDevice(cd)
	}
	klog.Infof("Encoded pod Devices %s\n", res)
	return res
}

func DecodeContainerDevices(str string) (ContainerDevices, error) {
	if len(str) == 0 {
		return ContainerDevices{}, nil
	}
	cd := strings.Split(str, OneContainerMultiDeviceSplitSymbol)
	contdev := ContainerDevices{}
	tmpdev := ContainerDevice{}
	klog.V(5).Infof("Start to decode container device %s", str)
	for _, val := range cd {
		if strings.Contains(val, ",") {
			//fmt.Println("cd is ", val)
			tmpstr := strings.Split(val, ",")
			if len(tmpstr) < 4 {
				return ContainerDevices{}, fmt.Errorf("pod annotation format error; information missing, please do not use nodeName field in task")
			}
			tmpdev.UUID = tmpstr[0]
			tmpdev.Type = tmpstr[1]
			devmem, _ := strconv.ParseInt(tmpstr[2], 10, 32)
			tmpdev.Usedmem = int32(devmem)
			devcores, _ := strconv.ParseInt(tmpstr[3], 10, 32)
			tmpdev.Usedcores = int32(devcores)
			contdev = append(contdev, tmpdev)
		}
	}
	klog.V(5).Infof("Finished decoding container devices. Total devices: %d", len(contdev))
	return contdev, nil
}

func DecodePodDevices(checklist map[string]string, annos map[string]string) (PodDevices, error) {
	klog.V(5).Infof("checklist is [%+v], annos is [%+v]", checklist, annos)
	if len(annos) == 0 {
		return PodDevices{}, nil
	}
	pd := make(PodDevices)
	for devID, devs := range checklist {
		str, ok := annos[devs]
		if !ok {
			continue
		}
		pd[devID] = make(PodSingleDevice, 0)
		for s := range strings.SplitSeq(str, OnePodMultiContainerSplitSymbol) {
			cd, err := DecodeContainerDevices(s)
			if err != nil {
				return PodDevices{}, nil
			}
			if len(cd) == 0 {
				continue
			}
			pd[devID] = append(pd[devID], cd)
		}
	}
	klog.V(5).InfoS("Decoded pod annos", "poddevices", pd)
	return pd, nil
}

func PlatternMIG(n *MigInUse, templates []Geometry, templateIdx int) {
	var err error
	for _, val := range templates[templateIdx] {
		count := 0
		for count < int(val.Count) {
			n.Index, err = safecast.Convert[int32](templateIdx)
			if err != nil {
				continue
			}
			n.UsageList = append(n.UsageList, MigTemplateUsage{
				Name:   val.Name,
				Memory: val.Memory,
				Core:   val.Core,
				InUse:  false,
			})
			count++
		}
	}
}

func GetDevicesUUIDList(infos []*DeviceInfo) []string {
	uuids := make([]string, 0)
	for _, info := range infos {
		uuids = append(uuids, info.ID)
	}
	return uuids
}

func CheckHealth(devType string, node *corev1.Node) (bool, bool) {
	handshake := node.Annotations[util.HandshakeAnnos[devType]]
	if strings.Contains(handshake, "Requesting") {
		formertime, _ := time.Parse(time.DateTime, strings.Split(handshake, "_")[1])
		return time.Now().Before(formertime.Add(time.Second * 60)), false
	} else if strings.Contains(handshake, "Deleted") {
		return true, false
	} else {
		_, ok := util.HandshakeAnnos[devType]
		if ok {
			tmppat := make(map[string]string)
			tmppat[util.HandshakeAnnos[devType]] = "Requesting_" + time.Now().Format(time.DateTime)
			klog.V(5).InfoS("New timestamp for annotation", "nodeName", node.Name, "annotationKey", util.HandshakeAnnos[devType], "annotationValue", tmppat[util.HandshakeAnnos[devType]])
			n, err := util.GetNode(node.Name)
			if err != nil {
				klog.ErrorS(err, "Failed to get node", "nodeName", node.Name)
				return true, false
			}
			klog.V(5).InfoS("Patching node annotations", "nodeName", node.Name, "annotations", tmppat)
			if err := util.PatchNodeAnnotations(n, tmppat); err != nil {
				klog.ErrorS(err, "Failed to patch node annotations", "nodeName", node.Name)
			}
		}
		return true, true
	}
}

// Enhanced ExtractMigTemplatesFromUUID with error handling.
func ExtractMigTemplatesFromUUID(uuid string) (int, int, error) {
	parts := strings.Split(uuid, "[")
	if len(parts) < 2 {
		return -1, -1, fmt.Errorf("invalid UUID format: missing '[' delimiter")
	}

	tmp := parts[1]
	parts = strings.Split(tmp, "]")
	if len(parts) < 2 {
		return -1, -1, fmt.Errorf("invalid UUID format: missing ']' delimiter")
	}

	tmp = parts[0]
	parts = strings.Split(tmp, "-")
	if len(parts) < 2 {
		return -1, -1, fmt.Errorf("invalid UUID format: missing '-' delimiter")
	}

	templateIdx, err := strconv.Atoi(parts[0])
	if err != nil {
		return -1, -1, fmt.Errorf("invalid template index: %v", err)
	}

	pos, err := strconv.Atoi(parts[1])
	if err != nil {
		return -1, -1, fmt.Errorf("invalid position: %v", err)
	}

	return templateIdx, pos, nil
}

func Resourcereqs(pod *corev1.Pod) (counts PodDeviceRequests) {
	counts = make(PodDeviceRequests, len(pod.Spec.Containers))
	klog.V(4).InfoS("Processing resource requirements",
		"pod", klog.KObj(pod),
		"containerCount", len(pod.Spec.Containers))
	//Count Nvidia GPU
	cnt := int32(0)
	for i := range pod.Spec.Containers {
		devices := GetDevices()
		counts[i] = make(ContainerDeviceRequests)
		klog.V(5).InfoS("Processing container resources",
			"pod", klog.KObj(pod),
			"containerIndex", i,
			"containerName", pod.Spec.Containers[i].Name)
		for idx, val := range devices {
			request := val.GenerateResourceRequests(&pod.Spec.Containers[i])
			if request.Nums > 0 {
				cnt += request.Nums
				counts[i][idx] = request
			}
		}
	}
	if cnt == 0 {
		klog.V(4).InfoS("No device requests found", "pod", klog.KObj(pod))
	} else {
		klog.V(4).InfoS("Resource requirements collected", "pod", klog.KObj(pod), "requests", counts)
	}
	return counts
}

func CheckUUID(annos map[string]string, id, useKey, noUseKey, deviceType string) bool {
	userUUID, ok := annos[useKey]
	if ok {
		klog.V(5).Infof("check uuid for %s user uuid [%s], device id is %s", deviceType, userUUID, id)
		// use , symbol to connect multiple uuid
		userUUIDs := strings.Split(userUUID, ",")
		return slices.Contains(userUUIDs, id)
	}

	noUserUUID, ok := annos[noUseKey]
	if ok {
		klog.V(5).Infof("check uuid for %s not user uuid [%s], device id is %s", deviceType, noUserUUID, id)
		// use , symbol to connect multiple uuid
		noUserUUIDs := strings.Split(noUserUUID, ",")
		return !slices.Contains(noUserUUIDs, id)
	}
	return true
}
