/*
 * Copyright (c) 2019, NVIDIA CORPORATION.  All rights reserved.
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

package plugin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	spec "github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/cdi"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/imex"
	"github.com/Project-HAMi/HAMi/pkg/nvidia-plugin/pkg/rm"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	deviceListEnvVar                          = "NVIDIA_VISIBLE_DEVICES"
	deviceListAsVolumeMountsHostPath          = "/dev/null"
	deviceListAsVolumeMountsContainerPathRoot = "/var/run/nvidia-container-devices"
)

// NvidiaDevicePlugin implements the Kubernetes device plugin API
type NvidiaDevicePlugin struct {
	rm                   rm.ResourceManager
	config               *spec.Config
	deviceListStrategies spec.DeviceListStrategies

	cdiHandler          cdi.Interface
	cdiAnnotationPrefix string

	socket string
	server *grpc.Server
	health chan *rm.Device
	stop   chan interface{}

	imexChannels imex.Channels

	mps mpsOptions
}

// devicePluginForResource creates a device plugin for the specified resource.
func (o *options) devicePluginForResource(resourceManager rm.ResourceManager) (Interface, error) {
	mpsOptions, err := o.getMPSOptions(resourceManager)
	if err != nil {
		return nil, err
	}

	plugin := NvidiaDevicePlugin{
		rm:                   resourceManager,
		config:               o.config,
		deviceListStrategies: o.deviceListStrategies,

		cdiHandler:          o.cdiHandler,
		cdiAnnotationPrefix: *o.config.Flags.Plugin.CDIAnnotationPrefix,

		imexChannels: o.imexChannels,

		mps: mpsOptions,

		socket: getPluginSocketPath(resourceManager.Resource()),
		// These will be reinitialized every
		// time the plugin server is restarted.
		server: nil,
		health: nil,
		stop:   nil,
	}
	return &plugin, nil
}

// getPluginSocketPath returns the socket to use for the specified resource.
func getPluginSocketPath(resource spec.ResourceName) string {
	_, name := resource.Split()
	pluginName := "nvidia-" + name
	return filepath.Join(pluginapi.DevicePluginPath, pluginName) + ".sock"
}

func (plugin *NvidiaDevicePlugin) initialize() {
	plugin.server = grpc.NewServer([]grpc.ServerOption{}...)
	plugin.health = make(chan *rm.Device)
	plugin.stop = make(chan interface{})
}

func (plugin *NvidiaDevicePlugin) cleanup() {
	close(plugin.stop)
	plugin.server = nil
	plugin.health = nil
	plugin.stop = nil
}

// Devices returns the full set of devices associated with the plugin.
func (plugin *NvidiaDevicePlugin) Devices() rm.Devices {
	return plugin.rm.Devices()
}

// Start starts the gRPC server, registers the device plugin with the Kubelet,
// and starts the device healthchecks.
func (plugin *NvidiaDevicePlugin) Start(kubeletSocket string) error {
	plugin.initialize()

	if err := plugin.mps.waitForDaemon(); err != nil {
		return fmt.Errorf("error waiting for MPS daemon: %w", err)
	}

	err := plugin.Serve()
	if err != nil {
		klog.Errorf("Could not start device plugin for '%s': %s", plugin.rm.Resource(), err)
		plugin.cleanup()
		return err
	}
	klog.Infof("Starting to serve '%s' on %s", plugin.rm.Resource(), plugin.socket)

	err = plugin.Register(kubeletSocket)
	if err != nil {
		klog.Errorf("Could not register device plugin: %s", err)
		return errors.Join(err, plugin.Stop())
	}
	klog.Infof("Registered device plugin for '%s' with Kubelet", plugin.rm.Resource())

	go func() {
		// TODO: add MPS health check
		err := plugin.rm.CheckHealth(plugin.stop, plugin.health)
		if err != nil {
			klog.Errorf("Failed to start health check: %v; continuing with health checks disabled", err)
		}
	}()

	return nil
}

// Stop stops the gRPC server.
func (plugin *NvidiaDevicePlugin) Stop() error {
	if plugin == nil || plugin.server == nil {
		return nil
	}
	klog.Infof("Stopping to serve '%s' on %s", plugin.rm.Resource(), plugin.socket)
	plugin.server.Stop()
	if err := os.Remove(plugin.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	plugin.cleanup()
	return nil
}

// Serve starts the gRPC server of the device plugin.
func (plugin *NvidiaDevicePlugin) Serve() error {
	os.Remove(plugin.socket)
	sock, err := net.Listen("unix", plugin.socket)
	if err != nil {
		return err
	}

	pluginapi.RegisterDevicePluginServer(plugin.server, plugin)

	go func() {
		lastCrashTime := time.Now()
		restartCount := 0

		for {
			// quite if it has been restarted too often
			// i.e. if server has crashed more than 5 times and it didn't last more than one hour each time
			if restartCount > 5 {
				// quit
				klog.Fatalf("GRPC server for '%s' has repeatedly crashed recently. Quitting", plugin.rm.Resource())
			}

			klog.Infof("Starting GRPC server for '%s'", plugin.rm.Resource())
			err := plugin.server.Serve(sock)
			if err == nil {
				break
			}

			klog.Infof("GRPC server for '%s' crashed with error: %v", plugin.rm.Resource(), err)

			timeSinceLastCrash := time.Since(lastCrashTime).Seconds()
			lastCrashTime = time.Now()
			if timeSinceLastCrash > 3600 {
				// it has been one hour since the last crash.. reset the count
				// to reflect on the frequency
				restartCount = 0
			} else {
				restartCount++
			}
		}
	}()

	// Wait for server to start by launching a blocking connection
	conn, err := plugin.dial(plugin.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

// Register registers the device plugin for the given resourceName with Kubelet.
func (plugin *NvidiaDevicePlugin) Register(kubeletSocket string) error {
	if kubeletSocket == "" {
		klog.Info("Skipping registration with Kubelet")
		return nil
	}

	conn, err := plugin.dial(kubeletSocket, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(plugin.socket),
		ResourceName: string(plugin.rm.Resource()),
		Options: &pluginapi.DevicePluginOptions{
			GetPreferredAllocationAvailable: true,
		},
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

// GetDevicePluginOptions returns the values of the optional settings for this plugin
func (plugin *NvidiaDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	options := &pluginapi.DevicePluginOptions{
		GetPreferredAllocationAvailable: true,
	}
	return options, nil
}

// ListAndWatch lists devices and update that list according to the health status
func (plugin *NvidiaDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	if err := s.Send(&pluginapi.ListAndWatchResponse{Devices: plugin.apiDevices()}); err != nil {
		return err
	}

	for {
		select {
		case <-plugin.stop:
			return nil
		case d := <-plugin.health:
			// FIXME: there is no way to recover from the Unhealthy state.
			d.Health = pluginapi.Unhealthy
			klog.Infof("'%s' device marked unhealthy: %s", plugin.rm.Resource(), d.ID)
			if err := s.Send(&pluginapi.ListAndWatchResponse{Devices: plugin.apiDevices()}); err != nil {
				return nil
			}
		}
	}
}

// GetPreferredAllocation returns the preferred allocation from the set of devices specified in the request
func (plugin *NvidiaDevicePlugin) GetPreferredAllocation(ctx context.Context, r *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	response := &pluginapi.PreferredAllocationResponse{}
	for _, req := range r.ContainerRequests {
		devices, err := plugin.rm.GetPreferredAllocation(req.AvailableDeviceIDs, req.MustIncludeDeviceIDs, int(req.AllocationSize))
		if err != nil {
			return nil, fmt.Errorf("error getting list of preferred allocation devices: %v", err)
		}

		resp := &pluginapi.ContainerPreferredAllocationResponse{
			DeviceIDs: devices,
		}

		response.ContainerResponses = append(response.ContainerResponses, resp)
	}
	return response, nil
}

// Allocate which return list of devices.
func (plugin *NvidiaDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	responses := pluginapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		if err := plugin.rm.ValidateRequest(req.DevicesIDs); err != nil {
			return nil, fmt.Errorf("invalid allocation request for %q: %w", plugin.rm.Resource(), err)
		}
		response, err := plugin.getAllocateResponse(req.DevicesIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get allocate response: %v", err)
		}
		responses.ContainerResponses = append(responses.ContainerResponses, response)
	}

	return &responses, nil
}

func (plugin *NvidiaDevicePlugin) getAllocateResponse(requestIds []string) (*pluginapi.ContainerAllocateResponse, error) {
	deviceIDs := plugin.deviceIDsFromAnnotatedDeviceIDs(requestIds)

	// Create an empty response that will be updated as required below.
	response := &pluginapi.ContainerAllocateResponse{
		Envs: make(map[string]string),
	}
	if plugin.deviceListStrategies.AnyCDIEnabled() {
		responseID := uuid.New().String()
		if err := plugin.updateResponseForCDI(response, responseID, deviceIDs...); err != nil {
			return nil, fmt.Errorf("failed to get allocate response for CDI: %v", err)
		}
	}
	if plugin.mps.enabled {
		plugin.updateResponseForMPS(response)
	}

	// The following modifications are only made if at least one non-CDI device
	// list strategy is selected.
	if plugin.deviceListStrategies.AllCDIEnabled() {
		return response, nil
	}

	if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyEnvVar) {
		plugin.updateResponseForDeviceListEnvVar(response, deviceIDs...)
		plugin.updateResponseForImexChannelsEnvVar(response)
	}
	if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyVolumeMounts) {
		plugin.updateResponseForDeviceMounts(response, deviceIDs...)
	}
	if *plugin.config.Flags.Plugin.PassDeviceSpecs {
		response.Devices = append(response.Devices, plugin.apiDeviceSpecs(*plugin.config.Flags.NvidiaDevRoot, requestIds)...)
	}
	if *plugin.config.Flags.GDSEnabled {
		response.Envs["NVIDIA_GDS"] = "enabled"
	}
	if *plugin.config.Flags.MOFEDEnabled {
		response.Envs["NVIDIA_MOFED"] = "enabled"
	}
	return response, nil
}

// updateResponseForMPS ensures that the ContainerAllocate response contains the information required to use MPS.
// This includes per-resource pipe and log directories as well as a global daemon-specific shm
// and assumes that an MPS control daemon has already been started.
func (plugin NvidiaDevicePlugin) updateResponseForMPS(response *pluginapi.ContainerAllocateResponse) {
	plugin.mps.updateReponse(response)
}

// updateResponseForCDI updates the specified response for the given device IDs.
// This response contains the annotations required to trigger CDI injection in the container engine or nvidia-container-runtime.
func (plugin *NvidiaDevicePlugin) updateResponseForCDI(response *pluginapi.ContainerAllocateResponse, responseID string, deviceIDs ...string) error {
	var devices []string
	for _, id := range deviceIDs {
		devices = append(devices, plugin.cdiHandler.QualifiedName("gpu", id))
	}
	for _, channel := range plugin.imexChannels {
		devices = append(devices, plugin.cdiHandler.QualifiedName("imex-channel", channel.ID))
	}
	if *plugin.config.Flags.GDSEnabled {
		devices = append(devices, plugin.cdiHandler.QualifiedName("gds", "all"))
	}
	if *plugin.config.Flags.MOFEDEnabled {
		devices = append(devices, plugin.cdiHandler.QualifiedName("mofed", "all"))
	}

	if len(devices) == 0 {
		return nil
	}

	if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyCDIAnnotations) {
		annotations, err := plugin.getCDIDeviceAnnotations(responseID, devices...)
		if err != nil {
			return err
		}
		response.Annotations = annotations
	}
	if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyCDICRI) {
		for _, device := range devices {
			cdiDevice := pluginapi.CDIDevice{
				Name: device,
			}
			response.CDIDevices = append(response.CDIDevices, &cdiDevice)
		}
	}

	return nil
}

func (plugin *NvidiaDevicePlugin) getCDIDeviceAnnotations(id string, devices ...string) (map[string]string, error) {
	annotations, err := cdiapi.UpdateAnnotations(map[string]string{}, "nvidia-device-plugin", id, devices)
	if err != nil {
		return nil, fmt.Errorf("failed to add CDI annotations: %v", err)
	}

	if plugin.cdiAnnotationPrefix == spec.DefaultCDIAnnotationPrefix {
		return annotations, nil
	}

	// update annotations if a custom CDI prefix is configured
	updatedAnnotations := make(map[string]string)
	for k, v := range annotations {
		newKey := plugin.cdiAnnotationPrefix + strings.TrimPrefix(k, spec.DefaultCDIAnnotationPrefix)
		updatedAnnotations[newKey] = v
	}

	return updatedAnnotations, nil
}

// PreStartContainer is unimplemented for this plugin
func (plugin *NvidiaDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

// dial establishes the gRPC communication with the registered device plugin.
func (plugin *NvidiaDevicePlugin) dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	//nolint:staticcheck  // TODO: Switch to grpc.NewClient
	c, err := grpc.DialContext(ctx, unixSocketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		//nolint:staticcheck  // TODO: WithBlock is deprecated.
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (plugin *NvidiaDevicePlugin) deviceIDsFromAnnotatedDeviceIDs(ids []string) []string {
	var deviceIDs []string
	if *plugin.config.Flags.Plugin.DeviceIDStrategy == spec.DeviceIDStrategyUUID {
		deviceIDs = rm.AnnotatedIDs(ids).GetIDs()
	}
	if *plugin.config.Flags.Plugin.DeviceIDStrategy == spec.DeviceIDStrategyIndex {
		deviceIDs = plugin.rm.Devices().Subset(ids).GetIndices()
	}
	return deviceIDs
}

func (plugin *NvidiaDevicePlugin) apiDevices() []*pluginapi.Device {
	return plugin.rm.Devices().GetPluginDevices()
}

// updateResponseForDeviceListEnvVar sets the environment variable for the requested devices.
func (plugin *NvidiaDevicePlugin) updateResponseForDeviceListEnvVar(response *pluginapi.ContainerAllocateResponse, deviceIDs ...string) {
	response.Envs[deviceListEnvVar] = strings.Join(deviceIDs, ",")
}

// updateResponseForImexChannelsEnvVar sets the environment variable for the requested IMEX channels.
func (plugin *NvidiaDevicePlugin) updateResponseForImexChannelsEnvVar(response *pluginapi.ContainerAllocateResponse) {
	var channelIDs []string
	for _, channel := range plugin.imexChannels {
		channelIDs = append(channelIDs, channel.ID)
	}
	if len(channelIDs) > 0 {
		response.Envs[spec.ImexChannelEnvVar] = strings.Join(channelIDs, ",")
	}
}

// updateResponseForDeviceMounts sets the mounts required to request devices if volume mounts are used.
func (plugin *NvidiaDevicePlugin) updateResponseForDeviceMounts(response *pluginapi.ContainerAllocateResponse, deviceIDs ...string) {
	plugin.updateResponseForDeviceListEnvVar(response, deviceListAsVolumeMountsContainerPathRoot)

	for _, id := range deviceIDs {
		mount := &pluginapi.Mount{
			HostPath:      deviceListAsVolumeMountsHostPath,
			ContainerPath: filepath.Join(deviceListAsVolumeMountsContainerPathRoot, id),
		}
		response.Mounts = append(response.Mounts, mount)
	}
	for _, channel := range plugin.imexChannels {
		mount := &pluginapi.Mount{
			HostPath:      deviceListAsVolumeMountsHostPath,
			ContainerPath: filepath.Join(deviceListAsVolumeMountsContainerPathRoot, "imex", channel.ID),
		}
		response.Mounts = append(response.Mounts, mount)
	}
}

func (plugin *NvidiaDevicePlugin) apiDeviceSpecs(devRoot string, ids []string) []*pluginapi.DeviceSpec {
	optional := map[string]bool{
		"/dev/nvidiactl":        true,
		"/dev/nvidia-uvm":       true,
		"/dev/nvidia-uvm-tools": true,
		"/dev/nvidia-modeset":   true,
	}

	paths := plugin.rm.GetDevicePaths(ids)

	var specs []*pluginapi.DeviceSpec
	for _, p := range paths {
		if optional[p] {
			if _, err := os.Stat(p); err != nil {
				continue
			}
		}
		spec := &pluginapi.DeviceSpec{
			ContainerPath: p,
			HostPath:      filepath.Join(devRoot, p),
			Permissions:   "rw",
		}
		specs = append(specs, spec)
	}

	for _, channel := range plugin.imexChannels {
		spec := &pluginapi.DeviceSpec{
			ContainerPath: channel.Path,
			// TODO: The HostPath property for a channel is not the correct value to use here.
			// The `devRoot` there represents the devRoot in the current container when discovering devices
			// and is set to "{{ .*config.Flags.Plugin.ContainerDriverRoot }}/dev".
			// The devRoot in this context is the {{ .config.Flags.NvidiaDevRoot }} and defines the
			// root for device nodes on the host. This is usually / or /run/nvidia/driver when the
			// driver container is used.
			HostPath:    filepath.Join(devRoot, channel.Path),
			Permissions: "rw",
		}
		specs = append(specs, spec)
	}

	return specs
}
