/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package device_plugin

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/device-plugin/config"
	"4pd.io/k8s-vgpu/pkg/util"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/klog/v2"

	"github.com/NVIDIA/go-gpuallocator/gpuallocator"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// Constants to represent the various device list strategies
const (
	DeviceListStrategyEnvvar       = "envvar"
	DeviceListStrategyVolumeMounts = "volume-mounts"
)

// Constants to represent the various device id strategies
const (
	DeviceIDStrategyUUID  = "uuid"
	DeviceIDStrategyIndex = "index"
)

// Constants for use by the 'volume-mounts' device list strategy
const (
	deviceListAsVolumeMountsHostPath          = "/dev/null"
	deviceListAsVolumeMountsContainerPathRoot = "/var/run/nvidia-container-devices"
)

// NvidiaDevicePlugin implements the Kubernetes device plugin API
type NvidiaDevicePlugin struct {
	ResourceManager
	//resourceManager  *ResourceManager
	deviceCache      *DeviceCache
	resourceName     string
	deviceListEnvvar string
	allocatePolicy   gpuallocator.Policy
	socket           string

	server        *grpc.Server
	cachedDevices []*Device
	health        chan *Device
	stop          chan interface{}
	changed       chan struct{}
	migStrategy   string
	//devRegister   *DeviceRegister
	//podManager    *PodManager
}

// NewNvidiaDevicePlugin returns an initialized NvidiaDevicePlugin
func NewNvidiaDevicePlugin(resourceName string, deviceCache *DeviceCache, allocatePolicy gpuallocator.Policy, socket string) *NvidiaDevicePlugin {
	return &NvidiaDevicePlugin{
		deviceCache:    deviceCache,
		resourceName:   resourceName,
		allocatePolicy: allocatePolicy,
		socket:         socket,
		migStrategy:    "none",

		// These will be reinitialized every
		// time the plugin server is restarted.
		server: nil,
		health: nil,
		stop:   nil,
	}
}

// NewNvidiaDevicePlugin returns an initialized NvidiaDevicePlugin
func NewMIGNvidiaDevicePlugin(resourceName string, resourceManager ResourceManager, deviceListEnvvar string, allocatePolicy gpuallocator.Policy, socket string) *NvidiaDevicePlugin {
	return &NvidiaDevicePlugin{
		ResourceManager:  resourceManager,
		resourceName:     resourceName,
		deviceListEnvvar: deviceListEnvvar,
		allocatePolicy:   allocatePolicy,
		socket:           socket,

		// These will be reinitialized every
		// time the plugin server is restarted.
		cachedDevices: nil,
		server:        nil,
		health:        nil,
		stop:          nil,
		migStrategy:   "mixed",
	}
}

func (m *NvidiaDevicePlugin) initialize() {
	var err error
	if strings.Compare(m.migStrategy, "mixed") == 0 {
		m.cachedDevices = m.ResourceManager.Devices()
	}
	m.server = grpc.NewServer([]grpc.ServerOption{}...)
	m.health = make(chan *Device)
	m.stop = make(chan interface{})
	check(err)
}

func (m *NvidiaDevicePlugin) cleanup() {
	close(m.stop)
	m.server = nil
	m.health = nil
	m.stop = nil
	//m.podManager = nil
}

// Start starts the gRPC server, registers the device plugin with the Kubelet,
// and starts the device healthchecks.
func (m *NvidiaDevicePlugin) Start() error {
	m.initialize()

	err := m.Serve()
	if err != nil {
		log.Printf("Could not start device plugin for '%s': %s", m.resourceName, err)
		m.cleanup()
		return err
	}
	log.Printf("Starting to serve '%s' on %s", m.resourceName, m.socket)

	err = m.Register()
	if err != nil {
		log.Printf("Could not register device plugin: %s", err)
		m.Stop()
		return err
	}
	log.Printf("Registered device plugin for '%s' with Kubelet", m.resourceName)

	if strings.Compare(m.migStrategy, "none") == 0 {
		m.deviceCache.AddNotifyChannel("plugin", m.health)
	} else if strings.Compare(m.migStrategy, "mixed") == 0 {
		go m.CheckHealth(m.stop, m.cachedDevices, m.health)
	} else {
		log.Panicln("migstrategy not recognized", m.migStrategy)
	}
	return nil
}

