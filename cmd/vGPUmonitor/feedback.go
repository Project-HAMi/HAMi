package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	v1 "k8s.io/api/core/v1"
)

var cgroupDriver int

type hostGPUPid struct {
	hostGPUPid int
	mtime      uint64
}

type UtilizationPerDevice []int

var mutex sync.Mutex
var srPodList map[string]podusage

func init() {
	srPodList = make(map[string]podusage)
}

func setcGgroupDriver() int {
	// 1 for cgroupfs 2 for systemd
	kubeletconfig, err := ioutil.ReadFile("/hostvar/lib/kubelet/config.yaml")
	if err != nil {
		return 0
	}
	content := string(kubeletconfig)
	pos := strings.LastIndex(content, "cgroupDriver:")
	if pos < 0 {
		return 0
	}
	if strings.Contains(content, "systemd") {
		return 2
	}
	if strings.Contains(content, "cgroupfs") {
		return 1
	}
	return 0
}

func getUsedGPUPid() ([]uint, nvml.Return) {
	tmp := []nvml.ProcessInfo{}
	count, err := nvml.DeviceGetCount()
	if err != nvml.SUCCESS {
		return []uint{}, err
	}
	for i := 0; i < count; i++ {
		device, err := nvml.DeviceGetHandleByIndex(i)
		if err != nvml.SUCCESS {
			return []uint{}, err
		}
		ids, err := device.GetComputeRunningProcesses()
		if err != nvml.SUCCESS {
			return []uint{}, err
		}
		tmp = append(tmp, ids...)
	}
	result := make([]uint, 0)
	m := make(map[uint]bool)
	for _, v := range tmp {
		if _, ok := m[uint(v.Pid)]; !ok {
			result = append(result, uint(v.Pid))
			m[uint(v.Pid)] = true
		}
	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].Pid > tmp[j].Pid })
	return result, nvml.SUCCESS
}

func setHostPid(pod v1.Pod, ctr v1.ContainerStatus, sr *podusage) error {
	var pids []string
	mutex.Lock()
	defer mutex.Unlock()

	if cgroupDriver == 0 {
		cgroupDriver = setcGgroupDriver()
	}
	if cgroupDriver == 0 {
		return errors.New("can not identify cgroup driver")
	}
	usedGPUArray, err := getUsedGPUPid()
	if err != nvml.SUCCESS {
		return errors.New("get usedGPUID failed, ret:" + nvml.ErrorString(err))
	}
	if len(usedGPUArray) == 0 {
		return nil
	}
	qos := strings.ToLower(string(pod.Status.QOSClass))
	var filename string
	if cgroupDriver == 1 {
		/* Cgroupfs */
		filename = fmt.Sprintf("/sysinfo/fs/cgroup/memory/kubepods/%s/pod%s/%s/tasks", qos, pod.UID, strings.TrimPrefix(ctr.ContainerID, "docker://"))
	}
	if cgroupDriver == 2 {
		/* Systemd */
		cgroupuid := strings.ReplaceAll(string(pod.UID), "-", "_")
		filename = fmt.Sprintf("/sysinfo/fs/cgroup/systemd/kubepods.slice/kubepods-%s.slice/kubepods-%s-pod%s.slice/docker-%s.scope/tasks", qos, qos, cgroupuid, strings.TrimPrefix(ctr.ContainerID, "docker://"))
	}
	fmt.Println("filename=", filename)
	content, ferr := os.ReadFile(filename)
	if ferr != nil {
		return ferr
	}
	pids = strings.Split(string(content), "\n")
	hostPidArray := []hostGPUPid{}
	for _, val := range pids {
		tmp, _ := strconv.Atoi(val)
		if tmp != 0 {
			var stat os.FileInfo
			var err error
			if stat, err = os.Lstat(fmt.Sprintf("/proc/%v", tmp)); err != nil {
				return err
			}
			mtime := stat.ModTime().Unix()
			hostPidArray = append(hostPidArray, hostGPUPid{
				hostGPUPid: tmp,
				mtime:      uint64(mtime),
			})
		}
	}
	usedGPUHostArray := []hostGPUPid{}
	for _, val := range usedGPUArray {
		for _, hostpid := range hostPidArray {
			if uint(hostpid.hostGPUPid) == val {
				usedGPUHostArray = append(usedGPUHostArray, hostpid)
			}
		}
	}
	//fmt.Println("usedHostGPUArray=", usedGPUHostArray)
	sort.Slice(usedGPUHostArray, func(i, j int) bool { return usedGPUHostArray[i].mtime > usedGPUHostArray[j].mtime })
	if sr == nil || sr.sr == nil {
		return nil
	}
	for idx, val := range sr.sr.procs {
		//fmt.Println("pid=", val.pid)
		if val.pid == 0 {
			break
		}
		if idx < len(usedGPUHostArray) {
			if val.hostpid == 0 || val.hostpid != int32(usedGPUHostArray[idx].hostGPUPid) {
				fmt.Println("Assign host pid to pid instead", usedGPUHostArray[idx].hostGPUPid, val.pid, val.hostpid)
				sr.sr.procs[idx].hostpid = int32(usedGPUHostArray[idx].hostGPUPid)
				fmt.Println("val=", val.hostpid, sr.sr.procs[idx].hostpid)
			}
		}
	}
	return nil

}

