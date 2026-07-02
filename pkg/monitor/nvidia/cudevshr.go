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

package nvidia

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Project-HAMi/HAMi/pkg/monitor/nvidia/api"
	"github.com/Project-HAMi/HAMi/pkg/util"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const SharedRegionMagicFlag = 19920718

type HeaderT = api.Header
type UsageInfo = api.UsageInfo

type ContainerUsage struct {
	PodUID        string
	ContainerName string
	data          []byte
	Info          UsageInfo
}

type ContainerLister struct {
	containerPath string
	containers    map[string]*ContainerUsage
	mutex         sync.Mutex
	clientset     *kubernetes.Clientset
	nodeName      string

	// Fields for the informer-based pod cache mechanism
	informerFactory informers.SharedInformerFactory
	podInformer     cache.SharedIndexInformer
	podLister       corelisters.PodLister
	podListerSynced cache.InformerSynced
	stopCh          chan struct{}
}

var resyncInterval = 5 * time.Minute

func init() {
	if os.Getenv("HAMI_RESYNC_INTERVAL") != "" {
		// If HAMI_RESYNC_INTERVAL is set, parse it
		if interval, err := time.ParseDuration(os.Getenv("HAMI_RESYNC_INTERVAL")); err == nil {
			resyncInterval = interval
		} else {
			klog.Warningf("Invalid HAMI_RESYNC_INTERVAL value: %s, using default %v", os.Getenv("HAMI_RESYNC_INTERVAL"), resyncInterval)
		}
	}
}

func NewContainerLister() (*ContainerLister, error) {
	ensureBuiltinsRegistered()
	hookPath, ok := os.LookupEnv("HOOK_PATH")
	if !ok {
		return nil, fmt.Errorf("HOOK_PATH not set")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		klog.Errorf("Failed to build kubeconfig: %v", err)
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Errorf("Failed to build clientset: %v", err)
		return nil, err
	}

	nodeName := os.Getenv(util.NodeNameEnvName)
	if nodeName == "" {
		return nil, fmt.Errorf("env %s not set", util.NodeNameEnvName)
	}

	lister := &ContainerLister{
		containerPath: filepath.Join(hookPath, "containers"),
		containers:    make(map[string]*ContainerUsage),
		clientset:     clientset,
		nodeName:      nodeName,
		stopCh:        make(chan struct{}),
	}

	// Initialize the informer
	if err := lister.initInformerWithConfig(resyncInterval); err != nil {
		return nil, err
	}

	return lister, nil
}

func (l *ContainerLister) Lock() {
	l.mutex.Lock()
}

func (l *ContainerLister) UnLock() {
	l.mutex.Unlock()
}

func (l *ContainerLister) ListContainers() map[string]*ContainerUsage {
	return l.containers
}

func (l *ContainerLister) Clientset() *kubernetes.Clientset {
	return l.clientset
}

func (l *ContainerLister) Update() error {

	l.mutex.Lock()
	defer l.mutex.Unlock()

	entries, err := os.ReadDir(l.containerPath)
	if err != nil {
		return err
	}

	pods, err := l.podLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	podUIDs := make(map[string]bool, len(pods))
	for _, pod := range pods {
		podUIDs[string(pod.UID)] = true
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := filepath.Join(l.containerPath, entry.Name())
		podUID := strings.Split(entry.Name(), "_")[0]
		if !podUIDs[podUID] {
			dirInfo, err := os.Stat(dirName)
			if err == nil && dirInfo.ModTime().Add(resyncInterval).After(time.Now()) {
				continue
			}
			klog.Infof("Removing dirname %s in monitorpath", dirName)
			if c, ok := l.containers[entry.Name()]; ok {
				syscall.Munmap(c.data)
				delete(l.containers, entry.Name())
			}
			_ = os.RemoveAll(dirName)
			continue
		}
		if _, ok := l.containers[entry.Name()]; ok {
			continue
		}
		usage, err := loadCache(dirName)
		if err != nil {
			klog.Errorf("Failed to load cache: %s, error: %v", dirName, err)
			continue
		}
		if usage == nil {
			// no cuInit in container
			continue
		}
		usage.PodUID = podUID
		usage.ContainerName = strings.Split(entry.Name(), "_")[1]
		l.containers[entry.Name()] = usage
		klog.Infof("Adding ctr dirname %s in monitorpath", dirName)
	}
	return nil
}

func loadCache(fpath string) (*ContainerUsage, error) {
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
	cacheFile := ""
	for _, val := range files {
		if strings.Contains(val.Name(), "libvgpu.so") {
			continue
		}
		if !strings.Contains(val.Name(), ".cache") {
			continue
		}
		cacheFile = filepath.Join(fpath, val.Name())
		break
	}
	if cacheFile == "" {
		klog.Infof("No cache file in %s", fpath)
		return nil, nil
	}
	info, err := os.Stat(cacheFile)
	if err != nil {
		klog.Errorf("Failed to stat cache file: %s, error: %v", cacheFile, err)
		return nil, err
	}
	if info.Size() < int64(unsafe.Sizeof(HeaderT{})) {
		return nil, fmt.Errorf("cache file size %d too small", info.Size())
	}
	f, err := os.OpenFile(cacheFile, os.O_RDWR, 0666)
	if err != nil {
		klog.Errorf("Failed to open cache file: %s, error: %v", cacheFile, err)
		return nil, err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)
	usage := &ContainerUsage{}
	usage.data, err = syscall.Mmap(int(f.Fd()), 0, int(info.Size()), syscall.PROT_WRITE|syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		klog.Errorf("Failed to mmap cache file: %s, error: %v", cacheFile, err)
		return nil, err
	}
	head := (*HeaderT)(unsafe.Pointer(&usage.data[0]))
	if head.InitializedFlag != SharedRegionMagicFlag {
		_ = syscall.Munmap(usage.data)
		return nil, fmt.Errorf("cache file magic flag not matched")
	}
	factory := findFactory(head, info.Size())
	if factory == nil {
		majorVersion := head.MajorVersion
		minorVersion := head.MinorVersion
		_ = syscall.Munmap(usage.data)
		return nil, fmt.Errorf("unknown cache file size %d version %d.%d", info.Size(), majorVersion, minorVersion)
	}
	klog.Infof("casting......%s", factory.Name())
	usage.Info = factory.Cast(usage.data)
	return usage, nil
}

func (l *ContainerLister) initInformerWithConfig(resyncInterval time.Duration) error {
	// Create informer factory with a longer resync period to reduce API calls
	l.informerFactory = informers.NewSharedInformerFactoryWithOptions(
		l.clientset,
		resyncInterval,
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.FieldSelector = fmt.Sprintf("spec.nodeName=%s", l.nodeName)
		}),
	)

	podInformer := l.informerFactory.Core().V1().Pods()
	l.podInformer = podInformer.Informer()
	l.podLister = podInformer.Lister()
	l.podListerSynced = l.podInformer.HasSynced

	l.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: l.onPodDelete,
	})

	// Start the informer
	l.informerFactory.Start(l.stopCh)

	// Wait for cache sync
	if !cache.WaitForCacheSync(l.stopCh, l.podListerSynced) {
		return fmt.Errorf("failed to sync pod informer cache")
	}

	klog.Info("Pod informer started successfully")
	return nil
}

func (l *ContainerLister) onPodDelete(obj any) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("couldn't get object from tombstone %+v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			klog.Errorf("tombstone contained object that is not a Pod: %+v", obj)
			return
		}
	}
	klog.V(5).Infof("Pod removed: %s/%s", pod.Namespace, pod.Name)
}