// Stop stops the gRPC server.
func (m *NvidiaDevicePlugin) Stop() error {
	if m == nil || m.server == nil {
		return nil
	}
	log.Printf("Stopping to serve '%s' on %s", m.resourceName, m.socket)
	m.deviceCache.RemoveNotifyChannel("plugin")
	m.server.Stop()
	if err := os.Remove(m.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.cleanup()
	return nil
}

// Serve starts the gRPC server of the device plugin.
func (m *NvidiaDevicePlugin) Serve() error {
	os.Remove(m.socket)
	sock, err := net.Listen("unix", m.socket)
	if err != nil {
		return err
	}

	pluginapi.RegisterDevicePluginServer(m.server, m)

	go func() {
		lastCrashTime := time.Now()
		restartCount := 0
		for {
			log.Printf("Starting GRPC server for '%s'", m.resourceName)
			err := m.server.Serve(sock)
			if err == nil {
				break
			}

			log.Printf("GRPC server for '%s' crashed with error: %v", m.resourceName, err)

			// restart if it has not been too often
			// i.e. if server has crashed more than 5 times and it didn't last more than one hour each time
			if restartCount > 5 {
				// quit
				log.Fatalf("GRPC server for '%s' has repeatedly crashed recently. Quitting", m.resourceName)
			}
			timeSinceLastCrash := time.Since(lastCrashTime).Seconds()
			lastCrashTime = time.Now()
			if timeSinceLastCrash > 3600 {
				// it has been one hour since the last crash.. reset the count
				// to reflect on the frequency
				restartCount = 1
			} else {
				restartCount++
			}
		}
	}()

	// Wait for server to start by launching a blocking connexion
	conn, err := m.dial(m.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

// Register registers the device plugin for the given resourceName with Kubelet.
func (m *NvidiaDevicePlugin) Register() error {
	conn, err := m.dial(pluginapi.KubeletSocket, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(m.socket),
		ResourceName: m.resourceName,
		Options: &pluginapi.DevicePluginOptions{
			GetPreferredAllocationAvailable: false,
		},
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

// GetDevicePluginOptions returns the values of the optional settings for this plugin
func (m *NvidiaDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	options := &pluginapi.DevicePluginOptions{
		GetPreferredAllocationAvailable: false,
	}
	return options, nil
}

// ListAndWatch lists devices and update that list according to the health status
func (m *NvidiaDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	_ = s.Send(&pluginapi.ListAndWatchResponse{Devices: m.apiDevices()})
	for {
		select {
		case <-m.stop:
			return nil
		case d := <-m.health:
			// FIXME: there is no way to recover from the Unhealthy state.
			//d.Health = pluginapi.Unhealthy
			log.Printf("'%s' device marked unhealthy: %s", m.resourceName, d.ID)
			_ = s.Send(&pluginapi.ListAndWatchResponse{Devices: m.apiDevices()})
		}
	}
}

// GetPreferredAllocation returns the preferred allocation from the set of devices specified in the request
func (m *NvidiaDevicePlugin) GetPreferredAllocation(ctx context.Context, r *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {

	return &pluginapi.PreferredAllocationResponse{}, nil
}

func (m *NvidiaDevicePlugin) MIGAllocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	responses := pluginapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		for _, id := range req.DevicesIDs {
			if !m.deviceExists(id) {
				return nil, fmt.Errorf("invalid allocation request for '%s': unknown device: %s", m.resourceName, id)
			}
		}

		response := pluginapi.ContainerAllocateResponse{}

		uuids := req.DevicesIDs
		deviceIDs := m.deviceIDsFromUUIDs(uuids)

		//if deviceListStrategyFlag == DeviceListStrategyEnvvar {
		response.Envs = m.apiEnvs(m.deviceListEnvvar, deviceIDs)
		//}
		//if deviceListStrategyFlag == DeviceListStrategyVolumeMounts {
		//	response.Envs = m.apiEnvs(m.deviceListEnvvar, []string{deviceListAsVolumeMountsContainerPathRoot})
		//	response.Mounts = m.apiMounts(deviceIDs)
		//}
		//if passDeviceSpecsFlag {
		//	response.Devices = m.apiDeviceSpecs(nvidiaDriverRootFlag, uuids)
		//}

		klog.Infof("response=", response.Envs)
		responses.ContainerResponses = append(responses.ContainerResponses, &response)
	}

	return &responses, nil
}

// Allocate which return list of devices.
func (m *NvidiaDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	klog.Infoln("Allocate", reqs.ContainerRequests)
	if len(reqs.ContainerRequests) > 1 {
		return &pluginapi.AllocateResponse{}, errors.New("multiple Container Requests not supported")
	}
	if strings.Compare(m.migStrategy, "mixed") == 0 {
		return m.MIGAllocate(ctx, reqs)
	}
	responses := pluginapi.AllocateResponse{}
	nodename := os.Getenv("NODE_NAME")

	current, err := util.GetPendingPod(nodename)
	if err != nil {
		util.ReleaseNodeLock(nodename)
		return &pluginapi.AllocateResponse{}, err
	}

	for idx := range reqs.ContainerRequests {
		devreq, err := util.GetNextDeviceRequest(util.NvidiaGPUDevice, *current)
		klog.Infoln("deviceAllocateFromAnnotation=", devreq)
		if err != nil {
			util.PodAllocationFailed(nodename, current)
			return &pluginapi.AllocateResponse{}, err
		}
		if len(devreq) != len(reqs.ContainerRequests[idx].DevicesIDs) {
			util.PodAllocationFailed(nodename, current)
			return &pluginapi.AllocateResponse{}, errors.New("device number not matched")
		}

		err = util.EraseNextDeviceTypeFromAnnotation(util.NvidiaGPUDevice, *current)
		if err != nil {
			util.PodAllocationFailed(nodename, current)
			return &pluginapi.AllocateResponse{}, err
		}

		response := pluginapi.ContainerAllocateResponse{}
		response.Envs = make(map[string]string)
		for i, dev := range devreq {
			limitKey := fmt.Sprintf("CUDA_DEVICE_MEMORY_LIMIT_%v", i)
			response.Envs[limitKey] = fmt.Sprintf("%vm", dev.Usedmem)
			tmp := response.Envs["NVIDIA_VISIBLE_DEVICES"]
			if i > 0 {
				response.Envs["NVIDIA_VISIBLE_DEVICES"] = fmt.Sprintf("%v,%v", tmp, dev.UUID)
			} else {
				response.Envs["NVIDIA_VISIBLE_DEVICES"] = dev.UUID
			}
		}
		response.Envs["CUDA_DEVICE_SM_LIMIT"] = fmt.Sprint(devreq[0].Usedcores)
		response.Envs["CUDA_DEVICE_MEMORY_SHARED_CACHE"] = fmt.Sprintf("/tmp/%v.cache", uuid.NewUUID())
		if config.DeviceMemoryScaling > 1 {
			response.Envs["CUDA_OVERSUBSCRIBE"] = "true"
		}
		if config.DisableCoreLimit {
			response.Envs[api.CoreLimitSwitch] = "disable"
		}
		response.Mounts = append(response.Mounts,
			&pluginapi.Mount{ContainerPath: "/usr/local/vgpu/libvgpu.so",
				HostPath: "/usr/local/vgpu/libvgpu.so",
				ReadOnly: true},
			&pluginapi.Mount{ContainerPath: "/etc/ld.so.preload",
				HostPath: "/usr/local/vgpu/ld.so.preload",
				ReadOnly: true},
		)
		responses.ContainerResponses = append(responses.ContainerResponses, &response)
	}
	time.Sleep(time.Second * 5)
	klog.Infoln("Allocate Response", responses.ContainerResponses)
	util.PodAllocationTrySuccess(nodename, current)
	return &responses, nil
}

// PreStartContainer is unimplemented for this plugin
func (m *NvidiaDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// dial establishes the gRPC communication with the registered device plugin.
func (m *NvidiaDevicePlugin) dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	c, err := grpc.Dial(unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)

	if err != nil {
		return nil, err
	}

	return c, nil
}

func (m *NvidiaDevicePlugin) Devices() []*Device {
	if strings.Compare(m.migStrategy, "none") == 0 {
		return m.deviceCache.GetCache()
	}
	if strings.Compare(m.migStrategy, "mixed") == 0 {
		return m.ResourceManager.Devices()
	}
	log.Panic("migStrategy not recognized,exiting...")
	return []*Device{}
}

func (m *NvidiaDevicePlugin) deviceExists(id string) bool {
	//for _, d := range m.deviceCache.GetCache() {
	for _, d := range m.cachedDevices {
		if d.ID == id {
			return true
		}
	}
	return false
}

func (m *NvidiaDevicePlugin) deviceIDsFromUUIDs(uuids []string) []string {
	return uuids
	/*
		if deviceIDStrategyFlag == DeviceIDStrategyUUID {
			return uuids
		}

		var deviceIDs []string
		if deviceIDStrategyFlag == DeviceIDStrategyIndex {
			for _, d := range m.cachedDevices {
				for _, id := range uuids {
					if d.ID == id {
						deviceIDs = append(deviceIDs, d.Index)
					}
				}
			}
		}
		return deviceIDs*/
}

//func (m *NvidiaDevicePlugin) getDevices(ids []string) ([]*Device, error) {
//    var res []*Device
//    for _, id := range ids {
//        found := false
//        for _, dev := range m.deviceCache.GetCache() {
//            if id == dev.ID {
//                res = append(res, dev)
//                found = true
//                break
//            }
//        }
//        if !found {
//            return res, fmt.Errorf("device %v not found", id)
//        }
//    }
//    return res, nil
//}

func (m *NvidiaDevicePlugin) apiDevices() []*pluginapi.Device {
	if strings.Compare(m.migStrategy, "mixed") == 0 {
		var pdevs []*pluginapi.Device
		for _, d := range m.cachedDevices {
			pdevs = append(pdevs, &d.Device)
		}
		return pdevs
	}
	devices := m.Devices()
	var res []*pluginapi.Device
	for _, dev := range devices {
		for i := uint(0); i < config.DeviceSplitCount; i++ {
			id := fmt.Sprintf("%v-%v", dev.ID, i)
			res = append(res, &pluginapi.Device{
				ID:       id,
				Health:   dev.Health,
				Topology: nil,
			})
		}
	}
	return res
}

func (m *NvidiaDevicePlugin) apiEnvs(envvar string, deviceIDs []string) map[string]string {
	return map[string]string{
		envvar: strings.Join(deviceIDs, ","),
	}
}

//func (m *NvidiaDevicePlugin) apiMounts(deviceIDs []string) []*pluginapi.Mount {
//    var mounts []*pluginapi.Mount
//
//    for _, id := range deviceIDs {
//        mount := &pluginapi.Mount{
//            HostPath:      deviceListAsVolumeMountsHostPath,
//            ContainerPath: filepath.Join(deviceListAsVolumeMountsContainerPathRoot, id),
//        }
//        mounts = append(mounts, mount)
//    }
//
//    return mounts
//}

//func (m *NvidiaDevicePlugin) apiDeviceSpecs(driverRoot string, uuids []string) []*pluginapi.DeviceSpec {
//    var specs []*pluginapi.DeviceSpec
//
//    paths := []string{
//        "/dev/nvidiactl",
//        "/dev/nvidia-uvm",
//        "/dev/nvidia-uvm-tools",
//        "/dev/nvidia-modeset",
//    }
//
//    for _, p := range paths {
//        if _, err := os.Stat(p); err == nil {
//            spec := &pluginapi.DeviceSpec{
//                ContainerPath: p,
//                HostPath:      filepath.Join(driverRoot, p),
//                Permissions:   "rw",
//            }
//            specs = append(specs, spec)
//        }
//    }
//
//    for _, d := range m.deviceCache.GetCache() {
//        for _, id := range uuids {
//            if d.ID == id {
//                for _, p := range d.Paths {
//                    spec := &pluginapi.DeviceSpec{
//                        ContainerPath: p,
//                        HostPath:      filepath.Join(driverRoot, p),
//                        Permissions:   "rw",
//                    }
//                    specs = append(specs, spec)
//                }
//            }
//        }
//    }
//
//    return specs
//}
