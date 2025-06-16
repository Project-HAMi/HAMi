/*
Copyright 2025 The HAMi Authors.

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

package metax

import (
	"sync"

	"k8s.io/klog/v2"
)

type JitteryQosCache struct {
	sync.Mutex
	cache map[string]string
}

func NewJitteryQosCache() *JitteryQosCache {
	return &JitteryQosCache{
		cache: map[string]string{},
	}
}

func (c *JitteryQosCache) Sync(devices []*MetaxSDeviceInfo) {
	c.Lock()
	defer c.Unlock()

	isSync := false

	for _, dev := range devices {
		expectedQos, ok := c.cache[dev.UUID]

		if ok {
			if expectedQos == dev.QosPolicy {
				delete(c.cache, dev.UUID)
				klog.Infof("%T: device[%s] qos changed to expected [%s], delete data",
					c, dev.UUID, dev.QosPolicy)

				isSync = true
			}
		}
	}

	if isSync {
		klog.Infof("%T: sync done, current cache: %v", c, c.cache)
	}
}

func (c *JitteryQosCache) Add(uuid string, expectedQos string) {
	c.Lock()
	defer c.Unlock()

	c.cache[uuid] = expectedQos
	klog.Infof("%T: device[%s] add to cache, expected qos [%s]", c, uuid, expectedQos)
}

func (c *JitteryQosCache) Get(uuid string) (string, bool) {
	c.Lock()
	defer c.Unlock()

	qos, ok := c.cache[uuid]
	return qos, ok
}
