/*
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
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
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
 */

package nvidia_plugin

import (
	"context"
	"fmt"
	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/internal/cdi"
	"github.com/Project-HAMi/HAMi/pkg/device-plugin/internal/rm"
	"github.com/Project-HAMi/HAMi/pkg/device"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"os"
	"strings"
)

const (
	HandshakeAnnos = "hami.io/node-handshake"
	RegisterAnnos  = "hami.io/node-nvidia-register"
	NodeLockNvidia = "hami.io/mutex.lock"
	GPUUseUUID     = "nvidia.com/use-gpuuuid"
	GPUNoUseUUID   = "nvidia.com/nouse-gpuuuid"
	AllocateMode   = "nvidia.com/vgpu-mode"
)

// HamiDevicePlugin embeds the NvidiaDevicePlugin and adds custom fields
type HamiDevicePlugin struct {
	*nvidiaDevicePlugin             // Embed the NvidiaDevicePlugin from k8s-device-plugin
	CustomField              string // Custom field to store additional data
	DevicePluginFilterDevice *FilterDevice
}

// FilterDevice is used to filter devices
type FilterDevice struct {
	UUID  []string `json:"uuid"`
	Index []uint   `json:"index"`
}

// NewHamiDevicePlugin creates a new HamiDevicePlugin instance
func NewHamiDevicePlugin(config *spec.Config, resourceManager rm.ResourceManager, cdiHandler cdi.Interface, cdiAnnotationPrefix string) *HamiDevicePlugin {
	nvidiaPlugin := &nvidiaDevicePlugin{
		rm:                  resourceManager,
		config:              config,
		cdiHandler:          cdiHandler,
		cdiAnnotationPrefix: cdiAnnotationPrefix,
		socket:              getPluginSocketPath(resourceManager.Resource()),
		server:              nil,
		health:              nil,
		stop:                nil,
		imexChannels:        imex.Channels{}, // Assuming imex.Channels is defined elsewhere
		mps:                 mpsOptions{},    // Assuming mpsOptions is defined elsewhere
	}
	return &HamiDevicePlugin{
		nvidiaDevicePlugin:       nvidiaPlugin,
		CustomField:              "default_value", // Set a default value for the custom field
		DevicePluginFilterDevice: &FilterDevice{}, // Initialize the filter device
	}
}

// Allocate which return list of devices
func (plugin *HamiDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	// Custom Allocate logic for Hami
	klog.InfoS("Allocate", "request", reqs)
	responses := pluginapi.AllocateResponse{}
	nodename := os.Getenv(util.NodeNameEnvName)
	current, err := util.GetPendingPod(ctx, nodename)
	if err != nil {
		//nodelock.ReleaseNodeLock(nodename, NodeLockNvidia, current)
		return &pluginapi.AllocateResponse{}, err
	}
	klog.Infof("Allocate pod name is %s/%s, annotation is %+v", current.Namespace, current.Name, current.Annotations)

	for idx, req := range reqs.ContainerRequests {
		// If the devices being allocated are replicas, then (conditionally)
		// error out if more than one resource is being allocated.

		if strings.Contains(req.DevicesIDs[0], "MIG") {
			if plugin.config.Sharing.TimeSlicing.FailRequestsGreaterThanOne && rm.AnnotatedIDs(req.DevicesIDs).AnyHasAnnotations() {
				if len(req.DevicesIDs) > 1 {
					device.PodAllocationFailed(nodename, current, NodeLockNvidia)
					return nil, fmt.Errorf("request for '%v: %v' too large: maximum request size for shared resources is 1", plugin.rm.Resource(), len(req.DevicesIDs))
				}
			}

			for _, id := range req.DevicesIDs {
				if !plugin.rm.Devices().Contains(id) {
					device.PodAllocationFailed(nodename, current, NodeLockNvidia)
					return nil, fmt.Errorf("invalid allocation request for '%s': unknown device: %s", plugin.rm.Resource(), id)
				}
			}

			response, err := plugin.getAllocateResponse(req.DevicesIDs)
			if err != nil {
				device.PodAllocationFailed(nodename, current, NodeLockNvidia)
				return nil, fmt.Errorf("failed to get allocate response: %v", err)
