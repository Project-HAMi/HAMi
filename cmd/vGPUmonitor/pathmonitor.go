package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"

	vGPUmonitor "4pd.io/k8s-vgpu/cmd/vGPUmonitor/noderpc"
	"google.golang.org/grpc"
<<<<<<< HEAD
)

const containerpath = "/tmp/vgpu/containers"

=======
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

>>>>>>> c7a3893 (Remake this repo to HAMi)
type podusage struct {
	idstr string
	sr    sharedRegionT
}

<<<<<<< HEAD
func checkfiles(fpath string) (sharedRegionT, error) {
	fmt.Println("Checking path", fpath)
=======
var (
	containerPath string
	lock          sync.Mutex
)

func init() {
	hookPath, ok := os.LookupEnv("HOOK_PATH")
	if ok {
		containerPath = filepath.Join(hookPath, "containers")
	}
}

func checkfiles(fpath string) (*sharedRegionT, error) {
	klog.Infof("Checking path %s", fpath)
>>>>>>> c7a3893 (Remake this repo to HAMi)
	files, err := ioutil.ReadDir(fpath)
	if err != nil {
		return sharedRegionT{}, err
	}
	if len(files) > 1 {
		return sharedRegionT{}, errors.New("cache num not matched")
	}
	if len(files) == 0 {
		return sharedRegionT{}, nil
	}
	for _, val := range files {
		strings.Contains(val.Name(), ".cache")
		cachefile := fpath + "/" + val.Name()
		nc := nvidiaCollector{
			cudevshrPath: cachefile,
			at:           nil,
		}
		sr, err := getvGPUMemoryInfo(&nc)
		if err != nil {
			klog.Errorf("getvGPUMemoryInfo failed: %v", err)
		} else {
<<<<<<< HEAD
			fmt.Println(sr)
=======
			klog.Infof("getvGPUMemoryInfo success with utilizationSwitch=%d, recentKernel=%d, priority=%d", sr.utilizationSwitch, sr.recentKernel, sr.priority)
>>>>>>> c7a3893 (Remake this repo to HAMi)
			return sr, nil
		}
	}
	return sharedRegionT{}, nil
}

<<<<<<< HEAD
func monitorpath() ([]podusage, error) {
	srlist := []podusage{}
	files, err := ioutil.ReadDir(containerpath)
=======
func isVaildPod(name string, pods *v1.PodList) bool {
	for _, val := range pods.Items {
		if strings.Contains(name, string(val.UID)) {
			return true
		}
	}
	return false
}

func monitorpath(podmap map[string]podusage) error {
	lock.Lock()
	defer lock.Unlock()
	files, err := ioutil.ReadDir(containerPath)
>>>>>>> c7a3893 (Remake this repo to HAMi)
	if err != nil {
		return srlist, err
	}
	for _, val := range files {
<<<<<<< HEAD
		fmt.Println("val=", val.Name())
		dirname := containerpath + "/" + val.Name()
		info, err1 := os.Stat(dirname)
		if err1 != nil {
			fmt.Println("removing" + dirname)
			err2 := os.RemoveAll(dirname)
			if err2 != nil {
				return srlist, err2
			}
		} else {
			fmt.Println(info.IsDir())
			sr, err2 := checkfiles(dirname)
			if err2 != nil {
				return srlist, err2
=======
		dirname := containerPath + "/" + val.Name()
		info, err1 := os.Stat(dirname)
		if err1 != nil || !isVaildPod(info.Name(), pods) {
			if info.ModTime().Add(time.Second * 300).Before(time.Now()) {
				klog.Infof("Removing dirname %s in in monitorpath", dirname)
				//syscall.Munmap(unsafe.Pointer(podmap[dirname].sr))
				delete(podmap, dirname)
				err2 := os.RemoveAll(dirname)
				if err2 != nil {
					klog.Errorf("Failed to remove dirname: %s , error: %v", dirname, err)
					return err2
				}
			}
		} else {
			_, ok := podmap[dirname]
			if !ok {
				klog.Infof("Adding ctr dirname %s in monitorpath", dirname)
				sr, err2 := checkfiles(dirname)
				if err2 != nil {
					klog.Errorf("Failed to checkfiles dirname: %s , error: %v", dirname, err)
					return err2
				}
				if sr == nil {
					/* This container haven't use any gpu-related operations */
					continue
				}
				podmap[dirname] = podusage{
					idstr: val.Name(),
					sr:    sr,
				}
>>>>>>> c7a3893 (Remake this repo to HAMi)
			}
			srlist = append(srlist, podusage{
				idstr: val.Name(),
				sr:    sr,
			})
		}
	}
	return srlist, nil
}

type server struct {
	vGPUmonitor.UnimplementedNodeVGPUInfoServer
}

func serveInfo(ch chan error) {
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", ":9395")
	if err != nil {
		ch <- fmt.Errorf("failed to listen: %v", err)
		// return respect the error, so the goroutine can end
		return
	}
	vGPUmonitor.RegisterNodeVGPUInfoServer(s, &server{})
	klog.Infof("server listening at %v", lis.Addr())
	if err = s.Serve(lis); err != nil {
		ch <- fmt.Errorf("failed to serve: %v", err)
		// return respect the error, so the goroutine can end
		return
	}
}
