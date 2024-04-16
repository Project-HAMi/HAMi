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

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	vGPUmonitor "github.com/Project-HAMi/HAMi/cmd/vGPUmonitor/noderpc"

	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
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
	hookPath, ok := os.LookupEnv("HOOK_PATH")
	if ok {
		containerPath = filepath.Join(hookPath, "containers")
	}
}

func checkfiles(fpath string) (*sharedRegionT, error) {
	klog.Infof("Checking path %s", fpath)
	files, err := os.ReadDir(fpath)
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
			klog.Errorf("getvGPUMemoryInfo failed: %v", err)
		} else {
			klog.Infof("getvGPUMemoryInfo success with utilizationSwitch=%d, recentKernel=%d, priority=%d", sr.utilizationSwitch, sr.recentKernel, sr.priority)
			return sr, nil
		}
	}
	return nil, nil
}

func isVaildPod(name string, pods *corev1.PodList) bool {
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
	files, err := os.ReadDir(containerPath)
	if err != nil {
		return err
	}
	pods, err := clientset.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil
	}
	for _, val := range files {
		dirname := containerPath + "/" + val.Name()
		if info, err1 := os.Stat(dirname); err1 != nil || !isVaildPod(info.Name(), pods) {
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
			}
		}
	}
	return nil
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
