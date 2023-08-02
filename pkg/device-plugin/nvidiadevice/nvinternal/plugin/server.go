/*
<<<<<<< HEAD
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
=======
 * Copyright (c) 2019, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
<<<<<<< HEAD
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
=======
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
 */

package plugin

import (
<<<<<<< HEAD
	"bytes"
	"context"
	"encoding/json"
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	"errors"
	"fmt"
	"net"
	"os"
<<<<<<< HEAD
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/google/uuid"
	"github.com/imdario/mergo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/imex"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/scheduler/config"
	"github.com/Project-HAMi/HAMi/pkg/util"
=======
	"path"
	"path/filepath"
	"strings"
	"time"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/device-plugin/nvidiadevice/nvinternal/cdi"
	"4pd.io/k8s-vgpu/pkg/device-plugin/nvidiadevice/nvinternal/rm"
	"4pd.io/k8s-vgpu/pkg/util"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	cdiapi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"

	"github.com/google/uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
)

// Constants for use by the 'volume-mounts' device list strategy
const (
	deviceListAsVolumeMountsHostPath          = "/dev/null"
	deviceListAsVolumeMountsContainerPathRoot = "/var/run/nvidia-container-devices"
<<<<<<< HEAD
	NodeLockNvidia                            = "hami.io/mutex.lock"
	ConfigFilePath                            = "/config/config.json"
	deviceListEnvVar                          = "NVIDIA_VISIBLE_DEVICES"
)

var (
	hostHookPath string
	ConfigFile   *string
)

func init() {
	hostHookPath, _ = os.LookupEnv("HOOK_PATH")
}

// NvidiaDevicePlugin implements the Kubernetes device plugin API
type NvidiaDevicePlugin struct {
	ctx                  context.Context
	rm                   rm.ResourceManager
	config               *nvidia.DeviceConfig
	deviceListEnvvar     string
	deviceListStrategies spec.DeviceListStrategies
	socket               string
	schedulerConfig      nvidia.NvidiaConfig

	applyMutex                 sync.Mutex
	disableHealthChecks        chan bool
	ackDisableHealthChecks     chan bool
	disableWatchAndRegister    chan bool
	ackDisableWatchAndRegister chan bool

	cdiHandler          cdi.Interface
	cdiAnnotationPrefix string

	operatingMode string
	migCurrent    nvidia.MigPartedSpec
	deviceCache   string

	imexChannels imex.Channels

	server *grpc.Server
	health chan *rm.Device
	stop   chan any
}

func readFromConfigFile(sConfig *nvidia.NvidiaConfig, path string) (string, error) {
	jsonbyte, err := os.ReadFile(path)
	mode := "hami-core"
	if err != nil {
		return "", err
	}
	var deviceConfigs nvidia.DevicePluginConfigs
	err = json.Unmarshal(jsonbyte, &deviceConfigs)
	if err != nil {
		return "", err
	}
	klog.Infof("Device Plugin Configs: %v", fmt.Sprintf("%v", deviceConfigs))
	for _, val := range deviceConfigs.Nodeconfig {
		if os.Getenv(util.NodeNameEnvName) == val.Name {
			klog.Infof("Reading config from file %s", val.Name)
			if err := mergo.Merge(&sConfig.NodeDefaultConfig, val.NodeDefaultConfig, mergo.WithOverride); err != nil {
				return "", err
			}
			if val.FilterDevice != nil && (len(val.FilterDevice.UUID) > 0 || len(val.FilterDevice.Index) > 0) {
				nvidia.DevicePluginFilterDevice = val.FilterDevice
			}
			if len(val.OperatingMode) > 0 {
				mode = val.OperatingMode
			}
			klog.Infof("FilterDevice: %v", val.FilterDevice)
		}
	}
	return mode, nil
}

func LoadNvidiaDevicePluginConfig() (*config.Config, string, error) {
	sConfig, err := config.LoadConfig(*ConfigFile)
	if err != nil {
		klog.Fatalf(`failed to load device config file %s: %v`, *ConfigFile, err)
	}
	mode, err := readFromConfigFile(&sConfig.NvidiaConfig, ConfigFilePath)
	if err != nil {
		klog.Errorf("readFromConfigFile err:%s", err.Error())
	}
	return sConfig, mode, nil
}

// getPluginSocketPath returns the socket to use for the specified resource.
func getPluginSocketPath(resource spec.ResourceName) string {
	_, name := resource.Split()
	pluginName := "nvidia-" + name
	return filepath.Join(kubeletdevicepluginv1beta1.DevicePluginPath, pluginName) + ".sock"
}

