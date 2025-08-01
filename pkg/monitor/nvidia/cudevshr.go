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

    v0 "github.com/Project-HAMi/HAMi/pkg/monitor/nvidia/v0"
    v1 "github.com/Project-HAMi/HAMi/pkg/monitor/nvidia/v1"
    "github.com/Project-HAMi/HAMi/pkg/util"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/informers"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/cache"
    "k8s.io/client-go/tools/clientcmd"
    "k8s.io/klog/v2"
)

const SharedRegionMagicFlag = 19920718

type headerT struct {
    initializedFlag int32
    majorVersion    int32
    minorVersion    int32
}

type UsageInfo interface {
    DeviceMax() int
    DeviceNum() int
    DeviceMemoryContextSize(idx int) uint64
    DeviceMemoryModuleSize(idx int) uint64
    DeviceMemoryBufferSize(idx int) uint64
    DeviceMemoryOffset(idx int) uint64
    DeviceMemoryTotal(idx int) uint64
    DeviceSmUtil(idx int) uint64
    SetDeviceSmLimit(l uint64)
    IsValidUUID(idx int) bool
    DeviceUUID(idx int) string
    DeviceMemoryLimit(idx int) uint64
    SetDeviceMemoryLimit(l uint64)
    LastKernelTime() int64
    //UsedMemory(idx int) (uint64, error)
    GetPriority() int
    GetRecentKernel() int32
    SetRecentKernel(v int32)
    GetUtilizationSwitch() int32
    SetUtilizationSwitch(v int32)
}

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
    
    // Fields for the informer-based pod cache mechanism
    informerFactory informers.SharedInformerFactory
    podInformer     cache.SharedIndexInformer
    stopCh          chan struct{}
    
    // Cache related fields
    podCache       map[string]*corev1.Pod
    podCacheMutex  sync.RWMutex
    lastUpdateTime time.Time
    updateInterval time.Duration
}

var resyncInterval time.Duration=5 * time.Minute
func init() {

	if os.Getenv("HAMI_RESYNC_INTERVAL") != "" {
		// If RESYNC_INTERVAL is set, parse it
		if interval, err := time.ParseDuration(os.Getenv("HAMI_RESYNC_INTERVAL")); err == nil {
			resyncInterval = interval
		} else {
			klog.Warningf("Invalid RESYNC_INTERVAL value: %s, using default %v", os.Getenv("RESYNC_INTERVAL"), resyncInterval)
		}
	}
}
 