func CheckBlocking(utSwitchOn map[string]UtilizationPerDevice, p int, pu podusage) bool {
	for _, devuuid := range pu.sr.uuids {
		_, ok := utSwitchOn[string(devuuid.uuid[:])]
		if ok {
			for i := 0; i < p; i++ {
				if utSwitchOn[string(devuuid.uuid[:])][i] > 0 {
					return true
				}
			}
			return false
		}
	}
	return false
}

// Check whether task with higher priority use GPU or there are other tasks with the same priority
func CheckPriority(utSwitchOn map[string]UtilizationPerDevice, p int, pu podusage) bool {
	for _, devuuid := range pu.sr.uuids {
		_, ok := utSwitchOn[string(devuuid.uuid[:])]
		if ok {
			for i := 0; i < p; i++ {
				if utSwitchOn[string(devuuid.uuid[:])][i] > 0 {
					return true
				}
			}
			if utSwitchOn[string(devuuid.uuid[:])][p] > 1 {
				return true
			}
		}
	}
	return false
}

func Observe(srlist *map[string]podusage) error {
	utSwitchOn := map[string]UtilizationPerDevice{}

	for idx, val := range *srlist {
		if val.sr == nil {
			continue
		}
		/*for ii, _ := range val.sr.uuids {
			fmt.Println("using uuid=", string(val.sr.uuids[ii].uuid[:]))
		}*/
		if val.sr.recentKernel > 0 {
			(*srlist)[idx].sr.recentKernel--
			if (*srlist)[idx].sr.recentKernel > 0 {
				for _, devuuid := range val.sr.uuids {
					// Null device condition
					if devuuid.uuid[0] == 0 {
						continue
					}
					if len(utSwitchOn[string(devuuid.uuid[:])]) == 0 {
						utSwitchOn[string(devuuid.uuid[:])] = []int{0, 0}
					}
					utSwitchOn[string(devuuid.uuid[:])][val.sr.priority]++
				}
			}
		}
	}
	for idx, val := range *srlist {
		if val.sr == nil {
			continue
		}
		if CheckBlocking(utSwitchOn, int(val.sr.priority), val) {
			if (*srlist)[idx].sr.recentKernel >= 0 {
				fmt.Println("utSwitchon=", utSwitchOn)
				fmt.Println("Setting Blocking to on", idx)
				(*srlist)[idx].sr.recentKernel = -1
			}
		} else {
			if (*srlist)[idx].sr.recentKernel < 0 {
				fmt.Println("utSwitchon=", utSwitchOn)
				fmt.Println("Setting Blocking to off", idx)
				(*srlist)[idx].sr.recentKernel = 0
			}
		}
		if CheckPriority(utSwitchOn, int(val.sr.priority), val) {
			if (*srlist)[idx].sr.utilizationSwitch != 1 {
				fmt.Println("utSwitchon=", utSwitchOn)
				fmt.Println("Setting UtilizationSwitch to on", idx)
				(*srlist)[idx].sr.utilizationSwitch = 1
			}
		} else {
			if (*srlist)[idx].sr.utilizationSwitch != 0 {
				fmt.Println("utSwitchon=", utSwitchOn)
				fmt.Println("Setting UtilizationSwitch to off", idx)
				(*srlist)[idx].sr.utilizationSwitch = 0
			}
		}
	}
	return nil
}

func watchAndFeedback() {
	nvml.Init()
	for {
		time.Sleep(time.Second * 5)
		err := monitorpath(srPodList)
		if err != nil {
			fmt.Println("monitorPath failed", err.Error())
		}
		//fmt.Println("watchAndFeedback", srPodList)
		Observe(&srPodList)

	}
}
