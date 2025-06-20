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
	"reflect"
	"testing"
)

func TestJitteryQosCacheSync(t *testing.T) {
	for _, ts := range []struct {
		name         string
		devices      []*MetaxSDeviceInfo
		currentCache map[string]string

		expectedCache map[string]string
	}{
		{
			name: "no sync",
			devices: []*MetaxSDeviceInfo{
				{
					UUID:      "GPU-123",
					QosPolicy: BestEffort,
				},
				{
					UUID:      "GPU-456",
					QosPolicy: BestEffort,
				},
				{
					UUID:      "GPU-789",
					QosPolicy: BestEffort,
				},
			},
			currentCache: map[string]string{
				"GPU-123": FixedShare,
				"GPU-789": BurstShare,
			},

			expectedCache: map[string]string{
				"GPU-123": FixedShare,
				"GPU-789": BurstShare,
			},
		},
		{
			name: "sync sccuess",
			devices: []*MetaxSDeviceInfo{
				{
					UUID:      "GPU-123",
					QosPolicy: BestEffort,
				},
				{
					UUID:      "GPU-456",
					QosPolicy: BestEffort,
				},
				{
					UUID:      "GPU-789",
					QosPolicy: BestEffort,
				},
			},
			currentCache: map[string]string{
				"GPU-123": BestEffort,
				"GPU-789": BurstShare,
			},

			expectedCache: map[string]string{
				"GPU-789": BurstShare,
			},
		},
		{
			name: "sync sccuess",
			devices: []*MetaxSDeviceInfo{
				{
					UUID:      "GPU-123",
					QosPolicy: BestEffort,
				},
				{
					UUID:      "GPU-456",
					QosPolicy: BestEffort,
				},
				{
					UUID:      "GPU-789",
					QosPolicy: BestEffort,
				},
			},
			currentCache: map[string]string{
				"GPU-123": BestEffort,
				"GPU-789": BestEffort,
			},

			expectedCache: map[string]string{},
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			jqCache := JitteryQosCache{
				cache: ts.currentCache,
			}

			jqCache.Sync(ts.devices)

			if !reflect.DeepEqual(ts.expectedCache, jqCache.cache) {
				t.Errorf("JitteryQosCache Sync failed: result %v, expected %v",
					jqCache.cache, ts.expectedCache)
			}
		})
	}
}

func TestGet(t *testing.T) {
	for _, ts := range []struct {
		name         string
		currentCache map[string]string
		uuid         string

		expectedValue string
		expectedOk    bool
	}{
		{
			name: "test get",
			currentCache: map[string]string{
				"GPU-123": FixedShare,
				"GPU-789": BurstShare,
			},
			uuid: "GPU-789",

			expectedValue: BurstShare,
			expectedOk:    true,
		},
	} {
		t.Run(ts.name, func(t *testing.T) {
			jqCache := JitteryQosCache{
				cache: ts.currentCache,
			}

			resValue, resOk := jqCache.Get(ts.uuid)

			if resValue != ts.expectedValue {
				t.Errorf("Get failed: result %v, expected %v",
					resValue, ts.expectedValue)
			}

			if resOk != ts.expectedOk {
				t.Errorf("Get failed: result %v, expected %v",
					resOk, ts.expectedOk)
			}
		})
	}
}