func NewContainerLister() (*ContainerLister, error) {
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
    
    lister := &ContainerLister{
        containerPath: filepath.Join(hookPath, "containers"),
        containers:    make(map[string]*ContainerUsage),
        clientset:     clientset,
        stopCh:        make(chan struct{}),
        podCache:      make(map[string]*corev1.Pod),
        updateInterval: 30 * time.Second, // Default 30 seconds update interval
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
    // Check if an update is needed based on time interval
    now := time.Now()
    if now.Sub(l.lastUpdateTime) < l.updateInterval {
        // Skip update if not enough time has passed since last update
        return nil
    }
    l.lastUpdateTime = now
    
    l.mutex.Lock()
    defer l.mutex.Unlock()
    
    entries, err := os.ReadDir(l.containerPath)
    if err != nil {
        return err
    }
    
    // Use cached pod information instead of making API calls
    l.podCacheMutex.RLock()
    podList := make([]*corev1.Pod, 0, len(l.podCache))
    for _, pod := range l.podCache {
        podList = append(podList, pod)
    }
    l.podCacheMutex.RUnlock()
    
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        
        dirName := filepath.Join(l.containerPath, entry.Name())
        if !l.isValidPodWithCache(entry.Name(), podList) {
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
        usage.PodUID = strings.Split(entry.Name(), "_")[0]
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
    if info.Size() < int64(unsafe.Sizeof(headerT{})) {
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
    head := (*headerT)(unsafe.Pointer(&usage.data[0]))
    if head.initializedFlag != SharedRegionMagicFlag {
        _ = syscall.Munmap(usage.data)
        return nil, fmt.Errorf("cache file magic flag not matched")
    }
    if info.Size() == 1197897 {
		klog.Infoln("casting......v0")
        usage.Info = v0.CastSpec(usage.data)
    } else if head.majorVersion == 1 {
		klog.Infoln("casting......v1")
        usage.Info = v1.CastSpec(usage.data)
    } else {
        _ = syscall.Munmap(usage.data)
        return nil, fmt.Errorf("unknown cache file size %d version %d.%d", info.Size(), head.majorVersion, head.minorVersion)
    }
    return usage, nil
}

// Initialize the informer with the specified resync interval
func (l *ContainerLister) initInformerWithConfig(resyncInterval time.Duration) error {
    nodename := os.Getenv(util.NodeNameEnvName)
    if nodename == "" {
        return fmt.Errorf("env %s not set", util.NodeNameEnvName)
    }
    
    // Create informer factory with a longer resync period to reduce API calls
    l.informerFactory = informers.NewSharedInformerFactoryWithOptions(
        l.clientset,
        resyncInterval,
        informers.WithTweakListOptions(func(options *metav1.ListOptions) {
            options.FieldSelector = fmt.Sprintf("spec.nodeName=%s", nodename)
        }),
    )
    
    l.podInformer = l.informerFactory.Core().V1().Pods().Informer()
    
    // Add event handlers
    l.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
        AddFunc:    l.onPodAdd,
        UpdateFunc: l.onPodUpdate,
        DeleteFunc: l.onPodDelete,
    })
    
    // Start the informer
    l.informerFactory.Start(l.stopCh)
    
    // Wait for cache sync
    if !cache.WaitForCacheSync(l.stopCh, l.podInformer.HasSynced) {
        return fmt.Errorf("failed to sync pod informer cache")
    }
    
    klog.Info("Pod informer started successfully")
    return nil
}

// Handle pod addition events
func (l *ContainerLister) onPodAdd(obj interface{}) {
    pod, ok := obj.(*corev1.Pod)
    if !ok {
        return
    }
    
    l.podCacheMutex.Lock()
    l.podCache[string(pod.UID)] = pod
    l.podCacheMutex.Unlock()
    
    klog.V(4).Infof("Pod added to cache: %s/%s", pod.Namespace, pod.Name)
}

// Handle pod update events
func (l *ContainerLister) onPodUpdate(oldObj, newObj interface{}) {
    oldPod, ok := oldObj.(*corev1.Pod)
    if !ok {
        return
    }
    newPod, ok := newObj.(*corev1.Pod)
    if !ok {
        return
    }
    
    // Only update cache when pod phase changes
    if oldPod.Status.Phase != newPod.Status.Phase {
        l.podCacheMutex.Lock()
        l.podCache[string(newPod.UID)] = newPod
        l.podCacheMutex.Unlock()
        
        klog.V(4).Infof("Pod status updated in cache: %s/%s, phase: %s -> %s", 
            newPod.Namespace, newPod.Name, oldPod.Status.Phase, newPod.Status.Phase)
    }
}

// Handle pod deletion events
func (l *ContainerLister) onPodDelete(obj interface{}) {
    pod, ok := obj.(*corev1.Pod)
    if !ok {
        return
    }
    
    l.podCacheMutex.Lock()
    delete(l.podCache, string(pod.UID))
    l.podCacheMutex.Unlock()
    
    klog.V(4).Infof("Pod removed from cache: %s/%s", pod.Namespace, pod.Name)
}

// Check if a pod is valid using cached pod information
func (l *ContainerLister) isValidPodWithCache(name string, pods []*corev1.Pod) bool {
    for _, pod := range pods {
        if strings.Contains(name, string(pod.UID)) {
            return true
        }
    }
    return false
}

// Set the update interval for container list refresh
func (l *ContainerLister) SetUpdateInterval(interval time.Duration) {
    l.updateInterval = interval
}

// Stop the informer and clean up resources
func (l *ContainerLister) Stop() {
    close(l.stopCh)
    if l.informerFactory != nil {
        l.informerFactory.Shutdown()
    }
}