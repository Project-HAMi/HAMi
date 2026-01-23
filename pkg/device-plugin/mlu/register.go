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

package mlu

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/device-plugin/mlu/cndev"
<<<<<<< HEAD
	"google.golang.org/grpc"
=======
	"4pd.io/k8s-vgpu/pkg/device/cambricon"
	"4pd.io/k8s-vgpu/pkg/util"
>>>>>>> 21785f7 (update to v2.3.2)
)

type DevListFunc func() []*pluginapi.Device

type DeviceRegister struct {
	deviceCache *DeviceCache
	unhealthy   chan *pluginapi.Device
	stopCh      chan struct{}
}

func NewDeviceRegister(deviceCache *DeviceCache) *DeviceRegister {
	return &DeviceRegister{
		deviceCache: deviceCache,
		unhealthy:   make(chan *pluginapi.Device),
		stopCh:      make(chan struct{}),
	}
}

func (r *DeviceRegister) Start(opt Options) {
	r.deviceCache.AddNotifyChannel("register", r.unhealthy)
	go r.WatchAndRegister(opt)
}

func (r *DeviceRegister) Stop() {
	close(r.stopCh)
}

func (r *DeviceRegister) apiDevices() *[]*api.DeviceInfo {
	devs := r.deviceCache.GetCache()
	res := make([]*api.DeviceInfo, 0, len(devs))
	for i, dev := range devs {
		//klog.V(3).Infoln("ndev type=", ndev.Model)
		memory, _ := cndev.GetDeviceMemory(uint(i))
		fmt.Println("mlu registered device id=", dev.dev.ID, "memory=", memory, "type=", cndev.GetDeviceModel(uint(i)))
		registeredmem := int32(memory)
<<<<<<< HEAD
<<<<<<< HEAD
		if config.DeviceMemoryScaling > 1 {
			fmt.Println("Memory Scaling to", config.DeviceMemoryScaling)
			registeredmem = int32(float64(registeredmem) * config.DeviceMemoryScaling)
=======
		if *util.DeviceMemoryScaling != float64(1) {
			registeredmem = int32(float64(registeredmem) * *util.DeviceMemoryScaling)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
		}
=======
>>>>>>> 21785f7 (update to v2.3.2)
		res = append(res, &api.DeviceInfo{
			Id:     dev.dev.ID,
			Count:  int32(*util.DeviceSplitCount),
			Devmem: registeredmem,
			Type:   cndev.GetDeviceModel(uint(i)),
			Health: dev.dev.Health == "healthy",
		})
	}
	return &res
}

<<<<<<< HEAD
func (r *DeviceRegister) Register(ctx context.Context, endpoint string) error {
	klog.Infof("Into Register")
	conn, err := grpc.DialContext(
		ctx,
		endpoint,
		grpc.WithInsecure(),
		grpc.WithBlock(),
		//grpc.WithConnectParams(grpc.ConnectParams{MinConnectTimeout: 3}),
	)
=======
func (r *DeviceRegister) RegistrInAnnotation() error {
	devices := r.apiDevices()
	annos := make(map[string]string)
	node, err := util.GetNode(util.NodeName)
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	if err != nil {
		return fmt.Errorf("connect scheduler error, %v", err)
	}
	client := api.NewDeviceServiceClient(conn)
	register, err := client.Register(ctx)
	if err != nil {
		klog.Errorf("register error %v", err)
		err = fmt.Errorf("client register error, %v", err)
		return err
	}
<<<<<<< HEAD
	klog.Infof("after client register")
	req := api.RegisterRequest{Node: config.NodeName, Devices: *r.apiDevices()}
	err = register.Send(&req)
=======
	encodeddevices := util.EncodeNodeDevices(*devices)
	annos[cambricon.HandshakeAnnos] = "Reported " + time.Now().String()
	annos[cambricon.RegisterAnnos] = encodeddevices
	klog.Infoln("Reporting devices", encodeddevices, "in", time.Now().String())
	err = util.PatchNodeAnnotations(node, annos)

>>>>>>> 21785f7 (update to v2.3.2)
	if err != nil {
		klog.Errorf("register send error, %v", err)
		return err
	}
	klog.V(3).Infof("register info %v", req.String())
	closeCh := make(chan struct{})
	go func() {
		reply := api.RegisterReply{}
		err := register.RecvMsg(reply)
		if err != nil {
			klog.Errorf("register recv error, %v", err)
		} else {
			klog.Errorf("register recv closed")
		}
		closeCh <- struct{}{}
	}()
	for {
		select {
		case <-r.unhealthy:
			err = register.Send(&api.RegisterRequest{
				Node:    config.NodeName,
				Devices: *r.apiDevices(),
			})
			if err != nil {
				klog.Errorf("register send error, %v", err)
				return err
			}
			klog.V(3).Infof("register info %v", req.String())
		case <-closeCh:
			return fmt.Errorf("register server closed")
		case <-r.stopCh:
			return nil
		}
	}
}

func (r *DeviceRegister) WatchAndRegister(opt Options) {
	//ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	//defer cancel()
	klog.Infof("into WatchAndRegister")
	ctx := context.Background()
	for {
		err := r.Register(ctx, opt.SchedulerEndpoint)
		if err != nil {
			klog.Errorf("register error, %v", err)
			time.Sleep(time.Second * 5)
		} else {
			klog.Infof("register stopped")
			break
		}
	}
}
