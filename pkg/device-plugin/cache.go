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
    "sync"

    pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type DeviceCache struct {
    GpuDeviceManager

    cache     []*Device
    stopCh    chan interface{}
    unhealthy chan *Device
    notifyCh  map[string]chan *Device
    mutex     sync.Mutex
}

func NewDeviceCache() *DeviceCache {
    return &DeviceCache{
        GpuDeviceManager: GpuDeviceManager{true},
        stopCh:           make(chan interface{}),
        unhealthy:        make(chan *Device),
        notifyCh:         make(map[string]chan *Device),
    }
}

func (d *DeviceCache) AddNotifyChannel(name string, ch chan *Device) {
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

func (d *DeviceCache) GetCache() []*Device {
    return d.cache
}

func (d *DeviceCache) notify() {
    for {
        select {
        case <-d.stopCh:
            return
        case dev := <-d.unhealthy:
            dev.Health = pluginapi.Unhealthy
            d.mutex.Lock()
            for _, ch := range d.notifyCh {
                ch <- dev
            }
            d.mutex.Unlock()
        }
    }
}
