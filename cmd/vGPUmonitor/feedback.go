package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var cgroupDriver int

type hostGPUPid struct {
	hostGPUPid int
	mtime      uint64
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

func getUsedGPUPid() ([]uint, error) {
	tmp := []uint{}
	count, err := nvml.GetDeviceCount()
	if err != nil {
		return []uint{}, err
	}
	for i := uint(0); i < count; i++ {
		device, err := nvml.NewDevice(i)
		if err != nil {
			return []uint{}, err
		}
		ids, _, err := device.GetComputeRunningProcesses()
		if err != nil {
			return []uint{}, err
		}
		tmp = append(tmp, ids...)
	}
	result := make([]uint, 0)
	m := make(map[uint]bool)
	for _, v := range tmp {
		if _, ok := m[v]; !ok {
			result = append(result, v)
			m[v] = true
		}
	}
	sort.Slice(tmp, func(i, j int) bool { return tmp[i] > tmp[j] })
	return result, nil
}

func setHostPid(pod v1.Pod, ctr v1.ContainerStatus, sr *podusage) error {
	var pids []string
	if cgroupDriver == 0 {
		cgroupDriver = setcGgroupDriver()
	}
	if cgroupDriver == 0 {
		return errors.New("can not identify cgroup driver")
	}
	usedGPUArray, err := getUsedGPUPid()
	if err != nil {
		return err
	}
	if len(usedGPUArray) == 0 {
		return nil
	}
	qos := strings.ToLower(string(pod.Status.QOSClass))
	var filename string
	if cgroupDriver == 1 {
		/* Cgroupfs */
		filename = fmt.Sprintf("/sysinfo/fs/cgroup/memory/kubepods/%s/pod%s/%s/tasks", qos, pod.UID, strings.TrimLeft(ctr.ContainerID, "docker://"))
	}
	if cgroupDriver == 2 {
		/* Systemd */
		cgroupuid := strings.ReplaceAll(string(pod.UID), "-", "_")
		filename = fmt.Sprintf("/sysinfo/fs/cgroup/systemd/kubepods.slice/kubepods-%s.slice/kubepods-%s-pod%s.slice/docker-%s.scope/tasks", qos, qos, cgroupuid, strings.TrimLeft(ctr.ContainerID, "docker://"))
	}
	fmt.Println("filename=", filename)
	content, err := os.ReadFile(filename)
	if err != nil {
		return err
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
	fmt.Println("usedHostGPUArray=", usedGPUHostArray)
	sort.Slice(usedGPUHostArray, func(i, j int) bool { return usedGPUHostArray[i].mtime > usedGPUHostArray[j].mtime })
	for idx, val := range sr.sr.procs {
		fmt.Println("pid=", val.pid)
		if val.pid == 0 {
			break
		}
		if val.hostpid == 0 {
			fmt.Println("Assign host pid to pid", usedGPUHostArray[idx].hostGPUPid, val.pid)
			sr.sr.procs[idx].hostpid = int32(usedGPUHostArray[idx].hostGPUPid)
			fmt.Println("val=", val.hostpid, sr.sr.procs[idx].hostpid)
		}
	}
	return nil

}

func watchAndFeedback() {
	for {
		time.Sleep(time.Second * 5)
		//fmt.Println("watchAndFeedback", srlist)
		pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			fmt.Println("err=", err.Error())
		}
		for _, val := range pods.Items {
			for idx, _ := range srlist {
				pod_uid := strings.Split(srlist[idx].idstr, "_")[0]
				ctr_name := strings.Split(srlist[idx].idstr, "_")[1]
				if strings.Compare(string(val.UID), pod_uid) == 0 {
					for ctridx, ctr := range val.Spec.Containers {
						if strings.Compare(ctr.Name, ctr_name) == 0 {
							setHostPid(val, val.Status.ContainerStatuses[ctridx], &srlist[idx])
						}
					}
				}
			}
		}
	}
}
