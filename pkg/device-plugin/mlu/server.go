// Copyright 2020 Cambricon, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mlu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/allocator"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cndev"
	"github.com/Project-HAMi/HAMi/pkg/device/cambricon"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// CambriconDevicePlugin implements the Kubernetes device plugin API
type CambriconDevicePlugin struct {
	devs         []*pluginapi.Device
	devsInfo     map[string]*cndev.Device
	socket       string
	stop         chan interface{}
	health       chan *pluginapi.Device
	server       *grpc.Server
	deviceList   *deviceList
	allocator    allocator.Allocator
	nodeHostname string
	clientset    kubernetes.Interface
	options      Options
	sync.RWMutex
	containerIndex uint
}

// NewCambriconDevicePlugin returns an initialized CambriconDevicePlugin
func NewCambriconDevicePlugin(o Options) *CambriconDevicePlugin {
	devs, devsInfo := getDevices(o.Mode, int(o.VirtualizationNum))
	return &CambriconDevicePlugin{
		devs:         devs,
		devsInfo:     devsInfo,
		socket:       serverSock,
		stop:         make(chan interface{}),
		health:       make(chan *pluginapi.Device),
		deviceList:   newDeviceList(),
		nodeHostname: o.NodeName,
		options:      o,
	}
}

func (m *CambriconDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		GetPreferredAllocationAvailable: m.options.Mode == topologyAware,
	}, nil
}

// dial establishes the gRPC communication with the registered device plugin.
func dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
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

// Start starts the gRPC server of the device plugin
func (m *CambriconDevicePlugin) Start() error {
	err := m.cleanup()
	if err != nil {
		return err
	}

	sock, err := net.Listen("unix", m.socket)
	if err != nil {
		return err
	}

	m.server = grpc.NewServer([]grpc.ServerOption{}...)
	pluginapi.RegisterDevicePluginServer(m.server, m)

	go m.server.Serve(sock)

	// Wait for server to start by launching a blocking connection
	conn, err := dial(m.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	if !m.options.DisableHealthCheck {
		go m.healthcheck()
	}

	return nil
}

// Stop stops the gRPC server
func (m *CambriconDevicePlugin) Stop() error {
	if m.server == nil {
		return nil
	}

	m.server.Stop()
	m.server = nil
	close(m.stop)

	return m.cleanup()
}

// Register registers the device plugin for the given resourceName with Kubelet.
func (m *CambriconDevicePlugin) Register(kubeletEndpoint, resourceName string) error {
	conn, err := dial(kubeletEndpoint, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(m.socket),
		ResourceName: resourceName,
		Options: &pluginapi.DevicePluginOptions{
			GetPreferredAllocationAvailable: m.options.Mode == topologyAware,
		},
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

// ListAndWatch lists devices and update that list according to the health status
func (m *CambriconDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	s.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})

	for {
		select {
		case <-m.stop:
			return nil
		case d := <-m.health:
			for i, dev := range m.devs {
				if dev.ID == d.ID {
					m.devs[i].Health = d.Health
					break
				}
			}
			s.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})
		}
	}
}

