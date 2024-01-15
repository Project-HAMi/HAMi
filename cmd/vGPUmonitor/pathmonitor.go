package main

import (
	"context"
	"errors"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	vGPUmonitor "4pd.io/k8s-vgpu/cmd/vGPUmonitor/noderpc"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type podusage struct {
	idstr string
	sr    *sharedRegionT
}

var (
	containerPath string
	lock          sync.Mutex
)

func init() {
	containerPath = getContainerPath()
	if containerPath == "" {
		klog.Fatal("containerPath not set")
	}
}

func getContainerPath() string {
	hookPath := os.Getenv("HOOK_PATH")
	if hookPath == "" {
		klog.Fatal("HOOK_PATH not set")
		return ""
	}
	return hookPath + "/containers"
}

func checkfiles(fpath string) (*sharedRegionT, error) {
	klog.Infof("Checking path %s", fpath)
	files, err := ioutil.ReadDir(fpath)
	if err != nil {
		return nil, err
	}
	if len(files) > 2 {
		return nil, errors.New("cache num not matched")
	}
	if len(files) == 0 {
		return nil, nil
	}
	for _, val := range files {
		if strings.Contains(val.Name(), "libvgpu.so") {
			continue
		}
		if !strings.Contains(val.Name(), ".cache") {
			continue
		}
		cachefile := fpath + "/" + val.Name()
		nc := nvidiaCollector{
			cudevshrPath: cachefile,
			at:           nil,
		}
		sr, err := getvGPUMemoryInfo(&nc)
		if err != nil {
			klog.Infof("getvGPUMemoryInfo failed %s", err.Error())
		} else {
			klog.Infof("getvGPUMemoryInfo sr=%v", sr)
			return sr, nil
		}
	}
	return nil, nil
}

func checkpodvalid(name string, pods *v1.PodList) bool {
	for _, val := range pods.Items {
		if strings.Contains(name, string(val.UID)) {
			return true
		}
	}
	return false
}

func monitorPath(podmap map[string]podusage) error {
	lock.Lock()
	defer lock.Unlock()
	files, err := ioutil.ReadDir(getContainerPath())
	if err != nil {
		return err
	}
	pods, err := clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil
	}
	for _, val := range files {
		dirname := getContainerPath() + "/" + val.Name()
		info, err1 := os.Stat(dirname)
		if err1 != nil || !checkpodvalid(info.Name(), pods) {
			if info.ModTime().Add(time.Second * 300).Before(time.Now()) {
				klog.Infof("Removing %s", dirname)
				//syscall.Munmap(unsafe.Pointer(podmap[dirname].sr))
				delete(podmap, dirname)
				err2 := os.RemoveAll(dirname)
				if err2 != nil {
					return err2
				}
			}
		} else {
			_, ok := podmap[dirname]
			if !ok {
				klog.Infof("Adding ctr %s", dirname)
				sr, err2 := checkfiles(dirname)
				if err2 != nil {
					klog.Infof("checkfiles failed %s", err2.Error())
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
			}
		}
	}
	return nil
}

type server struct {
	vGPUmonitor.UnimplementedNodeVGPUInfoServer
}

func serveinfo(ch chan error) {
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", ":9395")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	vGPUmonitor.RegisterNodeVGPUInfoServer(s, &server{})
	klog.Infof("server listening at", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
