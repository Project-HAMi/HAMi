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
	"sync"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/mlu/cndev"
	"github.com/Project-HAMi/HAMi/pkg/util"
	kubeletdevicepluginv1beta1 "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type MLUDevice struct {
	dev    kubeletdevicepluginv1beta1.Device
	handle *cndev.Device
}

type DeviceCache struct {
	cache     []*MLUDevice
	stopCh    chan interface{}
	unhealthy chan *kubeletdevicepluginv1beta1.Device
	notifyCh  map[string]chan *kubeletdevicepluginv1beta1.Device
	mutex     sync.Mutex
}

func NewDeviceCache() *DeviceCache {
	return &DeviceCache{
		stopCh:    make(chan interface{}),
		unhealthy: make(chan *kubeletdevicepluginv1beta1.Device),
		notifyCh:  make(map[string]chan *kubeletdevicepluginv1beta1.Device),
	}
}

func (d *DeviceCache) AddNotifyChannel(name string, ch chan *kubeletdevicepluginv1beta1.Device) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.notifyCh[name] = ch
}

func (d *DeviceCache) RemoveNotifyChannel(name string) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	delete(d.notifyCh, name)
}

func (d *DeviceCache) Start() {
	d.cache = d.Devices()
	go d.CheckHealth(d.stopCh, d.cache, d.unhealthy)
	go d.notify()
}

func (d *DeviceCache) Stop() {
	close(d.stopCh)
}

func (d *DeviceCache) GetCache() []*MLUDevice {
	return d.cache
}

func (d *DeviceCache) notify() {
	for {
		select {
		case <-d.stopCh:
			return
		case dev := <-d.unhealthy:
			dev.Health = kubeletdevicepluginv1beta1.Unhealthy
			d.mutex.Lock()
			for _, ch := range d.notifyCh {
				ch <- dev
			}
			d.mutex.Unlock()
		}
	}
}

// Devices returns a list of devices from the GpuDeviceManager
func (d *DeviceCache) Devices() []*MLUDevice {
	n, err := cndev.GetDeviceCount()
	check(err)
	if n > util.DeviceLimit {
		n = util.DeviceLimit
	}

	var devs []*MLUDevice
	for i := uint(0); i < n; i++ {
		d, err := cndev.NewDeviceLite(i, false)
		check(err)

		devs = append(devs, &MLUDevice{
			dev:    kubeletdevicepluginv1beta1.Device{ID: d.UUID},
			handle: d,
		})
	}

	return devs
}

// CheckHealth performs health checks on a set of devices, writing to the 'unhealthy' channel with any unhealthy devices
func (d *DeviceCache) CheckHealth(stop <-chan interface{}, devices []*MLUDevice, unhealthy chan<- *kubeletdevicepluginv1beta1.Device) {
	// mlu.checkHealth...
	WatchUnhealthy(context.Background(), devices, unhealthy)
}