func (m *CambriconDevicePlugin) PrepareResponse(uuids []string) pluginapi.ContainerAllocateResponse {

	resp := pluginapi.ContainerAllocateResponse{}

	resp.Mounts = []*pluginapi.Mount{
		{
			ContainerPath: mluRPMsgDir,
			HostPath:      mluRPMsgDir,
		},
	}

	if m.options.CnmonPath != "" {
		resp.Mounts = append(resp.Mounts, &pluginapi.Mount{
			ContainerPath: m.options.CnmonPath,
			HostPath:      m.options.CnmonPath,
			ReadOnly:      true,
		})
	}

	if m.deviceList.hasSplitDev {
		addDevice(&resp, mluSplitDeviceName, mluSplitDeviceName)
	}

	devpaths := m.uuidToPath(uuids)

	if m.deviceList.hasCtrlDev {
		addDevice(&resp, mluMonitorDeviceName, mluMonitorDeviceName)
	}

	for id, devpath := range devpaths {
		if m.options.Mode == sriov {
			vfid := strings.Split(devpath, mluDeviceName)[1]
			if m.deviceList.hasCommuDev {
				addDevice(&resp, mluCommuDeviceName+vfid, mluCommuDeviceName+strconv.Itoa(id))
			}
			addDevice(&resp, devpath, mluDeviceName+strconv.Itoa(id))
			continue
		}

		var index int
		_, err := fmt.Sscanf(devpath, mluDeviceName+"%d", &index)
		if err != nil {
			log.Printf("Failed to get device index for device path %v", err)
			continue
		}
		if m.deviceList.hasMsgqDev {
			addDevice(&resp, fmt.Sprintf(mluMsgqDeviceName+":%d", index), fmt.Sprintf(mluMsgqDeviceName+":%d", id))
		}
		if m.deviceList.hasRPCDev {
			addDevice(&resp, fmt.Sprintf(mluRPCDeviceName+":%d", index), fmt.Sprintf(mluRPCDeviceName+":%d", id))
		}
		if m.deviceList.hasCmsgDev {
			addDevice(&resp, fmt.Sprintf(mluCmsgDeviceName+"%d", index), fmt.Sprintf(mluCmsgDeviceName+"%d", id))
		}
		if m.deviceList.hasCommuDev {
			addDevice(&resp, fmt.Sprintf(mluCommuDeviceName+"%d", index), fmt.Sprintf(mluCommuDeviceName+"%d", id))
		}
		if m.deviceList.hasIpcmDev {
			addDevice(&resp, fmt.Sprintf(mluIpcmDeviceName+"%d", index), fmt.Sprintf(mluIpcmDeviceName+"%d", id))
		}
		if m.deviceList.hasUARTConsoleDev && m.options.EnableConsole {
			addDevice(&resp, fmt.Sprintf(mluUARTConsoleDeviceName+"%d", index), fmt.Sprintf(mluUARTConsoleDeviceName+"%d", id))
		}
		addDevice(&resp, devpath, mluDeviceName+strconv.Itoa(id))
	}
	return resp
}

func (m *CambriconDevicePlugin) GetDeviceUUIDByIndex(index uint) (uuid string, found bool) {
	for uuid, info := range m.devsInfo {
		if info.Slot == index {
			return uuid, true
		}
	}
	return "", false
}

func (m *CambriconDevicePlugin) GetDeviceIndexByUUID(uuid string) (int, bool) {
	for u, info := range m.devsInfo {
		if strings.Compare(uuid, u) == 0 {
			return int(info.Slot), true
		}
	}
	return 0, false
}

func (m *CambriconDevicePlugin) allocateMLUShare(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	m.Lock()
	defer m.Unlock()

	responses := pluginapi.AllocateResponse{}
	nodename := os.Getenv("NODE_NAME")
	current, err := util.GetPendingPod(nodename)
	if err != nil {
		nodelock.ReleaseNodeLock(nodename)
		return &pluginapi.AllocateResponse{}, err
	}
	for idx := range reqs.ContainerRequests {
		_, devreq, err := util.GetNextDeviceRequest(cambricon.CambriconMLUDevice, *current)
		klog.Infoln("deviceAllocateFromAnnotation=", devreq)
		if err != nil {
			device.PodAllocationFailed(nodename, current)
			return &pluginapi.AllocateResponse{}, err
		}
		if len(devreq) != len(reqs.ContainerRequests[idx].DevicesIDs) {
			device.PodAllocationFailed(nodename, current)
			return &pluginapi.AllocateResponse{}, errors.New("device number not matched")
		}

		err = util.EraseNextDeviceTypeFromAnnotation(cambricon.CambriconMLUDevice, *current)
		if err != nil {
			device.PodAllocationFailed(nodename, current)
			return &pluginapi.AllocateResponse{}, err
		}

		deviceToMount := []string{}
		for _, val := range devreq {
			deviceToMount = append(deviceToMount, val.UUID)
		}

		reqMem := devreq[0].Usedmem / 1024
		resp := m.PrepareResponse(deviceToMount)
		idxval := ""
		for i, v := range devreq {
			devidx, found := m.GetDeviceIndexByUUID(v.UUID)
			if !found {
				device.PodAllocationFailed(nodename, current)
				return nil, errors.New("device uuid" + v.UUID + "not found")
			}
			if i == 0 {
				idxval = fmt.Sprintf("%d", devidx)
			} else {
				idxval = fmt.Sprintf("%v,%d", idxval, devidx)
			}
		}
		resp.Envs = map[string]string{
			mluMemSplitEnable: "1",
			mluMemSplitIndex:  idxval,
			mluMemSplitLimit:  fmt.Sprintf("%d", reqMem),
		}
		if reqMem > 0 {
			resp.Mounts = append(resp.Mounts, &pluginapi.Mount{
				ContainerPath: "/usr/bin/smlu-containerd",
				HostPath:      os.Getenv("HOOK_PATH") + "/smlu-containerd",
				ReadOnly:      true,
			})
		}
		responses.ContainerResponses = append(responses.ContainerResponses, &resp)
	}
	klog.Infoln("response=", responses)
	device.PodAllocationTrySuccess(nodename, cambricon.CambriconMLUDevice, current)
	return &responses, nil
}

