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
	"context"
	"fmt"
	"strconv"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/device-plugin/config"
	"google.golang.org/grpc"
)

type VGPURuntimeService struct {
	deviceCache *DeviceCache
}

func NewVGPURuntimeService(deviceCache *DeviceCache) *VGPURuntimeService {
	return &VGPURuntimeService{deviceCache: deviceCache}
}

func (s *VGPURuntimeService) GetDevice(ctx context.Context, req *api.GetDeviceRequest) (*api.GetDeviceReply, error) {
	conn, err := grpc.DialContext(
		ctx,
		config.SchedulerEndpoint,
		grpc.WithInsecure(),
		grpc.WithBlock(),
		//grpc.WithConnectParams(grpc.ConnectParams{MinConnectTimeout: 3}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect scheduler error, %v", err)
	}
	client := api.NewDeviceServiceClient(conn)
	sReq := api.GetContainerRequest{Uuid: req.CtrUUID}
	sResp, err := client.GetContainer(ctx, &sReq)
	if err != nil {
		return nil, err
	}
	envs, err := s.containerEnvs(sResp.DevList)
	if err != nil {
		return nil, err
	}
	resp := api.GetDeviceReply{
		Envs:         envs,
		PodUID:       sResp.PodUID,
		CtrName:      sResp.CtrName,
		PodNamespace: sResp.PodNamespace,
		PodName:      sResp.PodName,
	}
	return &resp, nil
}

func (s *VGPURuntimeService) containerEnvs(devIDs []*api.DeviceUsage) (map[string]string, error) {
	envs := make(map[string]string)
	var devs []*Device
	for _, id := range devIDs {
		found := false
		for _, d := range s.deviceCache.GetCache() {
			if id.GetId() == d.ID {
				found = true
				devs = append(devs, d)
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("device %v not found", id)
		}
	}

	devenv := ""
	for idx, val := range devIDs {
		if idx == 0 {
			devenv = "" + val.GetId()
		} else {
			devenv = devenv + "," + val.GetId()
		}
	}
	//envs["NVIDIA_VISIBLE_DEVICES"] = strings.Join(devIDs, ",")
	//fmt.Println("Assigneing NVIDIA_VISIBLE_DEVICES:", devenv)
	envs["NVIDIA_VISIBLE_DEVICES"] = devenv
	for i, d := range devIDs {
		limitKey := fmt.Sprintf("CUDA_DEVICE_MEMORY_LIMIT_%v", i)
		envs[limitKey] = strconv.Itoa(int(d.GetDevmem())) + "m"
		//envs[limitKey] = fmt.Sprintf("%vm", config.DeviceMemoryScaling*float64(d.Memory)/float64(config.DeviceSplitCount))
	}
	return envs, nil
}