// NewNvidiaDevicePlugin returns an initialized NvidiaDevicePlugin
func (o *options) devicePluginForResource(ctx context.Context, nvconfig *nvidia.DeviceConfig, resourceManager rm.ResourceManager, sConfig *config.Config, mode string) (Interface, error) {
	_, name := resourceManager.Resource().Split()

	deviceListStrategies, _ := spec.NewDeviceListStrategies(*nvconfig.Flags.Plugin.DeviceListStrategy)

	klog.Infoln("reading config=", nvconfig, "resourceName", nvconfig.ResourceName, "configfile=", *ConfigFile, "sconfig=", sConfig)

	// Initialize devices with configuration
	if err := config.InitDevicesWithConfig(sConfig); err != nil {
		klog.Fatalf("failed to initialize devices: %v", err)
	}
	return &NvidiaDevicePlugin{
		ctx:                        ctx,
		rm:                         resourceManager,
		config:                     nvconfig,
		deviceListEnvvar:           "NVIDIA_VISIBLE_DEVICES",
		deviceListStrategies:       deviceListStrategies,
		applyMutex:                 sync.Mutex{},
		disableHealthChecks:        nil,
		ackDisableHealthChecks:     nil,
		disableWatchAndRegister:    nil,
		ackDisableWatchAndRegister: nil,
		socket:                     kubeletdevicepluginv1beta1.DevicePluginPath + "nvidia-" + name + ".sock",
		cdiHandler:                 o.cdiHandler,
		cdiAnnotationPrefix:        *o.config.Flags.Plugin.CDIAnnotationPrefix,
		schedulerConfig:            sConfig.NvidiaConfig,
		operatingMode:              mode,
		migCurrent:                 nvidia.MigPartedSpec{},
		deviceCache:                "",
=======
)

// NvidiaDevicePlugin implements the Kubernetes device plugin API
type NvidiaDevicePlugin struct {
	rm                   rm.ResourceManager
	config               *util.DeviceConfig
	deviceListEnvvar     string
	deviceListStrategies spec.DeviceListStrategies
	socket               string

	cdiHandler          cdi.Interface
	cdiEnabled          bool
	cdiAnnotationPrefix string

	server *grpc.Server
	health chan *rm.Device
	stop   chan interface{}
}

// NewNvidiaDevicePlugin returns an initialized NvidiaDevicePlugin
func NewNvidiaDevicePlugin(config *util.DeviceConfig, resourceManager rm.ResourceManager, cdiHandler cdi.Interface, cdiEnabled bool) *NvidiaDevicePlugin {
	_, name := resourceManager.Resource().Split()

	deviceListStrategies, _ := spec.NewDeviceListStrategies(*config.Flags.Plugin.DeviceListStrategy)

	return &NvidiaDevicePlugin{
		rm:                   resourceManager,
		config:               config,
		deviceListEnvvar:     "NVIDIA_VISIBLE_DEVICES",
		deviceListStrategies: deviceListStrategies,
		socket:               pluginapi.DevicePluginPath + "nvidia-" + name + ".sock",
		cdiHandler:           cdiHandler,
		cdiEnabled:           cdiEnabled,
		cdiAnnotationPrefix:  *config.Flags.Plugin.CDIAnnotationPrefix,
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

		// These will be reinitialized every
		// time the plugin server is restarted.
		server: nil,
		health: nil,
		stop:   nil,
<<<<<<< HEAD
	}, nil
=======
	}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

func (plugin *NvidiaDevicePlugin) initialize() {
	plugin.server = grpc.NewServer([]grpc.ServerOption{}...)
	plugin.health = make(chan *rm.Device)
<<<<<<< HEAD
	plugin.stop = make(chan any)
	plugin.disableHealthChecks = make(chan bool, 1)
	plugin.ackDisableHealthChecks = make(chan bool, 1)
	plugin.disableWatchAndRegister = make(chan bool, 1)
	plugin.ackDisableWatchAndRegister = make(chan bool, 1)
=======
	plugin.stop = make(chan interface{})
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

func (plugin *NvidiaDevicePlugin) cleanup() {
	close(plugin.stop)
	plugin.server = nil
	plugin.health = nil
	plugin.stop = nil
<<<<<<< HEAD
	plugin.disableHealthChecks = nil
	plugin.ackDisableHealthChecks = nil
	plugin.disableWatchAndRegister = nil
	plugin.ackDisableWatchAndRegister = nil
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// Devices returns the full set of devices associated with the plugin.
func (plugin *NvidiaDevicePlugin) Devices() rm.Devices {
	return plugin.rm.Devices()
}

// Start starts the gRPC server, registers the device plugin with the Kubelet,
// and starts the device healthchecks.
<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) Start(kubeletSocket string) error {
	plugin.initialize()

	deviceNumbers, err := GetDeviceNums()
	if err != nil {
		return err
	}

	deviceNames, err := GetDeviceNames()
	if err != nil {
		return err
	}

	err = plugin.Serve()
=======
func (plugin *NvidiaDevicePlugin) Start() error {
	plugin.initialize()

	err := plugin.Serve()
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	if err != nil {
		klog.Infof("Could not start device plugin for '%s': %s", plugin.rm.Resource(), err)
		plugin.cleanup()
		return err
	}
	klog.Infof("Starting to serve '%s' on %s", plugin.rm.Resource(), plugin.socket)

<<<<<<< HEAD
	err = plugin.Register(kubeletSocket)
=======
	err = plugin.Register()
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	if err != nil {
		klog.Infof("Could not register device plugin: %s", err)
		plugin.Stop()
		return err
	}
	klog.Infof("Registered device plugin for '%s' with Kubelet", plugin.rm.Resource())
<<<<<<< HEAD
	// Prepare the lock file sub directory.Due to the sequence of startup processes, both the device plugin
	// and the vGPU monitor should attempt to create this directory by default to ensure its creation.
	err = CreateMigApplyLockDir()
	if err != nil {
		klog.Fatalf("CreateMIGLockSubDir failed:%v", err)
	}

	// If the temporary lock file still exists, it may be a leftover from the last incomplete mig  application process.
	// Delete the temporary lock file to make sure vgpu monitor can start.
	err = RemoveMigApplyLock()
	if err != nil {
		klog.Fatalf("RemoveMigApplyLock failed:%v", err)
	}

	var deviceSupportMig bool
	for _, name := range deviceNames {
		deviceSupportMig = false
		for _, migTemplate := range plugin.schedulerConfig.MigGeometriesList {
			if containsModel(name, migTemplate.Models) {
				deviceSupportMig = true
				break
			}
		}
		if !deviceSupportMig {
			break
		}
	}
	if deviceSupportMig {
		cmd := exec.Command("nvidia-mig-parted", "export")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if err != nil {
			klog.Fatalf("nvidia-mig-parted failed with %s\n", err)
		}
		outStr := stdout.Bytes()
		yaml.Unmarshal(outStr, &plugin.migCurrent)
		os.WriteFile("/tmp/migconfig.yaml", outStr, os.ModePerm)
		if plugin.operatingMode == "mig" {
			HamiInitMigConfig, err := plugin.processMigConfigs(plugin.migCurrent.MigConfigs, deviceNumbers)
			if err != nil {
				klog.Infof("no device in node:%v", err)
			}
			plugin.migCurrent.MigConfigs["current"] = HamiInitMigConfig
			klog.Infoln("Open Mig export", plugin.migCurrent)
		} else {
			plugin.migCurrent.MigConfigs = make(map[string]nvidia.MigConfigSpecSlice)
			configSlice := nvidia.MigConfigSpecSlice{}
			for i := 0; i < deviceNumbers; i++ {
				conf := nvidia.MigConfigSpec{MigEnabled: false, Devices: []int32{int32(i)}}
				configSlice = append(configSlice, conf)
			}
			plugin.migCurrent.MigConfigs["current"] = configSlice
			klog.Infoln("Close Mig export", plugin.migCurrent)
		}
	}
	go func() {
		err := plugin.rm.CheckHealth(plugin.stop, plugin.health, plugin.disableHealthChecks, plugin.ackDisableHealthChecks)
=======

	go func() {
		err := plugin.rm.CheckHealth(plugin.stop, plugin.health)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		if err != nil {
			klog.Infof("Failed to start health check: %v; continuing with health checks disabled", err)
		}
	}()

	go func() {
<<<<<<< HEAD
		plugin.WatchAndRegister(plugin.disableWatchAndRegister, plugin.ackDisableWatchAndRegister)
	}()

	if deviceSupportMig {
		plugin.ApplyMigTemplate()
	}

=======
		plugin.WatchAndRegister()
	}()

>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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

<<<<<<< HEAD
	kubeletdevicepluginv1beta1.RegisterDevicePluginServer(plugin.server, plugin)
=======
	pluginapi.RegisterDevicePluginServer(plugin.server, plugin)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

	go func() {
		lastCrashTime := time.Now()
		restartCount := 0
		for {
<<<<<<< HEAD
			// restart if it has not been too often
			// i.e. if server has crashed more than 5 times and it didn't last more than one hour each time
			if restartCount > 5 {
				// quit
				klog.Fatalf("GRPC server for '%s' has repeatedly crashed recently. Quitting", plugin.rm.Resource())
			}

=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			klog.Infof("Starting GRPC server for '%s'", plugin.rm.Resource())
			err := plugin.server.Serve(sock)
			if err == nil {
				break
			}

			klog.Infof("GRPC server for '%s' crashed with error: %v", plugin.rm.Resource(), err)

<<<<<<< HEAD
=======
			// restart if it has not been too often
			// i.e. if server has crashed more than 5 times and it didn't last more than one hour each time
			if restartCount > 5 {
				// quit
				klog.Fatalf("GRPC server for '%s' has repeatedly crashed recently. Quitting", plugin.rm.Resource())
			}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			timeSinceLastCrash := time.Since(lastCrashTime).Seconds()
			lastCrashTime = time.Now()
			if timeSinceLastCrash > 3600 {
				// it has been one hour since the last crash.. reset the count
				// to reflect on the frequency
<<<<<<< HEAD
				restartCount = 0
=======
				restartCount = 1
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			} else {
				restartCount++
			}
		}
	}()

	// Wait for server to start by launching a blocking connexion
	conn, err := plugin.dial(plugin.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

// Register registers the device plugin for the given resourceName with Kubelet.
<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) Register(kubeletSocket string) error {
	if kubeletSocket == "" {
		klog.Info("Skipping registration with Kubelet")
		return nil
	}

	conn, err := plugin.dial(kubeletSocket, 5*time.Second)
=======
func (plugin *NvidiaDevicePlugin) Register() error {
	conn, err := plugin.dial(pluginapi.KubeletSocket, 5*time.Second)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	if err != nil {
		return err
	}
	defer conn.Close()

<<<<<<< HEAD
	client := kubeletdevicepluginv1beta1.NewRegistrationClient(conn)
	reqt := &kubeletdevicepluginv1beta1.RegisterRequest{
		Version:      kubeletdevicepluginv1beta1.Version,
		Endpoint:     path.Base(plugin.socket),
		ResourceName: string(plugin.rm.Resource()),
		Options: &kubeletdevicepluginv1beta1.DevicePluginOptions{
			GetPreferredAllocationAvailable: false,
=======
	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(plugin.socket),
		ResourceName: string(plugin.rm.Resource()),
		Options: &pluginapi.DevicePluginOptions{
			GetPreferredAllocationAvailable: true,
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		},
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

// GetDevicePluginOptions returns the values of the optional settings for this plugin
<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) GetDevicePluginOptions(context.Context, *kubeletdevicepluginv1beta1.Empty) (*kubeletdevicepluginv1beta1.DevicePluginOptions, error) {
	options := &kubeletdevicepluginv1beta1.DevicePluginOptions{
		GetPreferredAllocationAvailable: false,
=======
func (plugin *NvidiaDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	options := &pluginapi.DevicePluginOptions{
		GetPreferredAllocationAvailable: true,
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	}
	return options, nil
}

// ListAndWatch lists devices and update that list according to the health status
<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) ListAndWatch(e *kubeletdevicepluginv1beta1.Empty, s kubeletdevicepluginv1beta1.DevicePlugin_ListAndWatchServer) error {
	s.Send(&kubeletdevicepluginv1beta1.ListAndWatchResponse{Devices: plugin.apiDevices()})
=======
func (plugin *NvidiaDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	s.Send(&pluginapi.ListAndWatchResponse{Devices: plugin.apiDevices()})
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

	for {
		select {
		case <-plugin.stop:
			return nil
		case d := <-plugin.health:
			// FIXME: there is no way to recover from the Unhealthy state.
<<<<<<< HEAD
			d.Health = kubeletdevicepluginv1beta1.Unhealthy
			klog.Infof("'%s' device marked unhealthy: %s", plugin.rm.Resource(), d.ID)
			s.Send(&kubeletdevicepluginv1beta1.ListAndWatchResponse{Devices: plugin.apiDevices()})
=======
			d.Health = pluginapi.Unhealthy
			klog.Infof("'%s' device marked unhealthy: %s", plugin.rm.Resource(), d.ID)
			s.Send(&pluginapi.ListAndWatchResponse{Devices: plugin.apiDevices()})
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		}
	}
}

// GetPreferredAllocation returns the preferred allocation from the set of devices specified in the request
<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) GetPreferredAllocation(ctx context.Context, r *kubeletdevicepluginv1beta1.PreferredAllocationRequest) (*kubeletdevicepluginv1beta1.PreferredAllocationResponse, error) {
	response := &kubeletdevicepluginv1beta1.PreferredAllocationResponse{}
=======
func (plugin *NvidiaDevicePlugin) GetPreferredAllocation(ctx context.Context, r *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	response := &pluginapi.PreferredAllocationResponse{}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	/*for _, req := range r.ContainerRequests {
		devices, err := plugin.rm.GetPreferredAllocation(req.AvailableDeviceIDs, req.MustIncludeDeviceIDs, int(req.AllocationSize))
		if err != nil {
			return nil, fmt.Errorf("error getting list of preferred allocation devices: %v", err)
		}

<<<<<<< HEAD
		resp := &kubeletdevicepluginv1beta1.ContainerPreferredAllocationResponse{
=======
		resp := &pluginapi.ContainerPreferredAllocationResponse{
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			DeviceIDs: devices,
		}

		response.ContainerResponses = append(response.ContainerResponses, resp)
	}*/
	return response, nil
}

// Allocate which return list of devices.
<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) Allocate(ctx context.Context, reqs *kubeletdevicepluginv1beta1.AllocateRequest) (*kubeletdevicepluginv1beta1.AllocateResponse, error) {
	klog.InfoS("Allocate", "request", reqs)
	responses := kubeletdevicepluginv1beta1.AllocateResponse{}
	nodename := os.Getenv(util.NodeNameEnvName)
	current, err := util.GetPendingPod(ctx, nodename)
	if err != nil {
		//nodelock.ReleaseNodeLock(nodename, NodeLockNvidia, current)
		return &kubeletdevicepluginv1beta1.AllocateResponse{}, err
	}
	klog.Infof("Allocate pod name is %s/%s, annotation is %+v", current.Namespace, current.Name, current.Annotations)
=======
func (plugin *NvidiaDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	klog.Infoln("Allocate", reqs.ContainerRequests)
	responses := pluginapi.AllocateResponse{}
	nodename := os.Getenv("NodeName")
	current, err := util.GetPendingPod(nodename)
	if err != nil {
		util.ReleaseNodeLock(nodename)
		return &pluginapi.AllocateResponse{}, err
	}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)

	for idx, req := range reqs.ContainerRequests {
		// If the devices being allocated are replicas, then (conditionally)
		// error out if more than one resource is being allocated.

		if strings.Contains(req.DevicesIDs[0], "MIG") {
<<<<<<< HEAD
			if plugin.config.Sharing.TimeSlicing.FailRequestsGreaterThanOne && rm.AnnotatedIDs(req.DevicesIDs).AnyHasAnnotations() {
				if len(req.DevicesIDs) > 1 {
					PodAllocationFailed(nodename, current, NodeLockNvidia)
=======

			if plugin.config.Sharing.TimeSlicing.FailRequestsGreaterThanOne && rm.AnnotatedIDs(req.DevicesIDs).AnyHasAnnotations() {
				if len(req.DevicesIDs) > 1 {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
					return nil, fmt.Errorf("request for '%v: %v' too large: maximum request size for shared resources is 1", plugin.rm.Resource(), len(req.DevicesIDs))
				}
			}

			for _, id := range req.DevicesIDs {
				if !plugin.rm.Devices().Contains(id) {
<<<<<<< HEAD
					PodAllocationFailed(nodename, current, NodeLockNvidia)
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
					return nil, fmt.Errorf("invalid allocation request for '%s': unknown device: %s", plugin.rm.Resource(), id)
				}
			}

			response, err := plugin.getAllocateResponse(req.DevicesIDs)
			if err != nil {
<<<<<<< HEAD
				PodAllocationFailed(nodename, current, NodeLockNvidia)
=======
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
				return nil, fmt.Errorf("failed to get allocate response: %v", err)
			}
			responses.ContainerResponses = append(responses.ContainerResponses, response)
		} else {
<<<<<<< HEAD
			currentCtr, devreq, err := GetNextDeviceRequest(nvidia.NvidiaGPUDevice, *current)
			klog.Infoln("deviceAllocateFromAnnotation=", devreq)
			if err != nil {
				PodAllocationFailed(nodename, current, NodeLockNvidia)
				return &kubeletdevicepluginv1beta1.AllocateResponse{}, err
			}
			if len(devreq) != len(reqs.ContainerRequests[idx].DevicesIDs) {
				PodAllocationFailed(nodename, current, NodeLockNvidia)
				return &kubeletdevicepluginv1beta1.AllocateResponse{}, errors.New("device number not matched")
			}
			response, err := plugin.getAllocateResponse(plugin.GetContainerDeviceStrArray(devreq))
=======
			currentCtr, devreq, err := util.GetNextDeviceRequest(util.NvidiaGPUDevice, *current)
			klog.Infoln("deviceAllocateFromAnnotation=", devreq)
			if err != nil {
				util.PodAllocationFailed(nodename, current)
				return &pluginapi.AllocateResponse{}, err
			}
			if len(devreq) != len(reqs.ContainerRequests[idx].DevicesIDs) {
				util.PodAllocationFailed(nodename, current)
				return &pluginapi.AllocateResponse{}, errors.New("device number not matched")
			}
			klog.Infoln("[][[]]")
			response, err := plugin.getAllocateResponse(util.GetContainerDeviceStrArray(devreq))
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			if err != nil {
				return nil, fmt.Errorf("failed to get allocate response: %v", err)
			}

<<<<<<< HEAD
			err = EraseNextDeviceTypeFromAnnotation(nvidia.NvidiaGPUDevice, *current)
			if err != nil {
				PodAllocationFailed(nodename, current, NodeLockNvidia)
				return &kubeletdevicepluginv1beta1.AllocateResponse{}, err
			}

			if plugin.operatingMode != "mig" {
				for i, dev := range devreq {
					limitKey := fmt.Sprintf("CUDA_DEVICE_MEMORY_LIMIT_%v", i)
					response.Envs[limitKey] = fmt.Sprintf("%vm", dev.Usedmem)
				}
				response.Envs["CUDA_DEVICE_SM_LIMIT"] = fmt.Sprint(devreq[0].Usedcores)
				response.Envs["CUDA_DEVICE_MEMORY_SHARED_CACHE"] = fmt.Sprintf("%s/vgpu/%v.cache", hostHookPath, uuid.New().String())
				if *plugin.schedulerConfig.DeviceMemoryScaling > 1 {
					response.Envs["CUDA_OVERSUBSCRIBE"] = "true"
				}
				if *plugin.schedulerConfig.LogLevel != "" {
					response.Envs["LIBCUDA_LOG_LEVEL"] = string(*plugin.schedulerConfig.LogLevel)
				}
				if plugin.schedulerConfig.DisableCoreLimit {
					response.Envs[util.CoreLimitSwitch] = "disable"
				}
				cacheFileHostDirectory := fmt.Sprintf("%s/vgpu/containers/%s_%s", hostHookPath, current.UID, currentCtr.Name)
				os.RemoveAll(cacheFileHostDirectory)

				os.MkdirAll(cacheFileHostDirectory, 0777)
				os.Chmod(cacheFileHostDirectory, 0777)
				os.MkdirAll("/tmp/vgpulock", 0777)
				os.Chmod("/tmp/vgpulock", 0777)
				response.Mounts = append(response.Mounts,
					&kubeletdevicepluginv1beta1.Mount{ContainerPath: fmt.Sprintf("%s/vgpu/libvgpu.so", hostHookPath),
						HostPath: GetLibPath(),
						ReadOnly: true},
					&kubeletdevicepluginv1beta1.Mount{ContainerPath: fmt.Sprintf("%s/vgpu", hostHookPath),
						HostPath: cacheFileHostDirectory,
						ReadOnly: false},
					&kubeletdevicepluginv1beta1.Mount{ContainerPath: "/tmp/vgpulock",
						HostPath: "/tmp/vgpulock",
						ReadOnly: false},
				)
				found := false
				for _, val := range currentCtr.Env {
					if strings.Compare(val.Name, "CUDA_DISABLE_CONTROL") == 0 {
						// if env existed but is set to false or can not be parsed, ignore
						t, _ := strconv.ParseBool(val.Value)
						if !t {
							continue
						}
						// only env existed and set to true, we mark it "found"
						found = true
						break
					}
				}
				if !found {
					response.Mounts = append(response.Mounts, &kubeletdevicepluginv1beta1.Mount{ContainerPath: "/etc/ld.so.preload",
						HostPath: hostHookPath + "/vgpu/ld.so.preload",
						ReadOnly: true},
					)
				}
				_, err = os.Stat(fmt.Sprintf("%s/vgpu/license", hostHookPath))
				if err == nil {
					response.Mounts = append(response.Mounts, &kubeletdevicepluginv1beta1.Mount{
						ContainerPath: "/tmp/license",
						HostPath:      fmt.Sprintf("%s/vgpu/license", hostHookPath),
						ReadOnly:      true,
					})
					response.Mounts = append(response.Mounts, &kubeletdevicepluginv1beta1.Mount{
						ContainerPath: "/usr/bin/vgpuvalidator",
						HostPath:      fmt.Sprintf("%s/vgpu/vgpuvalidator", hostHookPath),
						ReadOnly:      true,
					})
				}
=======
			err = util.EraseNextDeviceTypeFromAnnotation(util.NvidiaGPUDevice, *current)
			if err != nil {
				util.PodAllocationFailed(nodename, current)
				return &pluginapi.AllocateResponse{}, err
			}

			for i, dev := range devreq {
				limitKey := fmt.Sprintf("CUDA_DEVICE_MEMORY_LIMIT_%v", i)
				response.Envs[limitKey] = fmt.Sprintf("%vm", dev.Usedmem)

				/*tmp := response.Envs["NVIDIA_VISIBLE_DEVICES"]
				if i > 0 {
					response.Envs["NVIDIA_VISIBLE_DEVICES"] = fmt.Sprintf("%v,%v", tmp, dev.UUID)
				} else {
					response.Envs["NVIDIA_VISIBLE_DEVICES"] = dev.UUID
				}*/
			}
			response.Envs["CUDA_DEVICE_SM_LIMIT"] = fmt.Sprint(devreq[0].Usedcores)
			response.Envs["CUDA_DEVICE_MEMORY_SHARED_CACHE"] = fmt.Sprintf("/usr/local/vgpu/%v.cache", uuid.New().String())
			if *util.DeviceMemoryScaling > 1 {
				response.Envs["CUDA_OVERSUBSCRIBE"] = "true"
			}
			if *util.DisableCoreLimit {
				response.Envs[api.CoreLimitSwitch] = "disable"
			}
			cacheFileHostDirectory := "/usr/local/vgpu/containers/" + string(current.UID) + "_" + currentCtr.Name
			os.MkdirAll(cacheFileHostDirectory, 0777)
			os.Chmod(cacheFileHostDirectory, 0777)
			os.MkdirAll("/tmp/vgpulock", 0777)
			os.Chmod("/tmp/vgpulock", 0777)
			hostHookPath := os.Getenv("HOOK_PATH")
			response.Mounts = append(response.Mounts,
				&pluginapi.Mount{ContainerPath: "/usr/local/vgpu/libvgpu.so",
					HostPath: hostHookPath + "/libvgpu.so",
					ReadOnly: true},
				&pluginapi.Mount{ContainerPath: "/etc/ld.so.preload",
					HostPath: hostHookPath + "/ld.so.preload",
					ReadOnly: true},
				&pluginapi.Mount{ContainerPath: "/usr/local/vgpu",
					HostPath: cacheFileHostDirectory,
					ReadOnly: false},
				&pluginapi.Mount{ContainerPath: "/tmp/vgpulock",
					HostPath: "/tmp/vgpulock",
					ReadOnly: false},
			)
			_, err = os.Stat("/usr/local/vgpu/license")
			if err == nil {
				response.Mounts = append(response.Mounts, &pluginapi.Mount{
					ContainerPath: "/vgpu/",
					HostPath:      "/usr/local/vgpu/license",
					ReadOnly:      true,
				})
				response.Mounts = append(response.Mounts, &pluginapi.Mount{
					ContainerPath: "/usr/bin/vgpuvalidator",
					HostPath:      hostHookPath + "/vgpuvalidator",
					ReadOnly:      true,
				})
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			}
			responses.ContainerResponses = append(responses.ContainerResponses, response)
		}
	}
	klog.Infoln("Allocate Response", responses.ContainerResponses)
<<<<<<< HEAD
	PodAllocationTrySuccess(nodename, nvidia.NvidiaGPUDevice, NodeLockNvidia, current)
	return &responses, nil
}

func (plugin *NvidiaDevicePlugin) getAllocateResponse(requestIds []string) (*kubeletdevicepluginv1beta1.ContainerAllocateResponse, error) {
	deviceIDs := plugin.uniqueDeviceIDsFromAnnotatedDeviceIDs(requestIds)

	// Create an empty response that will be updated as required below.
	response := &kubeletdevicepluginv1beta1.ContainerAllocateResponse{
		Envs: make(map[string]string),
	}
	if plugin.deviceListStrategies.AnyCDIEnabled() {
		responseID := uuid.New().String()
		if err := plugin.updateResponseForCDI(response, responseID, deviceIDs...); err != nil {
			return nil, fmt.Errorf("failed to get allocate response for CDI: %v", err)
		}
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
	if plugin.config.Flags.Plugin.PassDeviceSpecs != nil && *plugin.config.Flags.Plugin.PassDeviceSpecs {
		response.Devices = append(response.Devices, plugin.apiDeviceSpecs(*plugin.config.Flags.NvidiaDevRoot, requestIds)...)
	}
	if plugin.config.Flags.GDRCopyEnabled != nil && *plugin.config.Flags.GDRCopyEnabled {
		response.Envs["NVIDIA_GDRCOPY"] = "enabled"
	}
	if plugin.config.Flags.GDSEnabled != nil && *plugin.config.Flags.GDSEnabled {
		response.Envs["NVIDIA_GDS"] = "enabled"
	}
	if plugin.config.Flags.MOFEDEnabled != nil && *plugin.config.Flags.MOFEDEnabled {
		response.Envs["NVIDIA_MOFED"] = "enabled"
	}
	return response, nil
}

// updateResponseForCDI updates the specified response for the given device IDs.
// This response contains the annotations required to trigger CDI injection in the container engine or nvidia-container-runtime.
func (plugin *NvidiaDevicePlugin) updateResponseForCDI(response *kubeletdevicepluginv1beta1.ContainerAllocateResponse, responseID string, deviceIDs ...string) error {
=======
	util.PodAllocationTrySuccess(nodename, current)
	return &responses, nil
}

func (plugin *NvidiaDevicePlugin) getAllocateResponse(requestIds []string) (*pluginapi.ContainerAllocateResponse, error) {
	deviceIDs := plugin.deviceIDsFromAnnotatedDeviceIDs(requestIds)

	responseID := uuid.New().String()
	response, err := plugin.getAllocateResponseForCDI(responseID, deviceIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get allocate response for CDI: %v", err)
	}

	response.Envs = plugin.apiEnvs(plugin.deviceListEnvvar, deviceIDs)
	//if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyVolumeMounts) || plugin.deviceListStrategies.Includes(spec.DeviceListStrategyEnvvar) {
	//	response.Envs = plugin.apiEnvs(plugin.deviceListEnvvar, deviceIDs)
	//}
	/*
		if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyVolumeMounts) {
			response.Envs = plugin.apiEnvs(plugin.deviceListEnvvar, []string{deviceListAsVolumeMountsContainerPathRoot})
			response.Mounts = plugin.apiMounts(deviceIDs)
		}*/
	if *plugin.config.Flags.Plugin.PassDeviceSpecs {
		response.Devices = plugin.apiDeviceSpecs(*plugin.config.Flags.NvidiaDriverRoot, requestIds)
	}
	if *plugin.config.Flags.GDSEnabled {
		response.Envs["NVIDIA_GDS"] = "enabled"
	}
	if *plugin.config.Flags.MOFEDEnabled {
		response.Envs["NVIDIA_MOFED"] = "enabled"
	}

	return &response, nil
}

// getAllocateResponseForCDI returns the allocate response for the specified device IDs.
// This response contains the annotations required to trigger CDI injection in the container engine or nvidia-container-runtime.
func (plugin *NvidiaDevicePlugin) getAllocateResponseForCDI(responseID string, deviceIDs []string) (pluginapi.ContainerAllocateResponse, error) {
	response := pluginapi.ContainerAllocateResponse{}

	if !plugin.cdiEnabled {
		return response, nil
	}

>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	var devices []string
	for _, id := range deviceIDs {
		devices = append(devices, plugin.cdiHandler.QualifiedName("gpu", id))
	}
<<<<<<< HEAD
	for _, channel := range plugin.imexChannels {
		devices = append(devices, plugin.cdiHandler.QualifiedName("imex-channel", channel.ID))
	}

	devices = append(devices, plugin.cdiHandler.AdditionalDevices()...)

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
			cdiDevice := kubeletdevicepluginv1beta1.CDIDevice{
				Name: device,
			}
			response.CDIDevices = append(response.CDIDevices, &cdiDevice)
		}
	}

	return nil
}

func (plugin *NvidiaDevicePlugin) getCDIDeviceAnnotations(id string, devices ...string) (map[string]string, error) {
=======

	if *plugin.config.Flags.GDSEnabled {
		devices = append(devices, plugin.cdiHandler.QualifiedName("gds", "all"))
	}
	if *plugin.config.Flags.MOFEDEnabled {
		devices = append(devices, plugin.cdiHandler.QualifiedName("mofed", "all"))
	}

	if len(devices) == 0 {
		return response, nil
	}

	if plugin.deviceListStrategies.Includes(spec.DeviceListStrategyCDIAnnotations) {
		annotations, err := plugin.getCDIDeviceAnnotations(responseID, devices)
		if err != nil {
			return response, err
		}
		response.Annotations = annotations
	}

	return response, nil
}

func (plugin *NvidiaDevicePlugin) getCDIDeviceAnnotations(id string, devices []string) (map[string]string, error) {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
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
<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) PreStartContainer(context.Context, *kubeletdevicepluginv1beta1.PreStartContainerRequest) (*kubeletdevicepluginv1beta1.PreStartContainerResponse, error) {
	return &kubeletdevicepluginv1beta1.PreStartContainerResponse{}, nil
=======
func (plugin *NvidiaDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

// dial establishes the gRPC communication with the registered device plugin.
func (plugin *NvidiaDevicePlugin) dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
<<<<<<< HEAD
	ctx, cancel := context.WithTimeout(plugin.ctx, timeout)
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
=======
	c, err := grpc.Dial(unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)

>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	if err != nil {
		return nil, err
	}

	return c, nil
}

<<<<<<< HEAD
func (plugin *NvidiaDevicePlugin) uniqueDeviceIDsFromAnnotatedDeviceIDs(ids []string) []string {
=======
func (plugin *NvidiaDevicePlugin) deviceIDsFromAnnotatedDeviceIDs(ids []string) []string {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	var deviceIDs []string
	if *plugin.config.Flags.Plugin.DeviceIDStrategy == spec.DeviceIDStrategyUUID {
		deviceIDs = rm.AnnotatedIDs(ids).GetIDs()
	}
	if *plugin.config.Flags.Plugin.DeviceIDStrategy == spec.DeviceIDStrategyIndex {
		deviceIDs = plugin.rm.Devices().Subset(ids).GetIndices()
	}
<<<<<<< HEAD
	var uniqueIDs []string
	seen := make(map[string]bool)
	for _, id := range deviceIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		uniqueIDs = append(uniqueIDs, id)
	}
	return uniqueIDs
}

// updateResponseForDeviceListEnvVar sets the environment variable for the requested devices.
func (plugin *NvidiaDevicePlugin) updateResponseForDeviceListEnvVar(response *kubeletdevicepluginv1beta1.ContainerAllocateResponse, deviceIDs ...string) {
	response.Envs[deviceListEnvVar] = strings.Join(deviceIDs, ",")
}

// updateResponseForImexChannelsEnvVar sets the environment variable for the requested IMEX channels.
func (plugin *NvidiaDevicePlugin) updateResponseForImexChannelsEnvVar(response *kubeletdevicepluginv1beta1.ContainerAllocateResponse) {
	var channelIDs []string
	for _, channel := range plugin.imexChannels {
		channelIDs = append(channelIDs, channel.ID)
	}
	if len(channelIDs) > 0 {
		response.Envs[spec.ImexChannelEnvVar] = strings.Join(channelIDs, ",")
	}
}

// updateResponseForDeviceMounts sets the mounts required to request devices if volume mounts are used.
func (plugin *NvidiaDevicePlugin) updateResponseForDeviceMounts(response *kubeletdevicepluginv1beta1.ContainerAllocateResponse, deviceIDs ...string) {
	plugin.updateResponseForDeviceListEnvVar(response, deviceListAsVolumeMountsContainerPathRoot)

	for _, id := range deviceIDs {
		mount := &kubeletdevicepluginv1beta1.Mount{
			HostPath:      deviceListAsVolumeMountsHostPath,
			ContainerPath: filepath.Join(deviceListAsVolumeMountsContainerPathRoot, id),
		}
		response.Mounts = append(response.Mounts, mount)
	}
	for _, channel := range plugin.imexChannels {
		mount := &kubeletdevicepluginv1beta1.Mount{
			HostPath:      deviceListAsVolumeMountsHostPath,
			ContainerPath: filepath.Join(deviceListAsVolumeMountsContainerPathRoot, "imex", channel.ID),
		}
		response.Mounts = append(response.Mounts, mount)
	}
}

func (plugin *NvidiaDevicePlugin) apiDeviceSpecs(devRoot string, ids []string) []*kubeletdevicepluginv1beta1.DeviceSpec {
=======
	return deviceIDs
}

func (plugin *NvidiaDevicePlugin) apiDevices() []*pluginapi.Device {
	return plugin.rm.Devices().GetPluginDevices()
}

func (plugin *NvidiaDevicePlugin) apiEnvs(envvar string, deviceIDs []string) map[string]string {
	return map[string]string{
		envvar: strings.Join(deviceIDs, ","),
	}
}

/*
func (plugin *NvidiaDevicePlugin) apiMounts(deviceIDs []string) []*pluginapi.Mount {
	var mounts []*pluginapi.Mount

	for _, id := range deviceIDs {
		mount := &pluginapi.Mount{
			HostPath:      deviceListAsVolumeMountsHostPath,
			ContainerPath: filepath.Join(deviceListAsVolumeMountsContainerPathRoot, id),
		}
		mounts = append(mounts, mount)
	}

	return mounts
}*/

func (plugin *NvidiaDevicePlugin) apiDeviceSpecs(driverRoot string, ids []string) []*pluginapi.DeviceSpec {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	optional := map[string]bool{
		"/dev/nvidiactl":        true,
		"/dev/nvidia-uvm":       true,
		"/dev/nvidia-uvm-tools": true,
		"/dev/nvidia-modeset":   true,
	}

	paths := plugin.rm.GetDevicePaths(ids)

<<<<<<< HEAD
	var specs []*kubeletdevicepluginv1beta1.DeviceSpec
=======
	var specs []*pluginapi.DeviceSpec
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	for _, p := range paths {
		if optional[p] {
			if _, err := os.Stat(p); err != nil {
				continue
			}
		}
<<<<<<< HEAD
		spec := &kubeletdevicepluginv1beta1.DeviceSpec{
			ContainerPath: p,
			HostPath:      filepath.Join(devRoot, p),
=======
		spec := &pluginapi.DeviceSpec{
			ContainerPath: p,
			HostPath:      filepath.Join(driverRoot, p),
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
			Permissions:   "rw",
		}
		specs = append(specs, spec)
	}

<<<<<<< HEAD
	for _, channel := range plugin.imexChannels {
		spec := &kubeletdevicepluginv1beta1.DeviceSpec{
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

func (plugin *NvidiaDevicePlugin) apiDevices() []*kubeletdevicepluginv1beta1.Device {
	return plugin.rm.Devices().GetPluginDevices(*plugin.schedulerConfig.DeviceSplitCount)
}

func (plugin *NvidiaDevicePlugin) processMigConfigs(migConfigs map[string]nvidia.MigConfigSpecSlice, deviceCount int) (nvidia.MigConfigSpecSlice, error) {
	if migConfigs == nil {
		return nil, fmt.Errorf("migConfigs cannot be nil")
	}
	if deviceCount <= 0 {
		return nil, fmt.Errorf("deviceCount must be positive")
	}

	transformConfigs := func() (nvidia.MigConfigSpecSlice, error) {
		var result nvidia.MigConfigSpecSlice

		if len(migConfigs["current"]) == 1 && len(migConfigs["current"][0].Devices) == 0 {
			for i := 0; i < deviceCount; i++ {
				config := deepCopyMigConfig(migConfigs["current"][0])
				config.Devices = []int32{int32(i)}
				result = append(result, config)
			}
			return result, nil
		}

		deviceToConfig := make(map[int32]*nvidia.MigConfigSpec)
		for i := range migConfigs["current"] {
			for _, device := range migConfigs["current"][i].Devices {
				deviceToConfig[device] = &migConfigs["current"][i]
			}
		}

		for i := 0; i < deviceCount; i++ {
			deviceIndex := int32(i)
			config, exists := deviceToConfig[deviceIndex]
			if !exists {
				return nil, fmt.Errorf("device %d does not match any MIG configuration", i)
			}
			newConfig := deepCopyMigConfig(*config)
			newConfig.Devices = []int32{deviceIndex}
			result = append(result, newConfig)

		}
		return result, nil
	}

	return transformConfigs()
}
=======
	return specs
}
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