// Allocate which return list of devices.
func (m *CambriconDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {

	klog.Info("Into Allocate")
	return m.allocateMLUShare(ctx, reqs)
}

func (m *CambriconDevicePlugin) uuidToPath(uuids []string) []string {
	var paths []string
	for _, uuid := range uuids {
		dev := m.devsInfo[uuid]
		paths = append(paths, dev.Path)
	}
	return paths
}

func (m *CambriconDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (m *CambriconDevicePlugin) cleanup() error {
	if err := os.Remove(m.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *CambriconDevicePlugin) healthcheck() {
	ctx, cancel := context.WithCancel(context.Background())
	health := make(chan *pluginapi.Device)

	go watchUnhealthy(ctx, m.devsInfo, health)

	for {
		select {
		case <-m.stop:
			cancel()
			return
		case dev := <-health:
			m.health <- dev
		}
	}
}

// Serve starts the gRPC server and register the device plugin to Kubelet
func (m *CambriconDevicePlugin) Serve() error {
	if m.options.CnmonPath != "" && !path.IsAbs(m.options.CnmonPath) {
		log.Panicf("invalid cnmon path: %s", m.options.CnmonPath)
	}

	if m.options.Mode == topologyAware {
		m.allocator = allocator.New(m.options.MLULinkPolicy, m.devsInfo)
		m.clientset = initClientSet()

		if m.options.MLULinkPolicy != BestEffort {
			if err := m.updateNodeMLULinkAnnotation(0); err != nil {
				return err
			}
		}
	}

	if m.options.Mode == mluShare {
		m.clientset = initClientSet()
		if num, err := cndev.GetDeviceCount(); err != nil {
			return err
		} else if err = m.patchMLUCount(int(num)); err != nil {
			return err
		}
		if err := m.releaseNodeLock(); err != nil {
			return err
		}
	}

	if err := m.Start(); err != nil {
		return fmt.Errorf("start device plugin err: %v", err)
	}

	log.Printf("Starting to serve on socket %v", m.socket)
	resourceName := "cambricon.com/mlunum"
	if m.options.EnableDeviceType {
		model := cndev.GetDeviceModel(uint(0))
		if model == "" {
			m.Stop()
			return errors.New("device type enabled, but got empty device model from cndev")
		}
		if strings.EqualFold(model, "MLU270-X5K") {
			resourceName = "cambricon.com/" + strings.ToLower(model)
		} else {
			resourceName = "cambricon.com/" + strings.Split(strings.ToLower(model), "-")[0]
		}
	}
	if m.options.Mode == mluShare {
		resourceName = mluMemResourceName
	}
	if err := m.Register(pluginapi.KubeletSocket, resourceName); err != nil {
		m.Stop()
		return fmt.Errorf("register resource %s err: %v", resourceName, err)
	}
	log.Printf("Registered resource %s", resourceName)
	return nil
}

func (m *CambriconDevicePlugin) GetPreferredAllocation(ctx context.Context, r *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	klog.Infoln("into GetPreferredAllocation")
	response := &pluginapi.PreferredAllocationResponse{}
	for _, req := range r.ContainerRequests {
		available := m.getSlots(req.AvailableDeviceIDs)
		required := m.getSlots(req.MustIncludeDeviceIDs)
		allocated, err := m.GetPreferredAllocatedDeviceUUIDs(available, required, int(req.AllocationSize))
		if err != nil {
			log.Printf("failed to get preferred allocated devices, available: %v, size: %d, err: %v \n", available, req.AllocationSize, err)
			return response, err
		}
		resp := &pluginapi.ContainerPreferredAllocationResponse{
			DeviceIDs: allocated,
		}
		response.ContainerResponses = append(response.ContainerResponses, resp)
	}
	return response, nil
}

func (m *CambriconDevicePlugin) GetPreferredAllocatedDeviceUUIDs(available []uint, required []uint, size int) ([]string, error) {

	// todo: consider required list for init containers and numa. ignore it for now.
	if len(required) != 0 {
		log.Printf("required device slice not empty, ignore it. %v \n", required)
	}

	log.Println("=== Start GetPreferredAllocatedDeviceUUIDs ===")
	log.Printf("available devs: %v, size %d", available, size)

	devs, err := m.allocator.Allocate(available, required, size)
	if err != nil {
		if e := m.updateNodeMLULinkAnnotation(size); e != nil {
			log.Printf("updateNodeMLULinkAnnotation err: %v", e)
		}
		return nil, err
	}

	log.Printf("preferred devices %v", devs)

	uuids := []string{}
	for _, dev := range devs {
		uuid, found := m.GetDeviceUUIDByIndex(dev)
		if !found {
			return nil, fmt.Errorf("uuid not found for dev %d", dev)
		}
		uuids = append(uuids, uuid)
	}

	log.Println("=== Finish GetPreferredAllocatedDeviceUUIDs ===")
	return uuids, nil
}

func (m *CambriconDevicePlugin) createAnnotationWithTimestamp(size int) error {
	node, err := m.clientset.CoreV1().Nodes().Get(context.TODO(), m.nodeHostname, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node err %v", err)
	}
	if size == 0 {
		delete(node.Annotations, mluLinkPolicyUnsatisfied)
	} else {
		timeStamp := strconv.FormatInt(time.Now().Unix(), 10)
		if len(node.Annotations) == 0 {
			node.Annotations = make(map[string]string)
		}
		node.Annotations[mluLinkPolicyUnsatisfied] = fmt.Sprintf("%d-%s-%s", size, m.options.MLULinkPolicy, timeStamp)
	}
	_, err = m.clientset.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update node err: %v", err)
	}
	return nil
}

func (m *CambriconDevicePlugin) updateNodeMLULinkAnnotation(size int) error {
	err := m.createAnnotationWithTimestamp(size)
	for i := 0; i < retries && err != nil; i++ {
		log.Printf("createAnnotationWithTimestamp err: %v, retried times: %d", err, i+1)
		time.Sleep(100 * time.Millisecond)
		err = m.createAnnotationWithTimestamp(size)
	}
	return err
}

func (m *CambriconDevicePlugin) getSlots(ids []string) []uint {
	slots := []uint{}
	for _, id := range ids {
		mlu := m.devsInfo[id]
		slots = append(slots, mlu.Slot)
	}
	return slots
}

func addDevice(car *pluginapi.ContainerAllocateResponse, hostPath string, containerPath string) {
	dev := new(pluginapi.DeviceSpec)
	dev.HostPath = hostPath
	dev.ContainerPath = containerPath
	dev.Permissions = "rw"
	car.Devices = append(car.Devices, dev)
}

func initClientSet() kubernetes.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Failed to get in cluser config, err: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Failed to init clientset, err: %v", err)
	}
	return clientset
}
